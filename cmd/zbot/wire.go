package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jeremylerwick-max/zbot/internal/agent"
	"github.com/jeremylerwick-max/zbot/internal/audit"
	"github.com/jeremylerwick-max/zbot/internal/gateway"
	"github.com/jeremylerwick-max/zbot/internal/llm"
	"github.com/jeremylerwick-max/zbot/internal/memory"
	"github.com/jeremylerwick-max/zbot/internal/platform"
	"github.com/jeremylerwick-max/zbot/internal/planner"
	"github.com/jeremylerwick-max/zbot/internal/prompts"
	"github.com/jeremylerwick-max/zbot/internal/scheduler"
	"github.com/jeremylerwick-max/zbot/internal/scraper"
	"github.com/jeremylerwick-max/zbot/internal/secrets"
	"github.com/jeremylerwick-max/zbot/internal/skills"
	skillEmail "github.com/jeremylerwick-max/zbot/internal/skills/email"
	skillGHL "github.com/jeremylerwick-max/zbot/internal/skills/ghl"
	skillGitHub "github.com/jeremylerwick-max/zbot/internal/skills/github"
	skillMemory "github.com/jeremylerwick-max/zbot/internal/skills/memory"
	skillSearch "github.com/jeremylerwick-max/zbot/internal/skills/search"
	skillSheets "github.com/jeremylerwick-max/zbot/internal/skills/sheets"
	"github.com/jeremylerwick-max/zbot/internal/research"
	"github.com/jeremylerwick-max/zbot/internal/security"
	"github.com/jeremylerwick-max/zbot/internal/tools"
	"github.com/jeremylerwick-max/zbot/internal/webui"
	"github.com/jeremylerwick-max/zbot/internal/workflow"
)

// systemPrompt is ZBOT's base instruction set.
const systemPrompt = `You are ZBOT, a personal AI agent built exclusively for Jeremy Lerwick, CEO and founder of Ziloss Technologies in Salt Lake City, Utah.

You are NOT a general assistant. You are Jeremy's dedicated agent with full context about his businesses and goals.

ABOUT JEREMY'S BUSINESSES:
- Lead Certain: Performance-based lead nurturing service, $200K/month revenue, 75% margins
- Ziloss CRM: SaaS platform under development (GoHighLevel competitor with AI-native features)
- Real estate automation for his mother Deborah Boler's brokerage in Midland, Texas
- Various AI agent orchestration and automation systems

YOUR INTERFACE:
- You are running as a Slack bot. You ARE Jeremy's Slack agent. Every message you receive is from Jeremy via Slack DM. You do not need Slack integration — you are already integrated.

YOUR CAPABILITIES:
- web_search: Search the internet for current information
- fetch_url: Fetch and read the content of any URL
- read_file: Read files from Jeremy's workspace
- write_file: Write files to Jeremy's workspace
- run_code: Execute Python, Go, JavaScript, or bash code in a secure sandbox
- save_memory: Save important facts to your long-term memory
- search_memory: Search your long-term memory for facts you've previously saved
- analyze_image: Analyze photos, screenshots, charts, or any image you receive
- When images are sent directly in the chat, Claude's vision is automatically activated — describe what you see and answer any questions about it
- For PDFs: text is automatically extracted and included in your context

YOUR PERSONALITY:
- Direct and efficient — no fluff, no unnecessary caveats
- Action-oriented — when in doubt, do it and report back
- Technically sophisticated — Jeremy is a senior developer, don't over-explain basics
- Honest about limitations and uncertainty
- Proactive — if you notice something important while doing a task, mention it

INSTRUCTIONS:
- Always use your tools when they would help — don't just answer from memory when you could verify with web_search
- For multi-step tasks, work through them systematically and report progress
- Save important information to memory using save_memory so you remember it next time
- Use search_memory when the user asks "do you remember..." or "what do you know about..."
- Keep responses concise unless detail is specifically needed
- If a task will take multiple steps, briefly outline your plan before starting
- Markdown formatting is fine — Slack renders it`

