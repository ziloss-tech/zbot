// Package factory implements the ZBOT Software Factory — an autonomous
// planning pipeline that takes a vague idea and produces a fully spec'd,
// security-reviewed, dependency-resolved, test-covered implementation plan.
//
// The pipeline runs in cycles through specialist agents:
//   Interviewer → PRD Writer → Architect → Security Reviewer →
//   Test Strategist → Critic → (loop if issues) → Manifest Generator
//
// Each specialist is a focused prompt running on a cheap/local model (Qwen)
// or a frontier model (Sonnet) for security-critical decisions.
//
// No code is written until the plan survives scrutiny from all specialists.
package factory

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// Phase represents a stage in the planning pipeline.
type Phase string

const (
	PhaseInterview  Phase = "interview"
	PhasePRD        Phase = "prd"
	PhaseArchitect  Phase = "architect"
	PhaseSecurity   Phase = "security"
	PhaseTesting    Phase = "testing"
	PhaseCritique   Phase = "critique"
	PhaseManifest   Phase = "manifest"
	PhaseComplete   Phase = "complete"
)

// PlanState holds the accumulated state of a planning session.
type PlanState struct {
	// Session metadata.
	ID        string    `json:"id"`
	Idea      string    `json:"idea"`
	Phase     Phase     `json:"phase"`
	Cycle     int       `json:"cycle"` // critique loop count
	StartedAt time.Time `json:"started_at"`

	// Outputs from each specialist.
	RawRequirements string `json:"raw_requirements,omitempty"`
	PRD             string `json:"prd,omitempty"`
	Architecture    string `json:"architecture,omitempty"`
	SecurityReview  string `json:"security_review,omitempty"`
	TestStrategy    string `json:"test_strategy,omitempty"`
	Critique        string `json:"critique,omitempty"`
	Manifest        string `json:"manifest,omitempty"`

	// Questions pending user input.
	PendingQuestions []string `json:"pending_questions,omitempty"`

	// Revision requests from the critic.
	RevisionRequests []RevisionRequest `json:"revision_requests,omitempty"`

	// Dependencies discovered by the architect.
	Dependencies []Dependency `json:"dependencies,omitempty"`

	// Errors encountered during planning.
	Errors []string `json:"errors,omitempty"`
}

// RevisionRequest is a change the critic wants a specialist to make.
type RevisionRequest struct {
	Target  Phase  `json:"target"`  // which specialist needs to revise
	Issue   string `json:"issue"`   // what's wrong
	Suggest string `json:"suggest"` // suggested fix
}

// Dependency is a package/library identified by the architect.
type Dependency struct {
	Name    string `json:"name"`    // e.g. "github.com/jackc/pgx/v5"
	Version string `json:"version"` // e.g. "v5.7.0"
	Why     string `json:"why"`     // rationale for choosing this
	License string `json:"license"` // e.g. "MIT", "Apache-2.0"
}

// Specialist is a focused AI agent for one phase of planning.
type Specialist struct {
	Name         string
	Phase        Phase
	SystemPrompt string
	Model        agent.LLMClient // which model runs this specialist
}

// Pipeline orchestrates the full planning cycle.
type Pipeline struct {
	cheapModel   agent.LLMClient // Qwen (local) — interviewer, PRD, tests
	smartModel   agent.LLMClient // Sonnet — architect, security, critic
	logger       *slog.Logger
	maxCycles    int // max critique→revision loops (default 3)
}

// NewPipeline creates a software factory planning pipeline.
func NewPipeline(cheapModel, smartModel agent.LLMClient, logger *slog.Logger) *Pipeline {
	return &Pipeline{
		cheapModel: cheapModel,
		smartModel: smartModel,
		logger:     logger,
		maxCycles:  3,
	}
}

