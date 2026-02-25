# ZBOT Sprint 7 — Coworker Mission Brief
## Objective: Skills System — GHL, GitHub, Google Sheets

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

ZBOT runs on a schedule autonomously. Sprint 6 is done.
Your job is to build an extensible skills system with GHL, GitHub, and Google Sheets integrations.

---

## Current State (Sprint 6 Complete)

- Real Claude responses via Slack ✅
- Cross-session memory via pgvector ✅
- Vision: images + PDFs ✅
- Anti-block web scraper ✅
- Multi-step parallel workflow engine ✅
- Cron scheduler + webhooks ✅
- go build ./... passes clean ✅

### Scaffolding already exists:
- internal/skills/ — empty directory, ready to fill

---

## Sprint 7 Tasks — Complete ALL of These

### TASK 1: Skill Interface + Registry

Create: internal/skills/registry.go

```go
package skills

import (
    "context"
    "github.com/jeremylerwick-max/zbot/internal/agent"
)

// Skill is a named collection of tools with a description and permission set.
// Skills are registered at startup and their tools are added to the agent.
type Skill interface {
    // Name returns the skill identifier (e.g. "ghl", "github", "sheets")
    Name() string

    // Description returns a human-readable summary of what this skill does.
    Description() string

    // Tools returns all agent.Tool implementations this skill provides.
    Tools() []agent.Tool

    // SystemPromptAddendum returns additional system prompt text specific to this skill.
    // Return "" if no additional context is needed.
    SystemPromptAddendum() string
}

// Registry holds all registered skills.
type Registry struct {
    skills map[string]Skill
}

func NewRegistry() *Registry

// Register adds a skill to the registry.
func (r *Registry) Register(s Skill)

// AllTools returns every tool from every registered skill, ready to inject into the agent.
func (r *Registry) AllTools() []agent.Tool

// SystemPromptAddendum returns combined addenda from all skills.
func (r *Registry) SystemPromptAddendum() string
```

### TASK 2: GoHighLevel (GHL) Skill

Create: internal/skills/ghl/skill.go and internal/skills/ghl/tools.go

GHL API base URL: https://services.leadconnectorhq.com
GHL API key stored in GCP Secret Manager as: "ghl-api-key"
GHL Location ID for Jeremy's account: fRrP1e3LGLFewc5dQDhS

Tools to implement:

**ghl_get_contacts**
```
GET /contacts/?locationId={locationId}&query={query}&limit={limit}
Returns: list of contacts with name, email, phone, tags, pipeline stage
```

**ghl_get_contact**
```
GET /contacts/{contactId}
Returns: full contact details
```

**ghl_update_contact**
```
PUT /contacts/{contactId}
Body: {tags, customFields, pipelineStageId}
Updates contact tags or pipeline stage
```

**ghl_get_conversations**
```
GET /conversations/search?locationId={locationId}&contactId={contactId}
Returns: recent SMS/email conversations for a contact
```

**ghl_send_message**
```
POST /conversations/messages
Body: {type: "SMS", contactId, message}
Sends an SMS to a contact — use with EXTREME caution, confirm before sending
```

**ghl_get_pipeline**
```
GET /opportunities/pipelines?locationId={locationId}
Returns: pipeline stages and opportunity counts
```

Authentication: Bearer token in Authorization header.
All GHL tools must log every API call to the audit logger.
ghl_send_message must include a confirmation step — return a preview and require explicit "confirm" before actually sending.

Create: internal/skills/ghl/client.go

```go
type GHLClient struct {
    apiKey     string
    locationID string
    httpClient *http.Client
}

func NewGHLClient(apiKey, locationID string) *GHLClient

// Get performs an authenticated GET request.
func (c *GHLClient) Get(ctx context.Context, path string, params url.Values) ([]byte, error)

// Post performs an authenticated POST request.
func (c *GHLClient) Post(ctx context.Context, path string, body any) ([]byte, error)

// Put performs an authenticated PUT request.
func (c *GHLClient) Put(ctx context.Context, path string, body any) ([]byte, error)
```

### TASK 3: GitHub Skill

Create: internal/skills/github/skill.go and internal/skills/github/tools.go

GitHub token stored in GCP Secret Manager as: "github-token"
Default repo: jeremylerwick-max/zbot (but tools accept any repo as input)

Tools to implement:

**github_list_issues**
```
GET /repos/{owner}/{repo}/issues?state=open&labels={labels}
Returns: issues with number, title, labels, assignee, created_at
```

**github_get_issue**
```
GET /repos/{owner}/{repo}/issues/{number}
Returns: full issue with body and comments
```

**github_create_issue**
```
POST /repos/{owner}/{repo}/issues
Body: {title, body, labels, assignees}
```

**github_list_prs**
```
GET /repos/{owner}/{repo}/pulls?state=open
Returns: PRs with number, title, status, author
```

