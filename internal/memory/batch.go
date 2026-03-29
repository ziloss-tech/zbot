// Package memory — Nightly Batch Builder for Thought Packages.
// Phase 3 of the Memory Overhaul.
//
// Pipeline:
//   1. Dump all facts from pgvector (paginated)
//   2. Cluster by topic using cheapLLM (DeepSeek V3.2, ~$0.04/run)
//   3. Compress each cluster into a ThoughtPackage
//   4. Store packages, archive old versions
//   5. Generate health report
//
// Designed to run at 2 AM MST via scheduler.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// MemoryReader is the interface the batch builder needs to read facts.
// Separate from agent.MemoryStore to avoid polluting the core interface.
type MemoryReader interface {
	List(ctx context.Context, limit int) ([]agent.Fact, error)
}

// PackageWriter is the interface the batch builder needs to write packages.
type PackageWriter interface {
	SavePackage(ctx context.Context, pkg agent.ThoughtPackage) error
	GetPackage(ctx context.Context, id string) (*agent.ThoughtPackage, error)
}

// BatchBuilder orchestrates the nightly memory organization pipeline.
type BatchBuilder struct {
	memReader MemoryReader
	pkgWriter PackageWriter
	llm       agent.LLMClient
	logger    *slog.Logger
}

// BatchResult captures the outcome of a nightly batch run.
type BatchResult struct {
	StartedAt       time.Time     `json:"started_at"`
	Duration        time.Duration `json:"duration"`
	FactsRead       int           `json:"facts_read"`
	ClustersFound   int           `json:"clusters_found"`
	PackagesCreated int           `json:"packages_created"`
	PackagesUpdated int           `json:"packages_updated"`
	LLMCalls        int           `json:"llm_calls"`
	Errors          []string      `json:"errors,omitempty"`
}

// Cluster is a group of related facts identified by the LLM.
type Cluster struct {
	Label    string   `json:"label"`
	Keywords []string `json:"keywords"`
	FactIDs  []string `json:"fact_ids"`
}

// NewBatchBuilder creates a batch builder.
func NewBatchBuilder(memReader MemoryReader, pkgWriter PackageWriter, llm agent.LLMClient, logger *slog.Logger) *BatchBuilder {
	return &BatchBuilder{
		memReader: memReader,
		pkgWriter: pkgWriter,
		llm:       llm,
		logger:    logger,
	}
}

// Run executes the full nightly batch pipeline.
// Safe to call multiple times — packages are upserted by label.
func (b *BatchBuilder) Run(ctx context.Context) (*BatchResult, error) {
	result := &BatchResult{StartedAt: time.Now()}
	b.logger.Info("batch builder starting")

	// Step 1: Dump all facts
	facts, err := b.memReader.List(ctx, 10000) // cap at 10K
	if err != nil {
		return nil, fmt.Errorf("batch: list facts: %w", err)
	}
	result.FactsRead = len(facts)
	b.logger.Info("batch: loaded facts", "count", len(facts))

	if len(facts) == 0 {
		b.logger.Info("batch: no facts to process, skipping")
		result.Duration = time.Since(result.StartedAt)
		return result, nil
	}

	// Step 2: Cluster facts by topic
	clusters, err := b.clusterFacts(ctx, facts)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("cluster: %v", err))
		b.logger.Error("batch: clustering failed", "err", err)
		result.Duration = time.Since(result.StartedAt)
		return result, err
	}
	result.ClustersFound = len(clusters)
	result.LLMCalls++
	b.logger.Info("batch: clustered", "clusters", len(clusters))

	// Step 3: Compress each cluster into a ThoughtPackage
	factMap := make(map[string]agent.Fact, len(facts))
	for _, f := range facts {
		factMap[f.ID] = f
	}

	for _, cluster := range clusters {
		// Gather fact contents for this cluster
		var contents []string
		var factIDs []string
		for _, fid := range cluster.FactIDs {
			if f, ok := factMap[fid]; ok {
				contents = append(contents, f.Content)
				factIDs = append(factIDs, fid)
			}
		}
		if len(contents) == 0 {
			continue
		}

		// Compress via LLM
		compressed, err := b.compressCluster(ctx, cluster.Label, contents)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("compress %s: %v", cluster.Label, err))
			b.logger.Warn("batch: compress failed", "cluster", cluster.Label, "err", err)
			continue
		}
		result.LLMCalls++

		// Build package
		pkg := agent.ThoughtPackage{
			ID:         "pkg-" + sanitizeID(cluster.Label),
			Label:      cluster.Label,
			Keywords:   cluster.Keywords,
			Content:    compressed,
			TokenCount: estimateTokens(compressed),
			MemoryIDs:  factIDs,
			Priority:   agent.PackageAuto,
			Freshness:  time.Now(),
			Version:    1, // will be incremented on update
		}

		// Check if package already exists (increment version)
		existing, _ := b.pkgWriter.GetPackage(ctx, pkg.ID)
		if existing != nil {
			pkg.Version = existing.Version + 1
			pkg.CreatedAt = existing.CreatedAt
			result.PackagesUpdated++
		} else {
			result.PackagesCreated++
		}

		if err := b.pkgWriter.SavePackage(ctx, pkg); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("save %s: %v", cluster.Label, err))
			b.logger.Error("batch: save package failed", "label", cluster.Label, "err", err)
			continue
		}
		b.logger.Info("batch: package saved",
			"label", cluster.Label,
			"tokens", pkg.TokenCount,
			"facts", len(factIDs),
			"version", pkg.Version,
		)
	}

	result.Duration = time.Since(result.StartedAt)
	b.logger.Info("batch builder complete",
		"duration", result.Duration,
		"facts", result.FactsRead,
		"clusters", result.ClustersFound,
		"created", result.PackagesCreated,
		"updated", result.PackagesUpdated,
		"llm_calls", result.LLMCalls,
		"errors", len(result.Errors),
	)
	return result, nil
}

