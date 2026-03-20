// Package tools — Vision tools for ZBOT.
// AnalyzeImageTool: explicit image analysis via Claude vision.
// PDFExtractTool: extract text from PDF files.
package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ziloss-tech/zbot/internal/agent"
)

// ─── ANALYZE IMAGE ──────────────────────────────────────────────────────────

// AnalyzeImageTool allows the agent to explicitly invoke Claude vision
// analysis on an image file from the workspace.
type AnalyzeImageTool struct {
	llm           agent.LLMClient
	workspaceRoot string
}

func NewAnalyzeImageTool(llm agent.LLMClient, workspaceRoot string) *AnalyzeImageTool {
	return &AnalyzeImageTool{llm: llm, workspaceRoot: workspaceRoot}
}

func (t *AnalyzeImageTool) Name() string { return "analyze_image" }

func (t *AnalyzeImageTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "analyze_image",
		Description: "Analyze a photo, screenshot, chart, or any image. Use to extract text, describe contents, or answer questions about an image file in the workspace.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]any{
				"path":     map[string]any{"type": "string", "description": "Path to image file in workspace"},
				"question": map[string]any{"type": "string", "description": "What to look for or extract from the image (optional)"},
			},
		},
	}
}

func (t *AnalyzeImageTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	relPath, _ := input["path"].(string)
	question, _ := input["question"].(string)
	if relPath == "" {
		return &agent.ToolResult{Content: "error: path is required", IsError: true}, nil
	}

	// Validate path stays inside workspace.
	abs, err := safePath(t.workspaceRoot, relPath)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	// Read the image file.
	data, err := os.ReadFile(abs)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("error reading image: %v", err), IsError: true}, nil
	}

	// Determine media type from extension.
	mediaType := mediaTypeFromExt(filepath.Ext(abs))
	if mediaType == "" {
		return &agent.ToolResult{Content: "error: unsupported image format. Supported: JPEG, PNG, GIF, WEBP", IsError: true}, nil
	}

	// Build a one-shot LLM call with the image.
	prompt := "Describe this image in detail."
	if question != "" {
		prompt = question
	}

	messages := []agent.Message{
		{
			Role:    agent.RoleUser,
			Content: prompt,
			Images: []agent.ImageAttachment{
				{Data: data, MediaType: mediaType},
			},
		},
	}

	result, err := t.llm.Complete(ctx, messages, nil)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("vision analysis failed: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: result.Content}, nil
}

var _ agent.Tool = (*AnalyzeImageTool)(nil)

// mediaTypeFromExt maps file extensions to MIME types.
func mediaTypeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

// ─── PDF EXTRACT ────────────────────────────────────────────────────────────

// PDFExtractTool extracts text from PDF files passed as attachments or in the workspace.
// Uses pdftotext (poppler) if available, otherwise returns an error.
type PDFExtractTool struct {
	workspaceRoot string
}

func NewPDFExtractTool(workspaceRoot string) *PDFExtractTool {
	return &PDFExtractTool{workspaceRoot: workspaceRoot}
}

func (t *PDFExtractTool) Name() string { return "extract_pdf" }

func (t *PDFExtractTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{
		Name:        "extract_pdf",
		Description: "Extract text content from a PDF file in the workspace.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Path to PDF file in workspace"},
			},
		},
	}
}

func (t *PDFExtractTool) Execute(ctx context.Context, input map[string]any) (*agent.ToolResult, error) {
	relPath, _ := input["path"].(string)
	if relPath == "" {
		return &agent.ToolResult{Content: "error: path is required", IsError: true}, nil
	}

	abs, err := safePath(t.workspaceRoot, relPath)
	if err != nil {
		return &agent.ToolResult{Content: err.Error(), IsError: true}, nil
	}

	text, err := ExtractPDFFromFile(abs)
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("PDF extraction failed: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: text}, nil
}

var _ agent.Tool = (*PDFExtractTool)(nil)

// ExtractPDFFromFile extracts text from a PDF file on disk using pdftotext.
func ExtractPDFFromFile(path string) (string, error) {
	cmd := exec.Command("pdftotext", path, "-")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext failed: %w (is poppler installed? brew install poppler)", err)
	}

	text := string(out)
	// Truncate at 100KB.
	if len(text) > 100*1024 {
		text = text[:100*1024] + "\n[TRUNCATED — PDF text exceeds 100KB]"
	}

	return text, nil
}

// ExtractPDFFromBytes extracts text from PDF bytes by writing to a temp file.
func ExtractPDFFromBytes(data []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "zbot-pdf-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write temp: %w", err)
	}
	tmpFile.Close()

	return ExtractPDFFromFile(tmpFile.Name())
}
