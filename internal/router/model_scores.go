// Package router selects the optimal LLM for each task based on benchmark
// scores, pricing, and capability flags. Updated March 2026.
//
// The Router classifies incoming tasks into categories, then picks the model
// with the best performance-per-dollar for that category. Benchmark data is
// baked in and can be refreshed without code changes via config overrides.
package router

import (
	"fmt"
	"sort"
)

// ─── Task Categories ────────────────────────────────────────────────────────

// TaskCategory classifies what the LLM needs to do.
type TaskCategory string

const (
	TaskCoding      TaskCategory = "coding"       // Write code, fix bugs, refactor
	TaskCodeReview  TaskCategory = "code_review"   // Review code for issues
	TaskMath        TaskCategory = "math"          // Mathematical reasoning
	TaskReasoning   TaskCategory = "reasoning"     // Complex multi-step logic
	TaskResearch    TaskCategory = "research"      // Gather and synthesize info
	TaskExtract     TaskCategory = "extract"       // Pull structured data from text
	TaskClassify    TaskCategory = "classify"      // Route, label, triage
	TaskWriting     TaskCategory = "writing"       // Prose, emails, docs
	TaskToolUse     TaskCategory = "tool_use"      // Function/tool calling
	TaskMultimodal  TaskCategory = "multimodal"    // Images, video, mixed input
	TaskConversation TaskCategory = "conversation" // Simple chat, Q&A
)

// ─── Capability Flags ───────────────────────────────────────────────────────

type Capability uint32

const (
	CapText       Capability = 1 << iota // Text in/out (all models)
	CapVision                            // Image input
	CapVideo                             // Video input
	CapAudio                             // Audio input
	CapToolCall                          // Native function/tool calling
	CapStreaming                         // Streaming output
	CapJSON                              // Structured JSON output mode
	CapBatch                             // Batch API available
	CapCaching                           // Prompt caching support
	CapExtThink                          // Extended thinking / chain-of-thought
	CapLocalRun                          // Can run locally via Ollama/MLX
)

func (c Capability) Has(flag Capability) bool { return c&flag != 0 }

// ─── Provider ───────────────────────────────────────────────────────────────

type Provider string

const (
	Anthropic  Provider = "anthropic"
	OpenAI     Provider = "openai"
	Google     Provider = "google"
	XAI        Provider = "xai"
	DeepSeek   Provider = "deepseek"
	Together   Provider = "together"
	Groq       Provider = "groq"
	Alibaba    Provider = "alibaba"
	MiniMax    Provider = "minimax"
	ZhipuAI    Provider = "zhipu"
	Ollama     Provider = "ollama"
)

// ─── Model Definition ───────────────────────────────────────────────────────

// ModelSpec holds everything ZBOT needs to select and call a model.
type ModelSpec struct {
	ID           string     // API model identifier (e.g. "claude-sonnet-4-6")
	Name         string     // Human-readable name
	Provider     Provider
	InputPer1M   float64    // USD per 1M input tokens
	OutputPer1M  float64    // USD per 1M output tokens
	CachedPer1M  float64    // USD per 1M cached input tokens (0 = no caching)
	ContextWindow int       // Max tokens
	Capabilities Capability
	Benchmarks   Benchmarks // Raw benchmark scores (0-100 scale)
}

// Benchmarks holds normalized scores (0-100) for key evaluations.
// Sources: SWE-bench, GPQA Diamond, AIME 2025, ARC-AGI-2, HumanEval,
// LiveCodeBench, IFEval, Terminal-Bench, BFCL v3, Chatbot Arena.
// Updated: March 2026.
type Benchmarks struct {
	SWEBenchVerified float64 // Real GitHub issue resolution
	SWEBenchPro      float64 // Harder real-world SWE tasks
	GPQADiamond      float64 // PhD-level science reasoning
	AIME2025         float64 // Competition math (olympiad level)
	ARCAGI2          float64 // Abstract novel reasoning
	HumanEval        float64 // Code generation (function level)
	LiveCodeBench    float64 // Real competitive coding
	TerminalBench    float64 // Command-line / systems tasks
	IFEval           float64 // Instruction following accuracy
	BFCLv3           float64 // Function/tool calling accuracy
	ChatbotArena     float64 // Human preference (Elo, normalized 0-100)
}

// ─── Task → Benchmark Weights ───────────────────────────────────────────────
//
// Each task category maps to a weighted combination of benchmarks.
// Weights sum to 1.0 per category. This is how the Router knows which
// benchmarks matter for which tasks.

type benchWeight struct {
	field  string  // Benchmarks field name
	weight float64
}

