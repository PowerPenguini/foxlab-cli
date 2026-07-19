package tui

import "strings"

const (
	LineHorizontal = '─'
	LineVertical   = '│'
	LineCross      = '┼'
	BoxTopLeft     = '┌'
	BoxTopRight    = '┐'
	BoxBottomLeft  = '└'
	BoxBottomRight = '┘'
	Ellipsis       = '…'
	ANSIReset      = "\x1b[0m"
	ANSIDim        = "\x1b[2m"
)

type LineMask uint8

const (
	LineUp LineMask = 1 << iota
	LineRight
	LineDown
	LineLeft
)

type Cell struct {
	Ch    rune
	Line  LineMask
	Style string
}

type Rect struct {
	X int
	Y int
	W int
	H int
}

func NewCanvas(width, height int) *Canvas {
	g := &Canvas{Width: width, Height: height, Cells: make([]Cell, width*height)}
	for i := range g.Cells {
		g.Cells[i].Ch = ' '
	}
	return g
}

type Canvas struct {
	Width  int
	Height int
	Cells  []Cell
}

func (g *Canvas) Set(x, y int, ch rune, style string) {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return
	}
	i := y*g.Width + x
	g.Cells[i] = Cell{Ch: ch, Line: RuneLineMask(ch), Style: style}
}

func (g *Canvas) SetLine(x, y int, mask LineMask, style string) {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return
	}
	i := y*g.Width + x
	g.Cells[i].Line |= mask
	g.Cells[i].Ch = LineRune(g.Cells[i].Line)
	if g.Cells[i].Style == "" {
		g.Cells[i].Style = style
	}
}

func (g *Canvas) SetStyle(x, y int, style string) {
	if x < 0 || y < 0 || x >= g.Width || y >= g.Height {
		return
	}
	g.Cells[y*g.Width+x].Style = style
}

func (g *Canvas) Text(x, y int, value, style string) {
	for _, ch := range value {
		g.Set(x, y, ch, style)
		x++
		if x >= g.Width {
			return
		}
	}
}

