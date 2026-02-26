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

	"github.com/jeremylerwick-max/zbot/internal/agent"
	"github.com/jeremylerwick-max/zbot/internal/audit"
	"github.com/jeremylerwick-max/zbot/internal/gateway"
	"github.com/jeremylerwick-max/zbot/internal/llm"
	"github.com/jeremylerwick-max/zbot/internal/memory"
	"github.com/jeremylerwick-max/zbot/internal/platform"
	"github.com/jeremylerwick-max/zbot/internal/scheduler"
	"github.com/jeremylerwick-max/zbot/internal/scraper"
	"github.com/jeremylerwick-max/zbot/internal/secrets"
	"github.com/jeremylerwick-max/zbot/internal/skills"
	skillEmail "github.com/jeremylerwick-max/zbot/internal/skills/email"
	skillGHL "github.com/jeremylerwick-max/zbot/internal/skills/ghl"
	skillGitHub "github.com/jeremylerwick-max/zbot/internal/skills/github"
	skillSheets "github.com/jeremylerwick-max/zbot/internal/skills/sheets"
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
	pgDB, pgErr := connectPostgres(ctx, logger)
	if pgErr != nil {
		logger.Warn("postgres unavailable — some features disabled", "err", pgErr)
	}

	// ── Memory Store (pgvector or in-memory fallback) ───────────────────────
	var memStore agent.MemoryStore
	if pgDB == nil {
		memStore = memory.NewInMemoryStore(logger)
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
		tools.NewMemorySaveTool(memStore),
		tools.NewSearchMemoryTool(memStore),
		tools.NewAnalyzeImageTool(llmClient, workspaceRoot),
		tools.NewPDFExtractTool(workspaceRoot),
	}

	// ── Skills System (Sprint 7) ────────────────────────────────────────────
	skillRegistry := skills.NewRegistry()

	// GHL skill.
	if ghlKey, ghlErr := sm.Get(ctx, "ghl-api-key"); ghlErr == nil && ghlKey != "" {
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
	agentCfg.SystemPrompt = systemPrompt + skillRegistry.SystemPromptAddendum()

	ag := agent.New(
		agentCfg,
		llmClient,
		memStore,
		auditLog,
		logger,
		allTools...,
	)

	// ── Workflow Engine (Sprint 5) ───────────────────────────────────────────
	var orch *workflow.Orchestrator
	if pgDB != nil {
		wfStore, wfErr := workflow.NewPGWorkflowStore(pgDB)
		if wfErr != nil {
			logger.Warn("workflow store init failed — workflows disabled", "err", wfErr)
		} else {
			dataStore := workflow.NewPGDataStore(pgDB)
			orch = workflow.NewOrchestrator(wfStore, dataStore, ag, cfg.WorkerCount, logger)
			go orch.Run(ctx)
			logger.Info("workflow orchestrator started", "workers", cfg.WorkerCount)
		}
	} else {
		logger.Warn("postgres unavailable — workflows disabled")
	}

	// ── Scheduler (Sprint 6) ────────────────────────────────────────────────
	var sched *scheduler.Scheduler
	if pgDB != nil {
		jobStore, jsErr := scheduler.NewPGJobStore(ctx, pgDB)
		if jsErr != nil {
			logger.Warn("scheduler job store init failed", "err", jsErr)
		} else {
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
			logger.Info("scheduler started")
		}
	}

	// ── Conversation History (in-memory, resets on restart) ──────────────────
	var histMu sync.Mutex
	history := make(map[string][]agent.Message)

	// Handler: msg → slash command or agent.Run() → reply
	handler := func(ctx context.Context, sessionID, userID, text string, attachments []gateway.Attachment) (string, error) {
		logger.Info("message received",
			"session", sessionID,
			"user", userID,
			"text_len", len(text),
			"attachments", len(attachments),
		)

		trimmed := strings.TrimSpace(text)

		// ── /status <workflowID> — show workflow progress ────────────────────
		if strings.HasPrefix(trimmed, "/status ") && orch != nil {
			wfID := strings.TrimSpace(strings.TrimPrefix(trimmed, "/status "))
			tasks, err := orch.Status(ctx, wfID)
			if err != nil {
				return fmt.Sprintf("❌ Could not get status for `%s`: %v", wfID, err), nil
			}
			done := 0
			for _, t := range tasks {
				if t.Status == agent.TaskDone {
					done++
				}
			}
			sum := &workflow.WorkflowSummary{WorkflowID: wfID, Tasks: tasks, Done: done, Total: len(tasks)}
			return sum.Format(), nil
		}

		// ── /cancel <workflowID> — cancel pending tasks ───────────────────────
		if strings.HasPrefix(trimmed, "/cancel ") && orch != nil {
			wfID := strings.TrimSpace(strings.TrimPrefix(trimmed, "/cancel "))
			if err := orch.Cancel(ctx, wfID); err != nil {
				return fmt.Sprintf("❌ Cancel failed: %v", err), nil
			}
			return fmt.Sprintf("🚫 Workflow `%s` — pending tasks canceled.", wfID), nil
		}

		// ── /workflow <instruction> — submit a multi-step workflow ────────────
		if (strings.HasPrefix(trimmed, "/workflow ") || isWorkflowRequest(trimmed)) && orch != nil {
			instruction := trimmed
			if strings.HasPrefix(trimmed, "/workflow ") {
				instruction = strings.TrimSpace(strings.TrimPrefix(trimmed, "/workflow "))
			}
			wfID, err := orch.Submit(ctx, sessionID, instruction)
			if err != nil {
				return fmt.Sprintf("❌ Failed to start workflow: %v", err), nil
			}
			return fmt.Sprintf("🚀 Workflow `%s` started — use `/status %s` to check progress.", wfID, wfID), nil
		}

		// ── /schedule <cron> | <instruction> — add a scheduled job ───────────
		if strings.HasPrefix(trimmed, "/schedule ") && sched != nil {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "/schedule "), " | ", 2)
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
			return fmt.Sprintf("⏰ Scheduled `%s` — cron: `%s`\nUse `/schedules` to see all.", id, cronExpr), nil
		}

		// ── /schedules — list all scheduled jobs ─────────────────────────────
		if trimmed == "/schedules" && sched != nil {
			jobs := sched.List()
			if len(jobs) == 0 {
				return "No scheduled jobs. Use `/schedule <cron> | <instruction>` to create one.", nil
			}
			var sb strings.Builder
			sb.WriteString("⏰ **Scheduled Jobs**\n\n")
			for _, j := range jobs {
				sb.WriteString(fmt.Sprintf("• `%s` — `%s` — %s\n  Next: %s\n",
					j.ID, j.CronExpr, truncateStr(j.Instruction, 50),
					j.NextRun.Format("2006-01-02 15:04 MST"),
				))
			}
			return sb.String(), nil
		}

		// ── /unschedule <id> — remove a scheduled job ───────────────────────
		if strings.HasPrefix(trimmed, "/unschedule ") && sched != nil {
			id := strings.TrimSpace(strings.TrimPrefix(trimmed, "/unschedule "))
			if err := sched.Remove(ctx, id); err != nil {
				return fmt.Sprintf("❌ Unschedule failed: %v", err), nil
			}
			return fmt.Sprintf("🗑️ Job `%s` removed.", id), nil
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

	// ── Web UI (Sprint 8) ───────────────────────────────────────────────────
	if pgDB != nil {
		webServer := webui.New(pgDB, logger)
		go webServer.Start(ctx)
		logger.Info("web UI available", "url", "http://localhost:18790")
	} else {
		logger.Warn("web UI disabled — no Postgres connection")
	}

	// ── Slack Gateway ────────────────────────────────────────────────────────
	slackGW := gateway.NewSlackGateway(
		botToken,
		appToken,
		cfg.TelegramAllowFrom,
		handler,
		logger,
	)

	logger.Info("ZBOT starting — connecting to Slack",
		"model", llmClient.ModelName(),
		"workspace", workspaceRoot,
		"skills", skillRegistry.Names(),
	)
	return slackGW.Start(ctx)
}

// connectPostgres connects to Cloud SQL pgvector instance.
func connectPostgres(ctx context.Context, logger *slog.Logger) (*pgxpool.Pool, error) {
	connStr := "postgresql://ziloss:ZilossMemory2024!@34.28.163.109:5432/ziloss_memory?sslmode=disable"

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