var taskBenchWeights = map[TaskCategory][]benchWeight{
	TaskCoding: {
		{"SWEBenchVerified", 0.35},
		{"SWEBenchPro", 0.25},
		{"HumanEval", 0.15},
		{"LiveCodeBench", 0.15},
		{"TerminalBench", 0.10},
	},
	TaskCodeReview: {
		{"SWEBenchPro", 0.40},
		{"SWEBenchVerified", 0.30},
		{"TerminalBench", 0.15},
		{"IFEval", 0.15},
	},
	TaskMath: {
		{"AIME2025", 0.50},
		{"GPQADiamond", 0.30},
		{"ARCAGI2", 0.20},
	},
	TaskReasoning: {
		{"ARCAGI2", 0.35},
		{"GPQADiamond", 0.30},
		{"AIME2025", 0.20},
		{"SWEBenchPro", 0.15},
	},
	TaskResearch: {
		{"GPQADiamond", 0.30},
		{"IFEval", 0.25},
		{"ChatbotArena", 0.25},
		{"LiveCodeBench", 0.20},
	},
	TaskExtract: {
		{"IFEval", 0.40},
		{"BFCLv3", 0.30},
		{"HumanEval", 0.30},
	},
	TaskClassify: {
		{"IFEval", 0.50},
		{"BFCLv3", 0.30},
		{"ChatbotArena", 0.20},
	},
	TaskWriting: {
		{"ChatbotArena", 0.40},
		{"IFEval", 0.30},
		{"GPQADiamond", 0.30},
	},
	TaskToolUse: {
		{"BFCLv3", 0.50},
		{"IFEval", 0.25},
		{"SWEBenchVerified", 0.25},
	},
	TaskMultimodal: {
		{"GPQADiamond", 0.30},
		{"ChatbotArena", 0.30},
		{"HumanEval", 0.20},
		{"IFEval", 0.20},
	},
	TaskConversation: {
		{"ChatbotArena", 0.50},
		{"IFEval", 0.30},
		{"GPQADiamond", 0.20},
	},
}

// ─── Model Registry ─────────────────────────────────────────────────────────
//
// Benchmark data sourced from: SWE-bench leaderboard, Vals AI, BenchLM,
// LXT, morphllm, official model papers. Updated March 2026.
// Scores of 0 mean "not tested" — the router ignores these fields.

