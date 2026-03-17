# ADR-005: Deep Research v2 — Haiku Gather, Sonnet Synthesize

**Status:** Accepted
**Date:** 2026-03-16

## Context

ZBOT v1's research pipeline used a single expensive model (Sonnet or Opus) for everything: generating search queries, fetching/reading pages, extracting facts, and writing the final report. This was expensive (~$1.50-$3.00 per deep research session) and slow (sequential processing).

The insight: source gathering and fact extraction is mechanical pattern-matching work. It doesn't need expensive reasoning. Synthesis — weighing evidence, resolving contradictions, structuring an argument — does.

## Decision

**Split deep research into two phases with different model tiers.**

### Phase 1: Gather (Haiku 4.5, cheap, parallel)

1. Haiku generates 20-50 search queries from the research question.
2. Queries fire in parallel via Brave Search API (10-goroutine worker pool, respecting 15 req/s rate limit).
3. Haiku reads each fetched page and extracts relevant facts/quotes into a structured `ResearchFact` JSON format.
4. Target: 30-50 sources per research session.
5. Cost: ~$0.01-0.05 total.

### Phase 2: Synthesize (Sonnet 4.6, smart, one pass)

1. All extracted `ResearchFact` objects are fed to Sonnet 4.6 with extended thinking enabled.
2. Single prompt: "Here are N sources on [topic]. Synthesize a comprehensive report with citations."
3. The model reasons through contradictions, weighs evidence quality, and structures the final report.
4. Cost: ~$0.10-0.50 per synthesis.

### Intermediate Format

```json
{
  "source": "https://example.com/article",
  "title": "Article Title",
  "facts": ["Fact 1", "Fact 2"],
  "quotes": ["Relevant quote with context"],
  "relevance": 0.85,
  "fetched_at": "2026-03-16T10:30:00Z"
}
```

## Consequences

### Positive

- **10-50x cheaper** than all-premium pipeline ($0.10-0.50 vs $1.50-$3.00).
- **More sources** — cheap gathering means we can afford 30-50 sources instead of 10-20.
- **Better synthesis** — extended thinking on Sonnet gives deeper reasoning than the old sequential approach.
- **Parallelizable** — Phase 1 is embarrassingly parallel. Wall-clock time for gathering drops from minutes to seconds.

### Negative

- **Lower extraction quality per source.** Haiku may miss nuance that Sonnet would catch. Mitigated by volume (more sources) and the synthesis pass catching inconsistencies.
- **Larger intermediate payload.** 50 sources × structured JSON = significant input to the synthesis call. Must stay within Sonnet's context window (200k tokens). For extremely large research sessions, may need chunked synthesis.
- **Two-phase complexity.** The pipeline now has a handoff point (gather → synthesize). If Phase 1 produces garbage, Phase 2 can't recover. Mitigated by a source quality threshold — if >50% of fetches fail, abort and notify user.

## Failure Handling

| Failure | Response |
|---------|----------|
| Individual source fetch fails | Log, skip, continue. Haiku notes "fetch failed" in the fact record. |
| >50% of sources fail | Abort research, notify user, offer retry with different queries. |
| Brave Search rate limit (429) | Exponential backoff, max 3 retries per query. |
| Synthesis truncated/incoherent | Auto-escalate to Opus 4.6 for synthesis retry. |
| Synthesis exceeds context window | Chunk sources into groups, synthesize each, then meta-synthesize. |
