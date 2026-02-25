# ADR-003: Code Execution — Docker-per-Session Sandboxing

**Status:** Accepted  
**Date:** 2026-02-25

## Decision

Every tool-invoked code execution runs in a **disposable Docker container**, destroyed
after completion.

## Security Constraints (enforced in CodeRunnerTool)

```
--rm                 destroy container immediately after exit
--network=none       no internet access by default
--memory=512m        RAM cap
--cpus=1             CPU cap
--read-only          read-only root filesystem
--tmpfs /tmp         writeable temp only (noexec, nosuid)
--user=1000:1000     non-root user
-v workspace:/workspace:rw  project files only, not home dir
```

## Network Policy

Tools that need internet (e.g. future `browse_web` tool in a container) may add
`--network=bridge` explicitly. Web search and URL fetch run in the Go process with
their own HTTP client — they don't use Docker containers.

## Why Not WebAssembly?

WASM sandboxing would be ideal but Go's WASM support for running arbitrary Python/Node
is immature. Docker is production-proven and available on all ZBOT deployment targets.

## Why Not Firecracker/gVisor?

Overkill for a personal agent. Docker with the above constraints eliminates the primary
threat vectors (network exfiltration, host filesystem access, privilege escalation).
Revisit in v2 if threat model changes.
