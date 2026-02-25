package scraper

import (
	"strings"

	"golang.org/x/net/html"
)

// skipTags are HTML elements whose content should be completely skipped.
var skipTags = map[string]bool{
	"script":   true,
	"style":    true,
	"noscript": true,
	"nav":      true,
	"header":   true,
	"footer":   true,
	"aside":    true,
	"svg":      true,
	"form":     true,
	"iframe":   true,
}

// ExtractText strips HTML and extracts readable content.
// Removes: scripts, styles, nav, header, footer, ads.
// Returns clean article text suitable for the LLM context.
func ExtractText(rawHTML string) string {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		// If HTML parsing fails, do basic tag stripping.
		return basicStripTags(rawHTML)
	}

	var sb strings.Builder
	extractNode(&sb, doc)

	// Clean up: collapse multiple newlines, trim whitespace.
	text := sb.String()
	text = collapseWhitespace(text)
	text = strings.TrimSpace(text)

	return text
}

// extractNode recursively walks the HTML tree and extracts text.
func extractNode(sb *strings.Builder, n *html.Node) {
	if n.Type == html.ElementNode {
		// Skip elements that don't contain useful content.
		if skipTags[n.Data] {
			return
		}

		// Add line breaks for block elements.
		if isBlockElement(n.Data) {
			sb.WriteString("\n")
		}
	}

	if n.Type == html.TextNode {
		text := strings.TrimSpace(n.Data)
		if text != "" {
			sb.WriteString(text)
			sb.WriteString(" ")
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractNode(sb, c)
	}

	if n.Type == html.ElementNode && isBlockElement(n.Data) {
		sb.WriteString("\n")
	}
}

// isBlockElement returns true for HTML elements that should get line breaks.
func isBlockElement(tag string) bool {
	switch tag {
	case "div", "p", "br", "h1", "h2", "h3", "h4", "h5", "h6",
		"li", "tr", "td", "th", "blockquote", "pre", "article",
		"section", "main", "dt", "dd":
		return true
	}
	return false
}

// collapseWhitespace reduces multiple blank lines to at most two newlines.
func collapseWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	blankCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			blankCount++
			if blankCount <= 1 {
				result = append(result, "")
			}
		} else {
			blankCount = 0
			result = append(result, trimmed)
		}
	}

	return strings.Join(result, "\n")
}

// basicStripTags is a fallback that removes HTML tags with basic string ops.
func basicStripTags(s string) string {
	var sb strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			sb.WriteRune(' ')
		case !inTag:
			sb.WriteRune(r)
		}
	}
	return collapseWhitespace(sb.String())
}
