package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zbot-ai/zbot/internal/agent"
	"github.com/zbot-ai/zbot/internal/audit"
	"github.com/zbot-ai/zbot/internal/gateway"
	"github.com/zbot-ai/zbot/internal/llm"
	"github.com/zbot-ai/zbot/internal/memory"
	"github.com/zbot-ai/zbot/internal/platform"
	"github.com/zbot-ai/zbot/internal/prompts"
	"github.com/zbot-ai/zbot/internal/scheduler"
	"github.com/zbot-ai/zbot/internal/scraper"
	"github.com/zbot-ai/zbot/internal/secrets"
	"github.com/zbot-ai/zbot/internal/skills"
	skillEmail "github.com/zbot-ai/zbot/internal/skills/email"
	skillGHL "github.com/zbot-ai/zbot/internal/skills/ghl"
	skillGitHub "github.com/zbot-ai/zbot/internal/skills/github"
	skillMemory "github.com/zbot-ai/zbot/internal/skills/memory"
	skillSearch "github.com/zbot-ai/zbot/internal/skills/search"
	skillSheets "github.com/zbot-ai/zbot/internal/skills/sheets"
	"github.com/zbot-ai/zbot/internal/research"
	"github.com/zbot-ai/zbot/internal/security"
	"github.com/zbot-ai/zbot/internal/tools"
	"github.com/zbot-ai/zbot/internal/webui"
	"github.com/zbot-ai/zbot/internal/workflow"
)

// defaultSystemPrompt is ZBOT's base instruction set.
// Override with ZBOT_SYSTEM_PROMPT env var for custom personas.
const defaultSystemPrompt = `You are ZBOT, a self-hosted AI agent with persistent memory and tool use.

YOUR CAPABILITIES:
- web_search: Search the internet for current information
- fetch_url: Fetch and read the content of any URL
- read_file / write_file: Read and write files in the workspace
- run_code: Execute Python, Go, JavaScript, or bash code in a secure sandbox
- save_memory: Save important facts to long-term memory
- search_memory: Search long-term memory for previously saved facts
- analyze_image: Analyze photos, screenshots, charts, or any image

YOUR PERSONALITY:
- Direct and efficient — no fluff, no unnecessary caveats
- Action-oriented — when in doubt, do it and report back
- Honest about limitations and uncertainty
- Proactive — if you notice something important while doing a task, mention it

INSTRUCTIONS:
- Always use your tools when they would help — don't just answer from memory when you could verify with web_search
- For multi-step tasks, work through them systematically and report progress
- Save important information to memory using save_memory so you remember it next time
- Use search_memory when the user asks "do you remember..." or "what do you know about..."
- Keep responses concise unless detail is specifically needed
- If a task will take multiple steps, briefly outline your plan before starting`

// systemPrompt resolves the active system prompt (env override or default).
var systemPrompt = func() string {
	if custom := os.Getenv("ZBOT_SYSTEM_PROMPT"); custom != "" {
		return custom
	}
	return defaultSystemPrompt
}()

