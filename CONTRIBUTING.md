# Contributing to ZBOT

Thanks for your interest in contributing! ZBOT is an open-source project and we welcome contributions of all kinds.

## Getting Started

### Prerequisites

- Go 1.22+ (check with `go version`)
- Node 18+ (for frontend builds)
- Postgres with pgvector (optional — ZBOT degrades gracefully without it)

### Development Setup

```bash
# Clone the repo
git clone https://github.com/ziloss-tech/zbot.git
cd zbot

# Copy environment config
cp .env.example .env
# Edit .env with at least one LLM backend (Ollama is the easiest)

# Build the frontend
cd internal/webui/frontend && npm ci && npx vite build && cd ../../..

# Build and run
go build -o zbot ./cmd/zbot
./zbot
```

### Running Tests

```bash
# All tests
go test ./...

# Specific package
go test ./internal/security/ -v

# Lint
go vet ./...
```

All tests must pass before submitting a PR.

## How to Contribute

### Adding a New Skill

ZBOT uses a modular skill system. Each skill wraps one external API. See `CLAUDE.md` for the full guide, but the short version:

1. Create `internal/skills/yourskill/` with `client.go`, `tools.go`, `skill.go`
2. Each tool implements the `agent.Tool` interface from `internal/agent/ports.go`
3. The skill implements the `skills.Skill` interface
4. Register it in `cmd/zbot/wire.go`
5. Add at least one test
6. Run `go build ./... && go test ./... && go vet ./...`

### Adding an MCP Server Config

Zero code required. Add a config to `~/zbot-workspace/mcp-servers.json`:

```json
[
  {
    "name": "your-service",
    "command": "npx",
    "args": ["-y", "@your-org/mcp-server"],
    "env": {"API_KEY": "vault:YOUR_API_KEY"}
  }
]
```

If you've tested a config that works well, submit a PR adding it to `docs/mcp-examples/`.

### Bug Fixes and Improvements

1. Fork the repo
2. Create a feature branch (`git checkout -b feat/your-feature`)
3. Make your changes
4. Run `go build ./... && go test ./... && go vet ./...`
5. Commit with a descriptive message using conventional prefixes: `feat:`, `fix:`, `docs:`, `chore:`, `test:`
6. Push and open a PR

## Architecture Quick Reference

ZBOT uses hexagonal architecture (ports and adapters):

- `internal/agent/ports.go` — Defines ALL interfaces. The agent core never imports adapters.
- `cmd/zbot/wire.go` — The ONLY file that knows about concrete types. All dependency injection happens here.
- `internal/skills/` — Each skill wraps one external API behind the `skills.Skill` interface.
- `internal/tools/` — Core tools (fetch, file I/O, code runner, etc.)

**Key rule:** If you're importing an adapter package anywhere other than `wire.go`, something's wrong.

## PR Requirements

- All tests pass (`go test ./...`)
- No vet warnings (`go vet ./...`)
- New tools have at least one test
- Commit messages use conventional prefixes
- No secrets, API keys, or personal data in commits

## Code of Conduct

We follow the [Contributor Covenant](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). Be kind, be respectful, assume good intent.

## Questions?

Open an issue with the `question` label or start a discussion.