func (g *Canvas) String(ansi bool) string {
	var b strings.Builder
	current := ""
	for y := 0; y < g.Height; y++ {
		current = ""
		for x := 0; x < g.Width; x++ {
			c := g.Cells[y*g.Width+x]
			if c.Ch == 0 {
				continue
			}
			if ansi && c.Style != current {
				if current != "" {
					b.WriteString(ANSIReset)
				}
				if c.Style != "" {
					b.WriteString(c.Style)
				}
				current = c.Style
			}
			b.WriteRune(c.Ch)
		}
		if ansi && current != "" {
			b.WriteString(ANSIReset)
		}
		if y != g.Height-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func DrawBox(g *Canvas, r Rect, title, style string) {
	if r.W < 2 || r.H < 2 {
		return
	}
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		g.Set(x, r.Y, LineHorizontal, style)
		g.Set(x, r.Y+r.H-1, LineHorizontal, style)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		g.Set(r.X, y, LineVertical, style)
		g.Set(r.X+r.W-1, y, LineVertical, style)
	}
	g.Set(r.X, r.Y, BoxTopLeft, style)
	g.Set(r.X+r.W-1, r.Y, BoxTopRight, style)
	g.Set(r.X, r.Y+r.H-1, BoxBottomLeft, style)
	g.Set(r.X+r.W-1, r.Y+r.H-1, BoxBottomRight, style)
	if title != "" && r.W > 4 {
		g.Text(r.X+2, r.Y, Fit(title, r.W-4), style)
	}
}

func FillRow(g *Canvas, x, y, width int, style string) {
	for i := 0; i < width; i++ {
		g.Set(x+i, y, ' ', style)
	}
}

func ClearRect(g *Canvas, r Rect) {
	for y := r.Y; y < r.Y+r.H; y++ {
		FillRow(g, r.X, y, r.W, "")
	}
}

func LineH(g *Canvas, x1, x2, y int) {
	LineHStyled(g, x1, x2, y, ANSIDim)
}

func LineHStyled(g *Canvas, x1, x2, y int, style string) {
	LineHAttached(g, x1, x2, y, 0, 0, style)
}

func LineHAttached(g *Canvas, x1, x2, y int, attachStart, attachEnd LineMask, style string) {
	if x1 == x2 && attachStart == 0 && attachEnd == 0 {
		g.SetLine(x1, y, LineLeft|LineRight, style)
		return
	}
	start := x1
	end := x2
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	for x := x1; x <= x2; x++ {
		mask := LineMask(0)
		if x > x1 {
			mask |= LineLeft
		}
		if x < x2 {
			mask |= LineRight
		}
		if x == start {
			mask |= attachStart
		}
		if x == end {
			mask |= attachEnd
		}
		if mask != 0 {
			g.SetLine(x, y, mask, style)
		}
	}
}

func LineV(g *Canvas, x, y1, y2 int) {
	LineVStyled(g, x, y1, y2, ANSIDim)
}

func LineVStyled(g *Canvas, x, y1, y2 int, style string) {
	LineVAttached(g, x, y1, y2, 0, 0, style)
}

func LineVAttached(g *Canvas, x, y1, y2 int, attachStart, attachEnd LineMask, style string) {
	if y1 == y2 {
		mask := attachStart | attachEnd
		if mask != 0 {
			g.SetLine(x, y1, mask, style)
		}
		return
	}
	start := y1
	end := y2
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	for y := y1; y <= y2; y++ {
		mask := LineUp | LineDown
		if y == y1 && y != y2 {
			mask = LineDown
		}
		if y == y2 && y != y1 {
			mask = LineUp
		}
		if y == start {
			mask |= attachStart
		}
		if y == end {
			mask |= attachEnd
		}
		g.SetLine(x, y, mask, style)
	}
}

func LineRune(mask LineMask) rune {
	switch mask {
	case LineLeft, LineRight, LineLeft | LineRight:
		return LineHorizontal
	case LineUp, LineDown, LineUp | LineDown:
		return LineVertical
	case LineRight | LineDown:
		return BoxTopLeft
	case LineLeft | LineDown:
		return BoxTopRight
	case LineRight | LineUp:
		return BoxBottomLeft
	case LineLeft | LineUp:
		return BoxBottomRight
	case LineUp | LineRight | LineDown:
		return '├'
	case LineUp | LineLeft | LineDown:
		return '┤'
	case LineLeft | LineRight | LineDown:
		return '┬'
	case LineLeft | LineRight | LineUp:
		return '┴'
	case LineUp | LineRight | LineDown | LineLeft:
		return LineCross
	default:
		return LineCross
	}
}

func RuneLineMask(ch rune) LineMask {
	switch ch {
	case LineHorizontal:
		return LineLeft | LineRight
	case LineVertical:
		return LineUp | LineDown
	case BoxTopLeft:
		return LineRight | LineDown
	case BoxTopRight:
		return LineLeft | LineDown
	case BoxBottomLeft:
		return LineRight | LineUp
	case BoxBottomRight:
		return LineLeft | LineUp
	case '├':
		return LineUp | LineRight | LineDown
	case '┤':
		return LineUp | LineLeft | LineDown
	case '┬':
		return LineLeft | LineRight | LineDown
	case '┴':
		return LineLeft | LineRight | LineUp
	case LineCross:
		return LineUp | LineRight | LineDown | LineLeft
	default:
		return 0
	}
}

func Fit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	length := RuneLen(value)
	if length <= width {
		return value
	}
	runes := []rune(value)
	if width == 1 {
		return string(runes[:1])
	}
	return string(runes[:width-1]) + string(Ellipsis)
}

func RuneLen(value string) int {
	return len([]rune(value))
}

func Clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func Abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func Scale(value, minValue, maxValue, target int) int {
	if maxValue == minValue {
		return target / 2
	}
	return (value - minValue) * target / (maxValue - minValue)
}