var DefaultModels = []ModelSpec{
	// ── Anthropic ───────────────────────────────────────────────────────
	{
		ID: "claude-opus-4-6", Name: "Claude Opus 4.6",
		Provider: Anthropic, InputPer1M: 5.00, OutputPer1M: 25.00, CachedPer1M: 0.50,
		ContextWindow: 200_000,
		Capabilities:  CapText | CapVision | CapToolCall | CapStreaming | CapJSON | CapBatch | CapCaching | CapExtThink,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 80.8, SWEBenchPro: 45.9, GPQADiamond: 88.0,
			AIME2025: 82.0, ARCAGI2: 37.6, HumanEval: 93.0,
			LiveCodeBench: 78.0, TerminalBench: 59.3, IFEval: 87.0,
			BFCLv3: 88.0, ChatbotArena: 92.0,
		},
	},
	{
		ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6",
		Provider: Anthropic, InputPer1M: 3.00, OutputPer1M: 15.00, CachedPer1M: 0.30,
		ContextWindow: 200_000,
		Capabilities:  CapText | CapVision | CapToolCall | CapStreaming | CapJSON | CapBatch | CapCaching | CapExtThink,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 79.6, SWEBenchPro: 43.6, GPQADiamond: 88.0,
			AIME2025: 78.0, ARCAGI2: 34.0, HumanEval: 92.0,
			LiveCodeBench: 76.0, TerminalBench: 55.0, IFEval: 87.0,
			BFCLv3: 87.0, ChatbotArena: 91.0,
		},
	},
	{
		ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5",
		Provider: Anthropic, InputPer1M: 0.25, OutputPer1M: 1.25, CachedPer1M: 0.025,
		ContextWindow: 200_000,
		Capabilities:  CapText | CapVision | CapToolCall | CapStreaming | CapJSON | CapBatch | CapCaching,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 65.0, SWEBenchPro: 0, GPQADiamond: 80.0,
			AIME2025: 55.0, ARCAGI2: 0, HumanEval: 85.0,
			LiveCodeBench: 60.0, TerminalBench: 0, IFEval: 82.0,
			BFCLv3: 82.0, ChatbotArena: 80.0,
		},
	},

	// ── OpenAI ──────────────────────────────────────────────────────────
	{
		ID: "gpt-5.4", Name: "GPT-5.4",
		Provider: OpenAI, InputPer1M: 2.50, OutputPer1M: 15.00, CachedPer1M: 0.625,
		ContextWindow: 1_000_000,
		Capabilities:  CapText | CapVision | CapToolCall | CapStreaming | CapJSON | CapBatch | CapCaching,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 80.0, SWEBenchPro: 57.7, GPQADiamond: 88.4,
			AIME2025: 100.0, ARCAGI2: 52.9, HumanEval: 95.0,
			LiveCodeBench: 80.0, TerminalBench: 52.0, IFEval: 86.0,
			BFCLv3: 85.0, ChatbotArena: 91.0,
		},
	},
	{
		ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini",
		Provider: OpenAI, InputPer1M: 0.40, OutputPer1M: 1.60, CachedPer1M: 0.10,
		ContextWindow: 1_000_000,
		Capabilities:  CapText | CapVision | CapToolCall | CapStreaming | CapJSON | CapBatch | CapCaching,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 68.0, SWEBenchPro: 0, GPQADiamond: 78.0,
			AIME2025: 70.0, ARCAGI2: 0, HumanEval: 88.0,
			LiveCodeBench: 65.0, TerminalBench: 0, IFEval: 84.0,
			BFCLv3: 83.0, ChatbotArena: 82.0,
		},
	},

	// ── Google ──────────────────────────────────────────────────────────
	{
		ID: "gemini-3.1-pro", Name: "Gemini 3.1 Pro",
		Provider: Google, InputPer1M: 2.00, OutputPer1M: 12.00, CachedPer1M: 0.50,
		ContextWindow: 1_000_000,
		Capabilities:  CapText | CapVision | CapVideo | CapAudio | CapToolCall | CapStreaming | CapJSON | CapCaching,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 80.6, SWEBenchPro: 54.2, GPQADiamond: 94.3,
			AIME2025: 92.0, ARCAGI2: 77.1, HumanEval: 92.0,
			LiveCodeBench: 84.9, TerminalBench: 68.5, IFEval: 87.0,
			BFCLv3: 84.0, ChatbotArena: 90.0,
		},
	},
	{
		ID: "gemini-3-flash", Name: "Gemini 3 Flash",
		Provider: Google, InputPer1M: 0.50, OutputPer1M: 3.00, CachedPer1M: 0.0625,
		ContextWindow: 1_000_000,
		Capabilities:  CapText | CapVision | CapVideo | CapAudio | CapToolCall | CapStreaming | CapJSON | CapCaching,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 78.0, SWEBenchPro: 0, GPQADiamond: 90.4,
			AIME2025: 80.0, ARCAGI2: 0, HumanEval: 89.0,
			LiveCodeBench: 72.0, TerminalBench: 0, IFEval: 85.0,
			BFCLv3: 82.0, ChatbotArena: 86.0,
		},
	},

	// ── xAI ─────────────────────────────────────────────────────────────
	{
		ID: "grok-4.1-fast", Name: "Grok 4.1 Fast",
		Provider: XAI, InputPer1M: 0.20, OutputPer1M: 0.50, CachedPer1M: 0,
		ContextWindow: 131_072,
		Capabilities:  CapText | CapVision | CapToolCall | CapStreaming | CapJSON,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 70.0, SWEBenchPro: 0, GPQADiamond: 88.0,
			AIME2025: 75.0, ARCAGI2: 0, HumanEval: 88.0,
			LiveCodeBench: 68.0, TerminalBench: 0, IFEval: 83.0,
			BFCLv3: 80.0, ChatbotArena: 85.0,
		},
	},

	// ── DeepSeek ────────────────────────────────────────────────────────
	{
		ID: "deepseek-v3.2", Name: "DeepSeek V3.2",
		Provider: DeepSeek, InputPer1M: 0.28, OutputPer1M: 0.42, CachedPer1M: 0.07,
		ContextWindow: 128_000,
		Capabilities:  CapText | CapToolCall | CapStreaming | CapJSON | CapLocalRun,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 73.0, SWEBenchPro: 0, GPQADiamond: 79.9,
			AIME2025: 89.3, ARCAGI2: 0, HumanEval: 89.6,
			LiveCodeBench: 74.1, TerminalBench: 0, IFEval: 85.0,
			BFCLv3: 82.0, ChatbotArena: 86.0,
		},
	},

	// ── MiniMax ─────────────────────────────────────────────────────────
	{
		ID: "minimax-m2.5", Name: "MiniMax M2.5",
		Provider: MiniMax, InputPer1M: 0.30, OutputPer1M: 1.20, CachedPer1M: 0,
		ContextWindow: 205_000,
		Capabilities:  CapText | CapToolCall | CapStreaming | CapJSON | CapLocalRun,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 80.2, SWEBenchPro: 0, GPQADiamond: 85.2,
			AIME2025: 86.3, ARCAGI2: 0, HumanEval: 89.6,
			LiveCodeBench: 0, TerminalBench: 0, IFEval: 87.5,
			BFCLv3: 82.0, ChatbotArena: 87.0,
		},
	},

	// ── Zhipu AI ────────────────────────────────────────────────────────
	{
		ID: "glm-4.7", Name: "GLM-4.7",
		Provider: ZhipuAI, InputPer1M: 0.50, OutputPer1M: 2.00, CachedPer1M: 0,
		ContextWindow: 200_000,
		Capabilities:  CapText | CapToolCall | CapStreaming | CapJSON | CapLocalRun,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 77.8, SWEBenchPro: 0, GPQADiamond: 86.0,
			AIME2025: 95.7, ARCAGI2: 0, HumanEval: 94.2,
			LiveCodeBench: 84.9, TerminalBench: 0, IFEval: 88.0,
			BFCLv3: 83.0, ChatbotArena: 89.0,
		},
	},

	// ── Alibaba (Qwen) ─────────────────────────────────────────────────
	{
		ID: "qwen3.5-plus", Name: "Qwen 3.5 Plus",
		Provider: Alibaba, InputPer1M: 0.50, OutputPer1M: 2.00, CachedPer1M: 0,
		ContextWindow: 128_000,
		Capabilities:  CapText | CapVision | CapToolCall | CapStreaming | CapJSON | CapLocalRun,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 70.0, SWEBenchPro: 0, GPQADiamond: 82.0,
			AIME2025: 91.3, ARCAGI2: 0, HumanEval: 90.0,
			LiveCodeBench: 83.6, TerminalBench: 0, IFEval: 85.0,
			BFCLv3: 82.0, ChatbotArena: 86.0,
		},
	},

	// ── Moonshot (Kimi) ─────────────────────────────────────────────────
	{
		ID: "kimi-k2.5", Name: "Kimi K2.5",
		Provider: Together, InputPer1M: 0.40, OutputPer1M: 1.60, CachedPer1M: 0,
		ContextWindow: 256_000,
		Capabilities:  CapText | CapToolCall | CapStreaming | CapJSON | CapLocalRun,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 76.8, SWEBenchPro: 0, GPQADiamond: 80.0,
			AIME2025: 80.0, ARCAGI2: 0, HumanEval: 88.0,
			LiveCodeBench: 85.0, TerminalBench: 0, IFEval: 84.0,
			BFCLv3: 82.0, ChatbotArena: 85.0,
		},
	},

	// ── Groq (fast inference) ───────────────────────────────────────────
	{
		ID: "groq-llama-3.3-70b", Name: "Groq Llama 3.3 70B",
		Provider: Groq, InputPer1M: 0.05, OutputPer1M: 0.10, CachedPer1M: 0,
		ContextWindow: 128_000,
		Capabilities:  CapText | CapToolCall | CapStreaming | CapJSON,
		Benchmarks: Benchmarks{
			SWEBenchVerified: 55.0, SWEBenchPro: 0, GPQADiamond: 65.0,
			AIME2025: 40.0, ARCAGI2: 0, HumanEval: 78.0,
			LiveCodeBench: 50.0, TerminalBench: 0, IFEval: 78.0,
			BFCLv3: 75.0, ChatbotArena: 75.0,
		},
	},
}

