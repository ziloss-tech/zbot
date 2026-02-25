# ZBOT Sprint 3 — Coworker Mission Brief
## Objective: Vision + File Analysis — ZBOT Can See

You are working on ZBOT, a personal AI agent for Jeremy Lerwick (CEO, Ziloss Technologies).
The codebase is at: ~/Desktop/zbot
GitHub: https://github.com/jeremylerwick-max/zbot (private)
GCP Project: ziloss (project number: 203743871797)

ZBOT remembers things across sessions. Sprint 2 is done.
Your job is to make ZBOT able to receive and analyze images, PDFs, and screenshots via Slack.

---

## Current State (Sprint 2 Complete)

- Real Claude responses via Slack ✅
- pgvector memory with cross-session persistence ✅
- save_memory + search_memory tools ✅
- AutoSave wired ✅
- memcli CLI ✅
- go build ./... passes clean ✅

---

## Sprint 3 Tasks — Complete ALL of These

### TASK 1: Image Handling in Slack Gateway

File: internal/gateway/slack.go

The Slack gateway currently only handles text messages. Add file/image attachment handling.

When a Slack message event contains files:
1. Download the file using the Slack Files API with the bot token
2. Check the mimetype — handle: image/jpeg, image/png, image/gif, image/webp, application/pdf
3. Pass file data to the handler alongside the text message

Update the handler signature to accept attachments:
```go
// Update HandlerFunc type in gateway/slack.go
type HandlerFunc func(ctx context.Context, sessionID, userID, text string, attachments []Attachment) (string, error)

type Attachment struct {
    Data      []byte
    MediaType string // "image/jpeg", "image/png", "application/pdf"
    Filename  string
}
```

To download a file from Slack:
```go
req, _ := http.NewRequestWithContext(ctx, "GET", file.URLPrivateDownload, nil)
req.Header.Set("Authorization", "Bearer "+botToken)
resp, _ := http.DefaultClient.Do(req)
data, _ := io.ReadAll(resp.Body)
```

File size limit: 20MB max. Reject larger files with a helpful message.

### TASK 2: Multimodal Messages to Claude

File: internal/llm/anthropic.go

Update the Complete() method to accept image attachments in messages.

The agent.Message struct already has:
```go
type ImageAttachment struct {
    Data      []byte
    MediaType string
}
```

When building the Anthropic API request, if a message has Images:
- Add them as image content blocks BEFORE the text content block
- Format:
```go
anthropic.ImageBlockParam{
    Type: "image",
    Source: anthropic.ImageBlockParamSourceParam{
        Type:      "base64",
        MediaType: anthropic.ImageBlockParamSourceMediaTypeJpeg, // or png, gif, webp
        Data:      base64.StdEncoding.EncodeToString(img.Data),
    },
}
```

Claude supports up to 20 images per request. Enforce this limit.

### TASK 3: PDF Text Extraction

Create: internal/tools/vision.go

```go
// PDFExtractTool extracts text from PDF files passed as attachments.
// Uses pdftotext (poppler) if available, falls back to pure Go extraction.
type PDFExtractTool struct{ workspaceRoot string }
```

Implementation:
1. Write PDF bytes to a temp file in /tmp
2. Try: exec.Command("pdftotext", tmpFile, "-") — poppler is likely installed on Mac
3. If pdftotext unavailable, use: github.com/ledongthuc/pdf (pure Go)
4. Return extracted text (truncated at 100KB)
5. Clean up temp file with defer

Add to go.mod: go get github.com/ledongthuc/pdf

### TASK 4: analyze_image Tool

In internal/tools/vision.go, add:

```go
type AnalyzeImageTool struct {
    llm agent.LLMClient
}

func (t *AnalyzeImageTool) Name() string { return "analyze_image" }
```

This tool allows the agent to explicitly invoke vision analysis mid-conversation.
Input schema:
```json
{
  "path": "path to image file in workspace (if already saved)",
  "question": "what to look for or extract from the image"
}
```

Execute():
1. Read the image from workspace path
2. Build a one-shot LLM call with the image + question
3. Return Claude's analysis as the tool result

### TASK 5: Wire Attachments Through the Pipeline

In cmd/zbot/wire.go, update the handler to:
1. Accept []gateway.Attachment from the Slack gateway
2. Convert each attachment to agent.ImageAttachment (images) or call PDFExtract (PDFs)
3. Attach images to the agent.Message.Images field before calling ag.Run()
4. For PDFs: extract text first, prepend it to the user message text

```go
handler := func(ctx context.Context, sessionID, userID, text string, attachments []gateway.Attachment) (string, error) {
    var images []agent.ImageAttachment
    pdfText := ""
    
    for _, att := range attachments {
        switch att.MediaType {
        case "image/jpeg", "image/png", "image/gif", "image/webp":
            images = append(images, agent.ImageAttachment{
                Data:      att.Data,
                MediaType: att.MediaType,
            })
        case "application/pdf":
            extracted, err := extractPDF(att.Data)
            if err == nil {
                pdfText += "\n\n[PDF: " + att.Filename + "]\n" + extracted
            }
        }
    }
    
    userMsg := agent.Message{
        Role:      agent.RoleUser,
        SessionID: sessionID,
        Content:   text + pdfText,
        Images:    images,
        CreatedAt: time.Now(),
    }
    // ... rest of handler
}
```

### TASK 6: Update System Prompt

Add to the ZBOT system prompt in wire.go:

```
- analyze_image: Analyze photos, screenshots, charts, or any image you receive
- When images are sent directly in the chat, Claude's vision is automatically activated — describe what you see and answer any questions about it
- For PDFs: text is automatically extracted and included in your context
```

---

## Definition of Done

1. Send ZBOT a screenshot of anything (your desktop, a chart, a website).
2. Ask "what do you see?" — ZBOT describes it accurately using Claude vision.
3. Send ZBOT a PDF — ZBOT summarizes the contents.
4. Send ZBOT a screenshot of a spreadsheet — ZBOT extracts the data and offers to save it as CSV.
5. go build ./... passes clean.

---

## Go Dependencies

```bash
cd ~/Desktop/zbot
go get github.com/ledongthuc/pdf
```

Also check that poppler is installed: which pdftotext
If not: brew install poppler

---

## Git Commit

```bash
cd ~/Desktop/zbot
git add -A
git commit -m "Sprint 3: Vision live — image + PDF analysis via Claude multimodal

- internal/gateway/slack.go: File download + Attachment type
- internal/llm/anthropic.go: Multimodal image content blocks
- internal/tools/vision.go: AnalyzeImageTool + PDFExtractTool
- cmd/zbot/wire.go: Attachments wired through full pipeline
- ZBOT can now see images and read PDFs sent via Slack"
git push origin main
```

## Important Notes

- Never put secrets in code. All secrets via GCP Secret Manager only.
- go build ./... must pass after every change.
- Image size limit 20MB — reject gracefully with a message, don't crash.
- Claude supports JPEG, PNG, GIF, WEBP — reject other image types with a helpful message.
- Clean up all temp files — use defer os.Remove(tmpFile).
