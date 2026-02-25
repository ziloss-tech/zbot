# ADR-001: Language — Go 1.22+

**Status:** Accepted  
**Date:** 2026-02-25  
**Deciders:** Jeremy Lerwick

## Context

ZBOT is an autonomous AI agent that executes code, manages files, makes HTTP requests,
and runs long background workflows. It needs to be secure, concurrent, and maintainable
by a small team over years.

## Decision

**Go 1.22+** is the implementation language for all ZBOT components.

## Rationale

| Criterion | Go | Python | TypeScript |
|-----------|-----|--------|------------|
| Memory safety | ✅ GC, no pointer arithmetic | ⚠️ GC but C extensions exist | ✅ GC |
| Concurrency | ✅ Native goroutines, CSP | ⚠️ GIL limits true parallelism | ⚠️ Event loop, callback hell |
| Single binary | ✅ `go build` → one binary | ❌ Runtime + deps required | ❌ Node.js runtime required |
| Supply chain | ✅ go.sum pins exact hashes | ⚠️ pip/poetry, large dep trees | ⚠️ npm, inherits OpenClaw CVEs |
| Prompt injection resistance | N/A (host language) | N/A | N/A (TypeScript = OpenClaw risk) |
| Stdlib quality | ✅ Excellent HTTP/TLS/crypto | ✅ Good | ✅ Good |

**Rejected: Python.** GIL limits concurrency. Dependency trees are enormous (typical AI
Python project: 200+ transitive deps). Prompt injection via eval() is trivial to exploit.

**Rejected: TypeScript/Node.** TypeScript is the OpenClaw implementation language and
carries all its CVEs by ecosystem association. npm supply chain attacks are well-documented.

**Trade-off accepted:** Go is 30–40% slower to write than Python. Correct for a
security-sensitive autonomous agent that runs for years.

## Consequences

- All contributors must know Go (or learn it).
- Third-party Go libraries reviewed before adoption (govulncheck on every build).
- Docker runtime images for code execution are language-specific (python:3.12-slim etc.)
  but ZBOT itself is a single Go binary.
