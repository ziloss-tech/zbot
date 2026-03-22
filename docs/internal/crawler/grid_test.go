package crawler

import (
	"testing"
)

// TestNewGrid verifies rows/cols computation for different viewports.
func TestNewGrid(t *testing.T) {
	tests := []struct {
		name      string
		viewportW int
		viewportH int
		cellSize  int
		expCols   int
		expRows   int
	}{
		{
			name:      "1280x960 with 64px cells",
			viewportW: 1280,
			viewportH: 960,
			cellSize:  64,
			expCols:   20,
			expRows:   15,
		},
		{
			name:      "640x480 with 64px cells",
			viewportW: 640,
			viewportH: 480,
			cellSize:  64,
			expCols:   10,
			expRows:   7,
		},
		{
			name:      "1024x768 with 32px cells",
			viewportW: 1024,
			viewportH: 768,
			cellSize:  32,
			expCols:   32,
			expRows:   24,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGrid(tt.viewportW, tt.viewportH, tt.cellSize)
			if g.Cols != tt.expCols {
				t.Errorf("expected %d cols, got %d", tt.expCols, g.Cols)
			}
			if g.Rows != tt.expRows {
				t.Errorf("expected %d rows, got %d", tt.expRows, g.Rows)
			}
		})
	}
}

// TestDefaultGrid verifies 1280×960 = 20 cols × 15 rows.
func TestDefaultGrid(t *testing.T) {
	g := DefaultGrid()

	if g.ViewportW != 1280 {
		t.Errorf("expected viewport width 1280, got %d", g.ViewportW)
	}
	if g.ViewportH != 960 {
		t.Errorf("expected viewport height 960, got %d", g.ViewportH)
	}
	if g.Cols != 20 {
		t.Errorf("expected 20 cols, got %d", g.Cols)
	}
	if g.Rows != 15 {
		t.Errorf("expected 15 rows, got %d", g.Rows)
	}
	if g.CellWidth != 64 {
		t.Errorf("expected cell width 64, got %d", g.CellWidth)
	}
	if g.CellHeight != 64 {
		t.Errorf("expected cell height 64, got %d", g.CellHeight)
	}
}

// TestCellFromLabel tests cell creation from labels with expected pixel centers.
func TestCellFromLabel(t *testing.T) {
	g := DefaultGrid()

	tests := []struct {
		name      string
		label     string
		expCenterX int
		expCenterY int
		expRow    int
		expCol    int
	}{
		{
			name:       "A1",
			label:      "A1",
			expCenterX: 32,  // (0*64) + 32
			expCenterY: 32,  // (0*64) + 32
			expRow:     0,
			expCol:     0,
		},
		{
			name:       "C7",
			label:      "C7",
			expCenterX: 416, // (6*64) + 32
			expCenterY: 160, // (2*64) + 32
			expRow:     2,
			expCol:     6,
		},
		{
			name:       "O20",
			label:      "O20",
			expCenterX: 1248, // (19*64) + 32
			expCenterY: 928,  // (14*64) + 32
			expRow:     14,
			expCol:     19,
		},
		{
			name:       "K10",
			label:      "K10",
			expCenterX: 608, // (9*64) + 32
			expCenterY: 672, // (10*64) + 32
			expRow:     10,
			expCol:     9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cell, err := g.CellFromLabel(tt.label)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cell.Label != tt.label {
				t.Errorf("expected label %s, got %s", tt.label, cell.Label)
			}
			if cell.Row != tt.expRow {
				t.Errorf("expected row %d, got %d", tt.expRow, cell.Row)
			}
			if cell.Col != tt.expCol {
				t.Errorf("expected col %d, got %d", tt.expCol, cell.Col)
			}
			if cell.CenterX != tt.expCenterX {
				t.Errorf("expected centerX %d, got %d", tt.expCenterX, cell.CenterX)
			}
			if cell.CenterY != tt.expCenterY {
				t.Errorf("expected centerY %d, got %d", tt.expCenterY, cell.CenterY)
			}
		})
	}
}

// TestCellFromLabel_Invalid tests that invalid labels return errors.
func TestCellFromLabel_Invalid(t *testing.T) {
	g := DefaultGrid()

	tests := []struct {
		name  string
		label string
	}{
		{
			name:  "empty string",
			label: "",
		},
		{
			name:  "no column number",
			label: "A",
		},
		{
			name:  "no row letter",
			label: "7",
		},
		{
			name:  "invalid format with spaces",
			label: "A 7",
		},
		{
			name:  "column 0 (out of bounds)",
			label: "A0",
		},
		{
			name:  "row out of bounds",
			label: "AB1",
		},
		{
			name:  "column out of bounds",
			label: "A25",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := g.CellFromLabel(tt.label)
			if err == nil {
				t.Errorf("expected error for label %q, got nil", tt.label)
			}
		})
	}
}