// StartPlan begins a new planning session from a user's idea.
// Returns the initial state with questions for the user.
func (p *Pipeline) StartPlan(ctx context.Context, id, idea string) (*PlanState, error) {
	state := &PlanState{
		ID:        id,
		Idea:      idea,
		Phase:     PhaseInterview,
		Cycle:     0,
		StartedAt: time.Now(),
	}

	p.logger.Info("factory: starting plan", "id", id, "idea_len", len(idea))

	// Run the interviewer to generate clarifying questions.
	questions, err := p.runInterviewer(ctx, state)
	if err != nil {
		state.Errors = append(state.Errors, fmt.Sprintf("interviewer: %v", err))
		return state, err
	}

	state.PendingQuestions = questions
	return state, nil
}

// ContinuePlan advances the pipeline with user answers.
// Call this each time the user answers questions.
func (p *Pipeline) ContinuePlan(ctx context.Context, state *PlanState, userInput string) (*PlanState, error) {
	switch state.Phase {
	case PhaseInterview:
		// Accumulate the user's answers into raw requirements.
		state.RawRequirements += "\n\nUser answers:\n" + userInput

		// Check if we have enough info to proceed.
		ready, moreQuestions, err := p.checkReadiness(ctx, state)
		if err != nil {
			state.Errors = append(state.Errors, fmt.Sprintf("readiness check: %v", err))
			return state, err
		}
		if !ready {
			state.PendingQuestions = moreQuestions
			return state, nil
		}

		// Move to PRD phase.
		state.Phase = PhasePRD
		state.PendingQuestions = nil
		return p.runPRDPhase(ctx, state)

	case PhasePRD:
		// User approved PRD, move to architecture.
		state.Phase = PhaseArchitect
		return p.runArchitectPhase(ctx, state)

	case PhaseArchitect:
		// User approved architecture, move to security.
		state.Phase = PhaseSecurity
		return p.runSecurityPhase(ctx, state)

	case PhaseSecurity:
		// User approved security review, move to testing.
		state.Phase = PhaseTesting
		return p.runTestingPhase(ctx, state)

	case PhaseTesting:
		// User approved test strategy, move to critique.
		state.Phase = PhaseCritique
		return p.runCritiquePhase(ctx, state)

	case PhaseCritique:
		// If critic found issues and we haven't maxed out cycles, loop back.
		if len(state.RevisionRequests) > 0 && state.Cycle < p.maxCycles {
			state.Cycle++
			return p.runRevisions(ctx, state)
		}
		// Move to manifest generation.
		state.Phase = PhaseManifest
		return p.runManifestPhase(ctx, state)

	case PhaseManifest:
		state.Phase = PhaseComplete
		return state, nil

	default:
		return state, fmt.Errorf("unknown phase: %s", state.Phase)
	}
}

// ─── SPECIALIST RUNNERS ─────────────────────────────────────────────────────

func (p *Pipeline) runInterviewer(ctx context.Context, state *PlanState) ([]string, error) {
	prompt := fmt.Sprintf(`You are a senior product manager conducting a discovery interview.

The user has this idea: "%s"

Ask 5-7 clarifying questions to understand:
1. Who are the target users? (personas)
2. What specific problem does this solve?
3. What are the must-have features vs nice-to-have?
4. Are there existing solutions? What's wrong with them?
5. Technical constraints (platform, language preference, hosting)?
6. Security/privacy requirements?
7. Scale expectations (users, data volume)?

Output ONLY the numbered questions, one per line. No preamble.`, state.Idea)

	result, err := p.cheapModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: interviewerSystem},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return nil, err
	}

	// Parse questions from response.
	var questions []string
	for _, line := range strings.Split(result.Content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && (len(line) > 3) { // skip empty/short lines
			questions = append(questions, line)
		}
	}

	state.RawRequirements = fmt.Sprintf("Original idea: %s\n\nInterviewer questions:\n%s",
		state.Idea, result.Content)

	return questions, nil
}

