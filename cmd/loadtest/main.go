// Package main implements a load test for ZBOT's agent handler.
// Simulates 20 concurrent Slack users each sending 5 messages.
// Bypasses Slack — calls the handler function directly.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/jeremylerwick-max/zbot/internal/agent"
	"github.com/jeremylerwick-max/zbot/internal/audit"
	"github.com/jeremylerwick-max/zbot/internal/memory"
)

const (
	concurrentUsers    = 20
	messagesPerUser    = 5
	totalMessages      = concurrentUsers * messagesPerUser
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	fmt.Println("═══════════════════════════════════════════")
	fmt.Println("  ZBOT Load Test — Sprint 9")
	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("  Concurrent users: %d\n", concurrentUsers)
	fmt.Printf("  Messages per user: %d\n", messagesPerUser)
	fmt.Printf("  Total messages:    %d\n", totalMessages)
	fmt.Println()

	// Build a minimal agent with an in-memory store and a mock LLM.
	memStore := memory.NewInMemoryStore(logger)
	auditLog := audit.NewNoopLogger(logger)
	mockLLM := &mockLLMClient{}

	cfg := agent.DefaultConfig()
	cfg.SystemPrompt = "You are a test agent."

	ag := agent.New(cfg, mockLLM, memStore, auditLog, logger)

	// Capture memory stats before.
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	goroutinesBefore := runtime.NumGoroutine()

	// Run the load test.
	var (
		mu        sync.Mutex
		latencies []time.Duration
		errors    int
	)

	var wg sync.WaitGroup
	start := time.Now()

	for u := 0; u < concurrentUsers; u++ {
		wg.Add(1)
		go func(userIdx int) {
			defer wg.Done()
			for m := 0; m < messagesPerUser; m++ {
				msgStart := time.Now()
				sessionID := fmt.Sprintf("loadtest-session-%d", userIdx)
				userMsg := agent.Message{
					Role:      agent.RoleUser,
					SessionID: sessionID,
					Content:   fmt.Sprintf("Load test message %d from user %d", m+1, userIdx),
					CreatedAt: time.Now(),
				}

				input := agent.TurnInput{
					SessionID: sessionID,
					UserMsg:   userMsg,
				}

				_, err := ag.Run(context.Background(), input)
				elapsed := time.Since(msgStart)

				mu.Lock()
				latencies = append(latencies, elapsed)
				if err != nil {
					errors++
				}
				mu.Unlock()
			}
		}(u)
	}

	wg.Wait()
	totalDuration := time.Since(start)

	// Capture memory stats after.
	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)
	goroutinesAfter := runtime.NumGoroutine()

	// Calculate percentiles.
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	p50 := percentile(latencies, 0.50)
	p95 := percentile(latencies, 0.95)
	p99 := percentile(latencies, 0.99)
	errorRate := float64(errors) / float64(totalMessages) * 100

	// Print results.
	fmt.Println("─── Results ────────────────────────────────")
	fmt.Printf("  Total duration:    %s\n", totalDuration.Round(time.Millisecond))
	fmt.Printf("  Throughput:        %.1f msgs/sec\n", float64(totalMessages)/totalDuration.Seconds())
	fmt.Println()
	fmt.Println("─── Latency ────────────────────────────────")
	fmt.Printf("  P50:   %s\n", p50.Round(time.Millisecond))
	fmt.Printf("  P95:   %s\n", p95.Round(time.Millisecond))
	fmt.Printf("  P99:   %s\n", p99.Round(time.Millisecond))
	fmt.Println()
	fmt.Println("─── Reliability ────────────────────────────")
	fmt.Printf("  Errors:     %d / %d\n", errors, totalMessages)
	fmt.Printf("  Error rate: %.1f%%\n", errorRate)
	fmt.Println()
	fmt.Println("─── Resources ──────────────────────────────")
	fmt.Printf("  Goroutines before: %d\n", goroutinesBefore)
	fmt.Printf("  Goroutines after:  %d\n", goroutinesAfter)
	fmt.Printf("  Goroutine delta:   %+d\n", goroutinesAfter-goroutinesBefore)
	fmt.Printf("  Heap alloc before: %s\n", formatBytes(memBefore.HeapAlloc))
	fmt.Printf("  Heap alloc after:  %s\n", formatBytes(memAfter.HeapAlloc))
	fmt.Printf("  Heap delta:        %s\n", formatBytes(memAfter.HeapAlloc-memBefore.HeapAlloc))
	fmt.Println()

	// Pass/fail verdict.
	pass := true
	fmt.Println("─── Verdict ────────────────────────────────")
	if p95 > 10*time.Second {
		fmt.Printf("  ❌ FAIL: P95 latency %s > 10s target\n", p95)
		pass = false
	} else {
		fmt.Printf("  ✅ PASS: P95 latency %s < 10s target\n", p95)
	}
	if errorRate > 0 {
		fmt.Printf("  ❌ FAIL: Error rate %.1f%% > 0%% target\n", errorRate)
		pass = false
	} else {
		fmt.Println("  ✅ PASS: 0% error rate")
	}
	goroutineLeak := goroutinesAfter - goroutinesBefore
	if goroutineLeak > 5 {
		fmt.Printf("  ❌ FAIL: Goroutine leak detected (%+d)\n", goroutineLeak)
		pass = false
	} else {
		fmt.Println("  ✅ PASS: No goroutine leaks")
	}

	fmt.Println()
	if pass {
		fmt.Println("  🎉 ALL CHECKS PASSED")
	} else {
		fmt.Println("  ⚠️  SOME CHECKS FAILED")
		os.Exit(1)
	}
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p)
	return sorted[idx]
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ─── Mock LLM Client ────────────────────────────────────────────────────────

// mockLLMClient returns a fixed response immediately — no network calls.
// This tests the agent loop, memory, tool dispatch, and concurrency handling
// without hitting the real Claude API.
type mockLLMClient struct{}

func (m *mockLLMClient) Complete(_ context.Context, msgs []agent.Message, _ []agent.ToolDefinition) (*agent.CompletionResult, error) {
	// Simulate a small processing delay.
	time.Sleep(5 * time.Millisecond)
	return &agent.CompletionResult{
		Content:      "Load test response OK",
		Model:        "mock-model",
		InputTokens:  100,
		OutputTokens: 20,
	}, nil
}

func (m *mockLLMClient) CompleteStream(_ context.Context, _ []agent.Message, _ []agent.ToolDefinition, out chan<- string) error {
	out <- "Load test stream response"
	close(out)
	return nil
}

func (m *mockLLMClient) ModelName() string { return "mock-model" }
