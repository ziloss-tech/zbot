package research

// orchestrator_v2.go — Two-phase deep research pipeline.
//
// v2 Architecture:
//   Phase 1 (Haiku): Plan → Search → Extract (cheap, parallel-safe)
//   Phase 2 (Sonnet): Critique → Synthesize (quality, prose)
//
// Replaces the 5-model pipeline (Mistral/Llama/GPT-4o/Claude) with
// just two Claude tiers. Same iterative loop, same schemas, ~60% cheaper.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
	"github.com/jeremylerwick-max/zbot/internal/prompts"
)

// V2ResearchOrchestrator is the two-phase deep research pipeline.
// Phase 1 (Haiku): planning, searching, extracting — bulk work, cheap.
// Phase 2 (Sonnet): critiquing, synthesizing — quality work.
type V2ResearchOrchestrator struct {
	haiku   agent.LLMClient // Phase 1: plan, search, extract
	sonnet  agent.LLMClient // Phase 2: critique, synthesize

	searchTool  agent.Tool
	memStore    agent.MemoryStore
	claimMemory *ClaimMemory
	store       *PGResearchStore
	budget      *BudgetTracker
	logger      *slog.Logger

	*researchBase // shared emitter/cancel management
}

// researchBase holds the shared session management extracted from the v1 orchestrator.
type researchBase = ResearchOrchestrator

// NewV2ResearchOrchestrator creates the two-phase pipeline.
// haiku: Claude Haiku for cheap bulk work (planning, searching, extraction).
// sonnet: Claude Sonnet for quality work (critique, synthesis).
func NewV2ResearchOrchestrator(
	haiku agent.LLMClient,
	sonnet agent.LLMClient,
	searchTool agent.Tool,
	memStore agent.MemoryStore,
	claimMemory *ClaimMemory,
	store *PGResearchStore,
	budget *BudgetTracker,
	logger *slog.Logger,
) *V2ResearchOrchestrator {
	// Build a base orchestrator for session management (emitters, cancel).
	// We pass nil for the v1-specific clients since we won't use them.
	base := &ResearchOrchestrator{
		searchTool:  searchTool,
		memStore:    memStore,
		claimMemory: claimMemory,
		store:       store,
		budget:      budget,
		logger:      logger,
		emitters:    make(map[string]*EventEmitter),
		cancelFns:   make(map[string]context.CancelFunc),
	}

	return &V2ResearchOrchestrator{
		haiku:        haiku,
		sonnet:       sonnet,
		searchTool:   searchTool,
		memStore:     memStore,
		claimMemory:  claimMemory,
		store:        store,
		budget:       budget,
		logger:       logger,
		researchBase: base,
	}
}