func (p *Pipeline) checkReadiness(ctx context.Context, state *PlanState) (bool, []string, error) {
	prompt := fmt.Sprintf(`Review these requirements gathered so far:

%s

Are there enough details to write a PRD? Consider:
- Do we know the target users?
- Do we know the core features?
- Do we know technical constraints?
- Do we know security needs?

If YES, respond with exactly: READY

If NO, respond with 2-3 additional questions we need answered.
Output ONLY "READY" or the questions, nothing else.`, state.RawRequirements)

	result, err := p.cheapModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: "You are a product manager assessing if requirements are complete enough to proceed."},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return false, nil, err
	}

	if strings.Contains(strings.ToUpper(result.Content), "READY") {
		return true, nil, nil
	}

	var questions []string
	for _, line := range strings.Split(result.Content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			questions = append(questions, line)
		}
	}
	return false, questions, nil
}

func (p *Pipeline) runPRDPhase(ctx context.Context, state *PlanState) (*PlanState, error) {
	prompt := fmt.Sprintf(`Based on these gathered requirements:

%s

Write a Product Requirements Document (PRD) in markdown with these sections:
1. **Overview** — What is this product, one paragraph
2. **Target Users** — Personas with names and pain points
3. **User Stories** — "As a [user], I want [feature], so that [benefit]"
4. **Must-Have Features** — Numbered list with brief descriptions
5. **Nice-to-Have Features** — Numbered list, clearly separated from must-haves
6. **Success Metrics** — How we measure if this works
7. **Constraints** — Technical, business, or regulatory limits
8. **Out of Scope** — What this product is NOT

Be specific and actionable. No fluff.`, state.RawRequirements)

	result, err := p.cheapModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: prdWriterSystem},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		state.Errors = append(state.Errors, fmt.Sprintf("prd: %v", err))
		return state, err
	}

	state.PRD = result.Content
	p.logger.Info("factory: PRD generated", "id", state.ID, "len", len(state.PRD))
	return state, nil
}

func (p *Pipeline) runArchitectPhase(ctx context.Context, state *PlanState) (*PlanState, error) {
	prompt := fmt.Sprintf(`Based on this PRD:

%s

Design the system architecture. Include:
1. **Stack Selection** — Language, framework, database, with WHY for each choice
2. **System Diagram** — Describe components and their connections (text-based)
3. **Data Model** — Database schema with table names, columns, types, relationships
4. **API Contracts** — Key endpoints with method, path, request/response shape
5. **Module Boundaries** — What packages/modules exist, what each owns
6. **Dependencies** — External packages needed, version, license, and why chosen over alternatives
7. **Infrastructure** — How it deploys (Docker? Serverless? VPS?)
8. **Configuration** — What env vars / config files are needed

For each dependency, explain WHY it was chosen over alternatives.
Flag any dependencies that are less than 1 year old or have fewer than 1000 GitHub stars.`, state.PRD)

	// Use smart model for architecture — needs broader reasoning.
	result, err := p.smartModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: architectSystem},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		state.Errors = append(state.Errors, fmt.Sprintf("architect: %v", err))
		return state, err
	}

	state.Architecture = result.Content
	p.logger.Info("factory: architecture generated", "id", state.ID, "len", len(state.Architecture))
	return state, nil
}

func (p *Pipeline) runSecurityPhase(ctx context.Context, state *PlanState) (*PlanState, error) {
	prompt := fmt.Sprintf(`Review this PRD and architecture for security:

## PRD
%s

## Architecture
%s

Perform a security review covering:
1. **Authentication** — How are users identified? Is it sufficient?
2. **Authorization** — Are permissions properly scoped?
3. **Input Validation** — SQL injection, XSS, command injection vectors?
4. **Secrets Management** — Are API keys, tokens stored securely?
5. **Data Protection** — Encryption at rest and in transit?
6. **Dependency Risks** — Any packages with known CVEs? Abandoned maintainers?
7. **Rate Limiting** — API abuse prevention?
8. **Logging** — Is sensitive data kept out of logs?
9. **OWASP Top 10** — Address each item explicitly
10. **Supply Chain** — Lock files? Checksum verification?

For each finding, rate severity: CRITICAL / HIGH / MEDIUM / LOW
List required changes vs recommended changes separately.`, state.PRD, state.Architecture)

	// Security review MUST use frontier model.
	result, err := p.smartModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: securityReviewerSystem},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		state.Errors = append(state.Errors, fmt.Sprintf("security: %v", err))
		return state, err
	}

	state.SecurityReview = result.Content
	p.logger.Info("factory: security review generated", "id", state.ID, "len", len(state.SecurityReview))
	return state, nil
}