// ─── Router ─────────────────────────────────────────────────────────────────

// Router selects the best model for a given task category and constraints.
type Router struct {
	models      []ModelSpec
	preferences Preferences
}

// Preferences tune the Router's behavior.
type Preferences struct {
	MaxCostPerQuery  float64  // Max USD per query (0 = no limit)
	PreferAmerican   bool     // Prefer American companies (Anthropic, OpenAI, xAI, Google)
	RequireToolCall  bool     // Only models with CapToolCall
	RequireVision    bool     // Only models with CapVision
	RequireLocal     bool     // Only models that can run locally
	BlockedProviders []Provider // Never use these providers
	BlockedModels    []string // Never use these model IDs
}

// NewRouter creates a Router with the default model registry.
func NewRouter(prefs Preferences) *Router {
	return &Router{models: DefaultModels, preferences: prefs}
}

// Recommendation is a scored model pick.
type Recommendation struct {
	Model          ModelSpec
	TaskScore      float64 // Weighted benchmark score for this task (0-100)
	CostEfficiency float64 // TaskScore / cost_per_1K_output_tokens
	Reason         string  // Human-readable explanation
}

// Route picks the best model for a task category.
// Returns up to 3 recommendations sorted by cost-efficiency.
func (r *Router) Route(task TaskCategory) []Recommendation {
	weights, ok := taskBenchWeights[task]
	if !ok {
		weights = taskBenchWeights[TaskConversation] // fallback
	}

	var recs []Recommendation
	for _, m := range r.models {
		if r.isBlocked(m) {
			continue
		}
		if r.preferences.RequireToolCall && !m.Capabilities.Has(CapToolCall) {
			continue
		}
		if r.preferences.RequireVision && !m.Capabilities.Has(CapVision) {
			continue
		}
		if r.preferences.RequireLocal && !m.Capabilities.Has(CapLocalRun) {
			continue
		}

		score := computeTaskScore(m.Benchmarks, weights)
		if score == 0 {
			continue
		}

		// Cost efficiency = score / (output cost per 1K tokens).
		// Higher = more bang for buck.
		costPer1K := m.OutputPer1M / 1000.0
		if costPer1K == 0 {
			costPer1K = 0.0001 // prevent div by zero for free models
		}
		efficiency := score / costPer1K

		// Apply max cost filter (estimate: ~500 output tokens per query).
		if r.preferences.MaxCostPerQuery > 0 {
			estCost := (500.0 / 1_000_000.0) * m.OutputPer1M
			if estCost > r.preferences.MaxCostPerQuery {
				continue
			}
		}

		reason := fmt.Sprintf("%s: score=%.1f, $%.2f/1M out, efficiency=%.0f",
			m.Name, score, m.OutputPer1M, efficiency)

		recs = append(recs, Recommendation{
			Model:          m,
			TaskScore:      score,
			CostEfficiency: efficiency,
			Reason:         reason,
		})
	}

	// Sort by cost-efficiency descending (best value first).
	sort.Slice(recs, func(i, j int) bool {
		return recs[i].CostEfficiency > recs[j].CostEfficiency
	})

	if len(recs) > 3 {
		recs = recs[:3]
	}
	return recs
}

