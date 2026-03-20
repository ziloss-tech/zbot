# ZBOT — GHL Agent Sprints (Coworker Briefs)
## Goal: Make ZBOT a proletariat-class GHL automation agent

**Context:** Your business depends on GoHighLevel workflows for appointment booking, client notifications, SMS campaigns, and lead nurturing across multiple franchise locations. Currently, workflow auditing and management requires manual work + tribal knowledge. ZBOT needs to be able to audit, manage, and eventually build GHL workflows autonomously.

**Prerequisites from existing roadmap:**
- Sprint 0 ✅ (repo, build, core structure)
- Web UI chat ✅ (streaming, event bus, Thalamus)
- Serper search ✅
- File tools ✅

**These 3 sprints can run in parallel with the existing UI polish sprints.**

---

## GHL Sprint 1 — GHL API Skill (Foundation)
**Estimated effort:** 3-4 days
**Branch:** feature/ghl-skill

### Objective
Give ZBOT the ability to read and interact with GoHighLevel via the official API. This is the foundation — read access to contacts, workflows, pipelines, conversations, calendars, and custom fields.

### Tasks

| # | Task | Details |
|---|------|---------|
| G1-1 | Create `internal/skills/ghl/` package | Skill manifest, config, API client |
| G1-2 | GHL API client with auth | Bearer token auth, rate limiting (10 req/sec), retry on 429. Base URL: `https://services.leadconnectorhq.com` |
| G1-3 | `ghl_list_workflows` tool | GET /workflows/?locationId={id} — returns all workflows with status, version, ID |
| G1-4 | `ghl_get_contacts` tool | Search/list contacts with filters (tags, DND, date range) |
| G1-5 | `ghl_get_contact` tool | Get single contact by ID — full profile, tags, custom fields |
| G1-6 | `ghl_search_contacts` tool | Advanced search — by tag, name, phone, email, custom field values |
| G1-7 | `ghl_get_pipelines` tool | List pipelines and stages |
| G1-8 | `ghl_get_opportunities` tool | List/search opportunities by pipeline, stage, contact |
| G1-9 | `ghl_get_conversations` tool | List conversations for a contact — SMS, email, calls |
| G1-10 | `ghl_get_custom_fields` tool | List all custom fields for a location |
| G1-11 | `ghl_get_calendars` tool | List calendars and appointment types |
| G1-12 | Multi-location support | Config accepts multiple location IDs + tokens, tool calls specify which location |
| G1-13 | Location registry | YAML config mapping location names → IDs + tokens (e.g., "client-name" → YOUR_LOCATION_ID) |

### API Details

```yaml
# config.yaml addition
ghl:
  locations:
    client-name:
      id: "YOUR_LOCATION_ID"
      token_secret: "ghl-token-client-name"  # GCP Secret Manager key
      name: "Renewal by Andersen - Esler CST"
    esler-email:
      id: "F4FDC6celS3tpC7iTP0z"
      token_secret: "ghl-token-esler-email"
      name: "Renewal by Andersen - Esler Email"
    oci:
      id: "WmSwOBLxEKfI2inyownY"
      token_secret: "ghl-token-oci"
      name: "OCI - Esler Companies"
  api_version: "2021-07-28"
  rate_limit: 10  # requests per second
```

### Skill Registration
```go
// internal/skills/ghl/skill.go
type GHLSkill struct {
    client    *GHLClient
    locations map[string]LocationConfig
}

func (s *GHLSkill) Name() string { return "ghl" }
func (s *GHLSkill) Description() string {
    return "GoHighLevel CRM — contacts, workflows, pipelines, conversations, calendars"
}
func (s *GHLSkill) Tools() []agent.ToolDef {
    return []agent.ToolDef{
        {Name: "ghl_list_workflows", Description: "List all workflows for a GHL location", ...},
        {Name: "ghl_get_contacts", Description: "Search contacts with filters", ...},
        // ... all tools
    }
}
```

### Definition of Done
1. `go test ./internal/skills/ghl/...` passes
2. ZBOT can answer: "list all published workflows in Esler CST"
3. ZBOT can answer: "find contacts tagged 'activate_dbr_ai' in the last 7 days"
4. ZBOT can answer: "how many contacts are marked DND in Esler CST?"
5. Multi-location works: "list workflows in OCI" vs "list workflows in Esler CST"
6. Rate limiting verified — 50 rapid requests don't get 429'd

---

## GHL Sprint 2 — Workflow Auditor
**Estimated effort:** 4-5 days
**Branch:** feature/ghl-auditor
**Depends on:** GHL Sprint 1

### Objective
ZBOT can audit GHL workflows across all locations — flag issues, compare configs, generate reports. This replaces the manual audit work that Jeremy + Claude did via Chrome DevTools.

### Tasks

| # | Task | Details |
|---|------|---------|
| G2-1 | `ghl_audit_workflows` tool | Pull all workflows for a location, analyze structure, flag issues |
| G2-2 | Workflow health checks | Flag: draft workflows that should be published, published workflows with no recent executions, workflows with identical names across locations |
| G2-3 | Trigger analysis | For each workflow, extract trigger type + conditions. Flag shared calendar triggers across workflows (the exact bug we just found) |
| G2-4 | Cross-location comparison | Compare workflow names/structures across locations — find drift (same-named workflow, different configs) |
| G2-5 | `ghl_audit_contacts` tool | Analyze contact health — DND rates, tag distribution, missing fields, stale contacts |
| G2-6 | `ghl_audit_pipelines` tool | Pipeline health — stuck opportunities, empty stages, conversion rates |
| G2-7 | Audit report generator | ZBOT generates a structured audit report (markdown + PDF) with findings, severity, recommendations |
| G2-8 | Known-good workflow patterns | Define "template" patterns (e.g., the 4-phase event registration template used in OCI) — flag workflows that deviate |
| G2-9 | stopOnResponse checker | Flag all workflows where stopOnResponse=false (we found this across all 154 OCI workflows) |
| G2-10 | Appointment workflow validator | Specifically check for the cross-workflow ejection pattern: two workflows sharing same calendar trigger with different counter conditions |

