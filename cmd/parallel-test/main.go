package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ziloss-tech/zbot/internal/llm"
	"github.com/ziloss-tech/zbot/internal/parallel"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ollamaClient := llm.NewOpenAICompatClient(
		"http://localhost:11434/v1",
		"ollama",
		"qwen2.5-coder:32b",
		logger,
	)

	dispatcher := parallel.NewDispatcher(ollamaClient, 2, logger)

	manifest, err := parallel.LoadManifest("test_manifest.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== Parallel Dispatch: %s ===\n", manifest.ProjectName)
	fmt.Printf("Tasks: %d\n", len(manifest.Tasks))
	fmt.Printf("Coder: %s\n", ollamaClient.ModelName())
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	results, err := dispatcher.Run(ctx, *manifest)
	elapsed := time.Since(start)

	if err != nil {
		fmt.Fprintf(os.Stderr, "dispatch error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("=== RESULTS ===")
	success, failed := 0, 0
	for _, r := range results {
		icon := "✅"
		if r.Status != "success" {
			icon = "❌"
			failed++
		} else {
			success++
		}
		fmt.Printf("%s %s → %s (%d attempts, %s)\n", icon, r.TaskID, r.OutputFile, r.Attempts, r.Duration.Round(time.Millisecond))
		if r.Error != "" {
			limit := len(r.Error)
			if limit > 300 {
				limit = 300
			}
			fmt.Printf("   Error: %s\n", r.Error[:limit])
		}
	}
	fmt.Printf("\nTotal: %d success, %d failed, %s elapsed\n", success, failed, elapsed.Round(time.Millisecond))
}