func (p *Pipeline) runTestingPhase(ctx context.Context, state *PlanState) (*PlanState, error) {
	prompt := fmt.Sprintf(`Based on this PRD and architecture:

## PRD
%s

## Architecture
%s

Design the testing strategy:
1. **Testing Framework** — Which tools/frameworks and why
2. **Unit Tests** — For each module, list the key functions to test and edge cases
3. **Integration Tests** — Module boundary tests, database interaction tests
4. **E2E Tests** — Full user flow scenarios to automate
5. **Security Tests** — Auth bypass, injection payloads, token expiry
6. **Performance Tests** — Load thresholds, response time budgets, memory limits
7. **Coverage Targets** — Overall and per-module minimums
8. **CI Pipeline** — What runs on every PR, what runs nightly

For each test category, provide concrete test names and what they verify.`, state.PRD, state.Architecture)

	result, err := p.cheapModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: testStrategistSystem},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		state.Errors = append(state.Errors, fmt.Sprintf("testing: %v", err))
		return state, err
	}

	state.TestStrategy = result.Content
	p.logger.Info("factory: test strategy generated", "id", state.ID, "len", len(state.TestStrategy))
	return state, nil
}

func (p *Pipeline) runCritiquePhase(ctx context.Context, state *PlanState) (*PlanState, error) {
	prompt := fmt.Sprintf(`You are reviewing a complete software plan. Find problems.

## PRD
%s

## Architecture
%s

## Security Review
%s

## Test Strategy
%s

Review for:
1. **Contradictions** — Does the architecture support all PRD requirements?
2. **Missing coverage** — Are there user stories without corresponding API endpoints?
3. **Security gaps** — Did the security review miss anything in the architecture?
4. **Test gaps** — Are there critical paths without test coverage?
5. **Over-engineering** — Is anything more complex than needed?
6. **Under-engineering** — Is anything too simple for the stated scale?
7. **Dependency risks** — Any single points of failure?

If you find issues, output them as:
REVISION: [target phase] | [issue description] | [suggested fix]

If the plan is solid, output exactly: APPROVED

Be critical. This is the last check before code is written.`,
		state.PRD, state.Architecture, state.SecurityReview, state.TestStrategy)

	// Critic MUST use a different model than the specialists.
	result, err := p.smartModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: criticSystem},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		state.Errors = append(state.Errors, fmt.Sprintf("critic: %v", err))
		return state, err
	}

	state.Critique = result.Content

	// Parse revision requests.
	state.RevisionRequests = nil
	if !strings.Contains(strings.ToUpper(result.Content), "APPROVED") {
		for _, line := range strings.Split(result.Content, "\n") {
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "REVISION:") {
				parts := strings.SplitN(line, "|", 3)
				if len(parts) >= 2 {
					target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(parts[0]), "REVISION:"))
					target = strings.TrimPrefix(target, "revision:")
					req := RevisionRequest{
						Target: Phase(strings.ToLower(strings.TrimSpace(target))),
						Issue:  strings.TrimSpace(parts[1]),
					}
					if len(parts) >= 3 {
						req.Suggest = strings.TrimSpace(parts[2])
					}
					state.RevisionRequests = append(state.RevisionRequests, req)
				}
			}
		}
	}

	p.logger.Info("factory: critique complete",
		"id", state.ID,
		"approved", len(state.RevisionRequests) == 0,
		"revisions", len(state.RevisionRequests),
		"cycle", state.Cycle,
	)
	return state, nil
}