func run(ctx context.Context, cfg platform.AppConfig, logger *slog.Logger) error {

	// ── Secrets ─────────────────────────────────────────────────────────────
	// Try GCP Secret Manager first; fall back to env vars for Docker/Coolify.
	var sm agent.SecretsManager
	if cfg.GCPProject != "" {
		gcpSM, gcpErr := secrets.NewGCPSecretManager(ctx, cfg.GCPProject)
		if gcpErr != nil {
			logger.Warn("GCP Secret Manager unavailable — using env var fallback", "err", gcpErr)
			sm = secrets.NewEnvSecretManager()
		} else {
			sm = gcpSM
			defer gcpSM.Close()
		}
	} else {
		logger.Info("no GCP project configured — using env var secrets")
		sm = secrets.NewEnvSecretManager()
	}

	botToken, _ := sm.Get(ctx, "zbot-slack-token")
	appToken, _ := sm.Get(ctx, "zbot-slack-app-token")
	anthropicKey, _ := sm.Get(ctx, secrets.SecretAnthropicAPIKey)
	braveKey, _ := sm.Get(ctx, secrets.SecretBraveAPIKey)
	serperKey := os.Getenv("ZBOT_SERPER_API_KEY")
	if serperKey == "" {
		serperKey, _ = sm.Get(ctx, "serper-api-key")
	}

	// Pick the cheapest available search tool.
	// Priority: Serper ($0.30/1K) > Brave ($5/1K)
	var searchTool agent.Tool
	if serperKey != "" {
		searchTool = tools.NewSerperSearchTool(serperKey)
		logger.Info("search provider: Serper (Google, $0.30/1K)")
	} else if braveKey != "" {
		searchTool = tools.NewWebSearchTool(braveKey)
		logger.Info("search provider: Brave ($5/1K)")
	} else {
		logger.Warn("no search API key configured — web_search disabled")
	}

	// Check for configurable LLM backend (OpenAI-compatible: Ollama, Together, Groq, etc.)
	llmBaseURL := os.Getenv("ZBOT_LLM_BASE_URL")
	llmModel := os.Getenv("ZBOT_LLM_MODEL")
	llmAPIKey := os.Getenv("ZBOT_LLM_API_KEY")
	if llmAPIKey == "" {
		llmAPIKey = anthropicKey // fallback to Anthropic key for OpenAI-compat providers
	}
	useOpenAICompat := llmBaseURL != "" && llmModel != ""

	// At least one LLM backend must be configured.
	if !useOpenAICompat && anthropicKey == "" {
		return fmt.Errorf("no LLM configured: set ZBOT_LLM_BASE_URL+ZBOT_LLM_MODEL for open models, or ZBOT_ANTHROPIC_API_KEY for Claude")
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
	// Supports two modes:
	//   1. OpenAI-compatible (Ollama, Together, Groq, vLLM, LM Studio, OpenRouter)
	//      Set ZBOT_LLM_BASE_URL + ZBOT_LLM_MODEL + optionally ZBOT_LLM_API_KEY
	//   2. Anthropic Claude (default)
	//      Set ZBOT_ANTHROPIC_API_KEY
	var llmClient agent.LLMClient
	if useOpenAICompat {
		llmClient = llm.NewOpenAICompatClient(llmBaseURL, llmAPIKey, llmModel, logger)
		logger.Info("LLM client ready (OpenAI-compatible)", "model", llmModel, "base_url", llmBaseURL)
	} else {
		llmClient = llm.New(anthropicKey, logger)
		logger.Info("LLM client ready (Anthropic)", "model", llmClient.ModelName())
	}

	// v2: Planner + Critic REMOVED — single-brain architecture.
	// Claude handles planning via orchestrator.decompose() and self-critiques.
	// OpenAI key is no longer required for core functionality.
	logger.Info("v2: single-brain architecture — Claude handles planning + execution + self-critique")

	// ── Sprint D: Memory Flusher (context window compaction safety net) ────
	var cheapModelClient agent.LLMClient
	if useOpenAICompat {
		cheapModelClient = llmClient
	} else {
		cheapModelClient = llm.NewHaikuClient(anthropicKey, logger)
	}

	if pgStore, ok := memStore.(*memory.Store); ok && pgDB != nil {
		flusher, flushErr := memory.NewFlusher(pgStore, cheapModelClient, pgDB, logger)
		if flushErr != nil {
			logger.Warn("memory flusher init failed", "err", flushErr)
		} else {
			_ = flusher // v2: Wired into agent loop when context window management is implemented.
			logger.Info("memory flusher ready (Sprint D)")
		}
	}

	// ── Sprint D Stretch: Markdown daily notes layer ────────────────────────
	dailyNotesDir := filepath.Join(workspaceRoot, "memory")
	dailyNotesWriter, dnErr := memory.NewDailyNotesWriter(dailyNotesDir)
	if dnErr != nil {
		logger.Warn("daily notes writer init failed", "err", dnErr)
	} else {
		logger.Info("daily notes writer ready", "dir", dailyNotesDir)
	}

	// ── Sprint D Stretch: Diversity re-ranker for memory search ─────────────
	diversityReranker := memory.NewDiversityReranker(0) // 0 → uses default threshold (0.92)
	logger.Info("diversity reranker ready", "threshold", memory.DefaultDiversityThreshold)

	// ── Sprint D Stretch: Memory curator (periodic fact promotion) ───────────
	if pgStore, ok := memStore.(*memory.Store); ok && dailyNotesWriter != nil {
		curator := memory.NewCurator(pgStore, dailyNotesWriter, cheapModelClient, logger)
		_ = curator // v2: Wired into scheduled task when periodic curation is enabled.
		_ = diversityReranker // v2: Wired into memory search when retrieval pipeline is enhanced.
		logger.Info("memory curator ready (Sprint D stretch)")
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

	// ── Sprint C: Site credentials for authenticated fetching ───────────────
	siteCredentials := []tools.SiteCredential{
		// Add domain → credential mappings here as needed.
		// Example: {DomainPattern: "*.github.com", Type: tools.CredBearer, SecretKey: "github-token"},
	}

	// ── Core Tools ──────────────────────────────────────────────────────────
	coreTools := []agent.Tool{
		searchTool,
		tools.NewFetchURLToolFull(proxyPool, rateLimiter, scrapeCache, browserFetcher),
		tools.NewCredentialedFetchTool(sm, siteCredentials, proxyPool, rateLimiter, scrapeCache, logger),
		tools.NewManageCredentialsTool(sm, logger), // Sprint C stretch: credential CRUD
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
		skillRegistry.Register(skillGHL.NewSkill(ghlKey, cfg.GHLLocationID))
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

	// Event bus for Thalamus oversight + UI streaming.
	eventBus := agent.NewMemEventBus(200)

	ag := agent.New(
		agentCfg,
		llmClient,
		memStore,
		auditLog,
		eventBus,
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

			// Sprint 12: Wire memory auto-save with cheap model for insight extraction.
			var cheapClient agent.LLMClient
			if useOpenAICompat {
				cheapClient = llmClient
			} else {
				cheapClient = llm.NewHaikuClient(anthropicKey, logger)
			}
			orch.SetMemoryAutoSave(memStore, cheapClient)

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
	// Forward reference: set when slackGW is created so scheduler can send replies.
	var slackSendFn func(ctx context.Context, channelID, text string) error
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
				// Send reply to Slack (if wired).
				if slackSendFn != nil && reply.Reply != "" {
					msg := fmt.Sprintf("⏰ *Scheduled task complete:*\n%s", reply.Reply)
					logger.Info("scheduled job sending to Slack", "session", sessionID, "msg_len", len(msg))
					if sendErr := slackSendFn(ctx, sessionID, msg); sendErr != nil {
						logger.Error("scheduled job: Slack send failed", "session", sessionID, "err", sendErr)
					} else {
						logger.Info("scheduled job: Slack send OK", "session", sessionID)
					}
				} else {
					logger.Warn("scheduled job: slackSendFn not wired or empty reply", "fn_nil", slackSendFn == nil, "reply_empty", reply.Reply == "")
				}
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
		logger.Warn("deep research v1 disabled — needs openrouter-api-key + Postgres")
	}

	// ── v2 Deep Research Pipeline (Haiku → Sonnet, no external deps) ─────
	var researchOrchV2 *research.V2ResearchOrchestrator
	if pgDB != nil && researchStore != nil {
		haikuClient := llm.NewHaikuClient(anthropicKey, logger)
		budgetTracker := research.NewBudgetTracker(pgDB)

		researchOrchV2 = research.NewV2ResearchOrchestrator(
			haikuClient,
			llmClient, // Sonnet
			searchTool,
			memStore,
			claimMem,
			researchStore,
			budgetTracker,
			logger,
		)
		logger.Info("v2 deep research pipeline ready",
			"phase1", haikuClient.ModelName(),
			"phase2", llmClient.ModelName(),
		)
	}
	// Prefer v2 if available; fall back to v1.
	_ = researchOrchV2

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

		// ── plan: <goal> — v2: Claude decomposes + executes (single-brain) ──
		if strings.HasPrefix(trimmed, "plan: ") && orch != nil {
			goal := strings.TrimSpace(strings.TrimPrefix(trimmed, "plan: "))
			if goal == "" {
				return "Usage: `plan: <goal>`\nExample: `plan: research top 5 GoHighLevel competitors and write a comparison report`", nil
			}

			logger.Info("plan requested (v2 single-brain)", "goal", goal)

			wfID, submitErr := orch.Submit(ctx, sessionID, goal)
			if submitErr != nil {
				return fmt.Sprintf("❌ Failed to plan + submit: %v", submitErr), nil
			}

			return fmt.Sprintf("🧠 *Claude decomposed and started workflow:*\n\n🚀 Workflow `%s` started.\nTrack progress: `//status %s`", wfID, wfID), nil
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

		// ── plan: without workflow engine ──────────────────────────────────
		if strings.HasPrefix(trimmed, "plan: ") && orch == nil {
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

	// ── Web UI (Sprint 8 + Sprint 11 Dual Brain Command Center) ─────────────
	// Always start web UI so Cloud Run health checks pass (pgDB may be nil).
	webServer = webui.New(pgDB, logger)

	// v2: Wire agentic quick chat — uses full agent.Run() with tool use + event bus.
	// Works with or without Postgres (memory degrades gracefully).
	webServer.SetQuickChat(func(ctx context.Context, message string) (string, error) {
		turnInput := agent.TurnInput{
			SessionID: "web-chat",
			UserMsg: agent.Message{
				Role:      agent.RoleUser,
				Content:   message,
				CreatedAt: time.Now(),
			},
		}

		result, err := ag.Run(ctx, turnInput)
		if err != nil {
			return "", fmt.Errorf("agent.Run: %w", err)
		}

		return result.Reply, nil
	})

	if pgDB != nil {
		// Sprint 12: Wire memory store for memory panel API.
		webServer.SetMemoryStore(memStore)

		// v2: Wire orchestrator for single-brain workflow execution.
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

			// v2: Critic REMOVED — Claude self-critiques within the single-brain loop.
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

	}

	// Always start the web server (even without Postgres — degraded mode).
	go webServer.Start(ctx)
	if pgDB != nil {
		logger.Info("web UI available", "url", "http://localhost:18790")
	} else {
		logger.Warn("web UI started in degraded mode — no Postgres connection")
	}

	// ── Slack Gateway (optional — skip if no tokens) ────────────────────────
	if botToken != "" && appToken != "" {
		slackGW := gateway.NewSlackGateway(
			botToken,
			appToken,
			allowedUsers,
			handler,
			logger,
		)

		// Wire Slack notifier for research completions and scheduled jobs.
		slackSendFn = slackGW.Send
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

	// No Slack tokens — run in web-only mode. Block until ctx is cancelled.
	logger.Info("ZBOT running in web-only mode (no Slack tokens)",
		"model", llmClient.ModelName(),
		"workspace", workspaceRoot,
		"skills", skillRegistry.Names(),
	)
	<-ctx.Done()
	return nil
}

// connectPostgres connects to Cloud SQL pgvector instance.
// Supports ZBOT_DATABASE_URL env var override for flexible deployment.
func connectPostgres(ctx context.Context, logger *slog.Logger, password string) (*pgxpool.Pool, error) {
	connStr := os.Getenv("ZBOT_DATABASE_URL")
	if connStr == "" {
		dbHost := os.Getenv("ZBOT_DB_HOST")
		if dbHost == "" {
			dbHost = "localhost"
		}
		dbName := os.Getenv("ZBOT_DB_NAME")
		if dbName == "" {
			dbName = "zbot"
		}
		dbUser := os.Getenv("ZBOT_DB_USER")
		if dbUser == "" {
			dbUser = "zbot"
		}
		connStr = fmt.Sprintf("postgresql://%s:%s@%s:5432/%s?sslmode=disable", dbUser, password, dbHost, dbName)
	}

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

	logger.Info("postgres connected")
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
