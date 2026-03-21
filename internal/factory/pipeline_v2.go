// Package factory implements the ZBOT Software Factory — an autonomous
// planning and execution pipeline that takes a vague idea and produces
// production-ready software through specialist AI agents.
//
// V2 ARCHITECTURE (Jeremy's vision, March 21, 2026):
//
// Key principles that distinguish this from a basic agent loop:
//
//   1. INTERVIEWER MUST EXIT WITH A CONCRETE SPEC
//      Not "nice to have" questions. Drive toward: what does this thing DO,
//      who uses it, what data does it touch, how private is that data.
//      Data privacy and security are ALWAYS asked about explicitly.
//
//   2. SEPARATION OF CONCERNS: TESTERS ≠ CODERS
//      The agent that writes code NEVER writes its own tests. A separate
//      test writer examines the interfaces and writes tests that verify
//      BEHAVIOR, not implementation. This prevents circular validation.
//
//   3. SECURITY IS CONTINUOUS, NOT A PHASE
//      After each layer of tests passes, security reviews that layer.
//      Not one big review at the end. Nitpick everything — it's free.
//
//   4. THE CRITIC IS ALWAYS PRESENT
//      Not a final gate. The critic watches every phase and provides
//      constructive feedback proportional to project complexity.
//
//   5. FOCUS METRIC: PRD DRIFT SCORE
//      The PRD defines the end result. Every module, every file, every
//      function must trace back to a user story or requirement. If it
//      can't, it's drift. Measure it. Flag it. Cut it.
//
//   6. DECISION LOG: EVERY CHOICE, EVERY REASON
//      Every model decision is logged: what was decided, why, what
//      alternatives were considered. The critic references this.
//      The user can audit it. It's the institutional memory of the build.
//
//   7. LAYERED CONCURRENT EXECUTION
//      The architect produces a file tree, then groups files into layers.
//      Layer 1 (interfaces, types) builds first. Layer 2 (implementations)
//      builds concurrently once Layer 1 tests pass. Layer 3 (integration)
//      after Layer 2. Testing happens layer-by-layer, not all-at-once.
package factory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── DECISION LOG ───────────────────────────────────────────────────────────

// Decision records a single choice made by any agent in the pipeline.
type Decision struct {
	Timestamp   time.Time `json:"timestamp"`
	Agent       string    `json:"agent"`       // which specialist made it
	Phase       Phase     `json:"phase"`       // during which phase
	Choice      string    `json:"choice"`      // what was decided
	Rationale   string    `json:"rationale"`   // why this over alternatives
	Alternatives []string `json:"alternatives"` // what else was considered
	Confidence  float64   `json:"confidence"`  // 0.0-1.0 self-assessed confidence
	PRDTrace    string    `json:"prd_trace"`   // which PRD requirement this serves
}

// DecisionLog is a thread-safe append-only log of all decisions.
type DecisionLog struct {
	mu        sync.Mutex
	decisions []Decision
}

func NewDecisionLog() *DecisionLog {
	return &DecisionLog{}
}

func (dl *DecisionLog) Record(d Decision) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if d.Timestamp.IsZero() {
		d.Timestamp = time.Now()
	}
	dl.decisions = append(dl.decisions, d)
}

func (dl *DecisionLog) All() []Decision {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	out := make([]Decision, len(dl.decisions))
	copy(out, dl.decisions)
	return out
}

func (dl *DecisionLog) ForPhase(phase Phase) []Decision {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	var out []Decision
	for _, d := range dl.decisions {
		if d.Phase == phase {
			out = append(out, d)
		}
	}
	return out
}

func (dl *DecisionLog) ToMarkdown() string {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	var sb strings.Builder
	sb.WriteString("# Decision Log\n\n")
	for i, d := range dl.decisions {
		sb.WriteString(fmt.Sprintf("## Decision %d — %s [%s]\n", i+1, d.Choice, d.Agent))
		sb.WriteString(fmt.Sprintf("**Phase:** %s | **Confidence:** %.0f%%\n", d.Phase, d.Confidence*100))
		sb.WriteString(fmt.Sprintf("**Rationale:** %s\n", d.Rationale))
		if len(d.Alternatives) > 0 {
			sb.WriteString(fmt.Sprintf("**Alternatives considered:** %s\n", strings.Join(d.Alternatives, ", ")))
		}
		if d.PRDTrace != "" {
			sb.WriteString(fmt.Sprintf("**PRD Trace:** %s\n", d.PRDTrace))
		}
		sb.WriteString("\n---\n\n")
	}
	return sb.String()
}

