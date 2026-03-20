// main.go is the composition root for ZBOT.
// It wires all adapters to their ports and starts the application.
// Business logic lives in internal/ — this file only wires things together.
// Target: ~80 lines. If this file grows, something is wrong architecturally.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ziloss-tech/zbot/internal/platform"
)

func main() {
	// ── Config ──────────────────────────────────────────────────────────────
	env := os.Getenv("ZBOT_ENV")
	if env == "" {
		env = "development"
	}

	cfg := platform.DefaultAppConfig()
	cfg.Env = env

	// Allow env var overrides for Docker/Coolify deployment.
	if gcpProject := os.Getenv("ZBOT_GCP_PROJECT"); gcpProject != "" {
		cfg.GCPProject = gcpProject
	}
	// In production with env var secrets available, skip GCP Secret Manager.
	if env == "production" && os.Getenv("ZBOT_ANTHROPIC_API_KEY") != "" {
		cfg.GCPProject = ""
	}

	logger := platform.NewLogger(env)
	logger.Info("ZBOT starting", "env", env)

	// ── Context with graceful shutdown ──────────────────────────────────────
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// ── Wire adapters ───────────────────────────────────────────────────────
	// All wiring happens in run() to keep main() clean and testable.
	if err := run(ctx, cfg, logger); err != nil {
		log.Fatalf("ZBOT fatal error: %v", err)
	}

	logger.Info("ZBOT shutdown complete")
}
