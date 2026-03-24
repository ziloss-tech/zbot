package router

import "strings"

// ClassifyTask maps a natural language task description to a TaskCategory.
// This is intentionally simple keyword-based — runs locally, no LLM call.
// For ambiguous cases, falls back to TaskConversation.
//
// Priority order matters: more specific categories are checked first.
func ClassifyTask(description string) TaskCategory {
	d := strings.ToLower(description)

	// ── Check specific/narrow categories first ──────────────────────────

	// Multimodal signals (check before reasoning — "analyze screenshot" is multimodal).
	mmWords := []string{"image", "photo", "screenshot", "video", "picture", "diagram",
		"chart", "visual", "scan"}
	for _, w := range mmWords {
		if strings.Contains(d, w) {
			return TaskMultimodal
		}
	}

	// Code review signals (check before coding — "review code" is review, not coding).
	reviewWords := []string{"review", "audit code", "lint", "pr review",
		"code quality", "vulnerability", "security scan", "check this code"}
	for _, w := range reviewWords {
		if strings.Contains(d, w) {
			return TaskCodeReview
		}
	}

	// Classification signals (check before coding — "classify" contains "class").
	classifyWords := []string{"classify", "categorize", "label", "triage", "route",
		"sort into", "which category"}
	for _, w := range classifyWords {
		if strings.Contains(d, w) {
			return TaskClassify
		}
	}

	// Math signals.
	mathWords := []string{"calculate", "equation", "formula", "derivative", "integral",
		"proof", "theorem", "algebra", "geometry", "statistics", "probability", "math"}
	for _, w := range mathWords {
		if strings.Contains(d, w) {
			return TaskMath
		}
	}

	// Research signals.
	researchWords := []string{"research", "find out", "investigate", "deep dive",
		"sources", "literature", "survey", "benchmark", "compare models"}
	for _, w := range researchWords {
		if strings.Contains(d, w) {
			return TaskResearch
		}
	}

	// Coding signals (before extract — "parse JSON" is coding, not extraction).
	codingWords := []string{"write code", "implement", "refactor", "function", "bug fix",
		"compile", "build", "golang", "go function", "python", "typescript", "javascript",
		"react", "component", "api endpoint", "migration", "crud", "module",
		"struct", "interface", "package", "deploy", "dockerfile", "makefile",
		"parse json", "parse xml", "parse csv"}
	for _, w := range codingWords {
		if strings.Contains(d, w) {
			return TaskCoding
		}
	}

	// Extraction signals (after coding — "parse" is ambiguous).
	extractWords := []string{"extract", "pull data", "structured data",
		"json from", "csv from", "table from", "scrape", "extract from"}
	for _, w := range extractWords {
		if strings.Contains(d, w) {
			return TaskExtract
		}
	}

	// Tool use signals.
	toolWords := []string{"call function", "tool", "api call", "webhook", "integration",
		"ghl", "slack", "calendar", "search the web"}
	for _, w := range toolWords {
		if strings.Contains(d, w) {
			return TaskToolUse
		}
	}

	// Reasoning signals.
	reasonWords := []string{"reason", "analyze", "compare", "evaluate", "trade-off",
		"architecture", "design system", "plan", "strategy", "decision"}
	for _, w := range reasonWords {
		if strings.Contains(d, w) {
			return TaskReasoning
		}
	}

	// Writing signals.
	writeWords := []string{"write", "draft", "email", "blog post", "article",
		"documentation", "readme", "report", "memo", "letter", "essay", "copy"}
	for _, w := range writeWords {
		if strings.Contains(d, w) {
			return TaskWriting
		}
	}

	return TaskConversation
}