func (p *Pipeline) runRevisions(ctx context.Context, state *PlanState) (*PlanState, error) {
	for _, rev := range state.RevisionRequests {
		p.logger.Info("factory: running revision",
			"target", rev.Target,
			"issue", rev.Issue[:min(len(rev.Issue), 80)],
		)

		switch rev.Target {
		case PhasePRD:
			state.Phase = PhasePRD
			state, _ = p.runPRDPhase(ctx, state)
		case PhaseArchitect:
			state.Phase = PhaseArchitect
			state, _ = p.runArchitectPhase(ctx, state)
		case PhaseSecurity:
			state.Phase = PhaseSecurity
			state, _ = p.runSecurityPhase(ctx, state)
		case PhaseTesting:
			state.Phase = PhaseTesting
			state, _ = p.runTestingPhase(ctx, state)
		}
	}

	// Re-run critique after revisions.
	state.Phase = PhaseCritique
	state.RevisionRequests = nil
	return p.runCritiquePhase(ctx, state)
}

func (p *Pipeline) runManifestPhase(ctx context.Context, state *PlanState) (*PlanState, error) {
	prompt := fmt.Sprintf(`Convert this approved plan into a parallel coding TaskManifest JSON.

## PRD
%s

## Architecture
%s

## Test Strategy
%s

Produce a JSON object with:
- project_name: string
- base_dir: "/tmp/factory-output/{project_name}"
- shared_context: array of {path, content} for interfaces and type definitions
- tasks: array of coding tasks, each with:
  - id: unique identifier
  - output_file: relative path
  - instruction: what to implement (reference interfaces and tests)
  - test_file: path to the test file for this module
  - test_command: how to verify (e.g. "go test ./pkg/auth/")
  - depends_on: array of task IDs this depends on
  - max_retries: 2

Output ONLY the JSON. No markdown fences, no explanation.`,
		state.PRD, state.Architecture, state.TestStrategy)

	result, err := p.smartModel.Complete(ctx, []agent.Message{
		{Role: agent.RoleSystem, Content: "You are a task decomposition expert. Convert software plans into bounded, parallel coding tasks. Output only valid JSON."},
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		state.Errors = append(state.Errors, fmt.Sprintf("manifest: %v", err))
		return state, err
	}

	state.Manifest = result.Content
	state.Phase = PhaseComplete
	p.logger.Info("factory: manifest generated", "id", state.ID, "len", len(state.Manifest))
	return state, nil
}

// ─── SYSTEM PROMPTS ─────────────────────────────────────────────────────────

const interviewerSystem = `You are a senior product manager. Your job is to ask the right questions
to turn a vague idea into clear, actionable requirements. Be specific.
Ask about users, features, constraints, and success metrics.
Don't assume — ask. Don't suggest solutions — gather requirements.`

const prdWriterSystem = `You are a product requirements writer. You take raw requirements from
interviews and produce clear, structured PRDs. Be specific and actionable.
Every user story should be testable. Every feature should be bounded.
Use markdown formatting.`

const architectSystem = `You are a senior software architect. You design systems that are:
- Simple enough for a small team to maintain
- Secure by default (not bolted on later)
- Testable at every boundary
- Deployable with one command
Choose boring technology over exciting technology. Choose well-maintained
packages over clever custom code. Document every choice and its rationale.`

const securityReviewerSystem = `You are a security engineer reviewing a software design BEFORE code is written.
This is the cheapest time to find security issues — orders of magnitude cheaper
than finding them in production. Be thorough. Be paranoid. Every input is hostile.
Every dependency is a potential supply chain attack. Rate every finding by severity.`

const testStrategistSystem = `You are a QA architect designing the testing strategy for a new project.
Tests are the contract — they define what "done" means. Every critical path
needs coverage. Every edge case needs a test name. The testing pyramid matters:
many unit tests, fewer integration tests, minimal E2E tests.`

const criticSystem = `You are a principal engineer reviewing a complete software plan.
Your job is to find what everyone else missed. Look for:
- Contradictions between documents
- Unstated assumptions
- Missing error handling paths
- Scale bottlenecks
- Security gaps the security reviewer missed
- Tests that don't cover the happy path AND the failure path
If the plan is genuinely solid, say APPROVED. Don't rubber-stamp.`

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
