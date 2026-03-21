package mcpbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/ziloss-tech/zbot/internal/skills"
)

// LoadFromConfig reads MCP server configurations and registers them as skills.
// Config sources (checked in order):
//   1. ZBOT_MCP_SERVERS env var (JSON array of ServerConfig)
//   2. config file at configPath (JSON array of ServerConfig)
//
// Returns the list of connected skills (caller should Close() them on shutdown).
func LoadFromConfig(ctx context.Context, registry *skills.Registry, configPath string, logger *slog.Logger) ([]*Skill, error) {
	var configs []ServerConfig

	// Try env var first.
	if envJSON := os.Getenv("ZBOT_MCP_SERVERS"); envJSON != "" {
		if err := json.Unmarshal([]byte(envJSON), &configs); err != nil {
			return nil, fmt.Errorf("mcpbridge: parse ZBOT_MCP_SERVERS: %w", err)
		}
		logger.Info("MCP servers loaded from env", "count", len(configs))
	}

	// Try config file.
	if len(configs) == 0 && configPath != "" {
		data, err := os.ReadFile(configPath)
		if err == nil {
			if err := json.Unmarshal(data, &configs); err != nil {
				return nil, fmt.Errorf("mcpbridge: parse %s: %w", configPath, err)
			}
			logger.Info("MCP servers loaded from file", "path", configPath, "count", len(configs))
		}
	}

	if len(configs) == 0 {
		return nil, nil
	}

	var connected []*Skill
	for _, cfg := range configs {
		// Resolve vault: prefixed env vars.
		for k, v := range cfg.Env {
			if strings.HasPrefix(v, "vault:") {
				// TODO: resolve from ZBOT vault when vault integration is wired.
				// For now, fall back to env var with the same name.
				envKey := strings.TrimPrefix(v, "vault:")
				cfg.Env[k] = os.Getenv(envKey)
			}
		}

		client, err := NewClient(ctx, cfg, logger)
		if err != nil {
			logger.Error("MCP server failed to connect", "name", cfg.Name, "err", err)
			continue // Don't fail the whole app — skip broken servers.
		}

		skill := NewSkill(client, cfg.Description)
		registry.Register(skill)
		connected = append(connected, skill)

		toolNames := make([]string, len(client.tools))
		for i, t := range client.tools {
			toolNames[i] = t.Name
		}
		logger.Info("skill registered via MCP bridge",
			"name", cfg.Name,
			"tools", len(client.tools),
			"tool_names", toolNames,
		)
	}

	return connected, nil
}
