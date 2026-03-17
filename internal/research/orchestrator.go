package research

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/zbot-ai/zbot/internal/agent"
	"github.com/zbot-ai/zbot/internal/llm"
	"github.com/zbot-ai/zbot/internal/prompts"
)

const (
	MaxIterations  = 4
	PassThreshold  = 0.7 // Must match critic prompt: passed = confidence_score >= 0.7
)

// ResearchOrchestrator manages the full deep research lifecycle.
// Core principle: a model never researches AND verifies itself.
type ResearchOrchestrator struct {
	// Multi-model pipeline (different providers by design).
	planner     *llm.OpenRouterClient // Mistral Large 2 · Mistral AI
	searcher    agent.LLMClient       // Llama 4 Scout · Meta (uses tools)
	extractor   *llm.OpenRouterClient // Llama 3.1 405B · Meta
	critic      *llm.OpenAIClient     // GPT-4o · OpenAI (different provider than extractor)
	synthesizer agent.LLMClient       // Claude Sonnet 4.6 · Anthropic (best prose)

	// Shared services.
	searchTool   agent.Tool        // web_search tool for the searcher
	memStore     agent.MemoryStore
	claimMemory  *ClaimMemory      // cross-session claim timeline
	store        *PGResearchStore
	budget       *BudgetTracker
	logger       *slog.Logger

	// Active sessions: sessionID → emitter.
	emitters   map[string]*EventEmitter
	cancelFns  map[string]context.CancelFunc
	mu         sync.Mutex
}

// NewResearchOrchestrator creates the orchestrator with the full model pipeline.
func NewResearchOrchestrator(
	planner *llm.OpenRouterClient,
	searcher agent.LLMClient,
	extractor *llm.OpenRouterClient,
	critic *llm.OpenAIClient,
	synthesizer agent.LLMClient,
	searchTool agent.Tool,
	memStore agent.MemoryStore,
	claimMemory *ClaimMemory,
	store *PGResearchStore,
	budget *BudgetTracker,
	logger *slog.Logger,
) *ResearchOrchestrator {
	return &ResearchOrchestrator{
		planner:     planner,
		searcher:    searcher,
		extractor:   extractor,
		critic:      critic,
		synthesizer: synthesizer,
		searchTool:  searchTool,
		memStore:    memStore,
		claimMemory: claimMemory,
		store:       store,
		budget:      budget,
		logger:      logger,
		emitters:    make(map[string]*EventEmitter),
		cancelFns:   make(map[string]context.CancelFunc),
	}
}

// GetEmitter returns the event emitter for a session (for SSE streaming).
func (ro *ResearchOrchestrator) GetEmitter(sessionID string) *EventEmitter {
	if em, ok := ro.emitters[sessionID]; ok {
		return em
	}
	return nil
}

// CancelSession cancels a running research session. Returns true if found and cancelled.
func (ro *ResearchOrchestrator) CancelSession(sessionID string) bool {
	ro.mu.Lock()
	defer ro.mu.Unlock()
	if cancel, ok := ro.cancelFns[sessionID]; ok {
		cancel()
		return true
	}
	return false
}

// IsRunning returns true if a session is currently active.
func (ro *ResearchOrchestrator) IsRunning(sessionID string) bool {
	ro.mu.Lock()
	defer ro.mu.Unlock()
	_, ok := ro.cancelFns[sessionID]
	return ok
}

