package crawler

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/go-rod/rod"
)

// MappedElement represents a DOM element mapped to grid coordinates
type MappedElement struct {
	Element     *ElementInfo `json:"element"`
	GridCells   []string     `json:"grid_cells"`
	PrimaryCell string       `json:"primary_cell"` // the "best" cell to click (center of element)
}

// rawElement represents the raw element data returned from JavaScript
type rawElement struct {
	Tag   string            `json:"tag"`
	Text  string            `json:"text"`
	Attrs map[string]string `json:"attrs"`
	Rect  struct {
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"rect"`
}

// InteractiveElements discovers all interactive elements on the page
func InteractiveElements(page *rod.Page) ([]*ElementInfo, error) {
	var rawElements []rawElement

	_, err := page.Eval(`() => {
		const selectors = 'a, button, input, select, textarea, [onclick], [role="button"], [role="link"], [tabindex]:not([tabindex="-1"]), label[for]';
		const els = document.querySelectorAll(selectors);
		const results = [];

		for (const el of els) {
			const rect = el.getBoundingClientRect();

			// Skip hidden or zero-size elements
			if (rect.width === 0 || rect.height === 0) continue;
			if (rect.bottom < 0 || rect.right < 0) continue;

			const style = window.getComputedStyle(el);
			if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') continue;

			// Extract relevant attributes
			const attrs = {};
			const attrNames = ['href', 'placeholder', 'type', 'name', 'value', 'class', 'id', 'aria-label', 'role', 'title', 'src', 'alt'];
			for (const a of attrNames) {
				const v = el.getAttribute(a);
				if (v) {
					attrs[a] = v.substring(0, 200);
				}
			}

			// Extract text content
			let text = (el.innerText || el.textContent || '').trim();
			if (text.length > 100) {
				text = text.substring(0, 100) + '...';
			}

			results.push({
				tag: el.tagName.toLowerCase(),
				text: text,
				attrs: attrs,
				rect: {
					x: rect.x,
					y: rect.y,
					width: rect.width,
					height: rect.height
				}
			});
		}

		return results;
	}`, &rawElements)

	if err != nil {
		return nil, fmt.Errorf("failed to evaluate interactive elements: %w", err)
	}

	// Convert raw elements to ElementInfo
	elements := make([]*ElementInfo, len(rawElements))
	for i, raw := range rawElements {
		elements[i] = &ElementInfo{
			Tag:        raw.Tag,
			Text:       raw.Text,
			Attrs:      raw.Attrs,
			BoundingBox: &Rect{
				X:      raw.Rect.X,
				Y:      raw.Rect.Y,
				Width:  raw.Rect.Width,
				Height: raw.Rect.Height,
			},
		}
	}

	return elements, nil
}

// MapElementsToGrid maps elements to grid coordinates
func MapElementsToGrid(elements []*ElementInfo, grid *Grid) []MappedElement {
	mapped := make([]MappedElement, 0, len(elements))

	for _, elem := range elements {
		// Find all grid cells that this element overlaps
		cells := grid.CellsFromRect(int(elem.BoundingBox.X), int(elem.BoundingBox.Y), int(elem.BoundingBox.X+elem.BoundingBox.Width), int(elem.BoundingBox.Y+elem.BoundingBox.Height))
		if len(cells) == 0 {
			continue
		}

		// Calculate the primary cell (closest to element center)
		centerX := elem.BoundingBox.X + elem.BoundingBox.Width/2
		centerY := elem.BoundingBox.Y + elem.BoundingBox.Height/2
		primaryCell := findClosestCell(grid, centerX, centerY, cells)

		mapped = append(mapped, MappedElement{
			Element:     elem,
			GridCells:   cells,
			PrimaryCell: primaryCell,
		})
	}

	// Sort by grid position (top to bottom, left to right)
	sort.Slice(mapped, func(i, j int) bool {
		cellI := mapped[i].PrimaryCell
		cellJ := mapped[j].PrimaryCell
		return compareGridPositions(cellI, cellJ) < 0
	})

	return mapped
}

// findClosestCell finds the grid cell closest to a given point
func findClosestCell(grid *Grid, x, y float64, cells []string) string {
	if len(cells) == 0 {
		return ""
	}
	if len(cells) == 1 {
		return cells[0]
	}

	minDist := math.MaxFloat64
	closest := cells[0]

	for _, cell := range cells {
		gridCell, err := grid.CellFromLabel(cell)
		if err != nil {
			continue
		}
		cellX := float64(gridCell.CenterX)
		cellY := float64(gridCell.CenterY)
		dist := math.Sqrt((x-cellX)*(x-cellX) + (y-cellY)*(y-cellY))
		if dist < minDist {
			minDist = dist
			closest = cell
		}
	}

	return closest
}

// compareGridPositions compares two grid cell positions (e.g., "A1", "B3")
// Returns negative if a < b, 0 if equal, positive if a > b
func compareGridPositions(a, b string) int {
	// Parse columns and rows from cell coordinates
	aCol, aRow := parseGridCell(a)
	bCol, bRow := parseGridCell(b)

	// Compare by row first (top to bottom), then column (left to right)
	if aRow != bRow {
		return aRow - bRow
	}
	return aCol - bCol
}

// parseGridCell parses a grid cell (e.g., "A1" or "B3") into column and row indices
func parseGridCell(cell string) (int, int) {
	if len(cell) < 2 {
		return 0, 0
	}

	// Extract column letter(s) and row number
	col := 0
	row := 0

	i := 0
	for i < len(cell) && cell[i] >= 'A' && cell[i] <= 'Z' {
		col = col*26 + int(cell[i]-'A') + 1
		i++
	}

	// Parse remaining digits as row
	if i < len(cell) {
		fmt.Sscanf(cell[i:], "%d", &row)
	}

	return col, row
}

// FormatElementList formats mapped elements for Cortex display
func FormatElementList(mapped []MappedElement) string {
	if len(mapped) == 0 {
		return "No interactive elements found"
	}

	lines := make([]string, 0, len(mapped))

	for _, m := range mapped {
		line := formatElement(m)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// formatElement formats a single mapped element
func formatElement(m MappedElement) string {
	elem := m.Element
	cell := m.PrimaryCell
	tag := elem.Tag

	// Build the attribute and text portion
	var parts []string

	// Add relevant attributes based on tag type
	switch tag {
	case "a":
		if href, ok := elem.Attrs["href"]; ok {
			parts = append(parts, fmt.Sprintf("href='%s'", truncateAttr(href, 80)))
		}
	case "input":
		if placeholder, ok := elem.Attrs["placeholder"]; ok {
			parts = append(parts, fmt.Sprintf("placeholder='%s'", truncateAttr(placeholder, 60)))
		}
		if inputType, ok := elem.Attrs["type"]; ok {
			parts = append(parts, fmt.Sprintf("type='%s'", inputType))
		}
		if name, ok := elem.Attrs["name"]; ok {
			parts = append(parts, fmt.Sprintf("name='%s'", truncateAttr(name, 40)))
		}
	case "select", "textarea":
		if name, ok := elem.Attrs["name"]; ok {
			parts = append(parts, fmt.Sprintf("name='%s'", truncateAttr(name, 40)))
		}
	}

	// Add aria-label if present and no specific attributes were added
	if len(parts) == 0 {
		if label, ok := elem.Attrs["aria-label"]; ok {
			parts = append(parts, fmt.Sprintf("aria-label='%s'", truncateAttr(label, 60)))
		}
	}

	// Add text content
	text := truncateText(elem.Text, 50)
	if text != "" {
		parts = append(parts, fmt.Sprintf("'%s'", text))
	}

	// Build final output: "CELL: <TAG> attrs 'text'"
	if len(parts) > 0 {
		return fmt.Sprintf("%s: <%s> %s", cell, tag, strings.Join(parts, " "))
	}
	return fmt.Sprintf("%s: <%s>", cell, tag)
}

// truncateText truncates text to specified length
func truncateText(text string, maxLen int) string {
	if len(text) > maxLen {
		return text[:maxLen] + "..."
	}
	return text
}

// truncateAttr truncates attribute values to specified length
func truncateAttr(attr string, maxLen int) string {
	if len(attr) > maxLen {
		return attr[:maxLen] + "..."
	}
	return attr
}

// GetInteractiveElements is a convenience method on Crawler
func (c *Crawler) GetInteractiveElements() ([]MappedElement, error) {
	if c.page == nil {
		return nil, fmt.Errorf("crawler page is not initialized")
	}

	elements, err := InteractiveElements(c.page)
	if err != nil {
		return nil, err
	}

	if c.grid == nil {
		return nil, fmt.Errorf("crawler grid is not initialized")
	}

	mapped := MapElementsToGrid(elements, c.grid)
	return mapped, nil
}

// FormatPageElements returns a formatted string of all interactive elements for Cortex
func (c *Crawler) FormatPageElements() (string, error) {
	if c.page == nil {
		return "", fmt.Errorf("crawler page is not initialized")
	}

	// Get page title and URL
	result, err := c.page.Eval("() => document.title")
	title := "Unknown"
	if err == nil && !result.Value.Nil() {
		title = result.Value.String()
	}

	urlStr := c.page.MustInfo().URL
	if urlStr == "" {
		urlStr = "Unknown"
	}

	// Get interactive elements
	mapped, err := c.GetInteractiveElements()
	if err != nil {
		return "", fmt.Errorf("failed to get interactive elements: %w", err)
	}

	// Format the elements
	elementList := FormatElementList(mapped)

	// Build final output
	output := fmt.Sprintf("Page: %s | URL: %s\nInteractive elements:\n%s", title, urlStr, elementList)
	return output, nil
}
