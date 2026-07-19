package tui

import "testing"

func TestCanvasStringSkipsWideGlyphContinuationCells(t *testing.T) {
	g := NewCanvas(4, 1)
	g.Set(0, 0, '界', "")
	g.Cells[1].Ch = 0
	g.Set(2, 0, 'x', "")
	if got := g.String(false); got != "界x " {
		t.Fatalf("wide canvas = %q, want %q", got, "界x ")
	}
}
