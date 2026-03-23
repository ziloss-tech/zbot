package crawler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Grid struct {
	CellWidth  int `json:"cell_width"`
	CellHeight int `json:"cell_height"`
	Cols       int `json:"cols"`
	Rows       int `json:"rows"`
	ViewportW  int `json:"viewport_w"`
	ViewportH  int `json:"viewport_h"`
}

type GridCell struct {
	Label    string `json:"label"`
	Row      int    `json:"row"`
	Col      int    `json:"col"`
	CenterX  int    `json:"center_x"`
	CenterY  int    `json:"center_y"`
	TopLeft  Point  `json:"top_left"`
	BotRight Point  `json:"bot_right"`
}

type GridOverlayJSON struct {
	Rows      int        `json:"rows"`
	Cols      int        `json:"cols"`
	CellW     int        `json:"cell_w"`
	CellH     int        `json:"cell_h"`
	ViewportW int        `json:"viewport_w"`
	ViewportH int        `json:"viewport_h"`
	Cells     []GridCell `json:"cells"`
}

// NewGrid creates a new grid with given viewport dimensions and cell size.
// It computes the number of rows and columns based on the viewport and cell size.
func NewGrid(viewportW, viewportH, cellSize int) *Grid {
	if cellSize <= 0 {
		cellSize = 64
	}
	if viewportW <= 0 {
		viewportW = 1280
	}
	if viewportH <= 0 {
		viewportH = 960
	}

	cols := viewportW / cellSize
	rows := viewportH / cellSize

	return &Grid{
		CellWidth:  cellSize,
		CellHeight: cellSize,
		Cols:       cols,
		Rows:       rows,
		ViewportW:  viewportW,
		ViewportH:  viewportH,
	}
}

// DefaultGrid returns a grid with 1280×960 viewport and 64px cells.
// This gives 20 columns × 15 rows.
func DefaultGrid() *Grid {
	return NewGrid(1280, 960, 64)
}

// rowToLabel converts a row index (0-based) to a letter label.
// 0 -> "A", 25 -> "Z", 26 -> "AA", 27 -> "AB", etc.
func rowToLabel(row int) string {
	if row < 0 {
		return ""
	}

	var result string
	for {
		result = string(rune('A'+(row%26))) + result
		row /= 26
		if row == 0 {
			break
		}
		row-- // Adjust for AA, AB, ... (26, 27) mapping
	}
	return result
}

// labelToRow converts a letter label to a row index (0-based).
// "A" -> 0, "Z" -> 25, "AA" -> 26, "AB" -> 27, etc.
func labelToRow(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty row label")
	}

	s = strings.ToUpper(s)
	row := 0

	for i, ch := range s {
		if ch < 'A' || ch > 'Z' {
			return 0, fmt.Errorf("invalid character in row label: %c", ch)
		}

		if i > 0 {
			row = (row + 1) * 26
		}
		row += int(ch - 'A')
	}

	return row, nil
}

// parseLabel splits a grid label like "C7" into row and column indices (0-based).
func parseLabel(label string) (row int, col int, err error) {
	if label == "" {
		return 0, 0, fmt.Errorf("empty label")
	}

	// Use regex to split letters and numbers
	re := regexp.MustCompile(`^([A-Za-z]+)(\d+)$`)
	matches := re.FindStringSubmatch(label)
	if len(matches) != 3 {
		return 0, 0, fmt.Errorf("invalid label format: %s (expected format like 'C7')", label)
	}

	rowLabel := matches[1]
	colStr := matches[2]

	// Parse row
	row, err = labelToRow(rowLabel)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid row label '%s': %w", rowLabel, err)
	}

	// Parse column (1-based in label, convert to 0-based)
	colNum, err := strconv.Atoi(colStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid column number '%s': %w", colStr, err)
	}

	if colNum < 1 {
		return 0, 0, fmt.Errorf("column number must be >= 1, got %d", colNum)
	}

	col = colNum - 1 // Convert to 0-based

	return row, col, nil
}