func run(ctx context.Context, cfg platform.AppConfig, logger *slog.Logger) error {

	// ── Secrets ─────────────────────────────────────────────────────────────
	sm, err := secrets.NewGCPSecretManager(ctx, cfg.GCPProject)
	if err != nil {
		return fmt.Errorf("secrets init: %w", err)
	}
	defer sm.Close()

	botToken, err := sm.Get(ctx, "zbot-slack-token")
	if err != nil {
		return fmt.Errorf("get slack bot token: %w", err)
	}
	appToken, err := sm.Get(ctx, "zbot-slack-app-token")
	if err != nil {
		return fmt.Errorf("get slack app token: %w", err)
	}
	anthropicKey, err := sm.Get(ctx, secrets.SecretAnthropicAPIKey)
	if err != nil {
		return fmt.Errorf("get anthropic key: %w", err)
	}
	braveKey, err := sm.Get(ctx, secrets.SecretBraveAPIKey)
	if err != nil {
		return fmt.Errorf("get brave api key: %w", err)
	}

	// Slack allowlist — who can message ZBOT. Empty/PENDING = dev mode (all users allowed).
	allowedUserID, _ := sm.Get(ctx, "zbot-allowed-user-id")
	var allowedUsers []string
	if allowedUserID != "" && allowedUserID != "PENDING" {
		allowedUsers = []string{allowedUserID}
		logger.Info("Slack allowlist active", "users", allowedUsers)
	} else {
		logger.Warn("Slack allowlist NOT set — all users can message ZBOT (dev mode)")
	}

	// DB password from Secret Manager.
	dbPassword, dbPassErr := sm.Get(ctx, "zbot-db-password")
	if dbPassErr != nil {
		logger.Warn("zbot-db-password not in Secret Manager — using env fallback")
		dbPassword = os.Getenv("ZBOT_DB_PASSWORD")
	}

	// ── Workspace ───────────────────────────────────────────────────────────
	workspaceRoot := cfg.WorkspaceRoot
	if workspaceRoot == "" {
		workspaceRoot = os.ExpandEnv("$HOME/zbot-workspace")
	}
	if err := os.MkdirAll(workspaceRoot, 0o750); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}
	logger.Info("workspace ready", "path", workspaceRoot)

	// ── Postgres ────────────────────────────────────────────────────────────
	pgDB, pgErr := connectPostgres(ctx, logger, dbPassword)
	if pgErr != nil {
		logger.Warn("postgres unavailable — some features disabled", "err", pgErr)
	}

	// ── Memory Store (pgvector or in-memory fallback) ───────────────────────
	var memStore agent.MemoryStore
	var sharedEmbedder memory.Embedder // hoisted for use in research pipeline (Sprint 16)
	if pgDB == nil {
		memStore = memory.NewInMemoryStore(logger)
		sharedEmbedder = memory.NoopEmbedder{}
	} else {
		var embedder memory.Embedder
		vertexEmbed, vertexErr := memory.NewVertexEmbedder(ctx, cfg.GCPProject, "us-central1", logger)
		if vertexErr != nil {
			logger.Warn("vertex AI embedder unavailable — using noop embedder", "err", vertexErr)
			embedder = memory.NoopEmbedder{}
		} else {
			embedder = vertexEmbed
			defer vertexEmbed.Close()
		}
		sharedEmbedder = embedder

		store, storeErr := memory.New(ctx, pgDB, embedder, logger, "zbot")
		if storeErr != nil {
			logger.Warn("memory store init failed — using in-memory fallback", "err", storeErr)
			memStore = memory.NewInMemoryStore(logger)
		} else {
			memStore = store
		}
	}

	// ── LLM Client ──────────────────────────────────────────────────────────
	llmClient := llm.New(anthropicKey, logger)
	logger.Info("LLM client ready", "model", llmClient.ModelName())

	// ── Planner + Critic (Sprint 10/11) — GPT-4o plans + reviews, Claude executes ─
	var taskPlanner *planner.Planner
	var taskCritic *planner.Critic
	if openaiKey, openaiErr := sm.Get(ctx, "openai-api-key"); openaiErr == nil && openaiKey != "" {
		openaiClient := llm.NewOpenAIClient(openaiKey, "gpt-4o", logger)
		taskPlanner = planner.New(openaiClient, logger)
		taskPlanner.SetMemoryStore(memStore) // Sprint 12: inject memory into planner context
		taskCritic = planner.NewCritic(openaiClient, logger)
		logger.Info("planner + critic ready", "model", "gpt-4o")
	} else {
		logger.Warn("OpenAI key not set — /plan command disabled")
	}

	// ── Scraper Stack (Sprint 4) ────────────────────────────────────────────
	var proxyPool *scraper.ProxyPool
	proxyList, proxyErr := sm.Get(ctx, "zbot-proxy-list")
	if proxyErr != nil {
		logger.Warn("proxy list unavailable — using direct connections", "err", proxyErr)
		proxyPool = scraper.NewProxyPool(nil)
	} else {
		urls := strings.Split(strings.TrimSpace(proxyList), "\n")
		proxyPool = scraper.NewProxyPool(urls)
		logger.Info("proxy pool ready", "count", proxyPool.Size())
	}

	rateLimiter := scraper.NewDomainRateLimiter(2 * time.Second)

	cachePath := filepath.Join(workspaceRoot, ".cache", "scrape.db")
	scrapeCache, cacheErr := scraper.NewScrapeCache(cachePath)
	if cacheErr != nil {
		logger.Warn("scrape cache unavailable — caching disabled", "err", cacheErr)
	} else {
		defer scrapeCache.Close()
		logger.Info("scrape cache ready", "path", cachePath)
	}

	browserFetcher := scraper.NewBrowserFetcher()
	if browserFetcher.Available() {
		logger.Info("headless browser available")
	} else {
		logger.Warn("headless browser unavailable — JS fallback disabled (install Chromium)")
	}

	// ── Audit Logger (Sprint 8) ─────────────────────────────────────────────
	var auditLog agent.AuditLogger
	if pgDB != nil {
		pgAudit := audit.NewPGAuditLogger(pgDB, logger)
		pgAudit.Start(ctx)
		auditLog = pgAudit
		logger.Info("audit logger ready (Postgres)")
	} else {
		auditLog = audit.NewNoopLogger(logger)
		logger.Info("audit logger ready (noop — no Postgres)")
	}

	// ── Core Tools ──────────────────────────────────────────────────────────
	coreTools := []agent.Tool{
		tools.NewWebSearchTool(braveKey),
		tools.NewFetchURLToolFull(proxyPool, rateLimiter, scrapeCache, browserFetcher),
		tools.NewReadFileTool(workspaceRoot),
		tools.NewWriteFileTool(workspaceRoot),
		tools.NewCodeRunnerTool(workspaceRoot),
		// Sprint 12: save_memory + search_memory moved to memory skill (below).
		tools.NewAnalyzeImageTool(llmClient, workspaceRoot),
		tools.NewPDFExtractTool(workspaceRoot),
	}

	// ── Skills System (Sprint 7) ────────────────────────────────────────────
	skillRegistry := skills.NewRegistry()

	// Memory skill — always registers (no secret required). Sprint 12.
	skillRegistry.Register(skillMemory.NewSkill(memStore))
	logger.Info("skill registered: memory")

	// Search skill.
	skillRegistry.Register(skillSearch.NewSkill())
	logger.Info("skill registered: search")

	if ghlKey, ghlErr := sm.Get(ctx, "ghl-api-token"); ghlErr == nil && ghlKey != "" {
		skillRegistry.Register(skillGHL.NewSkill(ghlKey, "fRrP1e3LGLFewc5dQDhS"))
		logger.Info("skill registered: ghl")
	} else {
		logger.Warn("GHL skill skipped — secret 'ghl-api-key' not available")
	}

	// GitHub skill.
	if githubToken, ghErr := sm.Get(ctx, "github-token"); ghErr == nil && githubToken != "" {
		skillRegistry.Register(skillGitHub.NewSkill(githubToken))
		logger.Info("skill registered: github")
	} else {
		logger.Warn("GitHub skill skipped — secret 'github-token' not available")
	}

	// Google Sheets skill.
	if sheetsCredJSON, shErr := sm.Get(ctx, "google-sheets-credentials"); shErr == nil && sheetsCredJSON != "" {
		sheetsSkill, shInitErr := skillSheets.NewSkill(ctx, sheetsCredJSON)
		if shInitErr != nil {
			logger.Warn("Sheets skill init failed", "err", shInitErr)
		} else {
			skillRegistry.Register(sheetsSkill)
			logger.Info("skill registered: sheets")
		}
	} else {
		logger.Warn("Sheets skill skipped — secret 'google-sheets-credentials' not available")
	}

	// Email skill.
	smtpHost, _ := sm.Get(ctx, "smtp-host")
	smtpUser, _ := sm.Get(ctx, "smtp-user")
	smtpPass, _ := sm.Get(ctx, "smtp-pass")
	smtpFrom, _ := sm.Get(ctx, "smtp-from")
	if smtpHost != "" && smtpUser != "" {
		skillRegistry.Register(skillEmail.NewSkill(smtpHost, 587, smtpUser, smtpPass, smtpFrom))
		logger.Info("skill registered: email")
	} else {
		logger.Warn("Email skill skipped — SMTP secrets not available")
	}

	// Merge core tools + skill tools.
	allTools := append(coreTools, skillRegistry.AllTools()...)

	// ── Agent ───────────────────────────────────────────────────────────────
	agentCfg := agent.DefaultConfig()
	agentCfg.SystemPrompt = prompts.ClaudeExecutorSystem + skillRegistry.SystemPromptAddendum()

	ag := agent.New(
		agentCfg,
		llmClient,
		memStore,
		auditLog,
		logger,
		allTools...,
	)

	// Sprint 9: Wire confirmation store for destructive operation gates.
	confirmStore := security.NewConfirmationStore()
	ag.SetConfirmationStore(confirmStore)

	// ── Workflow Engine (Sprint 5) ───────────────────────────────────────────
	var orch *workflow.Orchestrator
	if pgDB != nil {
		wfStore, wfErr := workflow.NewPGWorkflowStore(pgDB)
		if wfErr != nil {
			logger.Warn("workflow store init failed — workflows disabled", "err", wfErr)
		} else {
			dataStore := workflow.NewPGDataStore(pgDB)
			orch = workflow.NewOrchestrator(wfStore, dataStore, ag, cfg.WorkerCount, logger)

			// Sprint 12: Wire memory auto-save with Haiku for cheap insight extraction.
			haikuClient := llm.NewHaikuClient(anthropicKey, logger)
			orch.SetMemoryAutoSave(memStore, haikuClient)

			go orch.Run(ctx)
			logger.Info("workflow orchestrator started", "workers", cfg.WorkerCount)
		}
	} else {
		logger.Warn("postgres unavailable — workflows disabled")
	}

	// ── Scheduler + Runner (Sprint 6 + Sprint 14) ──────────────────────────
	var sched *scheduler.Scheduler
	var schedJobStore scheduler.JobStore
	var schedRunner *scheduler.Runner
	if pgDB != nil {
		jobStore, jsErr := scheduler.NewPGJobStore(ctx, pgDB)
		if jsErr != nil {
			logger.Warn("scheduler job store init failed", "err", jsErr)
		} else {
			schedJobStore = jobStore

			// Sprint 14: Create monitors table.
			if mErr := jobStore.CreateMonitorsTable(ctx); mErr != nil {
				logger.Warn("monitors table creation failed", "err", mErr)
			}

			// Legacy handler: direct agent.Run() for backward compat.
			schedHandler := func(ctx context.Context, sessionID, instruction string) {
				reply, err := ag.Run(ctx, agent.TurnInput{
					SessionID: sessionID,
					UserMsg: agent.Message{
						Role:      agent.RoleUser,
						SessionID: sessionID,
						Content:   instruction,
						CreatedAt: time.Now(),
					},
				})
				if err != nil {
					logger.Error("scheduled job failed", "session", sessionID, "err", err)
					return
				}
				logger.Info("scheduled job complete", "session", sessionID, "reply_len", len(reply.Reply))
			}
			sched = scheduler.New(jobStore, schedHandler, logger)
			sched.Start(ctx)

			// Sprint 14: Runner bridges scheduler to planner+orchestrator.
			schedRunner = scheduler.NewRunner(sched, jobStore, logger)
			schedRunner.Start(ctx)

			logger.Info("scheduler + runner started")
		}
	}

	// ── Deep Research Pipeline (Sprint Deep Research) ───────────────────
	var researchOrch *research.ResearchOrchestrator
	var researchStore *research.PGResearchStore
	var claimMem *research.ClaimMemory // hoisted for Sprint 17 command handler
	var webServer *webui.Server

	openRouterKey, orErr := sm.Get(ctx, "OPENROUTER_API_KEY")
	if orErr == nil && openRouterKey != "" && pgDB != nil {
		// OpenRouter models: planner, searcher (via tools), extractor.
		plannerClient := llm.NewOpenRouterClient(openRouterKey, "mistralai/mistral-large", logger)
		searcherClient := llm.NewOpenRouterClient(openRouterKey, "meta-llama/llama-4-scout", logger)
		// Mistral Small 3.1 — fast extraction (~0.10/1M vs $1.79/1M for 405B), 20x speed improvement.
		// 405B was taking 3-4 minutes per extraction call. Mistral Small handles JSON extraction well.
		// Switch to 405B if extraction quality drops: "meta-llama/llama-3.1-405b-instruct"
		extractorClient := llm.NewOpenRouterClient(openRouterKey, "mistralai/mistral-small-3.1-24b-instruct", logger)

		// GPT-4o critic (direct OpenAI — different provider than extractor).
		var criticClient *llm.OpenAIClient
		if openaiKey, openaiErr := sm.Get(ctx, "openai-api-key"); openaiErr == nil && openaiKey != "" {
			criticClient = llm.NewOpenAIClient(openaiKey, "gpt-4o", logger)
		}

		// Claude synthesizer (already have llmClient).
		synthesizerClient := llmClient // Claude Sonnet 4.6

		// Search tool for the searcher agent.
		searchTool := tools.NewWebSearchTool(braveKey)

		// Research store + budget tracker.
		rStore, rsErr := research.NewPGResearchStore(ctx, pgDB)
		if rsErr != nil {
			logger.Warn("research store init failed", "err", rsErr)
		} else {
			researchStore = rStore
		}

		budgetTracker := research.NewBudgetTracker(pgDB)

		// Sprint 16: Cross-session claim memory (timestamped, staleness-aware).
		if pgDB != nil {
			claimMem, _ = research.NewClaimMemory(ctx, pgDB, sharedEmbedder, logger)
			if claimMem != nil {
				logger.Info("claim memory ready")
			}
		}

		if criticClient != nil && researchStore != nil {
			researchOrch = research.NewResearchOrchestrator(
				plannerClient,
				searcherClient,
				extractorClient,
				criticClient,
				synthesizerClient,
				searchTool,
				memStore,
				claimMem,
				researchStore,
				budgetTracker,
				logger,
			)
			logger.Info("deep research pipeline ready",
				"planner", plannerClient.DisplayName(),
				"searcher", searcherClient.DisplayName(),
				"extractor", extractorClient.DisplayName(),
				"critic", "GPT-4o · OpenAI",
				"synthesizer", llmClient.ModelName(),
			)
		} else {
			logger.Warn("deep research partially unavailable — missing critic or store")
		}
	} else {
		logger.Warn("deep research disabled — needs openrouter-api-key + Postgres")
	}

	// ── Conversation History (in-memory, resets on restart) ──────────────────
	var histMu sync.Mutex
	history := make(map[string][]agent.Message)

	// ── Sprint 20: Persistent Claude Chat Store ───────────────────────────────
	var chatStore *webui.ChatStore
	if pgDB != nil {
		cs, chatStoreErr := webui.NewChatStore(ctx, pgDB)
		if chatStoreErr != nil {
			logger.Warn("chat store init failed", "err", chatStoreErr)
		} else {
			chatStore = cs
			logger.Info("claude chat store ready")
		}
	}

	// ── Sprint 20: Persistent Claude Chat function ────────────────────────────
	// Accepts history + new message, calls Claude with full context.
	var persistentChatFunc webui.PersistentChatFunc
	if chatStore != nil {
		persistentChatFunc = func(ctx context.Context, history []webui.ChatMessage, message string) (string, error) {
			// Build memory context.
			memContext := ""
			if facts, memErr := memStore.Search(ctx, message, 5); memErr == nil && len(facts) > 0 {
				memContext = "\n\n## What You Remember About Jeremy\n"
				for _, f := range facts {
					memContext += fmt.Sprintf("- %s\n", f.Content)
				}
			}

			// Build message list with history.
			chatSystemPrompt := systemPrompt + skillRegistry.SystemPromptAddendum() + memContext
			msgs := []agent.Message{
				{Role: agent.RoleSystem, Content: chatSystemPrompt},
			}
			// Inject history as alternating user/assistant turns.
			for _, h := range history {
				role := agent.RoleUser
				if h.Role == "assistant" {
					role = agent.RoleAssistant
				}
				msgs = append(msgs, agent.Message{
					Role:      role,
					Content:   h.Content,
					CreatedAt: h.CreatedAt,
				})
			}
			// Add new user message.
			msgs = append(msgs, agent.Message{
				Role:      agent.RoleUser,
				Content:   message,
				CreatedAt: time.Now(),
			})

			result, err := llmClient.Complete(ctx, msgs, nil)
			if err != nil {
				return "", fmt.Errorf("claude chat: %w", err)
			}
			return result.Content, nil
		}
	}

	// ── Sprint 17: Slack Command Handler ─────────────────────────────────────
	cmdHandler := &SlackCommands{
		orch:          orch,
		taskPlanner:   taskPlanner,
		sched:         sched,
		schedJobStore: schedJobStore,
		researchOrch:  researchOrch,
		researchStore: researchStore,
		memStore:      memStore,
		claimMemory:   claimMem, // hoisted from research pipeline init block
	}

	// Sprint 20: Wire //claude command to persistent chat.
	if chatStore != nil && persistentChatFunc != nil {
		cmdHandler.claudeChat = func(ctx context.Context, message, source string) (string, error) {
			history, _ := chatStore.History(ctx, 40)
			return persistentChatFunc(ctx, history, message)
		}
	}

	// Handler: msg → slash command or agent.Run() → reply
	handler := func(ctx context.Context, sessionID, userID, text string, attachments []gateway.Attachment) (string, error) {
		logger.Info("message received",
			"session", sessionID,
			"user", userID,
			"text_len", len(text),
			"attachments", len(attachments),
		)

		trimmed := strings.TrimSpace(text)

		// ── Sprint 9: Prompt injection detection + sanitization ──────────
		if security.IsLikelyInjection(trimmed, logger, sessionID, userID) {
			// Log and sanitize — still process the message but strip injection patterns.
			trimmed = security.SanitizeInput(trimmed)
			text = trimmed
		}

		// ── Sprint 9: Handle pending destructive operation confirmations ──
		if confirmStore.HasPending(sessionID) {
			if security.IsConfirmation(trimmed) {
				pending := confirmStore.GetPending(sessionID)
				if pending != nil {
					tool, ok := ag.GetTool(pending.ToolName)
					if ok {
						result, execErr := tool.Execute(ctx, pending.Input)
						if execErr != nil {
							return fmt.Sprintf("❌ Execution failed: %v", execErr), nil
						}
						return fmt.Sprintf("✅ Executed *%s*:\n%s", pending.ToolName, result.Content), nil
					}
					return "❌ Tool no longer available.", nil
				}
			}
			if security.IsCancellation(trimmed) {
				confirmStore.GetPending(sessionID) // clear it
				return "🚫 Action cancelled.", nil
			}
			// If neither confirm nor cancel, clear pending and process normally.
			confirmStore.GetPending(sessionID)
		}

		// ── Sprint 17: Route slash commands first ─────────────────────────
		if reply, handled := cmdHandler.Handle(ctx, sessionID, trimmed); handled {
			return reply, nil
		}

		// ── plan: <goal> — GPT-4o plans, Claude executes ─────────────────────
		if strings.HasPrefix(trimmed, "plan: ") && taskPlanner != nil && orch != nil {
			goal := strings.TrimSpace(strings.TrimPrefix(trimmed, "plan: "))
			if goal == "" {
				return "Usage: `/plan <goal>`\nExample: `/plan research top 5 GoHighLevel competitors and write a comparison report`", nil
			}

			logger.Info("plan requested", "goal", goal)

			graph, planErr := taskPlanner.Plan(ctx, goal)
			if planErr != nil {
				return fmt.Sprintf("❌ Planning failed: %v", planErr), nil
			}

			wfID, submitErr := planner.Submit(ctx, orch.Store(), graph, sessionID)
			if submitErr != nil {
				return fmt.Sprintf("❌ Failed to submit plan: %v", submitErr), nil
			}

			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("🧠 *GPT-4o planned %d tasks for Claude to execute:*\n\n", len(graph.Tasks)))
			for _, t := range graph.Tasks {
				parallel := ""
				if t.Parallel {
					parallel = " _(parallel)_"
				}
				deps := ""
				if len(t.DependsOn) > 0 {
					deps = fmt.Sprintf(" _(after %v)_", t.DependsOn)
				}
				sb.WriteString(fmt.Sprintf("*%d. %s*%s%s\n_%s_\n\n", t.Priority, t.Title, parallel, deps, truncateStr(t.Instruction, 120)))
			}
			sb.WriteString(fmt.Sprintf("🚀 Workflow `%s` started — Claude is on it.\nTrack progress: `//status %s`", wfID, wfID))

			return sb.String(), nil
		}

		// ── research: <goal> — deep multi-model research pipeline ──────────
		if strings.HasPrefix(trimmed, "research: ") {
			goal := strings.TrimSpace(strings.TrimPrefix(trimmed, "research: "))
			if goal == "" {
				return "Usage: `research: <goal>`\nExample: `research: what are the top GoHighLevel competitors and how do they compare?`", nil
			}

			if researchOrch == nil {
				return "❌ Deep Research not available — needs OpenRouter + Postgres.", nil
			}

			resID := "res_" + randomID()

			if researchStore != nil {
				_ = researchStore.CreateSession(ctx, resID, goal)
			}

			go func() {
				bgCtx := context.Background()
				state, resErr := researchOrch.RunDeepResearch(bgCtx, goal, resID)
				if resErr != nil {
					logger.Error("deep research failed", "session_id", resID, "err", resErr)
					if researchStore != nil {
						_ = researchStore.FailSession(bgCtx, resID, resErr.Error())
					}
					return
				}
				if researchStore != nil {
					_ = researchStore.CompleteSession(bgCtx, resID, state.FinalReport, state)
				}
				logger.Info("deep research completed", "session_id", resID, "iterations", state.Iteration, "cost", fmt.Sprintf("$%.4f", state.CostUSD))
			}()

			return fmt.Sprintf("🔬 *Deep Research started* — `%s`\nSession: `%s`\n\n5 AI models collaborating. Track: `/research status %s`\nUI: http://localhost:18790", goal, resID, resID[:12]), nil
		}

		// ── plan: without dependencies ────────────────────────────────────
		if strings.HasPrefix(trimmed, "plan: ") && (taskPlanner == nil || orch == nil) {
			if taskPlanner == nil {
				return "❌ Planner not available — add `openai-api-key` to Secret Manager.", nil
			}
			return "❌ Workflow engine not available — Postgres required.", nil
		}

		// ── //status <workflow_id> — check workflow progress ─────────────
		if strings.HasPrefix(trimmed, "//status ") && orch != nil {
			wfID := strings.TrimSpace(strings.TrimPrefix(trimmed, "//status "))
			tasks, err := orch.Status(ctx, wfID)
			if err != nil {
				return fmt.Sprintf("❌ Could not get workflow status: %v", err), nil
			}
			if len(tasks) == 0 {
				return fmt.Sprintf("No tasks found for workflow `%s`.", wfID), nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("📋 *Workflow `%s`* — %d tasks:\n", wfID, len(tasks)))
			for _, t := range tasks {
				icon := "⏳"
				switch t.Status {
				case agent.TaskDone:
					icon = "✅"
				case agent.TaskRunning:
					icon = "🔄"
				case agent.TaskFailed:
					icon = "❌"
				case agent.TaskCanceled:
					icon = "🚫"
				}
				sb.WriteString(fmt.Sprintf("%s Step %d: %s — _%s_\n", icon, t.Step, t.Name, t.Status))
			}
			return sb.String(), nil
		}

		// ── /workflow <instruction> — submit a multi-step workflow ────────────
		if (strings.HasPrefix(trimmed, "//workflow ") || isWorkflowRequest(trimmed)) && orch != nil {
			instruction := trimmed
			if strings.HasPrefix(trimmed, "//workflow ") {
				instruction = strings.TrimSpace(strings.TrimPrefix(trimmed, "//workflow "))
			}
			wfID, err := orch.Submit(ctx, sessionID, instruction)
			if err != nil {
				return fmt.Sprintf("❌ Failed to start workflow: %v", err), nil
			}
			return fmt.Sprintf("🚀 Workflow `%s` started — use `//status %s` to check progress.", wfID, wfID), nil
		}

		// ── /schedule <cron> | <instruction> — add a scheduled job ───────────
		if strings.HasPrefix(trimmed, "//schedule ") && sched != nil {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "//schedule "), " | ", 2)
			if len(parts) != 2 {
				return "Usage: `/schedule <cron_expr> | <instruction>`\nExample: `/schedule 0 8 * * 1 | Check open GHL leads`", nil
			}
			cronExpr := strings.TrimSpace(parts[0])
			instruction := strings.TrimSpace(parts[1])
			id := randomID()
			err := sched.Add(ctx, scheduler.Job{
				ID:          id,
				Name:        truncateStr(instruction, 60),
				CronExpr:    cronExpr,
				Instruction: instruction,
				SessionID:   sessionID,
			})
			if err != nil {
				return fmt.Sprintf("❌ Schedule failed: %v", err), nil
			}
			return fmt.Sprintf("⏰ Scheduled `%s` — cron: `%s`\nUse `/schedule list` to see all.", id, cronExpr), nil
		}

		// Process attachments: images go to Claude multimodal, PDFs get text extracted.
		var images []agent.ImageAttachment
		pdfText := ""

		for _, att := range attachments {
			switch att.MediaType {
			case "image/jpeg", "image/png", "image/gif", "image/webp":
				images = append(images, agent.ImageAttachment{
					Data:      att.Data,
					MediaType: att.MediaType,
				})
			case "application/pdf":
				extracted, pdfErr := extractPDF(att.Data)
				if pdfErr == nil && extracted != "" {
					pdfText += "\n\n[PDF: " + att.Filename + "]\n" + extracted
				} else if pdfErr != nil {
					logger.Warn("PDF extraction failed", "file", att.Filename, "err", pdfErr)
				}
			}
		}

		// Build user message.
		userMsg := agent.Message{
			Role:      agent.RoleUser,
			SessionID: sessionID,
			Content:   text + pdfText,
			Images:    images,
			CreatedAt: time.Now(),
		}

		// Get conversation history for this session.
		histMu.Lock()
		hist := history[sessionID]
		histMu.Unlock()

		// Run the agent.
		input := agent.TurnInput{
			SessionID: sessionID,
			History:   hist,
			UserMsg:   userMsg,
		}

		output, err := ag.Run(ctx, input)
		if err != nil {
			return "", fmt.Errorf("agent.Run: %w", err)
		}

		// Auto-save substantial replies to long-term memory.
		if pgStore, ok := memStore.(*memory.Store); ok {
			pgStore.AutoSave(ctx, sessionID, output.Reply)
		}

		// Update conversation history.
		histMu.Lock()
		history[sessionID] = append(history[sessionID], userMsg, agent.Message{
			Role:      agent.RoleAssistant,
			SessionID: sessionID,
			Content:   output.Reply,
			CreatedAt: time.Now(),
		})
		if len(history[sessionID]) > 50 {
			history[sessionID] = history[sessionID][len(history[sessionID])-50:]
		}
		histMu.Unlock()

		logger.Info("agent turn complete",
			"session", sessionID,
			"tokens", output.TokensUsed,
			"tools", output.ToolsInvoked,
			"reply_len", len(output.Reply),
		)

		return output.Reply, nil
	}

	// ── Webhook Gateway (Sprint 6) ──────────────────────────────────────────
	webhookSecret, _ := sm.Get(ctx, "zbot-webhook-secret")
	webhookGW := gateway.NewWebhookGateway(18791, webhookSecret, handler, logger)
	go webhookGW.Start(ctx)
	logger.Info("webhook gateway ready", "port", 18791)

	// ── Haiku Client for cheap tasks (Sprint 12) ────────────────────────────
	haikuForChat := llm.NewHaikuClient(anthropicKey, logger)

	// ── Web UI (Sprint 8 + Sprint 11 Dual Brain Command Center) ─────────────
	if pgDB != nil {
		webServer = webui.New(pgDB, logger)

		// Sprint 12: Wire memory store for memory panel API.
		webServer.SetMemoryStore(memStore)

		// Sprint 12: Wire memory-aware quick chat handler.
		webServer.SetQuickChat(func(ctx context.Context, message string) (string, error) {
			// 1. Search memory for context relevant to the message.
			memContext := ""
			if facts, memErr := memStore.Search(ctx, message, 5); memErr == nil && len(facts) > 0 {
				memContext = "\n\n## What You Remember About Jeremy\n"
				for _, f := range facts {
					memContext += fmt.Sprintf("- %s\n", f.Content)
				}
				logger.Info("quick chat: injected memory context", "facts", len(facts))
			}

			// 2. Call Claude with memory-augmented system prompt.
			chatSystemPrompt := systemPrompt + skillRegistry.SystemPromptAddendum() + memContext
			chatMessages := []agent.Message{
				{Role: agent.RoleSystem, Content: chatSystemPrompt},
				{Role: agent.RoleUser, Content: message, CreatedAt: time.Now()},
			}

			result, err := llmClient.Complete(ctx, chatMessages, nil)
			if err != nil {
				return "", fmt.Errorf("quick chat llm: %w", err)
			}

			reply := result.Content

			// 3. Check if response contains a saveable fact (via Haiku — cheap).
			go func() {
				factCheckPrompt := fmt.Sprintf(`User said: %q
Assistant replied: %q

Does this conversation contain a fact worth saving to long-term memory about the user or their business?
If yes, respond with JSON: {"save": true, "fact": "the fact to save"}
If no, respond with JSON: {"save": false}`, message, reply)

				factCheckCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()

				factResult, factErr := haikuForChat.Complete(factCheckCtx, []agent.Message{
					{Role: agent.RoleUser, Content: factCheckPrompt},
				}, nil)
				if factErr != nil {
					return
				}

				// Parse the fact check response.
				var factCheck struct {
					Save bool   `json:"save"`
					Fact string `json:"fact"`
				}
				raw := factResult.Content
				// Find JSON in response.
				start := -1
				end := -1
				for i, c := range raw {
					if c == '{' && start == -1 {
						start = i
					}
					if c == '}' {
						end = i + 1
					}
				}
				if start >= 0 && end > start {
					if jsonErr := json.Unmarshal([]byte(raw[start:end]), &factCheck); jsonErr == nil && factCheck.Save && factCheck.Fact != "" {
						b := make([]byte, 8)
						rand.Read(b)
						fact := agent.Fact{
							ID:        hex.EncodeToString(b),
							Content:   factCheck.Fact,
							Source:    "quick_chat",
							Tags:      []string{"personal"},
							CreatedAt: time.Now(),
						}
						if saveErr := memStore.Save(factCheckCtx, fact); saveErr == nil {
							logger.Info("quick chat: auto-saved fact", "fact", factCheck.Fact)
						}
					}
				}
			}()

			return reply, nil
		})

		// Sprint 11: Wire planner + orchestrator for dual-brain streaming.
		if taskPlanner != nil {
			webServer.SetPlanner(taskPlanner)
		}
		if orch != nil {
			webServer.SetOrchestrator(orch)

			// Hook orchestrator task events into the SSE hub.
			hub := webServer.Hub()
			orch.SetTaskEventHook(func(workflowID, taskID, eventType, payload string) {
				hub.Publish(webui.Event{
					WorkflowID: workflowID,
					TaskID:     taskID,
					Source:     "executor",
					Type:       eventType,
					Payload:    payload,
				})
			})

			// Sprint 11: Wire GPT-4o critic loop into orchestrator.
			if taskCritic != nil {
				orch.SetCriticFunc(func(ctx context.Context, workflowID, taskID, instruction, output string) (string, string, bool, error) {
					// Publish "reviewing" event to SSE.
					hub.Publish(webui.Event{
						WorkflowID: workflowID,
						TaskID:     taskID,
						Source:     "critic",
						Type:       "reviewing",
						Payload:    "",
					})

					verdict, err := taskCritic.Review(ctx, taskID, instruction, output)
					if err != nil {
						return "", "", false, err
					}

					verdictJSON, _ := json.Marshal(verdict)

					// Publish verdict event to SSE.
					hub.Publish(webui.Event{
						WorkflowID: workflowID,
						TaskID:     taskID,
						Source:     "critic",
						Type:       "verdict",
						Payload:    string(verdictJSON),
					})

					shouldRetry := verdict.Verdict == "fail" && verdict.CorrectedInstruction != ""

					if shouldRetry {
						// Publish retrying event to SSE.
						hub.Publish(webui.Event{
							WorkflowID: workflowID,
							TaskID:     taskID,
							Source:     "critic",
							Type:       "status",
							Payload:    "retrying",
						})
					}

					return string(verdictJSON), verdict.CorrectedInstruction, shouldRetry, nil
				})
			}
		}

		// Sprint 14: Wire scheduler + job store for schedule panel API.
		if sched != nil && schedJobStore != nil {
			webServer.SetScheduler(sched, schedJobStore)
			webServer.SetLLMClient(llmClient)
			logger.Info("schedule panel wired into web UI")
		}

		// Deep Research: Wire research orchestrator + store into web server.
		if researchOrch != nil && researchStore != nil {
			webServer.SetResearch(researchOrch, researchStore)
			logger.Info("deep research panel wired into web UI")
		}

		go webServer.Start(ctx)
		logger.Info("web UI available", "url", "http://localhost:18790")
	} else {
		logger.Warn("web UI disabled — no Postgres connection")
	}

	// ── Slack Gateway ────────────────────────────────────────────────────────
	slackGW := gateway.NewSlackGateway(
		botToken,
		appToken,
		allowedUsers,
		handler,
		logger,
	)

	// Wire Slack notifier for research completions and scheduled jobs.
	// allowedUserID is the DM channel to post to (Slack DM channel = user ID).
	if allowedUserID != "" && allowedUserID != "PENDING" {
		if webServer != nil {
			webServer.SetSlackNotifier(slackGW, allowedUserID)
		}
		if schedRunner != nil {
			schedRunner.SetFallbackChannel(allowedUserID)
		}
	}

	logger.Info("ZBOT starting — connecting to Slack",
		"model", llmClient.ModelName(),
		"workspace", workspaceRoot,
		"skills", skillRegistry.Names(),
	)
	return slackGW.Start(ctx)
}

