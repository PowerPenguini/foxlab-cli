package topologyui

import "foxlab-cli/internal/tui"

type grid = tui.Canvas
type rect = tui.Rect
type lineMask = tui.LineMask

const (
	lineHorizontal = tui.LineHorizontal
	lineVertical   = tui.LineVertical
	lineCross      = tui.LineCross
	boxTopLeft     = tui.BoxTopLeft
	boxTopRight    = tui.BoxTopRight
	boxBottomLeft  = tui.BoxBottomLeft
	boxBottomRight = tui.BoxBottomRight

	lineUp    = tui.LineUp
	lineRight = tui.LineRight
	lineDown  = tui.LineDown
	lineLeft  = tui.LineLeft
)

func newGrid(width, height int) *grid { return tui.NewCanvas(width, height) }

func drawBox(g *grid, r rect, title, style string) { tui.DrawBox(g, r, title, style) }
func fillRow(g *grid, x, y, width int, style string) {
	tui.FillRow(g, x, y, width, style)
}
func clearRect(g *grid, r rect) { tui.ClearRect(g, r) }
func fillRect(g *grid, r rect, style string) {
	for y := r.Y; y < r.Y+r.H; y++ {
		fillRow(g, r.X, y, r.W, style)
	}
}

func lineH(g *grid, x1, x2, y int) { tui.LineH(g, x1, x2, y) }
func lineHStyled(g *grid, x1, x2, y int, style string) {
	tui.LineHStyled(g, x1, x2, y, style)
}
func lineHAttached(g *grid, x1, x2, y int, attachStart, attachEnd lineMask, style string) {
	tui.LineHAttached(g, x1, x2, y, attachStart, attachEnd, style)
}
func lineV(g *grid, x, y1, y2 int) { tui.LineV(g, x, y1, y2) }
func lineVStyled(g *grid, x, y1, y2 int, style string) {
	tui.LineVStyled(g, x, y1, y2, style)
}
func lineVAttached(g *grid, x, y1, y2 int, attachStart, attachEnd lineMask, style string) {
	tui.LineVAttached(g, x, y1, y2, attachStart, attachEnd, style)
}

func fit(value string, width int) string { return tui.Fit(value, width) }
func runeLen(value string) int           { return tui.RuneLen(value) }
func clamp(value, low, high int) int     { return tui.Clamp(value, low, high) }
func min(a, b int) int                   { return tui.Min(a, b) }
func max(a, b int) int                   { return tui.Max(a, b) }
func abs(v int) int                      { return tui.Abs(v) }
func scale(value, minValue, maxValue, target int) int {
	return tui.Scale(value, minValue, maxValue, target)
}