// CellFromLabel converts a label like "C7" into a GridCell with pixel coordinates.
func (g *Grid) CellFromLabel(label string) (GridCell, error) {
	row, col, err := parseLabel(label)
	if err != nil {
		return GridCell{}, err
	}

	if row >= g.Rows {
		return GridCell{}, fmt.Errorf("row %d out of bounds (grid has %d rows)", row, g.Rows)
	}

	if col >= g.Cols {
		return GridCell{}, fmt.Errorf("column %d out of bounds (grid has %d columns)", col, g.Cols)
	}

	topLeftX := col * g.CellWidth
	topLeftY := row * g.CellHeight
	centerX := topLeftX + g.CellWidth/2
	centerY := topLeftY + g.CellHeight/2
	botRightX := topLeftX + g.CellWidth
	botRightY := topLeftY + g.CellHeight

	return GridCell{
		Label:    label,
		Row:      row,
		Col:      col,
		CenterX:  centerX,
		CenterY:  centerY,
		TopLeft:  Point{X: topLeftX, Y: topLeftY},
		BotRight: Point{X: botRightX, Y: botRightY},
	}, nil
}

// LabelFromPixel converts pixel coordinates to a grid label.
// Returns the label of the cell containing the pixel (x, y).
func (g *Grid) LabelFromPixel(x, y int) string {
	// Clamp to viewport bounds
	if x < 0 {
		x = 0
	}
	if x >= g.ViewportW {
		x = g.ViewportW - 1
	}
	if y < 0 {
		y = 0
	}
	if y >= g.ViewportH {
		y = g.ViewportH - 1
	}

	col := x / g.CellWidth
	row := y / g.CellHeight

	rowLabel := rowToLabel(row)
	colNum := col + 1 // Convert 0-based to 1-based

	return fmt.Sprintf("%s%d", rowLabel, colNum)
}

// AllCells returns a slice of all GridCell objects in the grid.
func (g *Grid) AllCells() []GridCell {
	cells := make([]GridCell, 0, g.Rows*g.Cols)

	for row := 0; row < g.Rows; row++ {
		for col := 0; col < g.Cols; col++ {
			rowLabel := rowToLabel(row)
			colNum := col + 1
			label := fmt.Sprintf("%s%d", rowLabel, colNum)

			topLeftX := col * g.CellWidth
			topLeftY := row * g.CellHeight
			centerX := topLeftX + g.CellWidth/2
			centerY := topLeftY + g.CellHeight/2
			botRightX := topLeftX + g.CellWidth
			botRightY := topLeftY + g.CellHeight

			cell := GridCell{
				Label:    label,
				Row:      row,
				Col:      col,
				CenterX:  centerX,
				CenterY:  centerY,
				TopLeft:  Point{X: topLeftX, Y: topLeftY},
				BotRight: Point{X: botRightX, Y: botRightY},
			}

			cells = append(cells, cell)
		}
	}

	return cells
}

// OverlayData returns a GridOverlayJSON suitable for serialization to JSON.
func (g *Grid) OverlayData() GridOverlayJSON {
	return GridOverlayJSON{
		Rows:      g.Rows,
		Cols:      g.Cols,
		CellW:     g.CellWidth,
		CellH:     g.CellHeight,
		ViewportW: g.ViewportW,
		ViewportH: g.ViewportH,
		Cells:     g.AllCells(),
	}
}

// CellsFromRect returns a slice of cell labels that overlap with the bounding box
// defined by (x1, y1) as top-left and (x2, y2) as bottom-right.
func (g *Grid) CellsFromRect(x1, y1, x2, y2 int) []string {
	// Normalize coordinates
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}

	// Clamp to viewport
	if x1 < 0 {
		x1 = 0
	}
	if x2 > g.ViewportW {
		x2 = g.ViewportW
	}
	if y1 < 0 {
		y1 = 0
	}
	if y2 > g.ViewportH {
		y2 = g.ViewportH
	}

	var labels []string

	// Find all cells that overlap the rectangle
	startCol := x1 / g.CellWidth
	endCol := x2 / g.CellWidth
	startRow := y1 / g.CellHeight
	endRow := y2 / g.CellHeight

	for row := startRow; row <= endRow && row < g.Rows; row++ {
		for col := startCol; col <= endCol && col < g.Cols; col++ {
			rowLabel := rowToLabel(row)
			colNum := col + 1
			labels = append(labels, fmt.Sprintf("%s%d", rowLabel, colNum))
		}
	}

	return labels
}