// ─── FOCUS METRIC ───────────────────────────────────────────────────────────

// FocusScore measures how well the project stays aligned with the PRD.
type FocusScore struct {
	TotalModules    int      `json:"total_modules"`
	TracedModules   int      `json:"traced_modules"`   // modules linked to PRD requirements
	UntracedModules []string `json:"untraced_modules"` // modules with no PRD connection
	DriftScore      float64  `json:"drift_score"`      // 0.0 (total drift) to 1.0 (perfect focus)
	Warnings        []string `json:"warnings"`
}

// ─── BUILD LAYER ────────────────────────────────────────────────────────────

// BuildLayer represents a group of files that can be built/tested concurrently.
// Layers execute in order: Layer 0 (interfaces) → Layer 1 (implementations) → etc.
type BuildLayer struct {
	Order       int           `json:"order"`       // execution order (0 = first)
	Name        string        `json:"name"`        // e.g. "interfaces", "core_impl", "integration"
	Files       []FileSpec    `json:"files"`       // files in this layer
	TestFiles   []FileSpec    `json:"test_files"`  // tests for this layer (written by separate agent)
	DependsOn   []int         `json:"depends_on"`  // layer orders that must complete first
}

// FileSpec describes a single file to be created.
type FileSpec struct {
	Path        string   `json:"path"`         // relative path in project
	Purpose     string   `json:"purpose"`      // what this file does
	PRDTraces   []string `json:"prd_traces"`   // which PRD requirements it serves
	Interfaces  []string `json:"interfaces"`   // interfaces it implements
	Dependencies []string `json:"dependencies"` // other files it imports
}

// ─── V2 PLAN STATE ──────────────────────────────────────────────────────────

// PlanStateV2 holds the full state of a v2 planning session.
type PlanStateV2 struct {
	// Session metadata
	ID        string    `json:"id"`
	Idea      string    `json:"idea"`
	Phase     Phase     `json:"phase"`
	StartedAt time.Time `json:"started_at"`

	// Phase outputs
	FunctionalSpec string       `json:"functional_spec,omitempty"` // concrete spec from interviewer
	PRD            string       `json:"prd,omitempty"`
	FileTree       []FileSpec   `json:"file_tree,omitempty"`       // complete file tree from architect
	BuildLayers    []BuildLayer `json:"build_layers,omitempty"`    // layered build plan
	Architecture   string       `json:"architecture,omitempty"`
	SecurityNotes  []string     `json:"security_notes,omitempty"`  // accumulated across all layers

	// Decision log (every choice, every reason)
	Decisions *DecisionLog `json:"-"` // separate, always growing

	// Focus tracking
	Focus *FocusScore `json:"focus,omitempty"`

	// Critic feedback (continuous, not just at the end)
	CriticFeedback []CriticNote `json:"critic_feedback,omitempty"`

	// User interaction
	PendingQuestions []string `json:"pending_questions,omitempty"`

	// Privacy/security classification (always asked)
	DataSensitivity string `json:"data_sensitivity"` // "public", "internal", "confidential", "regulated"
	SecurityLevel   string `json:"security_level"`   // "standard", "elevated", "maximum"

	Errors []string `json:"errors,omitempty"`
}

// CriticNote is feedback from the always-present critic.
type CriticNote struct {
	Timestamp time.Time `json:"timestamp"`
	Phase     Phase     `json:"phase"`
	Severity  string    `json:"severity"` // "info", "warning", "concern", "blocker"
	Message   string    `json:"message"`
	Resolved  bool      `json:"resolved"`
}

// ─── V2 PIPELINE ────────────────────────────────────────────────────────────

