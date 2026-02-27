# ZBOT Observability Stack

Loki + Promtail + Prometheus + Grafana — runs locally via Docker Compose.

## One-time setup

1. Install Docker Desktop: https://www.docker.com/products/docker-desktop/
2. Start Docker Desktop, wait for it to be ready.

## Start the stack

```bash
cd ~/Desktop/zbot/deploy/observability
./start.sh
```

Open Grafana at http://localhost:3000
- Username: `admin`
- Password: `zbot-grafana`

Two dashboards load automatically:
- **ZBOT — Live Activity** — real-time log stream with error filter
- **ZBOT — Token Cost** — daily spend, tokens per turn, most expensive workflows

## Wire ZBOT logs into Loki

ZBOT outputs structured JSON to stdout. To ship it to Loki:

```bash
# Start ZBOT and tee its output to a log file
cd ~/Desktop/zbot
./zbot 2>&1 | tee /tmp/zbot.log
```

Then in `promtail.yml`, comment out the `zbot_docker` block and
uncomment the `zbot_file` block. Restart Promtail:

```bash
docker compose restart promtail
```

Logs will appear in Grafana within seconds.

## Stop the stack

```bash
docker compose down
```

Data persists in Docker volumes — restarts from where it left off.

## What's in each dashboard

### Live Activity
- Full log stream with JSON field expansion
- Error log stream (filtered)
- Recent task completions

### Token Cost
- Today's spend in USD (red threshold at $5)
- Input/output token counts
- Cost-per-turn time series
- Per-workflow cost breakdown (shows workflow_id, task_id, cost)

## Log fields available in Grafana

Every ZBOT log line is JSON. Key fields extracted as Loki labels:

| Field | Description |
|---|---|
| `level` | debug / info / warn / error |
| `component` | planner / executor / orchestrator / audit / gateway |
| `session` | user session or workflow task session ID |
| `workflow_id` | links agent turns to their workflow |
| `task_id` | links agent turns to their specific task |
| `model` | claude-sonnet-4-6 / gpt-4o |
| `tool` | web_search / fetch_url / etc |
| `input_tokens` | tokens sent to model |
| `output_tokens` | tokens returned by model |
| `cost_usd` | calculated cost for this turn |
| `duration_ms` | wall-clock time for the operation |
| `error` | error message (on error-level logs) |