// TestLabelFromPixel tests pixel-to-label conversion.
func TestLabelFromPixel(t *testing.T) {
	g := DefaultGrid()

	tests := []struct {
		name      string
		x         int
		y         int
		expLabel  string
	}{
		{
			name:     "top-left corner (A1)",
			x:        0,
			y:        0,
			expLabel: "A1",
		},
		{
			name:     "center of C7",
			x:        416,
			y:        160,
			expLabel: "C7",
		},
		{
			name:     "edge of C7 cell (start of C8)",
			x:        448,
			y:        160,
			expLabel: "C8",
		},
		{
			name:     "last cell top-left",
			x:        1216,
			y:        896,
			expLabel: "O20",
		},
		{
			name:     "pixel beyond viewport (clamped)",
			x:        2000,
			y:        2000,
			expLabel: "O20",
		},
		{
			name:     "negative pixels (clamped)",
			x:        -10,
			y:        -10,
			expLabel: "A1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label := g.LabelFromPixel(tt.x, tt.y)
			if label != tt.expLabel {
				t.Errorf("expected label %s, got %s", tt.expLabel, label)
			}
		})
	}
}

// TestLabelFromPixel_RoundTrip verifies CellFromLabel then LabelFromPixel(centerX, centerY) returns same label.
func TestLabelFromPixel_RoundTrip(t *testing.T) {
	g := DefaultGrid()

	labels := []string{"A1", "C7", "O20", "K10", "B15"}

	for _, label := range labels {
		t.Run(label, func(t *testing.T) {
			cell, err := g.CellFromLabel(label)
			if err != nil {
				t.Fatalf("CellFromLabel failed: %v", err)
			}

			roundTrip := g.LabelFromPixel(cell.CenterX, cell.CenterY)
			if roundTrip != label {
				t.Errorf("expected %s, got %s", label, roundTrip)
			}
		})
	}
}

// TestAllCells verifies count = rows × cols.
func TestAllCells(t *testing.T) {
	g := DefaultGrid()
	cells := g.AllCells()

	expectedCount := g.Rows * g.Cols
	if len(cells) != expectedCount {
		t.Errorf("expected %d cells, got %d", expectedCount, len(cells))
	}

	// Verify all cells have non-empty labels and valid coordinates
	for i, cell := range cells {
		if cell.Label == "" {
			t.Errorf("cell %d has empty label", i)
		}
		if cell.CenterX < 0 || cell.CenterX >= g.ViewportW {
			t.Errorf("cell %d has invalid centerX: %d", i, cell.CenterX)
		}
		if cell.CenterY < 0 || cell.CenterY >= g.ViewportH {
			t.Errorf("cell %d has invalid centerY: %d", i, cell.CenterY)
		}
	}
}

// TestCellsFromRect verifies bounding box returns correct overlapping cells.
func TestCellsFromRect(t *testing.T) {
	g := DefaultGrid()

	tests := []struct {
		name      string
		x1, y1    int
		x2, y2    int
		expLabels []string
	}{
		{
			name:      "single cell A1",
			x1:        0,
			y1:        0,
			x2:        32,
			y2:        32,
			expLabels: []string{"A1"},
		},
		{
			name:      "2x2 cell block",
			x1:        0,
			y1:        0,
			x2:        127,
			y2:        127,
			expLabels: []string{"A1", "A2", "B1", "B2"},
		},
		{
			name:      "spanning C7 to D8",
			x1:        400,
			y1:        150,
			x2:        500,
			y2:        250,
			expLabels: []string{"C7", "C8", "D7", "D8"},
		},
		{
			name:      "single row",
			x1:        0,
			y1:        64,
			x2:        191,
			y2:        100,
			expLabels: []string{"B1", "B2", "B3"},
		},
		{
			name:      "single column",
			x1:        100,
			y1:        0,
			x2:        120,
			y2:        191,
			expLabels: []string{"A2", "B2", "C2"},
		},
		{
			name:      "out of bounds clamped",
			x1:        -50,
			y1:        -50,
			x2:        200,
			y2:        200,
			expLabels: []string{"A1", "A2", "A3", "A4", "B1", "B2", "B3", "B4", "C1", "C2", "C3", "C4", "D1", "D2", "D3", "D4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := g.CellsFromRect(tt.x1, tt.y1, tt.x2, tt.y2)
			if len(labels) != len(tt.expLabels) {
				t.Errorf("expected %d labels, got %d: %v", len(tt.expLabels), len(labels), labels)
				return
			}

			labelMap := make(map[string]bool)
			for _, label := range labels {
				labelMap[label] = true
			}

			for _, expLabel := range tt.expLabels {
				if !labelMap[expLabel] {
					t.Errorf("expected label %s not found in results", expLabel)
				}
			}
		})
	}
}