// PipelineV2 orchestrates the full v2 planning and execution cycle.
type PipelineV2 struct {
	cheapModel  agent.LLMClient // Qwen (local) — interviewer, PRD writer, test writer
	smartModel  agent.LLMClient // Sonnet — architect, security, critic
	logger      *slog.Logger
}

// NewPipelineV2 creates a v2 software factory pipeline.
func NewPipelineV2(cheapModel, smartModel agent.LLMClient, logger *slog.Logger) *PipelineV2 {
	return &PipelineV2{
		cheapModel: cheapModel,
		smartModel: smartModel,
		logger:     logger,
	}
}

// StartPlan begins with the interview phase — drives toward a concrete functional spec.
func (p *PipelineV2) StartPlan(ctx context.Context, id, idea string) (*PlanStateV2, error) {
	state := &PlanStateV2{
		ID:        id,
		Idea:      idea,
		Phase:     PhaseInterview,
		StartedAt: time.Now(),
		Decisions: NewDecisionLog(),
	}

	p.logger.Info("factory v2: starting plan", "id", id)

	// The interviewer ALWAYS asks about data privacy/security.
	questions, err := p.runInterviewerV2(ctx, state)
	if err != nil {
		return state, err
	}
	state.PendingQuestions = questions
	return state, nil
}

// AnswerQuestions processes user answers and advances the pipeline.
func (p *PipelineV2) AnswerQuestions(ctx context.Context, state *PlanStateV2, answers string) (*PlanStateV2, error) {
	switch state.Phase {
	case PhaseInterview:
		return p.processInterviewAnswers(ctx, state, answers)
	default:
		// For other phases, user approval advances to next phase.
		return p.advancePhase(ctx, state)
	}
}

func (p *PipelineV2) runInterviewerV2(ctx context.Context, state *PlanStateV2) ([]string, error) {
	prompt := fmt.Sprintf(`You are conducting a product discovery interview. The user has an idea:

"%s"

Your job is to ask questions that drive toward a CONCRETE FUNCTIONAL SPECIFICATION.
Not vague requirements — specific, testable functionality.

You MUST ask about ALL of these:

1. CORE FUNCTIONALITY: What exactly does this thing DO? Be specific.
   Not "manages tasks" but "lets users create tasks with title, due date,
   and priority, assign them to team members, and mark them complete."

2. TARGET USERS: Who uses this? One person? A team? Public?

3. DATA & PRIVACY (MANDATORY — ALWAYS ASK):
   - What user data is collected?
   - How sensitive is it? (public / internal / confidential / regulated)
   - Where should data be stored? (local only? cloud? user's choice?)
   - Who should be able to see what?
   - Any compliance requirements? (HIPAA, GDPR, SOC2, etc.)
   - Should data be encrypted at rest? In transit?

4. SECURITY (MANDATORY — ALWAYS ASK):
   - Does it need authentication? What kind?
   - Who should have access to what?
   - Any API keys or third-party integrations?
   - What happens if the system is compromised?

5. PLATFORM & CONSTRAINTS: Web? Mobile? CLI? Desktop? API only?
   What language/framework preference if any?

6. SCALE: How many users? How much data? Growth expectations?

7. EXISTING SOLUTIONS: What exists? What's wrong with it?

Ask 8-10 focused questions. Number them. No preamble.`, state.Idea)

	result, err := p.cheapModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: interviewerV2System},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return nil, err
	}

	state.Decisions.Record(Decision{
		Agent:      "interviewer",
		Phase:      PhaseInterview,
		Choice:     "Generated initial discovery questions",
		Rationale:  "Standard discovery template with mandatory privacy/security questions",
		Confidence: 0.9,
	})

	var questions []string
	for _, line := range strings.Split(result.Content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && len(line) > 5 {
			questions = append(questions, line)
		}
	}
	return questions, nil
}

