// memcli is a CLI tool for inspecting, searching, and deleting ZBOT memories.
// It connects directly to the same Postgres/pgvector instance ZBOT uses.
//
// Usage:
//
//	memcli list [--limit N]     → list most recent N memories (default 20)
//	memcli search <query>       → semantic + BM25 search
//	memcli delete <id>          → delete by ID
//	memcli stats                → count, oldest, newest
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/zbot-ai/zbot/internal/memory"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	// Connect to Postgres.
	pgDB, err := connectPostgres(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to Postgres: %v\n", err)
		os.Exit(1)
	}
	defer pgDB.Close()

	// Try Vertex AI embedder; fall back to noop for BM25-only search.
	var embedder memory.Embedder
	vertexEmbed, vertexErr := memory.NewVertexEmbedder(ctx, os.Getenv("ZBOT_GCP_PROJECT"), "us-central1", logger)
	if vertexErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ Vertex AI unavailable — using BM25-only search\n")
		embedder = memory.NoopEmbedder{}
	} else {
		embedder = vertexEmbed
		defer vertexEmbed.Close()
	}

	store, err := memory.New(ctx, pgDB, embedder, logger, "zbot")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing memory store: %v\n", err)
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "list":
		limit := 20
		if len(os.Args) > 2 {
			if os.Args[2] == "--limit" && len(os.Args) > 3 {
				if n, err := strconv.Atoi(os.Args[3]); err == nil && n > 0 {
					limit = n
				}
			} else if n, err := strconv.Atoi(os.Args[2]); err == nil && n > 0 {
				limit = n
			}
		}
		cmdList(ctx, store, limit)

	case "search":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: memcli search <query>\n")
			os.Exit(1)
		}
		query := strings.Join(os.Args[2:], " ")
		cmdSearch(ctx, store, query)

	case "delete":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: memcli delete <id>\n")
			os.Exit(1)
		}
		cmdDelete(ctx, store, os.Args[2])

	case "stats":
		cmdStats(ctx, store)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func cmdList(ctx context.Context, store *memory.Store, limit int) {
	facts, err := store.List(ctx, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing memories: %v\n", err)
		os.Exit(1)
	}

	if len(facts) == 0 {
		fmt.Println("No memories found.")
		return
	}

	// Print header.
	fmt.Printf("%-16s  %-14s  %-12s  %s\n", "ID", "CREATED", "SOURCE", "CONTENT")
	fmt.Println(strings.Repeat("─", 100))

	for _, f := range facts {
		age := formatAge(f.CreatedAt)
		content := f.Content
		if len(content) > 80 {
			content = content[:77] + "..."
		}
		// Replace newlines with spaces for clean table output.
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Printf("%-16s  %-14s  %-12s  %s\n", truncID(f.ID), age, f.Source, content)
	}

	fmt.Printf("\n(%d memories shown)\n", len(facts))
}

func cmdSearch(ctx context.Context, store *memory.Store, query string) {
	facts, err := store.Search(ctx, query, 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error searching: %v\n", err)
		os.Exit(1)
	}

	if len(facts) == 0 {
		fmt.Printf("No memories found for: %q\n", query)
		return
	}

	fmt.Printf("Search results for: %q\n\n", query)
	fmt.Printf("%-16s  %-14s  %-6s  %-12s  %s\n", "ID", "CREATED", "SCORE", "SOURCE", "CONTENT")
	fmt.Println(strings.Repeat("─", 110))

	for _, f := range facts {
		age := formatAge(f.CreatedAt)
		content := f.Content
		if len(content) > 70 {
			content = content[:67] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Printf("%-16s  %-14s  %-6.3f  %-12s  %s\n", truncID(f.ID), age, f.Score, f.Source, content)
	}

	fmt.Printf("\n(%d results)\n", len(facts))
}

func cmdDelete(ctx context.Context, store *memory.Store, id string) {
	if err := store.Delete(ctx, id); err != nil {
		fmt.Fprintf(os.Stderr, "Error deleting memory: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Deleted memory: %s\n", id)
}

func cmdStats(ctx context.Context, store *memory.Store) {
	stats, err := store.Stats(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting stats: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("ZBOT Memory Stats")
	fmt.Println(strings.Repeat("─", 40))
	fmt.Printf("Total memories:  %d\n", stats.Total)
	if stats.Total > 0 {
		fmt.Printf("Oldest:          %s (%s)\n", stats.Oldest.Format("2006-01-02 15:04"), formatAge(stats.Oldest))
		fmt.Printf("Newest:          %s (%s)\n", stats.Newest.Format("2006-01-02 15:04"), formatAge(stats.Newest))
	}
}

func printUsage() {
	fmt.Println(`memcli — ZBOT Memory Viewer

Usage:
  memcli list [--limit N]     List most recent N memories (default 20)
  memcli search <query>       Semantic + BM25 search
  memcli delete <id>          Delete a memory by ID
  memcli stats                Show memory statistics`)
}

// connectPostgres connects to the same Cloud SQL instance ZBOT uses.
// Password comes from ZBOT_DB_PASSWORD env var (same fallback as cmd/zbot/wire.go).
func connectPostgres(ctx context.Context) (*pgxpool.Pool, error) {
	dbPass := os.Getenv("ZBOT_DB_PASSWORD")
	if dbPass == "" {
		return nil, fmt.Errorf("ZBOT_DB_PASSWORD env var not set — export it or source your .env")
	}
	connStr := fmt.Sprintf("postgresql://zbot:%s@localhost:5432/zbot?sslmode=disable", dbPass)
	poolCfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	poolCfg.MaxConns = 2
	poolCfg.MinConns = 1

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

// formatAge returns a human-readable time-since string.
func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

// truncID shortens IDs for display, showing first 16 chars.
func truncID(id string) string {
	if len(id) > 16 {
		return id[:16]
	}
	return id
}