**github_get_file**
```
GET /repos/{owner}/{repo}/contents/{path}
Returns: decoded file content (base64 decode the response)
```

Use the official GitHub REST API: https://api.github.com
Authentication: token in Authorization header as "Bearer {token}".

### TASK 4: Google Sheets Skill

Create: internal/skills/sheets/skill.go and internal/skills/sheets/tools.go

Google service account credentials stored in GCP Secret Manager as: "google-sheets-credentials" (JSON)
Use: google.golang.org/api/sheets/v4

Tools to implement:

**sheets_read**
```
GET spreadsheet values for a range
Input: {spreadsheetId, range} — e.g. range = "Sheet1!A1:Z100"
Returns: 2D array of values as JSON
```

**sheets_write**
```
PUT/append values to a range
Input: {spreadsheetId, range, values} — values is 2D array
```

**sheets_append**
```
POST append rows to end of sheet
Input: {spreadsheetId, sheetName, values}
```

**sheets_list**
```
GET sheet names within a spreadsheet
Input: {spreadsheetId}
Returns: list of sheet names
```

Dependencies:
```bash
go get google.golang.org/api/sheets/v4
go get google.golang.org/api/option
```

### TASK 5: Email Skill

Create: internal/skills/email/skill.go

SMTP credentials in GCP Secret Manager:
- "smtp-host" (e.g. smtp.gmail.com)
- "smtp-port" (e.g. 587)
- "smtp-user"
- "smtp-pass"
- "smtp-from" (e.g. jeremy@ziloss.com)

Tool: **send_email**
```
Input: {to, subject, body, cc?}
Sends via SMTP with TLS
Returns: confirmation with message ID
```

Use net/smtp from stdlib — no external email library needed.
Always confirm before sending — return preview, require "confirm send" reply.

### TASK 6: Wire Skills into wire.go

```go
// Initialize skills
skillRegistry := skills.NewRegistry()

// GHL skill
ghlKey, _ := sm.Get(ctx, "ghl-api-key")
skillRegistry.Register(ghl.NewSkill(ghlKey, "fRrP1e3LGLFewc5dQDhS", auditLog))

// GitHub skill
githubToken, _ := sm.Get(ctx, "github-token")
skillRegistry.Register(github.NewSkill(githubToken))

// Sheets skill
sheetsCredJSON, _ := sm.Get(ctx, "google-sheets-credentials")
sheetsSkill, _ := sheets.NewSkill(ctx, sheetsCredJSON)
skillRegistry.Register(sheetsSkill)

// Email skill
smtpHost, _ := sm.Get(ctx, "smtp-host")
smtpUser, _ := sm.Get(ctx, "smtp-user")
smtpPass, _ := sm.Get(ctx, "smtp-pass")
smtpFrom, _ := sm.Get(ctx, "smtp-from")
skillRegistry.Register(email.NewSkill(smtpHost, 587, smtpUser, smtpPass, smtpFrom))

// Add all skill tools to agent
allTools := append(coreTools, skillRegistry.AllTools()...)

// Append skill system prompt addenda
agentCfg.SystemPrompt = systemPrompt + "\n\n" + skillRegistry.SystemPromptAddendum()
```

---

## Definition of Done

1. DM ZBOT: "Pull all open GHL contacts tagged 'new lead', check if they're in my Google Sheet at {spreadsheetId}, flag any missing"
2. ZBOT uses ghl_get_contacts, sheets_read, compares, returns a list of missing contacts.
3. DM ZBOT: "List the open issues on the ziloss-crm repo"
4. ZBOT uses github_list_issues and returns a formatted list.
5. DM ZBOT: "Send an email to test@example.com with subject 'test' and body 'hello'"
6. ZBOT returns a preview and waits for confirmation before sending.
7. go build ./... passes clean.

---

## Go Dependencies

```bash
cd ~/Desktop/zbot
go get google.golang.org/api/sheets/v4
go get google.golang.org/api/option
```

---

## Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 7: Skills system — GHL, GitHub, Google Sheets, Email integrations

- internal/skills/registry.go: Skill interface + registry
- internal/skills/ghl/: GHL contacts, conversations, pipeline tools
- internal/skills/github/: Issues, PRs, file reading tools
- internal/skills/sheets/: Read/write/append Google Sheets
- internal/skills/email/: SMTP send with confirmation step
- cmd/zbot/wire.go: All skills wired and registered"
git push origin main
```

## Important Notes

- Never put secrets in code. All credentials via GCP Secret Manager only.
- go build ./... must pass after every change.
- ghl_send_message and send_email MUST have a confirmation step — never send without explicit user "confirm".
- All GHL API calls must be logged to the audit logger (tool call + response).
- If a skill's secret is missing from Secret Manager, that skill should be skipped with a warning log — don't crash.
- GHL Location ID is hardcoded as constant: fRrP1e3LGLFewc5dQDhS
