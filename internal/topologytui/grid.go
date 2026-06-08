package topologytui

import "strings"

const (
	lineHorizontal = '─'
	lineVertical   = '│'
	lineCross      = '┼'
	boxTopLeft     = '┌'
	boxTopRight    = '┐'
	boxBottomLeft  = '└'
	boxBottomRight = '┘'
	ellipsis       = '…'
)

type lineMask uint8

const (
	lineUp lineMask = 1 << iota
	lineRight
	lineDown
	lineLeft
)

type cell struct {
	ch    rune
	line  lineMask
	style string
}

type rect struct {
	x int
	y int
	w int
	h int
}

func newGrid(width, height int) *grid {
	g := &grid{width: width, height: height, cells: make([]cell, width*height)}
	for i := range g.cells {
		g.cells[i].ch = ' '
	}
	return g
}

type grid struct {
	width  int
	height int
	cells  []cell
}

func (g *grid) set(x, y int, ch rune, style string) {
	if x < 0 || y < 0 || x >= g.width || y >= g.height {
		return
	}
	i := y*g.width + x
	g.cells[i] = cell{ch: ch, line: runeLineMask(ch), style: style}
}

func (g *grid) setLine(x, y int, mask lineMask, style string) {
	if x < 0 || y < 0 || x >= g.width || y >= g.height {
		return
	}
	i := y*g.width + x
	g.cells[i].line |= mask
	g.cells[i].ch = lineRune(g.cells[i].line)
	if g.cells[i].style == "" {
		g.cells[i].style = style
	}
}

func (g *grid) setStyle(x, y int, style string) {
	if x < 0 || y < 0 || x >= g.width || y >= g.height {
		return
	}
	g.cells[y*g.width+x].style = style
}

func (g *grid) text(x, y int, value, style string) {
	for _, ch := range value {
		g.set(x, y, ch, style)
		x++
		if x >= g.width {
			return
		}
	}
}

func (g *grid) String(ansi bool) string {
	var b strings.Builder
	current := ""
	for y := 0; y < g.height; y++ {
		current = ""
		for x := 0; x < g.width; x++ {
			c := g.cells[y*g.width+x]
			if ansi && c.style != current {
				if current != "" {
					b.WriteString(ansiReset)
				}
				if c.style != "" {
					b.WriteString(c.style)
				}
				current = c.style
			}
			b.WriteRune(c.ch)
		}
		if ansi && current != "" {
			b.WriteString(ansiReset)
		}
		if y != g.height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func drawBox(g *grid, r rect, title, style string) {
	if r.w < 2 || r.h < 2 {
		return
	}
	for x := r.x + 1; x < r.x+r.w-1; x++ {
		g.set(x, r.y, lineHorizontal, style)
		g.set(x, r.y+r.h-1, lineHorizontal, style)
	}
	for y := r.y + 1; y < r.y+r.h-1; y++ {
		g.set(r.x, y, lineVertical, style)
		g.set(r.x+r.w-1, y, lineVertical, style)
	}
	g.set(r.x, r.y, boxTopLeft, style)
	g.set(r.x+r.w-1, r.y, boxTopRight, style)
	g.set(r.x, r.y+r.h-1, boxBottomLeft, style)
	g.set(r.x+r.w-1, r.y+r.h-1, boxBottomRight, style)
	if title != "" && r.w > 4 {
		g.text(r.x+2, r.y, fit(title, r.w-4), style)
	}
}

func fillRow(g *grid, x, y, width int, style string) {
	for i := 0; i < width; i++ {
		g.set(x+i, y, ' ', style)
	}
}

func clearRect(g *grid, r rect) {
	for y := r.y; y < r.y+r.h; y++ {
		fillRow(g, r.x, y, r.w, "")
	}
}

func lineH(g *grid, x1, x2, y int) {
	lineHStyled(g, x1, x2, y, ansiDim)
}

func lineHStyled(g *grid, x1, x2, y int, style string) {
	lineHAttached(g, x1, x2, y, 0, 0, style)
}

func lineHAttached(g *grid, x1, x2, y int, attachStart, attachEnd lineMask, style string) {
	if x1 == x2 && attachStart == 0 && attachEnd == 0 {
		g.setLine(x1, y, lineLeft|lineRight, style)
		return
	}
	start := x1
	end := x2
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	for x := x1; x <= x2; x++ {
		mask := lineMask(0)
		if x > x1 {
			mask |= lineLeft
		}
		if x < x2 {
			mask |= lineRight
		}
		if x == start {
			mask |= attachStart
		}
		if x == end {
			mask |= attachEnd
		}
		if mask != 0 {
			g.setLine(x, y, mask, style)
		}
	}
}

func lineV(g *grid, x, y1, y2 int) {
	lineVStyled(g, x, y1, y2, ansiDim)
}

func lineVStyled(g *grid, x, y1, y2 int, style string) {
	lineVAttached(g, x, y1, y2, 0, 0, style)
}

func lineVAttached(g *grid, x, y1, y2 int, attachStart, attachEnd lineMask, style string) {
	if y1 == y2 {
		mask := attachStart | attachEnd
		if mask != 0 {
			g.setLine(x, y1, mask, style)
		}
		return
	}
	start := y1
	end := y2
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	for y := y1; y <= y2; y++ {
		mask := lineUp | lineDown
		if y == y1 && y != y2 {
			mask = lineDown
		}
		if y == y2 && y != y1 {
			mask = lineUp
		}
		if y == start {
			mask |= attachStart
		}
		if y == end {
			mask |= attachEnd
		}
		g.setLine(x, y, mask, style)
	}
}

func lineRune(mask lineMask) rune {
	switch mask {
	case lineLeft, lineRight, lineLeft | lineRight:
		return lineHorizontal
	case lineUp, lineDown, lineUp | lineDown:
		return lineVertical
	case lineRight | lineDown:
		return boxTopLeft
	case lineLeft | lineDown:
		return boxTopRight
	case lineRight | lineUp:
		return boxBottomLeft
	case lineLeft | lineUp:
		return boxBottomRight
	case lineUp | lineRight | lineDown:
		return '├'
	case lineUp | lineLeft | lineDown:
		return '┤'
	case lineLeft | lineRight | lineDown:
		return '┬'
	case lineLeft | lineRight | lineUp:
		return '┴'
	case lineUp | lineRight | lineDown | lineLeft:
		return lineCross
	default:
		return lineCross
	}
}

func runeLineMask(ch rune) lineMask {
	switch ch {
	case lineHorizontal:
		return lineLeft | lineRight
	case lineVertical:
		return lineUp | lineDown
	case boxTopLeft:
		return lineRight | lineDown
	case boxTopRight:
		return lineLeft | lineDown
	case boxBottomLeft:
		return lineRight | lineUp
	case boxBottomRight:
		return lineLeft | lineUp
	case '├':
		return lineUp | lineRight | lineDown
	case '┤':
		return lineUp | lineLeft | lineDown
	case '┬':
		return lineLeft | lineRight | lineDown
	case '┴':
		return lineLeft | lineRight | lineUp
	case lineCross:
		return lineUp | lineRight | lineDown | lineLeft
	default:
		return 0
	}
}

func fit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	length := runeLen(value)
	if length <= width {
		return value
	}
	runes := []rune(value)
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + string(ellipsis)
}

func runeLen(value string) int {
	return len([]rune(value))
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func scale(value, minValue, maxValue, target int) int {
	if maxValue == minValue {
		return target / 2
	}
	return (value - minValue) * target / (maxValue - minValue)
}
