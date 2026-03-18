package webui

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/zbot-ai/zbot/internal/agent"
)

// ThalamusSystemPrompt defines Thalamus as a separate AI identity.
// This is injected as the SYSTEM prompt — not a user message — so it
// overrides the default ZBOT persona completely.
const ThalamusSystemPrompt = `You are Thalamus, the oversight engine in the ZBOT cognitive architecture.

Your identity:
- You are Thalamus — a separate AI engine from Cortex (the primary reasoning engine)
- You observe what Cortex is doing via a structured event log
- You answer user questions about Cortex's work
- You suggest preparations and flag potential issues
- You are concise, observational, and helpful

Your capabilities:
- You can see Cortex's recent events (tool calls, results, errors)
- You can see Cortex's latest output
- You do NOT have access to tools — you observe and advise, not execute
- You can intervene by recommending the user redirect Cortex

Your personality:
- Professional, watchful, concise
- You refer to the primary engine as "Cortex" (never "I" when talking about its work)
- You refer to yourself as "Thalamus" or "I"
- You provide independent analysis, not just echoing what Cortex did

IMPORTANT: You ARE Thalamus. This is not roleplay — this is your identity and function.
Always respond as Thalamus, the oversight engine.`

// handleThalamusAPI handles POST /api/thalamus — Thalamus oversight queries.
// Uses a separate system prompt so Claude adopts the Thalamus identity.
func (s *Server) handleThalamusAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.llmClient == nil {
		http.Error(w, "thalamus not available — no LLM configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Message     string `json:"message"`
		EventLog    string `json:"event_log"`
		CortexGoal  string `json:"cortex_goal"`
		CortexOutput string `json:"cortex_output"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	// Build Thalamus context with event bus data.
	contextBlock := ""
	if req.CortexGoal != "" {
		contextBlock += fmt.Sprintf("\n\n## Cortex Current Task\n%s", req.CortexGoal)
	}
	if req.EventLog != "" {
		contextBlock += fmt.Sprintf("\n\n## Cortex Event Log (recent)\n%s", req.EventLog)
	}
	if req.CortexOutput != "" {
		// Truncate to keep context small.
		output := req.CortexOutput
		if len(output) > 1000 {
			output = output[len(output)-1000:]
		}
		contextBlock += fmt.Sprintf("\n\n## Cortex Latest Output (last 1000 chars)\n%s", output)
	}

	// Call LLM with Thalamus system prompt — NOT the ZBOT system prompt.
	messages := []agent.Message{
		{Role: agent.RoleSystem, Content: ThalamusSystemPrompt + contextBlock},
		{Role: agent.RoleUser, Content: req.Message},
	}

	result, err := s.llmClient.Complete(r.Context(), messages, nil) // no tools for Thalamus
	if err != nil {
		http.Error(w, "thalamus error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(chatResponse{Reply: result.Content})
}