// connectPostgres connects to Cloud SQL pgvector instance.
func connectPostgres(ctx context.Context, logger *slog.Logger, password string) (*pgxpool.Pool, error) {
	connStr := fmt.Sprintf("postgresql://ziloss:%s@34.28.163.109:5432/ziloss_memory?sslmode=disable", password)

	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	poolCfg.MaxConns = 5
	poolCfg.MinConns = 1

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	logger.Info("postgres connected", "host", "34.28.163.109")
	return pool, nil
}

// isWorkflowRequest returns true for natural-language workflow trigger phrases.
func isWorkflowRequest(text string) bool {
	lower := strings.ToLower(text)
	triggers := []string{
		"research and compare", "do all of this", "run a workflow",
		"research 5 ", "research 10 ", "analyze all ", "find and compare",
	}
	for _, t := range triggers {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}

// extractPDF extracts text from a PDF byte slice via pdftotext (poppler).
func extractPDF(data []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "zbot-pdf-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	cmd := exec.CommandContext(context.Background(), "pdftotext", tmpFile.Name(), "-")
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		text := string(out)
		if len(text) > 100*1024 {
			text = text[:100*1024] + "\n[TRUNCATED — PDF text exceeds 100KB]"
		}
		return text, nil
	}

	return "", fmt.Errorf("pdftotext unavailable or failed: %w", err)
}

// randomID generates a short hex ID.
func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// truncateStr shortens a string for display.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