// clusterFacts sends facts to the LLM and asks it to group them by topic.
// Processes in chunks of 100 facts to stay within context limits.
func (b *BatchBuilder) clusterFacts(ctx context.Context, facts []agent.Fact) ([]Cluster, error) {
	var allClusters []Cluster

	chunkSize := 100
	for i := 0; i < len(facts); i += chunkSize {
		end := i + chunkSize
		if end > len(facts) {
			end = len(facts)
		}
		chunk := facts[i:end]

		// Build facts summary for LLM
		var sb strings.Builder
		for _, f := range chunk {
			sb.WriteString(fmt.Sprintf("[%s] %s\n", f.ID, trimTo(f.Content, 200)))
		}

		prompt := fmt.Sprintf(`You are a memory organizer. Group these %d facts into topic clusters.
Each cluster should have a descriptive label (like "projects/zbot", "ghl/workflows", "personal/health")
and 3-8 keywords for fast matching.

Facts:
%s

Respond with ONLY valid JSON — no markdown, no explanation:
[{"label":"topic/subtopic","keywords":["kw1","kw2","kw3"],"fact_ids":["id1","id2"]}]`, len(chunk), sb.String())

		resp, err := b.llm.Complete(ctx, []agent.Message{
			{Role: agent.RoleUser, Content: prompt},
		}, nil)
		if err != nil {
			return nil, fmt.Errorf("cluster LLM call: %w", err)
		}

		// Parse JSON response
		text := strings.TrimSpace(resp.Content)
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)

		var clusters []Cluster
		if err := json.Unmarshal([]byte(text), &clusters); err != nil {
			b.logger.Warn("batch: cluster parse failed, trying repair", "err", err)
			// Try to find JSON array in response
			start := strings.Index(text, "[")
			end := strings.LastIndex(text, "]")
			if start >= 0 && end > start {
				if err2 := json.Unmarshal([]byte(text[start:end+1]), &clusters); err2 != nil {
					return nil, fmt.Errorf("cluster JSON parse: %w (raw: %s)", err, trimTo(text, 200))
				}
			} else {
				return nil, fmt.Errorf("cluster JSON parse: %w (raw: %s)", err, trimTo(text, 200))
			}
		}
		allClusters = append(allClusters, clusters...)
	}

	return allClusters, nil
}

// compressCluster asks the LLM to compress N fact strings into a dense block.
func (b *BatchBuilder) compressCluster(ctx context.Context, label string, contents []string) (string, error) {
	var sb strings.Builder
	for i, c := range contents {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, trimTo(c, 300)))
	}

	prompt := fmt.Sprintf(`Compress these %d memories about "%s" into a single dense paragraph (200-400 tokens).
Preserve all specific facts, names, dates, IDs, and actionable details.
Remove redundancy and filler. Write in present tense, factual style.
Output ONLY the compressed text — no preamble, no markdown.

Memories:
%s`, len(contents), label, sb.String())

	resp, err := b.llm.Complete(ctx, []agent.Message{
		{Role: agent.RoleUser, Content: prompt},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("compress LLM call: %w", err)
	}

	compressed := strings.TrimSpace(resp.Content)
	if len(compressed) < 20 {
		return "", fmt.Errorf("compress returned too little content (%d chars)", len(compressed))
	}
	return compressed, nil
}

// ─── HELPERS ─────────────────────────────────────────────────────────────────

// trimTo limits a string to maxLen chars, adding "..." if truncated.
func trimTo(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// sanitizeID converts a label like "ghl/workflow-migration" to "ghl-workflow-migration".
func sanitizeID(label string) string {
	r := strings.NewReplacer("/", "-", " ", "-", "_", "-")
	s := r.Replace(strings.ToLower(label))
	// Remove consecutive dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// estimateTokens gives a rough token count (~4 chars per token for English).
func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}
