# ADR-006: Credential Storage — Apple Keychain Default, GCloud Fallback

**Status:** Accepted
**Date:** 2026-03-16

## Context

ZBOT v2 adds credentialed site research — logging into paywalled sites (WSJ, Bloomberg, Statista) to access content. This requires storing user credentials securely and using them in headless browser sessions without ever exposing them to the LLM, logs, or memory.

We evaluated four options:

1. **macOS Keychain** — built into every Mac, encrypted, Touch ID protected, zero dependencies.
2. **GCloud Secret Manager** — already built for ZBOT v1 (GCP secrets adapter exists).
3. **HashiCorp Vault** — industry standard, but massive operational overhead for a personal agent.
4. **Encrypted file on disk** — simple, but we'd be rolling our own crypto (never a good idea).

## Decision

**macOS Keychain as the default. GCloud Secret Manager as the power-user/cloud fallback.**

Configuration toggle in `config.yaml`:

```yaml
secrets:
  backend: keychain  # keychain | gcloud
```

### macOS Keychain Adapter

Uses the `security` CLI built into macOS. No dependencies.

```
Store:    security add-generic-password -s "zbot-{domain}" -a "{email}" -w "{password}" -U
Retrieve: security find-generic-password -s "zbot-{domain}" -w
Delete:   security delete-generic-password -s "zbot-{domain}"
List:     security dump-keychain | grep "zbot-"
```

macOS automatically prompts the user for permission when ZBOT accesses a keychain item. This is a feature, not a bug — it provides an access control UI that we don't have to build.

### GCloud Secret Manager Adapter

Already exists in `secrets/gcloud.go`. Refactor to conform to the updated `SecretsManager` port interface.

## Security Invariants

These are non-negotiable. Every code path that touches credentials must enforce all of these:

1. **Credentials never enter the LLM context.** The Go process fetches credentials, passes them to go-rod, and zeroes them. The LLM only sees the URL to fetch, never the credentials.
2. **Credentials never in logs.** All credential types implement `slog.LogValuer` returning `[REDACTED]`. The `security` CLI is called with stderr suppressed.
3. **Credentials never in memory store.** The memory injector explicitly filters out any content from credentialed fetch tool calls before saving to pgvector or daily notes.
4. **Credentials stored as `[]byte`, not `string`.** Go strings are immutable and can't be wiped from memory. `[]byte` can be zeroed after use.
5. **Browser sessions destroyed per-request.** go-rod creates a fresh browser context for each credentialed fetch. No persistent cookies, no shared profile. Session destroyed immediately after content extraction.
6. **Network scoping.** The headless browser for credentialed fetches is configured to only allow requests to the target domain. All other network requests are blocked via go-rod's request interception.

## Consequences

### Positive

- **Zero setup for Mac users.** Keychain is already there. User says "Z, add my WSJ login" and it just works.
- **Hardware-backed encryption.** Keychain uses the Secure Enclave on Apple Silicon for key storage. AES-256-GCM encryption at rest.
- **User-visible access control.** macOS permission popup means the user explicitly approves every credential access. Can't be silently exfiltrated.
- **GCloud for cloud deploys.** If ZBOT ever runs on a server (not just the Mac Studio), GCloud Secret Manager provides equivalent security.

### Negative

- **Mac-only default.** Linux/Windows users must use GCloud backend. This is acceptable — ZBOT's primary deployment target is the Mac Studio.
- **CLI dependency.** The `security` CLI is a macOS system binary. If Apple changes its interface, the adapter breaks. Low risk — this CLI has been stable for 15+ years.
- **No credential rotation automation.** Users must manually update passwords. Could add a reminder system in a future sprint.

## Credentialed Source Configuration

```yaml
credentialed_sources:
  - domain: wsj.com
    keychain_service: zbot-wsj
    auth_type: login_form
    login_url: https://accounts.wsj.com/login
  - domain: bloomberg.com
    keychain_service: zbot-bloomberg
    auth_type: api_key
  - domain: statista.com
    keychain_service: zbot-statista
    auth_type: login_form
    login_url: https://www.statista.com/login
```

## User Commands

- `Z, add my WSJ login` → prompts for email/password → stores in Keychain
- `Z, remove my WSJ login` → deletes from Keychain
- `Z, list my saved logins` → lists all `zbot-*` keychain entries (service names only, never credentials)
