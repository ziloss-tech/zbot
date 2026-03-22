package crawler

import (
	"fmt"
	"strings"
)

// Grid divides a browser viewport into labeled cells for precise click targeting.
// Cells are labeled by row (A-Z, AA-AZ for >26) and column (1-N).
// Example: "C7" = Row C (3rd row), Column 7.
type Grid struct {
	CellWidth  int `json:"cell_width"`
	CellHeight int `json:"cell_height"`
	Cols       int `json:"cols"`
	Rows       int `json:"rows"`
	ViewportW  int `json:"viewport_width"`
	ViewportH  int `json:"viewport_height"`
}

// GridCell represents a single cell in the grid with its pixel coordinates.
type GridCell struct {
	Label    string `json:"label"`
	Row      int    `json:"row"`
	Col      int    `json:"col"`
	CenterX  int    `json:"center_x"`
	CenterY  int    `json:"center_y"`
	TopLeftX int    `json:"top_left_x"`
	TopLeftY int    `json:"top_left_y"`
	BotRightX int  `json:"bot_right_x"`
	BotRightY int  `json:"bot_right_y"`
}

// NewGrid creates a grid for the given viewport and cell size.
// Default cell size is 64px if cellSize <= 0.
func NewGrid(viewportW, viewportH, cellSize int) *Grid {
	if cellSize <= 0 {
		cellSize = 64
	}
	return &Grid{
		CellWidth:  cellSize,
		CellHeight: cellSize,
		Cols:       viewportW / cellSize,
		Rows:       viewportH / cellSize,
		ViewportW:  viewportW,
		ViewportH:  viewportH,
	}
}

// rowLabel converts a 0-based row index to a letter label (A, B, ... Z, AA, AB, ...).
func rowLabel(row int) string {
	if row < 26 {
		return string(rune('A' + row))
	}
	return string(rune('A'+row/26-1)) + string(rune('A'+row%26))
}

// parseRowLabel converts a letter label back to a 0-based row index.
func parseRowLabel(label string) (int, error) {
	label = strings.ToUpper(label)
	if len(label) == 1 {
		r := rune(label[0])
		if r < 'A' || r > 'Z' {
			return 0, fmt.Errorf("invalid row label: %s", label)
		}
		return int(r - 'A'), nil
	}
	if len(label) == 2 {
		r0, r1 := rune(label[0]), rune(label[1])
		if r0 < 'A' || r0 > 'Z' || r1 < 'A' || r1 > 'Z' {
			return 0, fmt.Errorf("invalid row label: %s", label)
		}
		return int(r0-'A'+1)*26 + int(r1-'A'), nil
	}
	return 0, fmt.Errorf("row label too long: %s", label)
}

// CellFromLabel parses a grid label like "C7" into a GridCell with pixel coordinates.
func (g *Grid) CellFromLabel(label string) (GridCell, error) {
	label = strings.TrimSpace(strings.ToUpper(label))
	if len(label) < 2 {
		return GridCell{}, fmt.Errorf("invalid grid label: %q", label)
	}

	// Split into row letters and column number.
	var rowPart string
	var colPart string
	for i, r := range label {
		if r >= '0' && r <= '9' {
			rowPart = label[:i]
			colPart = label[i:]
			break
		}
	}
	if rowPart == "" || colPart == "" {
		return GridCell{}, fmt.Errorf("invalid grid label: %q", label)
	}

	row, err := parseRowLabel(rowPart)
	if err != nil {
		return GridCell{}, err
	}

	var col int
	if _, err := fmt.Sscanf(colPart, "%d", &col); err != nil {
		return GridCell{}, fmt.Errorf("invalid column in label %q: %w", label, err)
	}
	col-- // Convert 1-based to 0-based.

	if row < 0 || row >= g.Rows || col < 0 || col >= g.Cols {
		return GridCell{}, fmt.Errorf("grid label %q out of bounds (rows=%d, cols=%d)", label, g.Rows, g.Cols)
	}

	return g.cellAt(row, col), nil
}

// cellAt returns the GridCell at the given 0-based row and column.
func (g *Grid) cellAt(row, col int) GridCell {
	tlx := col * g.CellWidth
	tly := row * g.CellHeight
	return GridCell{
		Label:     fmt.Sprintf("%s%d", rowLabel(row), col+1),
		Row:       row,
		Col:       col,
		CenterX:   tlx + g.CellWidth/2,
		CenterY:   tly + g.CellHeight/2,
		TopLeftX:  tlx,
		TopLeftY:  tly,
		BotRightX: tlx + g.CellWidth,
		BotRightY: tly + g.CellHeight,
	}
}

// LabelFromPixel returns the grid label for the cell containing the given pixel.
func (g *Grid) LabelFromPixel(x, y int) string {
	col := x / g.CellWidth
	row := y / g.CellHeight
	if col >= g.Cols {
		col = g.Cols - 1
	}
	if row >= g.Rows {
		row = g.Rows - 1
	}
	if col < 0 {
		col = 0
	}
	if row < 0 {
		row = 0
	}
	return fmt.Sprintf("%s%d", rowLabel(row), col+1)
}

// AllCells returns every cell in the grid.
func (g *Grid) AllCells() []GridCell {
	cells := make([]GridCell, 0, g.Rows*g.Cols)
	for r := 0; r < g.Rows; r++ {
		for c := 0; c < g.Cols; c++ {
			cells = append(cells, g.cellAt(r, c))
		}
	}
	return cells
}
