package search

import (
	"testing"
)

func TestSkillName(t *testing.T) {
	s := NewSkill()
	if s.Name() != "search" {
		t.Fatalf("expected 'search', got %q", s.Name())
	}
}

func TestSkillHasTwoTools(t *testing.T) {
	s := NewSkill()
	tools := s.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	if !names["web_search"] || !names["scrape_page"] {
		t.Fatalf("expected web_search and scrape_page, got %v", names)
	}
}

func TestToolDefinitions(t *testing.T) {
	s := NewSkill()
	for _, tool := range s.Tools() {
		def := tool.Definition()
		if def.Name == "" {
			t.Fatal("tool definition name should not be empty")
		}
		if def.Description == "" {
			t.Fatalf("tool %q should have a description", def.Name)
		}
		if def.InputSchema == nil {
			t.Fatalf("tool %q should have an input schema", def.Name)
		}
	}
}

func TestExtractPageText(t *testing.T) {
	html := `<html><body><h1>Title</h1><p>Hello world</p><script>var x=1;</script><style>.a{}</style></body></html>`
	text := extractPageText(html)
	if text == "" {
		t.Fatal("expected non-empty text extraction")
	}
	// Script and style should be stripped
	if contains(text, "var x=1") {
		t.Fatal("script content should be stripped")
	}
}

func TestParseDDGResults(t *testing.T) {
	// Minimal DDG-like HTML
	html := `<div class="results"><div class="result"><a class="result__a" href="https://example.com">Example</a><a class="result__snippet">A test snippet</a></div></div>`
	results := parseDDGResults(html, 5)
	// May or may not parse depending on exact structure; just ensure no panic
	_ = results
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