func (p *PipelineV2) processInterviewAnswers(ctx context.Context, state *PlanStateV2, answers string) (*PlanStateV2, error) {
	// Build accumulated context.
	context := state.FunctionalSpec + "\n\nUser answers:\n" + answers

	// Ask the interviewer to assess: do we have a concrete spec yet?
	prompt := fmt.Sprintf(`Review these requirements gathered so far:

%s

Evaluate:
1. Do we have CONCRETE functionality defined (not vague)?
2. Do we know the data privacy classification?
3. Do we know the security requirements?
4. Do we know the platform and constraints?

If we have enough for a concrete functional spec, respond with:
SPEC_READY

Then write the functional spec as a numbered list of specific, testable features.

If we need more information, respond with:
NEED_MORE

Then list 3-5 specific follow-up questions.`, context)

	result, err := p.cheapModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: interviewerV2System},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return state, err
	}

	if strings.Contains(result.Content, "SPEC_READY") {
		// Extract the spec (everything after SPEC_READY).
		parts := strings.SplitN(result.Content, "SPEC_READY", 2)
		spec := result.Content
		if len(parts) > 1 {
			spec = strings.TrimSpace(parts[1])
		}
		state.FunctionalSpec = spec
		state.Phase = PhasePRD
		state.PendingQuestions = nil

		state.Decisions.Record(Decision{
			Agent:      "interviewer",
			Phase:      PhaseInterview,
			Choice:     "Functional spec finalized — moving to PRD",
			Rationale:  "All required areas covered: functionality, privacy, security, platform",
			Confidence: 0.85,
		})

		p.logger.Info("factory v2: interview complete, spec ready", "id", state.ID)

		// Run critic on the spec before moving forward.
		criticNote := p.runCriticCheck(ctx, state, "functional_spec", state.FunctionalSpec)
		state.CriticFeedback = append(state.CriticFeedback, criticNote)

		// Auto-advance to PRD.
		return p.runPRDPhaseV2(ctx, state)
	}

	// Need more questions.
	state.FunctionalSpec = context
	var questions []string
	for _, line := range strings.Split(result.Content, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(result.Content, "NEED_MORE") && line != "" && len(line) > 5 && !strings.Contains(line, "NEED_MORE") {
			questions = append(questions, line)
		}
	}
	state.PendingQuestions = questions
	return state, nil
}

func (p *PipelineV2) runPRDPhaseV2(ctx context.Context, state *PlanStateV2) (*PlanStateV2, error) {
	prompt := fmt.Sprintf(`Convert this functional spec into a formal PRD:

%s

The PRD MUST include:
1. **Overview** — One paragraph, what this is
2. **Users** — Personas with specific pain points
3. **Requirements** — Numbered, each one TESTABLE (can write a test for it)
4. **Data Classification** — What data, how sensitive, encryption needs
5. **Security Requirements** — Auth, authorization, audit logging
6. **Success Metrics** — How we measure if it works
7. **Out of Scope** — What this is NOT (prevents drift)
8. **Dependency Constraints** — Packages must be: actively maintained, MIT/Apache licensed, >500 GitHub stars

For each requirement, assign a unique ID (REQ-001, REQ-002, etc.).
These IDs will be used to trace every file and every decision back to a requirement.`, state.FunctionalSpec)

	result, err := p.cheapModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: prdWriterV2System},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		state.Errors = append(state.Errors, fmt.Sprintf("prd: %v", err))
		return state, err
	}

	state.PRD = result.Content

	state.Decisions.Record(Decision{
		Agent:      "prd_writer",
		Phase:      PhasePRD,
		Choice:     "PRD generated with requirement IDs for traceability",
		Rationale:  "Every requirement has a unique ID so files, tests, and decisions can trace back to it",
		Confidence: 0.8,
	})

	// Critic reviews the PRD.
	criticNote := p.runCriticCheck(ctx, state, "prd", state.PRD)
	state.CriticFeedback = append(state.CriticFeedback, criticNote)

	p.logger.Info("factory v2: PRD generated", "id", state.ID)
	return state, nil
}

func (p *PipelineV2) advancePhase(ctx context.Context, state *PlanStateV2) (*PlanStateV2, error) {
	switch state.Phase {
	case PhasePRD:
		state.Phase = PhaseArchitect
		return p.runArchitectPhaseV2(ctx, state)
	case PhaseArchitect:
		// After architect, security reviews the architecture.
		state.Phase = PhaseSecurity
		return p.runSecurityPhaseV2(ctx, state)
	case PhaseSecurity:
		state.Phase = PhaseManifest
		return p.runLayeredManifest(ctx, state)
	default:
		return state, nil
	}
}