// RunDeepResearch is the main research loop. Runs in a background goroutine.
// Returns the completed state, or error if the pipeline fails or is cancelled.
func (ro *ResearchOrchestrator) RunDeepResearch(ctx context.Context, goal, sessionID string) (*ResearchState, error) {
	// Create a cancelable context so CancelSession() can stop this run.
	ctx, cancel := context.WithCancel(ctx)
	ro.mu.Lock()
	ro.cancelFns[sessionID] = cancel
	ro.mu.Unlock()

	// Create emitter for SSE streaming.
	emitter := NewEventEmitter(100)
	ro.emitters[sessionID] = emitter
	defer func() {
		cancel()
		emitter.Close()
		ro.mu.Lock()
		delete(ro.emitters, sessionID)
		delete(ro.cancelFns, sessionID)
		ro.mu.Unlock()
	}()

	// Check budget before starting.
	if ro.budget != nil {
		if err := ro.budget.CheckBudget(ctx, 0.25); err != nil {
			return nil, err
		}
	}

	state := &ResearchState{
		WorkflowID: sessionID,
		Goal:       goal,
		MaxIter:    MaxIterations,
		Iteration:  0,
		StartedAt:  time.Now(),
	}

	ro.logger.Info("deep research started",
		"session_id", sessionID,
		"goal", goal,
	)

	// ── ITERATIVE RESEARCH LOOP ──────────────────────────────────────────
	for state.Iteration < MaxIterations {
		state.Iteration++

		// ─── Step 1: Plan (or re-plan with gaps) ─────────────────────────
		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "planning",
			Iteration: state.Iteration,
			Model:     ro.planner.DisplayName(),
			ModelID:   ro.planner.ModelName(),
			Message:   "Decomposing research goal into sub-questions and search terms...",
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		plan, planCost, err := ro.runPlanner(ctx, goal, state.Critique.NewSubQuestions)
		if err != nil {
			return nil, fmt.Errorf("planner failed (iter %d): %w", state.Iteration, err)
		}
		state.Plan = plan
		state.CostUSD += planCost
		ro.recordCost(ctx, sessionID, ro.planner.ModelName(), planCost)

		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "planning",
			Iteration: state.Iteration,
			Model:     ro.planner.DisplayName(),
			ModelID:   ro.planner.ModelName(),
			Message:   fmt.Sprintf("%d sub-questions, %d search terms. Depth: %s", len(plan.SubQuestions), len(plan.SearchTerms), plan.Depth),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		// ─── Step 2: Search ──────────────────────────────────────────────
		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "searching",
			Iteration: state.Iteration,
			Model:     displayNameForClient(ro.searcher),
			ModelID:   ro.searcher.ModelName(),
			Message:   fmt.Sprintf("Searching %d queries...", len(plan.SearchTerms)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		sources, searchCost, err := ro.runSearcher(ctx, plan.SearchTerms, state.Sources)
		if err != nil {
			ro.logger.Warn("searcher failed, continuing with existing sources",
				"err", err, "existing_sources", len(state.Sources))
		} else {
			state.Sources = mergeSources(state.Sources, sources)
			state.CostUSD += searchCost
			ro.recordCost(ctx, sessionID, ro.searcher.ModelName(), searchCost)
		}

		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "searching",
			Iteration: state.Iteration,
			Model:     displayNameForClient(ro.searcher),
			ModelID:   ro.searcher.ModelName(),
			Message:   fmt.Sprintf("Found %d sources total", len(state.Sources)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		if len(state.Sources) == 0 {
			return nil, fmt.Errorf("no sources found for research goal: %s", goal)
		}

		// ─── Step 3: Extract ─────────────────────────────────────────────
		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "extracting",
			Iteration: state.Iteration,
			Model:     ro.extractor.DisplayName(),
			ModelID:   ro.extractor.ModelName(),
			Message:   fmt.Sprintf("Extracting claims from %d sources...", len(state.Sources)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		claimSet, extractCost, err := ro.runExtractor(ctx, state.Sources, goal)
		if err != nil {
			return nil, fmt.Errorf("extractor failed (iter %d): %w", state.Iteration, err)
		}
		state.Claims = mergeClaims(state.Claims, claimSet.Claims)
		state.CostUSD += extractCost
		ro.recordCost(ctx, sessionID, ro.extractor.ModelName(), extractCost)

		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "extracting",
			Iteration: state.Iteration,
			Model:     ro.extractor.DisplayName(),
			ModelID:   ro.extractor.ModelName(),
			Message:   fmt.Sprintf("Extracted %d claims. Gaps: %s", len(claimSet.Claims), summarizeGaps(claimSet.Gaps)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		// ─── Step 4: Critique ────────────────────────────────────────────
		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "critiquing",
			Iteration: state.Iteration,
			Model:     "GPT-4o · OpenAI",
			ModelID:   ro.critic.ModelName(),
			Message:   fmt.Sprintf("Reviewing %d claims against %d sources...", len(state.Claims), len(state.Sources)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		critique, criticCost, err := ro.runCritic(ctx, state.Claims, state.Sources, goal)
		if err != nil {
			return nil, fmt.Errorf("critic failed (iter %d): %w", state.Iteration, err)
		}
		state.Critique = critique
		state.CostUSD += criticCost
		ro.recordCost(ctx, sessionID, ro.critic.ModelName(), criticCost)

		emitter.Emit(ResearchEvent{
			SessionID:  sessionID,
			Stage:      "evaluated",
			Iteration:  state.Iteration,
			Model:      "GPT-4o · OpenAI",
			ModelID:    ro.critic.ModelName(),
			Message:    fmt.Sprintf("Confidence: %.2f. %s", critique.ConfidenceScore, critiqueVerdict(critique)),
			Confidence: critique.ConfidenceScore,
			Passed:     critique.Passed,
			Sources:    len(state.Sources),
			Claims:     len(state.Claims),
			CostUSD:    state.CostUSD,
		})

		// Update DB.
		if ro.store != nil {
			_ = ro.store.UpdateSession(ctx, sessionID, state)
		}

		// ─── Loop controller decision ────────────────────────────────────
		// Use critique.Passed as authoritative signal — GPT-4o sets this using
		// the same 0.7 threshold that's in the critic prompt.
		// Fall back to raw score in case GPT-4o omits the field.
		if critique.Passed || critique.ConfidenceScore >= PassThreshold || len(critique.NewSubQuestions) == 0 {
			ro.logger.Info("research confidence threshold met",
				"session_id", sessionID,
				"confidence", critique.ConfidenceScore,
				"iteration", state.Iteration,
			)
			break
		}

		ro.logger.Info("research looping for more data",
			"session_id", sessionID,
			"confidence", critique.ConfidenceScore,
			"new_questions", len(critique.NewSubQuestions),
			"iteration", state.Iteration,
		)
	}

	// ─── Step 5: Synthesize ──────────────────────────────────────────────
	emitter.Emit(ResearchEvent{
		SessionID: sessionID,
		Stage:     "synthesizing",
		Model:     "Claude Sonnet 4.6 · Anthropic",
		ModelID:   ro.synthesizer.ModelName(),
		Message:   fmt.Sprintf("Writing final report from %d verified claims...", len(state.Claims)),
		Sources:   len(state.Sources),
		Claims:    len(state.Claims),
		CostUSD:   state.CostUSD,
	})

	report, synthCost, err := ro.runSynthesizer(ctx, state.Claims, state.Sources, state.Critique, goal)
	if err != nil {
		return nil, fmt.Errorf("synthesizer failed: %w", err)
	}
	state.FinalReport = report
	state.CostUSD += synthCost
	state.Complete = true
	state.FinishedAt = time.Now()
	ro.recordCost(ctx, sessionID, ro.synthesizer.ModelName(), synthCost)

	// ─── Step 6: Auto-save to memory ─────────────────────────────────────
	ro.saveToMemory(ctx, goal, state)

	// ─── Emit completion ─────────────────────────────────────────────────
	emitter.Emit(ResearchEvent{
		SessionID: sessionID,
		Stage:     "complete",
		Model:     "Pipeline",
		Message:   fmt.Sprintf("Research complete — %d iterations, %d claims, %d sources", state.Iteration, len(state.Claims), len(state.Sources)),
		Report:    report,
		Sources:   len(state.Sources),
		Claims:    len(state.Claims),
		CostUSD:   state.CostUSD,
	})

	ro.logger.Info("deep research complete",
		"session_id", sessionID,
		"iterations", state.Iteration,
		"claims", len(state.Claims),
		"sources", len(state.Sources),
		"cost", fmt.Sprintf("$%.4f", state.CostUSD),
		"duration", time.Since(state.StartedAt).Round(time.Second),
	)

	return state, nil
}

// ─── AGENT METHODS ──────────────────────────────────────────────────────────

func (ro *ResearchOrchestrator) runPlanner(ctx context.Context, goal string, followUpQuestions []string) (ResearchPlan, float64, error) {
	userMsg := fmt.Sprintf("Research goal: %s", goal)

	// Inject prior knowledge from past sessions (Sprint 16).
	if ro.claimMemory != nil && len(followUpQuestions) == 0 {
		prior, err := ro.claimMemory.SearchPriorKnowledge(ctx, goal)
		if err != nil {
			ro.logger.Warn("claimmemory search failed, continuing without prior knowledge", "err", err)
		} else if prior != "" {
			userMsg = userMsg + "\n\n" + prior
			ro.logger.Debug("prior knowledge injected into planner", "chars", len(prior))
		}
	}

	if len(followUpQuestions) > 0 {
		userMsg += "\n\nPrevious critique identified these gaps — focus on filling them:\n"
		for _, q := range followUpQuestions {
			userMsg += "- " + q + "\n"
		}
	}

	content, err := ro.planner.Chat(ctx, prompts.ResearchPlannerSystem, userMsg)
	if err != nil {
		return ResearchPlan{}, 0, err
	}

	plan, parseErr := parseJSON[ResearchPlan](content)
	if parseErr != nil {
		// Retry once with error feedback.
		retryMsg := userMsg + fmt.Sprintf("\n\nYour previous output failed JSON validation: %v. Output ONLY valid JSON.", parseErr)
		content, err = ro.planner.Chat(ctx, prompts.ResearchPlannerSystem, retryMsg)
		if err != nil {
			return ResearchPlan{}, 0, err
		}
		plan, parseErr = parseJSON[ResearchPlan](content)
		if parseErr != nil {
			return ResearchPlan{}, 0, fmt.Errorf("planner JSON parse failed twice: %w", parseErr)
		}
	}

	// Estimate cost (~2000 input + 500 output tokens for planning).
	cost := llm.EstimateCost(ro.planner.ModelName(), 2000, 500)
	return plan, cost, nil
}

func (ro *ResearchOrchestrator) runSearcher(ctx context.Context, searchTerms []string, existingSources []Source) ([]Source, float64, error) {
	// Build the search query by calling web_search for each term.
	var allSources []Source
	nextID := len(existingSources) + 1

	for _, term := range searchTerms {
		result, err := ro.searchTool.Execute(ctx, map[string]any{"query": term})
		if err != nil {
			ro.logger.Warn("search failed for term", "term", term, "err", err)
			continue
		}

		// Parse the search results and create Source entries.
		lines := strings.Split(result.Content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Extract URL and title from search result line.
			url, title, snippet := parseSearchResult(line)
			if url == "" {
				continue
			}

			src := Source{
				ID:      fmt.Sprintf("SRC_%03d", nextID),
				URL:     url,
				Title:   title,
				Snippet: snippet,
			}
			allSources = append(allSources, src)
			nextID++
		}
	}

	// Estimate cost (~100 tokens per search term via tool).
	cost := llm.EstimateCost(ro.searcher.ModelName(), len(searchTerms)*100, len(searchTerms)*50)
	return allSources, cost, nil
}

func (ro *ResearchOrchestrator) runExtractor(ctx context.Context, sources []Source, goal string) (ClaimSet, float64, error) {
	const batchSize = 5 // Mistral Small 3.1 max output is 8192 tokens; 5 sources fits safely with dense snippets.

	var allClaims []Claim
	var allGaps []string
	var allSourceIDs []string
	var totalCost float64
	claimOffset := 0 // keep CLM IDs unique across batches

	for i := 0; i < len(sources); i += batchSize {
		end := i + batchSize
		if end > len(sources) {
			end = len(sources)
		}
		batch := sources[i:end]

		var sourceContext strings.Builder
		sourceContext.WriteString("Research goal: " + goal + "\n\n")
		sourceContext.WriteString(fmt.Sprintf("Sources to extract claims from (batch %d/%d):\n\n",
			i/batchSize+1, (len(sources)+batchSize-1)/batchSize))
		sourceContext.WriteString(fmt.Sprintf("Continue claim IDs from CLM_%03d onward.\n\n", claimOffset+1))
		for _, s := range batch {
			sourceContext.WriteString(fmt.Sprintf("[%s] %s\nURL: %s\n%s\n\n", s.ID, s.Title, s.URL, s.Snippet))
		}

		content, err := ro.extractor.Chat(ctx, prompts.ResearchExtractorSystem, sourceContext.String())
		if err != nil {
			ro.logger.Warn("extractor batch failed", "batch_start", i, "err", err)
			continue
		}

		claimSet, parseErr := parseJSON[ClaimSet](content)
		if parseErr != nil {
			retryMsg := sourceContext.String() + fmt.Sprintf("\n\nYour previous output failed JSON validation: %v. Output ONLY valid JSON.", parseErr)
			content, err = ro.extractor.Chat(ctx, prompts.ResearchExtractorSystem, retryMsg)
			if err != nil {
				ro.logger.Warn("extractor batch retry failed", "batch_start", i, "err", err)
				continue
			}
			claimSet, parseErr = parseJSON[ClaimSet](content)
			if parseErr != nil {
				ro.logger.Warn("extractor batch parse failed twice, skipping", "batch_start", i, "err", parseErr)
				continue
			}
		}

		allClaims = append(allClaims, claimSet.Claims...)
		allGaps = append(allGaps, claimSet.Gaps...)
		allSourceIDs = append(allSourceIDs, claimSet.SourceIDs...)
		claimOffset += len(claimSet.Claims)

		inputTokens := len(sourceContext.String()) / 4
		totalCost += llm.EstimateCost(ro.extractor.ModelName(), inputTokens, 2000)
	}

	if len(allClaims) == 0 {
		return ClaimSet{}, totalCost, fmt.Errorf("extractor produced no claims from %d sources", len(sources))
	}

	return ClaimSet{
		Claims:    allClaims,
		Gaps:      allGaps,
		SourceIDs: allSourceIDs,
	}, totalCost, nil
}

func (ro *ResearchOrchestrator) runCritic(ctx context.Context, claims []Claim, sources []Source, goal string) (CritiqueReport, float64, error) {
	// Build review context for the critic.
	var reviewContext strings.Builder
	reviewContext.WriteString("Research goal: " + goal + "\n\n")

	reviewContext.WriteString("Claims to review:\n")
	for _, c := range claims {
		reviewContext.WriteString(fmt.Sprintf("[%s] (confidence: %.2f) %s — evidence: %v\n",
			c.ID, c.Confidence, c.Statement, c.EvidenceIDs))
	}

	reviewContext.WriteString("\nSources referenced:\n")
	for _, s := range sources {
		reviewContext.WriteString(fmt.Sprintf("[%s] %s — %s\n", s.ID, s.Title, s.URL))
	}

	content, err := ro.critic.Chat(ctx, prompts.ResearchCriticSystem, reviewContext.String())
	if err != nil {
		return CritiqueReport{}, 0, err
	}

	critique, parseErr := parseJSON[CritiqueReport](content)
	if parseErr != nil {
		retryMsg := reviewContext.String() + fmt.Sprintf("\n\nYour previous output failed JSON validation: %v. Output ONLY valid JSON.", parseErr)
		content, err = ro.critic.Chat(ctx, prompts.ResearchCriticSystem, retryMsg)
		if err != nil {
			return CritiqueReport{}, 0, err
		}
		critique, parseErr = parseJSON[CritiqueReport](content)
		if parseErr != nil {
			return CritiqueReport{}, 0, fmt.Errorf("critic JSON parse failed twice: %w", parseErr)
		}
	}

	inputTokens := len(reviewContext.String()) / 4
	cost := llm.EstimateCost(ro.critic.ModelName(), inputTokens, 1000)
	return critique, cost, nil
}

func (ro *ResearchOrchestrator) runSynthesizer(ctx context.Context, claims []Claim, sources []Source, critique CritiqueReport, goal string) (string, float64, error) {
	// Build synthesis context.
	var synthContext strings.Builder
	synthContext.WriteString("Research goal: " + goal + "\n\n")

	synthContext.WriteString("Verified claims:\n")
	for _, c := range claims {
		synthContext.WriteString(fmt.Sprintf("[%s] %s (evidence: %v, confidence: %.2f)\n",
			c.ID, c.Statement, c.EvidenceIDs, c.Confidence))
	}

	synthContext.WriteString("\nSources (map SRC_XXX → [N] in your report):\n")
	for i, s := range sources {
		synthContext.WriteString(fmt.Sprintf("[%d] %s — %s (ID: %s)\n", i+1, s.Title, s.URL, s.ID))
	}

	if len(critique.Gaps) > 0 {
		synthContext.WriteString("\nLimitations flagged by critic:\n")
		for _, g := range critique.Gaps {
			synthContext.WriteString("- " + g + "\n")
		}
	}

	// Use Claude (via agent.LLMClient interface) for synthesis.
	messages := []agent.Message{
		{Role: agent.RoleSystem, Content: prompts.ResearchSynthesizerSystem},
		{Role: agent.RoleUser, Content: synthContext.String()},
	}

	result, err := ro.synthesizer.Complete(ctx, messages, nil)
	if err != nil {
		return "", 0, fmt.Errorf("synthesizer: %w", err)
	}

	cost := llm.EstimateCost(ro.synthesizer.ModelName(), result.InputTokens, result.OutputTokens)
	return result.Content, cost, nil
}

// ─── HELPERS ────────────────────────────────────────────────────────────────

func (ro *ResearchOrchestrator) saveToMemory(ctx context.Context, goal string, state *ResearchState) {
	// Save summary fact to general memory store.
	if ro.memStore != nil {
		summary := fmt.Sprintf("Deep research on '%s': %d sources, %d claims, confidence %.2f. %d iterations.",
			goal, len(state.Sources), len(state.Claims), state.Critique.ConfidenceScore, state.Iteration)

		fact := agent.Fact{
			ID:        "research_" + state.WorkflowID,
			Content:   summary,
			Source:    "deep_research",
			Tags:      []string{"research", "report"},
			CreatedAt: time.Now(),
		}

		if err := ro.memStore.Save(ctx, fact); err != nil {
			ro.logger.Warn("failed to save research summary to memory", "err", err)
		}
	}

	// Save individual timestamped claims to ClaimMemory (Sprint 16).
	if ro.claimMemory != nil {
		if err := ro.claimMemory.SaveClaims(ctx, state.WorkflowID, goal, state.Claims); err != nil {
			ro.logger.Warn("failed to save claims to claimmemory", "err", err)
		} else {
			ro.logger.Info("claims saved to claimmemory",
				"session_id", state.WorkflowID,
				"claim_count", len(state.Claims),
			)
		}
	}
}

func (ro *ResearchOrchestrator) recordCost(ctx context.Context, sessionID, modelID string, cost float64) {
	if ro.budget != nil {
		ro.budget.RecordSpend(ctx, sessionID, modelID, 0, 0, cost)
	}
}

// parseJSON strips markdown fences and parses JSON into the target type.
func parseJSON[T any](content string) (T, error) {
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	// Find JSON object boundaries (in case model wraps in prose).
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		content = content[start : end+1]
	}

	var result T
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		maxLen := 200
		if len(content) < maxLen {
			maxLen = len(content)
		}
		return result, fmt.Errorf("JSON parse failed: %w\ncontent: %s", err, content[:maxLen])
	}
	return result, nil
}

// parseSearchResult extracts URL, title, and snippet from a search result line.
// The web_search tool returns markdown in this format:
//
//	### 1. Page Title
//	**URL:** https://example.com
//	Description snippet text
//
// We detect URL lines (starting with **URL:**) and use the preceding title line.
func parseSearchResult(line string) (url, title, snippet string) {
	// Handle **URL:** prefix from web_search tool output.
	if strings.HasPrefix(line, "**URL:**") {
		rawURL := strings.TrimSpace(strings.TrimPrefix(line, "**URL:**"))
		// Strip any trailing markdown.
		if end := strings.IndexAny(rawURL, " \t"); end >= 0 {
			rawURL = rawURL[:end]
		}
		if strings.HasPrefix(rawURL, "http") {
			return rawURL, rawURL, rawURL
		}
	}

	// Handle bare URL in line.
	if idx := strings.Index(line, "http"); idx >= 0 {
		rest := line[idx:]
		end := strings.IndexAny(rest, " \t\n")
		if end == -1 {
			url = rest
		} else {
			url = rest[:end]
		}
		title = strings.TrimSpace(line[:idx])
		title = strings.TrimRight(title, "—-: ")
		title = strings.TrimLeft(title, "*# ")
		if title == "" {
			title = url
		}
		snippet = strings.TrimSpace(line)
		return
	}

	return "", "", ""
}

func displayNameForClient(c agent.LLMClient) string {
	if or, ok := c.(*llm.OpenRouterClient); ok {
		return or.DisplayName()
	}
	modelID := c.ModelName()
	if name, ok := llm.ModelDisplayNames[modelID]; ok {
		return name
	}
	return modelID
}

func summarizeGaps(gaps []string) string {
	if len(gaps) == 0 {
		return "none"
	}
	if len(gaps) <= 2 {
		return strings.Join(gaps, ", ")
	}
	return fmt.Sprintf("%s (+%d more)", gaps[0], len(gaps)-1)
}

func critiqueVerdict(c CritiqueReport) string {
	if c.Passed {
		return "PASSED — proceeding to synthesis."
	}
	if len(c.NewSubQuestions) > 0 {
		return fmt.Sprintf("FAILED — %d gaps found, looping...", len(c.NewSubQuestions))
	}
	return "FAILED — needs more research."
}
