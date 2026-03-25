package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ziloss-tech/zbot/internal/agent"
	"github.com/ziloss-tech/zbot/internal/audit"
	"github.com/ziloss-tech/zbot/internal/gateway"
	"github.com/ziloss-tech/zbot/internal/llm"
	"github.com/ziloss-tech/zbot/internal/memory"
	"github.com/ziloss-tech/zbot/internal/platform"
	"github.com/ziloss-tech/zbot/internal/prompts"
	"github.com/ziloss-tech/zbot/internal/scheduler"
	"github.com/ziloss-tech/zbot/internal/scraper"
	"github.com/ziloss-tech/zbot/internal/secrets"
	"github.com/ziloss-tech/zbot/internal/skills"
	skillEmail "github.com/ziloss-tech/zbot/internal/skills/email"
	skillGHL "github.com/ziloss-tech/zbot/internal/skills/ghl"
	skillGitHub "github.com/ziloss-tech/zbot/internal/skills/github"
	skillMemory "github.com/ziloss-tech/zbot/internal/skills/memory"
	skillSearch "github.com/ziloss-tech/zbot/internal/skills/search"
	skillSheets "github.com/ziloss-tech/zbot/internal/skills/sheets"
	"github.com/ziloss-tech/zbot/internal/skills/mcpbridge"
	"github.com/ziloss-tech/zbot/internal/factory"
	"github.com/ziloss-tech/zbot/internal/parallel"
	"github.com/ziloss-tech/zbot/internal/healthcheck"
	"github.com/ziloss-tech/zbot/internal/research"
	"github.com/ziloss-tech/zbot/internal/vault"
	"github.com/ziloss-tech/zbot/internal/security"
	"github.com/ziloss-tech/zbot/internal/tools"
	"github.com/ziloss-tech/zbot/internal/crawler"
	"github.com/ziloss-tech/zbot/internal/reviewer"
	"github.com/ziloss-tech/zbot/internal/router"
	"github.com/ziloss-tech/zbot/internal/webui"
	"github.com/ziloss-tech/zbot/internal/workflow"
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
- When asked to write files or code, prefer executing the tool call directly rather than describing what you would do
- If you need confirmation before a potentially risky action, ask concisely
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

	// ── Dotenv ──────────────────────────────────────────────────────────────
	// Load .env file if it exists. Doesn't override explicit env vars.
	if envData, err := os.ReadFile(".env"); err == nil {
		loaded := 0
		for _, line := range strings.Split(string(envData), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, val, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			val = strings.TrimSpace(val)
			// Remove surrounding quotes if present.
			if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
				val = val[1 : len(val)-1]
			}
			// Only set if not already set — don't override explicit env vars.
			if _, exists := os.LookupEnv(key); !exists {
				os.Setenv(key, val)
				loaded++
			}
		}
		if loaded > 0 {
			logger.Info("loaded .env file", "vars", loaded)
		}
	}

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
	deepInfraKey, _ := sm.Get(ctx, secrets.SecretDeepInfraAPIKey)
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
	// Uses the cheapest available model: DeepSeek V3.2 > Haiku > primary model.
	var cheapModelClient agent.LLMClient
	if deepInfraKey != "" {
		cheapModelClient = llm.NewDeepSeekCheapClient(deepInfraKey, logger)
	} else if useOpenAICompat {
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
	coreTools := []agent.Tool{}
	if searchTool != nil {
		coreTools = append(coreTools, searchTool)
	}
	coreTools = append(coreTools,
		tools.NewFetchURLToolFull(proxyPool, rateLimiter, scrapeCache, browserFetcher),
		tools.NewCredentialedFetchTool(sm, siteCredentials, proxyPool, rateLimiter, scrapeCache, logger),
		tools.NewManageCredentialsTool(sm, logger), // Sprint C stretch: credential CRUD
		tools.NewReadFileTool(workspaceRoot),
		tools.NewWriteFileTool(workspaceRoot),
		tools.NewCodeRunnerTool(workspaceRoot),
		// Sprint 12: save_memory + search_memory moved to memory skill (below).
		tools.NewAnalyzeImageTool(llmClient, workspaceRoot),
		tools.NewPDFExtractTool(workspaceRoot),
	)

	// ── Hawkeye: Visual Crawler ─────────────────────────────────────────────
	crawlerSessions := crawler.NewSessionManager(nil) // eventBus wired after creation below
	crawlerTool := tools.NewCrawlerTool(crawlerSessions)
	coreTools = append(coreTools, crawlerTool)
	logger.Info("tool registered: web_crawl (Hawkeye visual crawler)")

	// ── Benchmark Router ────────────────────────────────────────────────────
	modelRouter := router.NewRouter(router.Preferences{
		PreferAmerican: true,
	})
	logger.Info("model router initialized", "models", len(router.DefaultModels))

	// ── Skills System (Sprint 7) ────────────────────────────────────────────
	skillRegistry := skills.NewRegistry()

	// Memory skill — always registers (no secret required). Sprint 12.
	skillRegistry.Register(skillMemory.NewSkill(memStore))
	logger.Info("skill registered: memory")

	// Search skill.
	skillRegistry.Register(skillSearch.NewSkill())
	logger.Info("skill registered: search")

	// ── Vault (encrypted secrets store) ──────────────────────────────────────
	var zbotVault *vault.Vault
	vaultMasterKey := os.Getenv("ZBOT_VAULT_MASTER_KEY")
	if vaultMasterKey == "" {
		// Try GCP Secret Manager.
		vaultMasterKey, _ = sm.Get(ctx, "zbot-vault-master-key")
	}
	if vaultMasterKey == "" {
		// Generate a random key for first-time setup.
		newKey := make([]byte, 32)
		rand.Read(newKey)
		vaultMasterKey = hex.EncodeToString(newKey)
		logger.Warn("ZBOT_VAULT_MASTER_KEY not set — generated ephemeral key (secrets won't survive restart!)",
			"hint", "set ZBOT_VAULT_MASTER_KEY to a 64-char hex string for persistence")
	}
	vaultKeyBytes, vkErr := hex.DecodeString(vaultMasterKey)
	if vkErr != nil || len(vaultKeyBytes) != 32 {
		logger.Warn("vault master key invalid — must be 64 hex chars (32 bytes)", "err", vkErr)
	} else {
		if pgDB != nil {
			vaultStore, vsErr := vault.NewPostgresStore(pgDB.Config().ConnConfig.ConnString())
			if vsErr != nil {
				logger.Warn("vault postgres store failed", "err", vsErr)
			} else {
				zbotVault, _ = vault.New(vaultKeyBytes, vaultStore)
				skillRegistry.Register(vault.NewSkill(zbotVault, "default"))
				logger.Info("skill registered: vault (AES-256-GCM, Postgres)")
			}
		} else {
			logger.Warn("vault disabled — requires Postgres")
		}
	}


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


	// ── MCP Bridge (dynamic MCP server loading) ─────────────────────────────
	// Load external MCP servers as ZBOT skills. Config via:
	//   - ZBOT_MCP_SERVERS env var (JSON array)
	//   - workspace/mcp-servers.json config file
	mcpConfigPath := filepath.Join(workspaceRoot, "mcp-servers.json")
	mcpSkills, mcpErr := mcpbridge.LoadFromConfig(ctx, skillRegistry, mcpConfigPath, logger)
	if mcpErr != nil {
		logger.Warn("MCP bridge loading failed", "err", mcpErr)
	} else if len(mcpSkills) > 0 {
		logger.Info("MCP bridge loaded", "servers", len(mcpSkills))
		// Ensure MCP servers are shut down on exit.
		defer func() {
			for _, s := range mcpSkills {
				s.Close()
			}
		}()
	}


	// ── Parallel Coding Dispatcher (Qwen Coder via Ollama) ──────────────────
	ollamaURL := os.Getenv("ZBOT_OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434/v1"
	}
	ollamaModel := os.Getenv("ZBOT_CODER_MODEL")
	if ollamaModel == "" {
		ollamaModel = "qwen2.5-coder:32b"
	}
	// Only register if Ollama is reachable.
	ollamaClient := llm.NewOpenAICompatClient(ollamaURL, "ollama", ollamaModel, logger)
	parallelDispatcher := parallel.NewDispatcher(ollamaClient, 4, logger)
	skillRegistry.Register(parallel.NewSkill(parallelDispatcher))
	logger.Info("skill registered: parallel_code", "model", ollamaModel, "url", ollamaURL)

	// ── Software Factory (autonomous planning pipeline) ─────────────────────
	// Uses cheap model for interview + PRD, smart model (Sonnet) for
	// architecture + security + critic. Always available — no secrets required.
	factoryPipeline := factory.NewPipelineV2(cheapModelClient, llmClient, logger)
	factorySkill := factory.NewSkill(factoryPipeline)
	if pgDB != nil {
		factoryStore, fsErr := factory.NewPGSessionStore(ctx, pgDB)
		if fsErr != nil {
			logger.Warn("factory session store failed", "err", fsErr)
		} else {
			factorySkill.SetStore(factoryStore)
			if restoreErr := factorySkill.RestoreSessions(ctx); restoreErr != nil {
				logger.Warn("factory session restore failed", "err", restoreErr)
			} else {
				logger.Info("factory session persistence ready")
			}
		}
	}
	skillRegistry.Register(factorySkill)
	logger.Info("skill registered: factory")

	// Merge core tools + skill tools.
	allTools := append(coreTools, skillRegistry.AllTools()...)

	// ── Agent ───────────────────────────────────────────────────────────────
	agentCfg := agent.DefaultConfig()
	agentCfg.SystemPrompt = prompts.ClaudeExecutorSystem + skillRegistry.SystemPromptAddendum()

	// Event bus for Thalamus oversight + UI streaming.
	eventBus := agent.NewMemEventBus(200)

	// Wire crawler event bus (SessionManager was created before eventBus existed).
	crawlerSessions.SetEventBus(eventBus)

	// Wire the Router prompt from the prompts package.
	agent.SetRouterPrompt(prompts.RouterSystem)

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

	// Wire cheap LLM for Frontal Lobe (planning) + Thalamus (verification).
	// Priority: DeepSeek V3.2 via DeepInfra ($0.14/M) > Haiku ($0.25/M) > primary model > disabled.
	if deepInfraKey != "" {
		cheapLLM := llm.NewDeepSeekCheapClient(deepInfraKey, logger)
		ag.SetCheapLLM(cheapLLM)
		logger.Info("cognitive stages enabled (DeepSeek V3.2 via DeepInfra — $0.14/M)",
			"model", llm.DeepSeekV3Model)
	} else if anthropicKey != "" {
		cheapLLM := llm.NewHaikuClient(anthropicKey, logger)
		ag.SetCheapLLM(cheapLLM)
		logger.Info("cognitive stages enabled (Haiku fallback — $0.25/M)")
	} else if useOpenAICompat {
		ag.SetCheapLLM(llmClient) // open models are already cheap
		logger.Info("cognitive stages enabled (using primary model for planning/verification)")
	} else {
		logger.Warn("cognitive stages disabled — no cheap LLM available")
	}

	// Wire benchmark router for optimal model selection.
	ag.SetModelRouter(router.NewRouterAdapter(modelRouter))
	logger.Info("benchmark router wired", "models", len(router.DefaultModels))

	// ── Background Reviewer (multi-model quality checking) ────────────────
	reviewCfg := reviewer.DefaultReviewConfig()
	if openaiKey := os.Getenv("ZBOT_OPENAI_API_KEY"); openaiKey != "" {
		reviewCfg.Enabled = true
		reviewCfg.APIKey = openaiKey
	}
	reviewEngine := reviewer.NewReviewEngine(reviewCfg, eventBus, logger)
	if reviewCfg.Enabled {
		reviewEngine.Start()
		defer reviewEngine.Stop()
		logger.Info("background reviewer started", "model", reviewCfg.ModelName, "interval", reviewCfg.ReviewInterval)
	}

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
			// Uses the cheapest available: DeepSeek V3.2 > Haiku > primary model.
			var cheapClient agent.LLMClient
			if deepInfraKey != "" {
				cheapClient = llm.NewDeepSeekCheapClient(deepInfraKey, logger)
			} else if useOpenAICompat {
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

	// ── v2 Deep Research Pipeline (cheap model → Sonnet, no external deps) ─────
	var researchOrchV2 *research.V2ResearchOrchestrator
	if pgDB != nil && researchStore != nil {
		// Phase 1 uses cheapest available: DeepSeek V3.2 > Haiku.
		var phase1Client agent.LLMClient
		if deepInfraKey != "" {
			phase1Client = llm.NewDeepSeekCheapClient(deepInfraKey, logger)
		} else {
			phase1Client = llm.NewHaikuClient(anthropicKey, logger)
		}
		haikuClient := phase1Client // keep name for backward compat with constructor
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

	// Conversation history moved to MessageHandler (handler.go).

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
	// Logic extracted to handler.go — MessageHandler holds all deps.
	msgHandler := &MessageHandler{
		logger:        logger,
		ag:            ag,
		confirmStore:  confirmStore,
		cmdHandler:    cmdHandler,
		orch:          orch,
		researchOrch:  researchOrch,
		researchStore: researchStore,
		sched:         sched,
		memStore:      memStore,
		history:       make(map[string][]agent.Message),
	}
	handler := msgHandler.Handle

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

	// Vault REST API.
	if zbotVault != nil {
		vaultHandler := vault.NewHandler(zbotVault, "default")
		webServer.RegisterVaultHandler(vaultHandler)
		logger.Info("vault REST API mounted at /api/vault/")
	}

	webServer.SetEventBus(eventBus)
	webServer.StartMetricsCollector(ctx)
	webServer.SetLLMClient(llmClient)
	// Persistent conversation history via SQLite (survives restarts).
	historyDBPath := filepath.Join(workspaceRoot, ".cache", "history.db")
	chatHistory, histErr := memory.NewSQLiteHistory(historyDBPath)
	if histErr != nil {
		logger.Warn("SQLite history unavailable — falling back to in-memory", "err", histErr)
	} else {
		logger.Info("persistent chat history ready", "path", historyDBPath)
		defer chatHistory.Close()
	}
	const maxHistory = 40 // 20 user + 20 assistant messages

	webServer.SetQuickChat(func(ctx context.Context, message string) (string, error) {
		sessionID := "web-chat"

		// Load history from SQLite (falls back to empty if unavailable).
		var historyMsgs []agent.Message
		if chatHistory != nil {
			loaded, loadErr := chatHistory.LoadHistory(sessionID, maxHistory)
			if loadErr != nil {
				logger.Warn("failed to load chat history", "err", loadErr)
			} else {
				historyMsgs = loaded
			}
		}

		turnInput := agent.TurnInput{
			SessionID: sessionID,
			History:   historyMsgs,
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

		// Persist both messages to SQLite.
		if chatHistory != nil {
			_ = chatHistory.SaveMessage(sessionID, string(agent.RoleUser), message)
			_ = chatHistory.SaveMessage(sessionID, string(agent.RoleAssistant), result.Reply)
		}

		return result.Reply, nil
	})

	// Wire chat history clear endpoint.
	if chatHistory != nil {
		webServer.SetClearChatHistory(func() error {
			return chatHistory.ClearHistory("web-chat")
		})
	}

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


	// ── Health Check (produced by Qwen Coder via parallel dispatch) ──────────
	hc := healthcheck.NewHealthChecker()
	hc.Register("ollama", healthcheck.OllamaCheck("http://localhost:11434"))
	hc.Register("disk", healthcheck.DiskCheck(workspaceRoot, 10.0))
	if pgDB != nil {
		hc.Register("postgres", healthcheck.PostgresCheck(pgDB.Config().ConnConfig.ConnString()))
	}
	webServer.Mux().HandleFunc("/health", healthcheck.Handler(hc))
	logger.Info("healthcheck endpoint ready", "path", "/health")

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

// Utility functions (isWorkflowRequest, extractPDF, randomID, truncateStr)
// have been extracted to cmd/zbot/util.go.