func (p *PipelineV2) runArchitectPhaseV2(ctx context.Context, state *PlanStateV2) (*PlanStateV2, error) {
	prompt := fmt.Sprintf(`Based on this PRD:

%s

Design the system architecture. You must produce:

1. **COMPLETE FILE TREE** — Every single file that will exist in the project.
   For each file, specify:
   - Path (e.g. internal/auth/handler.go)
   - Purpose (one sentence)
   - PRD traces (which REQ-XXX requirements it serves)
   - Interfaces it implements or defines

2. **BUILD LAYERS** — Group the files into layers for concurrent execution:
   - Layer 0: Types, interfaces, constants (no dependencies on other project code)
   - Layer 1: Core implementations (depend on Layer 0)
   - Layer 2: Integration, API handlers (depend on Layer 1)
   - Layer 3: E2E, CLI, main entry point (depend on Layer 2)
   Each layer can be built and tested concurrently within itself.

3. **DEPENDENCY MANIFEST** — For each external package:
   - Name and version
   - Why chosen (not just "it's popular" — specific technical reason)
   - What alternative was considered and why rejected
   - License
   - GitHub stars / maintenance status
   - Any known security advisories

4. **STACK DECISIONS** — Language, framework, database, with full rationale.
   Log each decision as:
   DECISION: [choice] | BECAUSE: [rationale] | OVER: [alternatives] | TRACES: [REQ-XXX]

Output the file tree as JSON array of objects with: path, purpose, prd_traces, layer.
Output decisions in the DECISION format above.`, state.PRD)

	result, err := p.smartModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: architectV2System},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return state, err
	}

	state.Architecture = result.Content

	// Parse decisions from the architect's output.
	for _, line := range strings.Split(result.Content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "DECISION:") {
			parts := strings.Split(line, "|")
			d := Decision{
				Agent: "architect",
				Phase: PhaseArchitect,
			}
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "DECISION:") {
					d.Choice = strings.TrimSpace(strings.TrimPrefix(part, "DECISION:"))
				} else if strings.HasPrefix(part, "BECAUSE:") {
					d.Rationale = strings.TrimSpace(strings.TrimPrefix(part, "BECAUSE:"))
				} else if strings.HasPrefix(part, "OVER:") {
					d.Alternatives = strings.Split(strings.TrimSpace(strings.TrimPrefix(part, "OVER:")), ",")
				} else if strings.HasPrefix(part, "TRACES:") {
					d.PRDTrace = strings.TrimSpace(strings.TrimPrefix(part, "TRACES:"))
				}
			}
			d.Confidence = 0.8
			state.Decisions.Record(d)
		}
	}

	// Critic reviews the architecture.
	criticNote := p.runCriticCheck(ctx, state, "architecture", state.Architecture)
	state.CriticFeedback = append(state.CriticFeedback, criticNote)

	// Calculate focus score.
	state.Focus = p.calculateFocus(state)

	p.logger.Info("factory v2: architecture generated",
		"id", state.ID,
		"decisions", len(state.Decisions.All()),
		"focus_score", fmt.Sprintf("%.0f%%", state.Focus.DriftScore*100),
	)
	return state, nil
}

