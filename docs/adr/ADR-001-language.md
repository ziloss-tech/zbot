# ADR-001: Programming Language — Go

**Status:** Accepted  
**Date:** 2026-02-25

## Context

Needed a language for building a security-sensitive autonomous AI agent that:
- Executes arbitrary tool calls
- Handles concurrent workflows
- Processes untrusted content from the web
- Runs long-lived processes without memory leaks
- Deploys as a single binary

Candidates evaluated: Python, TypeScript/Node, Rust, Go.

## Decision

**Go 1.22+**

## Rationale

| Criterion | Go | Python | TypeScript | Rust |
|-----------|----|----|----|----|
| Memory safety | ✅ GC, no ptr arithmetic | ✅ GC | ✅ GC | ✅ ownership |
| Concurrency model | ✅ goroutines + channels | ❌ GIL limits | ⚠️ event loop | ✅ async/await |
| Single binary deploy | ✅ | ❌ venv hell | ❌ node_modules | ✅ |
| Standard library | ✅ batteries included | ✅ | ⚠️ | ⚠️ |
| Security ecosystem | ✅ govulncheck official | ⚠️ pip audit | ⚠️ npm audit | ✅ |
| Speed to write | ⚠️ 30-40% slower than Python | ✅ fastest | ✅ fast | ❌ slow |
| Prompt injection risk | Low (no eval) | High | Medium | Low |

Python rejected: no safe sandboxing from own process, GIL kills parallelism, dependency hell.  
TypeScript rejected: inherits npm supply chain risk, OpenClaw CVEs are in the Node ecosystem.  
Rust rejected: correct for OS-level security, overkill for network/API services, slow dev velocity.

## Consequences

- Core is ~30-40% slower to write than Python equivalent
- Developers must write idiomatic Go (interfaces, not inheritance)
- All goroutines must accept and respect `context.Context`
