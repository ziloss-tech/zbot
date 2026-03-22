package crawler

import "testing"

func TestNewGrid(t *testing.T) {
	g := NewGrid(1280, 960, 64)
	if g.Cols != 20 {
		t.Errorf("expected 20 cols, got %d", g.Cols)
	}
	if g.Rows != 15 {
		t.Errorf("expected 15 rows, got %d", g.Rows)
	}
}

func TestCellFromLabel(t *testing.T) {
	g := NewGrid(1280, 960, 64)

	tests := []struct {
		label   string
		centerX int
		centerY int
	}{
		{"A1", 32, 32},
		{"A2", 96, 32},
		{"B1", 32, 96},
		{"C7", 416, 160},
	}

	for _, tt := range tests {
		cell, err := g.CellFromLabel(tt.label)
		if err != nil {
			t.Errorf("CellFromLabel(%q): %v", tt.label, err)
			continue
		}
		if cell.CenterX != tt.centerX || cell.CenterY != tt.centerY {
			t.Errorf("CellFromLabel(%q): got center (%d,%d), want (%d,%d)",
				tt.label, cell.CenterX, cell.CenterY, tt.centerX, tt.centerY)
		}
	}
}

func TestLabelFromPixelRoundTrip(t *testing.T) {
	g := NewGrid(1280, 960, 64)
	cell, err := g.CellFromLabel("D5")
	if err != nil {
		t.Fatal(err)
	}
	got := g.LabelFromPixel(cell.CenterX, cell.CenterY)
	if got != "D5" {
		t.Errorf("round trip failed: got %q, want D5", got)
	}
}

func TestCellFromLabelOutOfBounds(t *testing.T) {
	g := NewGrid(1280, 960, 64)
	_, err := g.CellFromLabel("Z99")
	if err == nil {
		t.Error("expected error for out-of-bounds label")
	}
}

func TestAllCells(t *testing.T) {
	g := NewGrid(1280, 960, 64)
	cells := g.AllCells()
	if len(cells) != 300 {
		t.Errorf("expected 300 cells, got %d", len(cells))
	}
}
