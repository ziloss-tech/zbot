// Package skills provides the extensible skills system for ZBOT.
// A Skill is a named collection of tools with its own system prompt addendum.
// Skills are registered at startup and their tools are injected into the agent.
package skills

import (
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// Skill is a named collection of tools with a description and permission set.
type Skill interface {
	// Name returns the skill identifier (e.g. "ghl", "github", "sheets")
	Name() string

	// Description returns a human-readable summary of what this skill does.
	Description() string

	// Tools returns all agent.Tool implementations this skill provides.
	Tools() []agent.Tool

	// SystemPromptAddendum returns additional system prompt text specific to this skill.
	// Return "" if no additional context is needed.
	SystemPromptAddendum() string
}

// Registry holds all registered skills.
type Registry struct {
	skills map[string]Skill
}

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]Skill)}
}

// Register adds a skill to the registry.
func (r *Registry) Register(s Skill) {
	r.skills[s.Name()] = s
}

// AllTools returns every tool from every registered skill.
func (r *Registry) AllTools() []agent.Tool {
	var all []agent.Tool
	for _, s := range r.skills {
		all = append(all, s.Tools()...)
	}
	return all
}

// SystemPromptAddendum returns combined addenda from all skills.
func (r *Registry) SystemPromptAddendum() string {
	var parts []string
	for _, s := range r.skills {
		if add := s.SystemPromptAddendum(); add != "" {
			parts = append(parts, add)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "\n\n## INTEGRATED SKILLS\n\n" + strings.Join(parts, "\n\n")
}

// Names returns the names of all registered skills.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	return names
}