func (p *PipelineV2) runSecurityPhaseV2(ctx context.Context, state *PlanStateV2) (*PlanStateV2, error) {
	prompt := fmt.Sprintf(`SECURITY REVIEW — be thorough, be paranoid.

Data sensitivity: %s
Security level: %s

## PRD
%s

## Architecture
%s

Review EVERY aspect:
1. Authentication — Is it sufficient for the data sensitivity?
2. Authorization — Are permissions properly scoped?
3. Input validation — Every user input, every API parameter
4. Secrets — How are they stored? Are they ever logged?
5. Encryption — At rest AND in transit, for the classified data level
6. Dependencies — Any with known CVEs? Any abandoned?
7. Rate limiting — On every public endpoint
8. Logging — Is sensitive data excluded from logs?
9. OWASP Top 10 — Address each explicitly
10. Supply chain — Lock files, checksums, CI verification

For each finding:
SECURITY: [severity: CRITICAL|HIGH|MEDIUM|LOW] | [finding] | [required action] | TRACES: [REQ-XXX]

If the architecture has security decisions that were WRONG, say so explicitly.
This is the cheapest time to fix security — before code exists.`, state.DataSensitivity, state.SecurityLevel, state.PRD, state.Architecture)

	result, err := p.smartModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: securityReviewerV2System},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return state, err
	}

	// Parse security findings.
	for _, line := range strings.Split(result.Content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "SECURITY:") {
			state.SecurityNotes = append(state.SecurityNotes, line)
			state.Decisions.Record(Decision{
				Agent:      "security_reviewer",
				Phase:      PhaseSecurity,
				Choice:     line,
				Rationale:  "Security finding from architecture review",
				Confidence: 0.9,
			})
		}
	}

	p.logger.Info("factory v2: security review complete",
		"id", state.ID,
		"findings", len(state.SecurityNotes),
	)
	return state, nil
}

func (p *PipelineV2) runLayeredManifest(ctx context.Context, state *PlanStateV2) (*PlanStateV2, error) {
	prompt := fmt.Sprintf(`Convert this architecture into a layered TaskManifest for parallel execution.

## Architecture
%s

## Security Requirements
%s

CRITICAL RULES:
1. Test files are in SEPARATE tasks from implementation files.
   The coder that writes auth.go MUST NOT write auth_test.go.
   A different task writes the tests based on the interface spec.

2. Group into build layers:
   Layer 0: interfaces, types, constants
   Layer 1: core implementations
   Layer 2: API handlers, integration
   Layer 3: main, CLI, E2E tests

3. Each task must have:
   - prd_traces: which REQ-XXX it serves (for focus tracking)
   - test_command: how to verify this specific file compiles/passes

4. Security tasks: After each layer's tests pass, a security review task
   runs on that layer's code.

Output valid JSON: {"layers": [...], "project_name": "...", "base_dir": "/tmp/factory/..."}`,
		state.Architecture, strings.Join(state.SecurityNotes, "\n"))

	_, err := p.smartModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: "You convert software architectures into bounded parallel coding tasks. Tests are ALWAYS separate tasks from implementations. Output only valid JSON."},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return state, err
	}

	state.Phase = PhaseComplete

	// Store the raw manifest. In a full implementation, we'd parse it
	// and feed it to the parallel dispatcher.
	state.Decisions.Record(Decision{
		Agent:      "manifest_generator",
		Phase:      PhaseManifest,
		Choice:     "Generated layered build manifest with separated test tasks",
		Rationale:  "Tests written by separate agent from implementation to prevent circular validation",
		Confidence: 0.85,
	})

	p.logger.Info("factory v2: manifest generated, plan complete",
		"id", state.ID,
		"total_decisions", len(state.Decisions.All()),
		"critic_notes", len(state.CriticFeedback),
		"focus", fmt.Sprintf("%.0f%%", state.Focus.DriftScore*100),
	)
	return state, nil
}

// ─── ALWAYS-PRESENT CRITIC ──────────────────────────────────────────────────