// BestModel returns the single best model for a task.
// If preferQuality is true, picks highest score regardless of cost.
// If false, picks best cost-efficiency.
func (r *Router) BestModel(task TaskCategory, preferQuality bool) *Recommendation {
	recs := r.Route(task)
	if len(recs) == 0 {
		return nil
	}
	if preferQuality {
		// Re-sort by raw score.
		sort.Slice(recs, func(i, j int) bool {
			return recs[i].TaskScore > recs[j].TaskScore
		})
	}
	return &recs[0]
}

// ─── Scoring Helpers ────────────────────────────────────────────────────────

func computeTaskScore(b Benchmarks, weights []benchWeight) float64 {
	var score float64
	var totalWeight float64
	for _, w := range weights {
		val := getBenchField(b, w.field)
		if val == 0 {
			continue // Skip benchmarks with no data.
		}
		score += val * w.weight
		totalWeight += w.weight
	}
	if totalWeight == 0 {
		return 0
	}
	return score / totalWeight // Normalize for missing benchmarks.
}

func getBenchField(b Benchmarks, field string) float64 {
	switch field {
	case "SWEBenchVerified":
		return b.SWEBenchVerified
	case "SWEBenchPro":
		return b.SWEBenchPro
	case "GPQADiamond":
		return b.GPQADiamond
	case "AIME2025":
		return b.AIME2025
	case "ARCAGI2":
		return b.ARCAGI2
	case "HumanEval":
		return b.HumanEval
	case "LiveCodeBench":
		return b.LiveCodeBench
	case "TerminalBench":
		return b.TerminalBench
	case "IFEval":
		return b.IFEval
	case "BFCLv3":
		return b.BFCLv3
	case "ChatbotArena":
		return b.ChatbotArena
	default:
		return 0
	}
}

func (r *Router) isBlocked(m ModelSpec) bool {
	for _, p := range r.preferences.BlockedProviders {
		if m.Provider == p {
			return true
		}
	}
	for _, id := range r.preferences.BlockedModels {
		if m.ID == id {
			return true
		}
	}
	if r.preferences.PreferAmerican {
		switch m.Provider {
		case Anthropic, OpenAI, Google, XAI:
			// OK
		default:
			return true
		}
	}
	return false
}


