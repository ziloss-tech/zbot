// Package main — utility functions used by wire.go and commands.go.
// Extracted from wire.go for readability.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// isWorkflowRequest returns true for natural-language workflow trigger phrases.
func isWorkflowRequest(text string) bool {
	lower := strings.ToLower(text)
	triggers := []string{
		"research and compare", "do all of this", "run a workflow",
		"research 5 ", "research 10 ", "analyze all ", "find and compare",
	}
	for _, t := range triggers {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}

// extractPDF extracts text from a PDF byte slice via pdftotext (poppler).
func extractPDF(data []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "zbot-pdf-*.pdf")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	cmd := exec.CommandContext(context.Background(), "pdftotext", tmpFile.Name(), "-")
	out, err := cmd.Output()
	if err == nil && len(out) > 0 {
		text := string(out)
		if len(text) > 100*1024 {
			text = text[:100*1024] + "\n[TRUNCATED — PDF text exceeds 100KB]"
		}
		return text, nil
	}

	return "", fmt.Errorf("pdftotext unavailable or failed: %w", err)
}

// randomID generates a short hex ID.
func randomID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// truncateStr shortens a string for display.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