### Audit Rules Engine
```go
// internal/skills/ghl/auditor.go
type AuditRule struct {
    Name     string
    Severity string // "critical", "warning", "info"
    Check    func(workflows []Workflow, location LocationConfig) []AuditFinding
}

var DefaultRules = []AuditRule{
    {
        Name:     "shared-calendar-triggers",
        Severity: "critical",
        Check:    checkSharedCalendarTriggers,
        // Flags: two+ published workflows using appointment triggers on same calendar
        // This is EXACTLY the bug that caused the notification failure
    },
    {
        Name:     "stop-on-response-disabled",
        Severity: "warning",
        Check:    checkStopOnResponse,
    },
    {
        Name:     "draft-workflow-count",
        Severity: "info",
        Check:    checkDraftCount,
    },
}
```

### Definition of Done
1. ZBOT can answer: "audit all workflows in Esler CST and flag issues"
2. Report identifies the shared-calendar-trigger bug we found today
3. Cross-location comparison works: "compare workflows between Esler CST and OCI"
4. Audit report saved as markdown + PDF to workspace
5. Severity levels work: critical (blocks operation) → warning (should fix) → info (nice to know)

---

## GHL Sprint 3 — Workflow Automation Agent
**Estimated effort:** 5-7 days
**Branch:** feature/ghl-automation
**Depends on:** GHL Sprint 2

### Objective
ZBOT can take action on GHL — enroll/remove contacts from workflows, update contact fields, manage tags, and eventually build/modify workflows. This is the "proletariat class" sprint — ZBOT becomes an autonomous GHL operator.

### Tasks

| # | Task | Details |
|---|------|---------|
| G3-1 | `ghl_add_contact_to_workflow` tool | Enroll a contact into a workflow |
| G3-2 | `ghl_remove_contact_from_workflow` tool | Remove a contact from a workflow |
| G3-3 | `ghl_update_contact` tool | Update contact fields, tags, DND status |
| G3-4 | `ghl_bulk_update_contacts` tool | Batch update contacts (with safety: max 50 per batch, confirmation required) |
| G3-5 | `ghl_send_sms` tool | Send SMS to a contact via GHL |
| G3-6 | `ghl_send_email` tool | Send email to a contact via GHL |
| G3-7 | Safety protocol | 3-phase safety: Phase 1 = read-only audit, Phase 2 = test on 5 contacts with confirmation, Phase 3 = full run with confirmation. ZBOT MUST follow this for any bulk operation. |
| G3-8 | Rollback capability | Before any bulk update, save contact state snapshot to workspace JSON. Provide `ghl_rollback` tool to revert. |
| G3-9 | DND review automation | Automate the Esler CST DND review: find ~7300 contacts incorrectly DND, validate, fix in 3-phase protocol |
| G3-10 | Workflow recommendation engine | Given an audit finding, ZBOT proposes a fix (e.g., "merge these two workflows into one") with a step-by-step plan |

### Safety Architecture
```
User: "fix the DND contacts in Esler CST"

ZBOT Phase 1 (auto):
  → Pull all contacts tagged Inbound+SMS with DND=true
  → Analyze: 7,312 contacts, 6,891 appear incorrectly DND
  → Report: "Found 6,891 contacts that should NOT be DND. Here's a sample of 10."
  → ASK: "Proceed to Phase 2 (test 5 contacts)?"

ZBOT Phase 2 (requires YES):
  → Pick 5 contacts, remove DND
  → Report results: "5/5 successful. SMS delivery confirmed."
  → ASK: "Proceed to Phase 3 (full 6,891 contacts)?"

ZBOT Phase 3 (requires YES):
  → Save snapshot of all 6,891 contacts to workspace
  → Process in batches of 50
  → Report progress every 500
  → Final report with success/failure counts
```

### Definition of Done
1. ZBOT can enroll/remove single contacts from workflows
2. Bulk update with 3-phase safety protocol works end-to-end
3. DND review runs successfully on test contacts
4. Rollback capability verified — can revert a batch update
5. All write operations require explicit user confirmation
6. Audit trail: every action logged with before/after state

---

## Summary: The Path to Proletariat Class

| Sprint | Days | What ZBOT Can Do After |
|--------|------|------------------------|
| GHL 1 | 3-4 | Read everything in GHL — contacts, workflows, pipelines, calendars |
| GHL 2 | 4-5 | Audit workflows, flag bugs, generate reports, compare across locations |
| GHL 3 | 5-7 | Take action — fix DND, enroll contacts, manage workflows, with safety rails |

**Total: ~2-3 weeks to a fully autonomous GHL agent.**

After GHL Sprint 3, ZBOT can:
- Run a full audit of any GHL location on command
- Flag the exact type of bug we found today (shared calendar triggers)
- Fix contact issues like the DND mess
- Generate reports for Brian/Jeremy without Steven
- Be the foundation for replacing manual GHL management entirely

**Next after these:** ZBOT Sprint 7 from the main roadmap (Skills System) naturally absorbs this work — the GHL skill becomes the first production skill in the registry.

---

*Sprint briefs created March 19, 2026 — Ziloss Technologies*