// RunDeepResearch executes the two-phase research loop.
func (v2 *V2ResearchOrchestrator) RunDeepResearch(ctx context.Context, goal, sessionID string) (*ResearchState, error) {
	ctx, cancel := context.WithCancel(ctx)
	v2.mu.Lock()
	v2.cancelFns[sessionID] = cancel
	v2.mu.Unlock()

	emitter := NewEventEmitter(100)
	v2.emitters[sessionID] = emitter
	defer func() {
		cancel()
		emitter.Close()
		v2.mu.Lock()
		delete(v2.emitters, sessionID)
		delete(v2.cancelFns, sessionID)
		v2.mu.Unlock()
	}()

	if v2.budget != nil {
		if err := v2.budget.CheckBudget(ctx, 0.10); err != nil {
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

	v2.logger.Info("v2 deep research started",
		"session_id", sessionID,
		"goal", goal,
		"phase1_model", v2.haiku.ModelName(),
		"phase2_model", v2.sonnet.ModelName(),
	)

	// ── ITERATIVE RESEARCH LOOP ──────────────────────────────────────────
	for state.Iteration < MaxIterations {
		state.Iteration++

		// ─── Phase 1: Haiku — Plan ──────────────────────────────────────
		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "planning",
			Iteration: state.Iteration,
			Model:     "Claude Haiku · Phase 1",
			ModelID:   v2.haiku.ModelName(),
			Message:   "Decomposing research goal into sub-questions and search terms...",
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		plan, planCost, err := v2.runHaikuPlanner(ctx, goal, state.Critique.NewSubQuestions)
		if err != nil {
			return nil, fmt.Errorf("v2 planner failed (iter %d): %w", state.Iteration, err)
		}
		state.Plan = plan
		state.CostUSD += planCost
		v2.recordCost(ctx, sessionID, v2.haiku.ModelName(), planCost)

		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "planning",
			Iteration: state.Iteration,
			Model:     "Claude Haiku · Phase 1",
			ModelID:   v2.haiku.ModelName(),
			Message:   fmt.Sprintf("%d sub-questions, %d search terms. Depth: %s", len(plan.SubQuestions), len(plan.SearchTerms), plan.Depth),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		// ─── Phase 1: Haiku — Search ────────────────────────────────────
		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "searching",
			Iteration: state.Iteration,
			Model:     "Claude Haiku · Phase 1",
			ModelID:   v2.haiku.ModelName(),
			Message:   fmt.Sprintf("Searching %d queries...", len(plan.SearchTerms)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		// Search uses the tool-based approach — no LLM needed.
		sources, searchCost, err := v2.runSearch(ctx, plan.SearchTerms, state.Sources)
		if err != nil {
			v2.logger.Warn("v2 searcher failed, continuing with existing sources",
				"err", err, "existing_sources", len(state.Sources))
		} else {
			state.Sources = mergeSources(state.Sources, sources)
			state.CostUSD += searchCost
			v2.recordCost(ctx, sessionID, v2.haiku.ModelName(), searchCost)
		}

		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "searching",
			Iteration: state.Iteration,
			Model:     "Claude Haiku · Phase 1",
			ModelID:   v2.haiku.ModelName(),
			Message:   fmt.Sprintf("Found %d sources total", len(state.Sources)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		if len(state.Sources) == 0 {
			return nil, fmt.Errorf("no sources found for research goal: %s", goal)
		}

		// ─── Phase 1: Haiku — Extract ───────────────────────────────────
		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "extracting",
			Iteration: state.Iteration,
			Model:     "Claude Haiku · Phase 1",
			ModelID:   v2.haiku.ModelName(),
			Message:   fmt.Sprintf("Extracting claims from %d sources...", len(state.Sources)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		claimSet, extractCost, err := v2.runHaikuExtractor(ctx, state.Sources, goal)
		if err != nil {
			return nil, fmt.Errorf("v2 extractor failed (iter %d): %w", state.Iteration, err)
		}
		state.Claims = mergeClaims(state.Claims, claimSet.Claims)
		state.CostUSD += extractCost
		v2.recordCost(ctx, sessionID, v2.haiku.ModelName(), extractCost)

		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "extracting",
			Iteration: state.Iteration,
			Model:     "Claude Haiku · Phase 1",
			ModelID:   v2.haiku.ModelName(),
			Message:   fmt.Sprintf("Extracted %d claims. Gaps: %s", len(claimSet.Claims), summarizeGaps(claimSet.Gaps)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		// ─── Phase 2: Sonnet — Critique ─────────────────────────────────
		emitter.Emit(ResearchEvent{
			SessionID: sessionID,
			Stage:     "critiquing",
			Iteration: state.Iteration,
			Model:     "Claude Sonnet · Phase 2",
			ModelID:   v2.sonnet.ModelName(),
			Message:   fmt.Sprintf("Reviewing %d claims against %d sources...", len(state.Claims), len(state.Sources)),
			Sources:   len(state.Sources),
			Claims:    len(state.Claims),
			CostUSD:   state.CostUSD,
		})

		critique, criticCost, err := v2.runSonnetCritic(ctx, state.Claims, state.Sources, goal)
		if err != nil {
			return nil, fmt.Errorf("v2 critic failed (iter %d): %w", state.Iteration, err)
		}
		state.Critique = critique
		state.CostUSD += criticCost
		v2.recordCost(ctx, sessionID, v2.sonnet.ModelName(), criticCost)

		emitter.Emit(ResearchEvent{
			SessionID:  sessionID,
			Stage:      "evaluated",
			Iteration:  state.Iteration,
			Model:      "Claude Sonnet · Phase 2",
			ModelID:    v2.sonnet.ModelName(),
			Message:    fmt.Sprintf("Confidence: %.2f. %s", critique.ConfidenceScore, critiqueVerdict(critique)),
			Confidence: critique.ConfidenceScore,
			Passed:     critique.Passed,
			Sources:    len(state.Sources),
			Claims:     len(state.Claims),
			CostUSD:    state.CostUSD,
		})

		if v2.store != nil {
			_ = v2.store.UpdateSession(ctx, sessionID, state)
		}

		if critique.Passed || critique.ConfidenceScore >= PassThreshold || len(critique.NewSubQuestions) == 0 {
			v2.logger.Info("v2 research confidence threshold met",
				"session_id", sessionID,
				"confidence", critique.ConfidenceScore,
				"iteration", state.Iteration,
			)
			break
		}

		v2.logger.Info("v2 research looping for more data",
			"session_id", sessionID,
			"confidence", critique.ConfidenceScore,
			"new_questions", len(critique.NewSubQuestions),
			"iteration", state.Iteration,
		)
	}

	// ─── Phase 2: Sonnet — Synthesize ───────────────────────────────────
	emitter.Emit(ResearchEvent{
		SessionID: sessionID,
		Stage:     "synthesizing",
		Model:     "Claude Sonnet · Phase 2",
		ModelID:   v2.sonnet.ModelName(),
		Message:   fmt.Sprintf("Writing final report from %d verified claims...", len(state.Claims)),
		Sources:   len(state.Sources),
		Claims:    len(state.Claims),
		CostUSD:   state.CostUSD,
	})

	report, synthCost, err := v2.runSonnetSynthesizer(ctx, state.Claims, state.Sources, state.Critique, goal)
	if err != nil {
		return nil, fmt.Errorf("v2 synthesizer failed: %w", err)
	}
	state.FinalReport = report
	state.CostUSD += synthCost
	state.Complete = true
	state.FinishedAt = time.Now()
	v2.recordCost(ctx, sessionID, v2.sonnet.ModelName(), synthCost)

	// Auto-save to memory.
	v2.researchBase.saveToMemory(ctx, goal, state)

	emitter.Emit(ResearchEvent{
		SessionID: sessionID,
		Stage:     "complete",
		Model:     "v2 Pipeline (Haiku → Sonnet)",
		Message:   fmt.Sprintf("Research complete — %d iterations, %d claims, %d sources", state.Iteration, len(state.Claims), len(state.Sources)),
		Report:    report,
		Sources:   len(state.Sources),
		Claims:    len(state.Claims),
		CostUSD:   state.CostUSD,
	})

	v2.logger.Info("v2 deep research complete",
		"session_id", sessionID,
		"iterations", state.Iteration,
		"claims", len(state.Claims),
		"sources", len(state.Sources),
		"cost", fmt.Sprintf("$%.4f", state.CostUSD),
		"duration", time.Since(state.StartedAt).Round(time.Second),
	)

	return state, nil
}

// ─── PHASE 1: HAIKU METHODS ─────────────────────────────────────────────────

func (v2 *V2ResearchOrchestrator) runHaikuPlanner(ctx context.Context, goal string, followUpQuestions []string) (ResearchPlan, float64, error) {
	userMsg := fmt.Sprintf("Research goal: %s", goal)

	if v2.claimMemory != nil && len(followUpQuestions) == 0 {
		prior, err := v2.claimMemory.SearchPriorKnowledge(ctx, goal)
		if err != nil {
			v2.logger.Warn("claimmemory search failed", "err", err)
		} else if prior != "" {
			userMsg = userMsg + "\n\n" + prior
		}
	}

	if len(followUpQuestions) > 0 {
		userMsg += "\n\nPrevious critique identified these gaps — focus on filling them:\n"
		for _, q := range followUpQuestions {
			userMsg += "- " + q + "\n"
		}
	}

	messages := []agent.Message{
		{Role: agent.RoleSystem, Content: prompts.ResearchPlannerSystem},
		{Role: agent.RoleUser, Content: userMsg},
	}

	result, err := v2.haiku.Complete(ctx, messages, nil)
	if err != nil {
		return ResearchPlan{}, 0, err
	}

	plan, parseErr := parseJSON[ResearchPlan](result.Content)
	if parseErr != nil {
		// Retry with error feedback.
		messages = append(messages,
			agent.Message{Role: agent.RoleAssistant, Content: result.Content},
			agent.Message{Role: agent.RoleUser, Content: fmt.Sprintf("JSON validation failed: %v. Output ONLY valid JSON.", parseErr)},
		)
		result, err = v2.haiku.Complete(ctx, messages, nil)
		if err != nil {
			return ResearchPlan{}, 0, err
		}
		plan, parseErr = parseJSON[ResearchPlan](result.Content)
		if parseErr != nil {
			return ResearchPlan{}, 0, fmt.Errorf("planner JSON parse failed twice: %w", parseErr)
		}
	}

	cost := estimateHaikuCost(result.InputTokens, result.OutputTokens)
	return plan, cost, nil
}

func (v2 *V2ResearchOrchestrator) runHaikuExtractor(ctx context.Context, sources []Source, goal string) (ClaimSet, float64, error) {
	const batchSize = 8 // Haiku handles larger batches efficiently

	var allClaims []Claim
	var allGaps []string
	var allSourceIDs []string
	var totalCost float64
	claimOffset := 0

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

		messages := []agent.Message{
			{Role: agent.RoleSystem, Content: prompts.ResearchExtractorSystem},
			{Role: agent.RoleUser, Content: sourceContext.String()},
		}

		result, err := v2.haiku.Complete(ctx, messages, nil)
		if err != nil {
			v2.logger.Warn("v2 extractor batch failed", "batch_start", i, "err", err)
			continue
		}

		claimSet, parseErr := parseJSON[ClaimSet](result.Content)
		if parseErr != nil {
			// Retry with error feedback.
			messages = append(messages,
				agent.Message{Role: agent.RoleAssistant, Content: result.Content},
				agent.Message{Role: agent.RoleUser, Content: fmt.Sprintf("JSON validation failed: %v. Output ONLY valid JSON.", parseErr)},
			)
			result, err = v2.haiku.Complete(ctx, messages, nil)
			if err != nil {
				v2.logger.Warn("v2 extractor batch retry failed", "batch_start", i, "err", err)
				continue
			}
			claimSet, parseErr = parseJSON[ClaimSet](result.Content)
			if parseErr != nil {
				v2.logger.Warn("v2 extractor parse failed twice, skipping", "batch_start", i, "err", parseErr)
				continue
			}
		}

		allClaims = append(allClaims, claimSet.Claims...)
		allGaps = append(allGaps, claimSet.Gaps...)
		allSourceIDs = append(allSourceIDs, claimSet.SourceIDs...)
		claimOffset += len(claimSet.Claims)

		totalCost += estimateHaikuCost(result.InputTokens, result.OutputTokens)
	}

	if len(allClaims) == 0 {
		return ClaimSet{}, totalCost, fmt.Errorf("v2 extractor produced no claims from %d sources", len(sources))
	}

	return ClaimSet{
		Claims:    allClaims,
		Gaps:      allGaps,
		SourceIDs: allSourceIDs,
	}, totalCost, nil
}

// ─── PHASE 2: SONNET METHODS ────────────────────────────────────────────────

func (v2 *V2ResearchOrchestrator) runSonnetCritic(ctx context.Context, claims []Claim, sources []Source, goal string) (CritiqueReport, float64, error) {
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

	// v2: Sonnet critiques its own Haiku's work — different tier provides independence.
	messages := []agent.Message{
		{Role: agent.RoleSystem, Content: prompts.ResearchCriticSystem},
		{Role: agent.RoleUser, Content: reviewContext.String()},
	}

	result, err := v2.sonnet.Complete(ctx, messages, nil)
	if err != nil {
		return CritiqueReport{}, 0, err
	}

	critique, parseErr := parseJSON[CritiqueReport](result.Content)
	if parseErr != nil {
		messages = append(messages,
			agent.Message{Role: agent.RoleAssistant, Content: result.Content},
			agent.Message{Role: agent.RoleUser, Content: fmt.Sprintf("JSON validation failed: %v. Output ONLY valid JSON.", parseErr)},
		)
		result, err = v2.sonnet.Complete(ctx, messages, nil)
		if err != nil {
			return CritiqueReport{}, 0, err
		}
		critique, parseErr = parseJSON[CritiqueReport](result.Content)
		if parseErr != nil {
			return CritiqueReport{}, 0, fmt.Errorf("v2 critic JSON parse failed twice: %w", parseErr)
		}
	}

	cost := estimateSonnetCost(result.InputTokens, result.OutputTokens)
	return critique, cost, nil
}

func (v2 *V2ResearchOrchestrator) runSonnetSynthesizer(ctx context.Context, claims []Claim, sources []Source, critique CritiqueReport, goal string) (string, float64, error) {
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

	messages := []agent.Message{
		{Role: agent.RoleSystem, Content: prompts.ResearchSynthesizerSystem},
		{Role: agent.RoleUser, Content: synthContext.String()},
	}

	result, err := v2.sonnet.Complete(ctx, messages, nil)
	if err != nil {
		return "", 0, fmt.Errorf("v2 synthesizer: %w", err)
	}

	cost := estimateSonnetCost(result.InputTokens, result.OutputTokens)
	return result.Content, cost, nil
}

// ─── SEARCH (tool-only, no LLM) ─────────────────────────────────────────────

// runSearch calls the web search tool for each term and parses results into Sources.
// Unlike v1's runSearcher, this doesn't use an LLM for cost tracking — it estimates
// a small Haiku-equivalent cost per search call for budget consistency.
func (v2 *V2ResearchOrchestrator) runSearch(ctx context.Context, searchTerms []string, existingSources []Source) ([]Source, float64, error) {
	var allSources []Source
	nextID := len(existingSources) + 1

	for _, term := range searchTerms {
		result, err := v2.searchTool.Execute(ctx, map[string]any{"query": term})
		if err != nil {
			v2.logger.Warn("v2 search failed for term", "term", term, "err", err)
			continue
		}

		lines := strings.Split(result.Content, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
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

	// Estimate a small cost: ~100 input + 50 output tokens per search term (tool overhead).
	cost := estimateHaikuCost(len(searchTerms)*100, len(searchTerms)*50)
	return allSources, cost, nil
}

// ─── COST HELPERS ───────────────────────────────────────────────────────────

// Haiku: $0.25/1M input, $1.25/1M output (Claude Haiku 4.5 pricing)
func estimateHaikuCost(inputTokens, outputTokens int) float64 {
	return float64(inputTokens)*0.25/1e6 + float64(outputTokens)*1.25/1e6
}

// Sonnet: $3/1M input, $15/1M output (Claude Sonnet 4.6 pricing)
func estimateSonnetCost(inputTokens, outputTokens int) float64 {
	return float64(inputTokens)*3.0/1e6 + float64(outputTokens)*15.0/1e6
}

func (v2 *V2ResearchOrchestrator) recordCost(ctx context.Context, sessionID, modelID string, cost float64) {
	if v2.budget != nil {
		v2.budget.RecordSpend(ctx, sessionID, modelID, 0, 0, cost)
	}
}