func (p *PipelineV2) runCriticCheck(ctx context.Context, state *PlanStateV2, docName, content string) CriticNote {
	prompt := fmt.Sprintf(`You are reviewing the "%s" document for a software project.

Original idea: %s

Document content:
%s

Previous decisions made:
%s

Provide constructive criticism. Look for:
- Vague requirements that can't be tested
- Missing error handling paths
- Security assumptions that aren't validated
- Scope creep (features not in the original idea)
- Over-engineering for the stated scale
- Under-engineering for the stated security level

Rate severity: info | warning | concern | blocker

Respond in ONE paragraph. Be specific. If it's solid, say so briefly.`,
		docName, state.Idea, truncate(content, 3000), p.formatRecentDecisions(state, 5))

	result, err := p.smartModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: criticV2System},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)

	note := CriticNote{
		Timestamp: time.Now(),
		Phase:     state.Phase,
		Severity:  "info",
	}

	if err != nil {
		note.Severity = "warning"
		note.Message = fmt.Sprintf("Critic unavailable: %v", err)
		return note
	}

	note.Message = result.Content

	// Detect severity from content.
	upper := strings.ToUpper(result.Content)
	if strings.Contains(upper, "BLOCKER") {
		note.Severity = "blocker"
	} else if strings.Contains(upper, "CONCERN") {
		note.Severity = "concern"
	} else if strings.Contains(upper, "WARNING") || strings.Contains(upper, "MISSING") {
		note.Severity = "warning"
	}

	return note
}

// ─── FOCUS TRACKING ─────────────────────────────────────────────────────────

func (p *PipelineV2) calculateFocus(state *PlanStateV2) *FocusScore {
	// Count how many decisions trace back to a PRD requirement.
	decisions := state.Decisions.All()
	traced := 0
	var untraced []string

	for _, d := range decisions {
		if d.PRDTrace != "" && d.PRDTrace != "N/A" {
			traced++
		} else if d.Phase == PhaseArchitect {
			untraced = append(untraced, d.Choice)
		}
	}

	archDecisions := 0
	for _, d := range decisions {
		if d.Phase == PhaseArchitect {
			archDecisions++
		}
	}

	drift := 1.0
	if archDecisions > 0 {
		drift = float64(traced) / float64(archDecisions)
	}

	score := &FocusScore{
		TotalModules:    archDecisions,
		TracedModules:   traced,
		UntracedModules: untraced,
		DriftScore:      drift,
	}

	if drift < 0.8 {
		score.Warnings = append(score.Warnings,
			fmt.Sprintf("Focus score %.0f%% — %d decisions don't trace to PRD requirements", drift*100, len(untraced)))
	}

	return score
}

func (p *PipelineV2) formatRecentDecisions(state *PlanStateV2, n int) string {
	all := state.Decisions.All()
	start := 0
	if len(all) > n {
		start = len(all) - n
	}
	var sb strings.Builder
	for _, d := range all[start:] {
		sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", d.Agent, d.Choice, d.Rationale))
	}
	return sb.String()
}

// ─── V2 SYSTEM PROMPTS ──────────────────────────────────────────────────────

const interviewerV2System = `You are a product discovery expert. Your job is to extract CONCRETE,
TESTABLE requirements from vague ideas. You ALWAYS ask about data privacy
and security — these are not optional. Drive toward specificity.
"It manages tasks" is not a requirement. "Users can create tasks with title,
due date, and priority" is a requirement.`

const prdWriterV2System = `You write formal PRDs with traceable requirements. Every requirement gets
a unique ID (REQ-001, REQ-002). Every requirement must be TESTABLE — you
could write a test that proves it works or doesn't. Include data classification
and security requirements as first-class sections, not afterthoughts.`

const architectV2System = `You are a senior architect who designs systems in LAYERS.
Layer 0 has zero internal dependencies. Each subsequent layer only depends on
layers below it. This enables concurrent build and test.

You document EVERY decision: what you chose, why, what you rejected.
Format: DECISION: [choice] | BECAUSE: [rationale] | OVER: [alternatives] | TRACES: [REQ-XXX]

Choose boring, well-maintained technology. Every dependency must justify its existence.`

const securityReviewerV2System = `You are a security engineer who reviews BEFORE code exists.
This is the cheapest time to find problems — orders of magnitude cheaper
than finding them in production. Be paranoid. Every input is hostile.
Every dependency is a potential supply chain attack. Nitpick everything —
the review is free, the breach is not.`

const criticV2System = `You are the always-present critic. You watch every phase of software planning.
Your job: find what everyone else missed. Contradictions between documents.
Unstated assumptions. Missing error paths. Scope creep. Over-engineering.
Be constructive — point out problems AND suggest fixes.
Scale your feedback to complexity: simple projects get brief notes,
complex projects get detailed analysis.`

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n[...truncated...]"
}
