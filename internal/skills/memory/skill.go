// Package memory provides the memory skill for ZBOT.
// Exposes save_memory and search_memory tools so Claude can persist
// and recall facts across sessions.
package memory

import (
	"github.com/jeremylerwick-max/zbot/internal/agent"
)

// Skill implements the skills.Skill interface for persistent memory.
type Skill struct {
	store agent.MemoryStore
}

// NewSkill creates a memory skill backed by the given store.
func NewSkill(store agent.MemoryStore) *Skill {
	return &Skill{store: store}
}

func (s *Skill) Name() string        { return "memory" }
func (s *Skill) Description() string { return "Cross-session persistent memory for ZBOT" }

func (s *Skill) Tools() []agent.Tool {
	return []agent.Tool{
		NewSaveMemoryTool(s.store),
		NewSearchMemoryTool(s.store),
	}
}

func (s *Skill) SystemPromptAddendum() string {
	return `### Memory (save_memory / search_memory)
You have persistent long-term memory that survives across sessions.
- Use save_memory when you learn something important about Jeremy, his business, preferences, or discover a useful insight during a task.
- Use search_memory when Jeremy references something from the past or when context from previous sessions would help.
- Categories for save_memory: "preference", "business", "technical", "personal", "workflow_insight"
- Memory is automatically searched at the start of each turn, but you can search explicitly for more targeted recall.`
}