// TestRowToLabel tests row index to letter conversion.
func TestRowToLabel(t *testing.T) {
	tests := []struct {
		row       int
		expLabel  string
	}{
		{0, "A"},
		{1, "B"},
		{25, "Z"},
		{26, "AA"},
		{27, "AB"},
		{51, "AZ"},
		{52, "BA"},
	}

	for _, tt := range tests {
		t.Run(tt.expLabel, func(t *testing.T) {
			label := rowToLabel(tt.row)
			if label != tt.expLabel {
				t.Errorf("row %d: expected %s, got %s", tt.row, tt.expLabel, label)
			}
		})
	}
}

// TestLabelToRow tests letter label to row index conversion.
func TestLabelToRow(t *testing.T) {
	tests := []struct {
		label    string
		expRow   int
		shouldErr bool
	}{
		{"A", 0, false},
		{"B", 1, false},
		{"Z", 25, false},
		{"AA", 26, false},
		{"AB", 27, false},
		{"AZ", 51, false},
		{"BA", 52, false},
		{"a", 0, false}, // lowercase should work (converted to uppercase)
		{"z", 25, false},
		{"", 0, true},    // empty should error
		{"1", 0, true},   // number should error
		{"A1", 0, true},  // number in label should error
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			row, err := labelToRow(tt.label)
			if tt.shouldErr && err == nil {
				t.Errorf("expected error for label %q, got nil", tt.label)
				return
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("unexpected error for label %q: %v", tt.label, err)
				return
			}
			if !tt.shouldErr && row != tt.expRow {
				t.Errorf("label %q: expected row %d, got %d", tt.label, tt.expRow, row)
			}
		})
	}
}

// TestParseLabel tests parsing edge cases.
func TestParseLabel(t *testing.T) {
	tests := []struct {
		label     string
		expRow    int
		expCol    int
		shouldErr bool
	}{
		{"A1", 0, 0, false},
		{"B5", 1, 4, false},
		{"Z20", 25, 19, false},
		{"AA1", 26, 0, false},
		{"AB7", 27, 6, false},
		{"", 0, 0, true},
		{"A", 0, 0, true},
		{"A0", 0, 0, true},        // column 0 is invalid
		{"123", 0, 0, true},       // no row letters
		{"ABC", 0, 0, true},       // no column number
		{"A 5", 0, 0, true},       // space in label
		{"a7", 0, 6, false},       // lowercase should work
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			row, col, err := parseLabel(tt.label)
			if tt.shouldErr && err == nil {
				t.Errorf("expected error for label %q, got nil", tt.label)
				return
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("unexpected error for label %q: %v", tt.label, err)
				return
			}
			if !tt.shouldErr {
				if row != tt.expRow {
					t.Errorf("label %q: expected row %d, got %d", tt.label, tt.expRow, row)
				}
				if col != tt.expCol {
					t.Errorf("label %q: expected col %d, got %d", tt.label, tt.expCol, col)
				}
			}
		})
	}
}

// TestOverlayData verifies OverlayData JSON generation.
func TestOverlayData(t *testing.T) {
	g := DefaultGrid()
	overlay := g.OverlayData()

	if overlay.Rows != g.Rows {
		t.Errorf("expected %d rows in overlay, got %d", g.Rows, overlay.Rows)
	}
	if overlay.Cols != g.Cols {
		t.Errorf("expected %d cols in overlay, got %d", g.Cols, overlay.Cols)
	}
	if overlay.CellW != g.CellWidth {
		t.Errorf("expected cell width %d, got %d", g.CellWidth, overlay.CellW)
	}
	if overlay.CellH != g.CellHeight {
		t.Errorf("expected cell height %d, got %d", g.CellHeight, overlay.CellH)
	}
	if len(overlay.Cells) != g.Rows*g.Cols {
		t.Errorf("expected %d cells, got %d", g.Rows*g.Cols, len(overlay.Cells))
	}
}
