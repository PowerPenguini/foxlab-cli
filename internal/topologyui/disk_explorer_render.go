package topologyui

import (
	"fmt"
	"strings"
)

type diskExplorerActionButton struct {
	label  string
	action string
	style  string
}

func diskExplorerActionButtons() []diskExplorerActionButton {
	return []diskExplorerActionButton{
		{label: " N create ", action: diskExplorerActionCreate, style: themeFooterActive},
		{label: " L layer ", action: diskExplorerActionLayer, style: ansiBgCyan + ansiWhite + ansiBold},
		{label: " E rename ", action: diskExplorerActionRename, style: themeFooterActive},
		{label: " R resize ", action: diskExplorerActionResize, style: themeFooterActive},
		{label: " M merge ", action: diskExplorerActionMerge, style: ansiBgGreen + ansiWhite + ansiBold},
		{label: " X delete ", action: diskExplorerActionDelete, style: ansiBgRed + ansiWhite + ansiBold},
		{label: " I info ", action: diskExplorerActionInfo, style: themeFooterActive},
	}
}

func drawDiskExplorer(g *grid, m Model, state ViewState, width, height int) {
	if !state.DiskExplorerOpen {
		return
	}
	layout, ok := diskExplorerLayout(width, height)
	if !ok {
		return
	}
	clearRect(g, layout)
	drawNodeBox(g, layout, ansiBrightCyan+ansiBold)
	title := " Disks "
	if m.ID != "" {
		title = " Disks: " + m.ID + " "
	}
	g.Text(layout.X+2, layout.Y, fit(title, layout.W-4), ansiBrightCyan+ansiBold)
	header := "ID / kind / size / format / attachment / path"
	g.Text(layout.X+1, layout.Y+1, fit(header, layout.W-2), themeMuted)
	for x := layout.X + 1; x < layout.X+layout.W-1; x++ {
		g.Set(x, layout.Y+2, lineHorizontal, themeMuted)
	}
	rows := state.DiskExplorerRows
	if len(rows) == 0 {
		g.Text(layout.X+1, layout.Y+3, fit("No disks. Press N to create one.", layout.W-2), themeMuted)
	} else {
		drawDiskExplorerRows(g, state, layout)
	}
	drawDiskExplorerInfo(g, state, layout)
	drawDiskExplorerActions(g, state, layout)
}

func drawDiskExplorerRows(g *grid, state ViewState, layout rect) {
	visible := diskExplorerVisibleRowsForState(state, layout)
	start := clamp(state.DiskExplorerScroll, 0, max(0, len(state.DiskExplorerRows)-1))
	selected := normalizedMenuSelection(state.DiskExplorerSelected, len(state.DiskExplorerRows))
	for row := 0; row < visible; row++ {
		index := start + row
		if index >= len(state.DiskExplorerRows) {
			break
		}
		y := layout.Y + 3 + row
		style := ""
		if index == selected {
			style = themeMenuActive
			fillRow(g, layout.X+1, y, layout.W-2, style)
		}
		label := state.DiskExplorerRows[index]
		if state.DiskExplorerEdit != "" && index == selected {
			label = diskExplorerEditLabel(label, state.DiskExplorerEdit, state.DiskExplorerEditValue, state.DiskExplorerEditCursor)
		}
		g.Text(layout.X+1, y, fit(label, layout.W-2), style)
	}
}

func drawDiskExplorerInfo(g *grid, state ViewState, layout rect) {
	lines := diskExplorerVisibleInfoLines(state, layout)
	if len(lines) == 0 {
		return
	}
	height := len(lines)
	y := layout.Y + layout.H - 2 - height
	separatorY := y - 1
	if separatorY > layout.Y+2 {
		for x := layout.X + 1; x < layout.X+layout.W-1; x++ {
			g.Set(x, separatorY, lineHorizontal, themeMuted)
		}
	}
	for i := 0; i < height; i++ {
		line := lines[i]
		style := themeMuted
		if i == 0 {
			style = ansiBrightCyan + ansiBold
		}
		fillRow(g, layout.X+1, y+i, layout.W-2, "")
		g.Text(layout.X+1, y+i, fit(line, layout.W-2), style)
	}
}

func diskExplorerEditLabel(label, edit, value string, cursor int) string {
	return label + "  " + edit + "=" + contextEditText(value, cursor)
}

func drawDiskExplorerActions(g *grid, state ViewState, layout rect) {
	y := layout.Y + layout.H - 2
	fillRow(g, layout.X+1, y, layout.W-2, themeFooter)
	x := layout.X + 1
	for _, action := range diskExplorerActionButtons() {
		if x >= layout.X+layout.W-1 {
			break
		}
		g.Text(x, y, fit(action.label, layout.X+layout.W-1-x), action.style)
		x += runeLen(action.label)
	}
	footer := "Esc close"
	if state.DiskExplorerEdit != "" {
		footer = "Enter apply  Esc cancel"
	}
	if x+1 < layout.X+layout.W-1 {
		g.Text(x+1, y, fit(footer, layout.X+layout.W-2-x), themeFooter+themeMuted)
	}
	if len(state.DiskExplorerRows) > diskExplorerVisibleRowsForState(state, layout) {
		pos := diskExplorerScrollText(state, layout)
		g.Text(layout.X+layout.W-1-runeLen(pos), layout.Y+1, pos, themeMuted)
	}
}

func diskExplorerScrollText(state ViewState, layout rect) string {
	visible := diskExplorerVisibleRowsForState(state, layout)
	if visible <= 0 || len(state.DiskExplorerRows) <= visible {
		return ""
	}
	top := min(len(state.DiskExplorerRows), state.DiskExplorerScroll+1)
	bottom := min(len(state.DiskExplorerRows), state.DiskExplorerScroll+visible)
	return fmt.Sprintf("%d-%d/%d", top, bottom, len(state.DiskExplorerRows))
}

func diskExplorerVisibleRowsForState(state ViewState, layout rect) int {
	visible := diskExplorerVisibleRows(layout)
	if diskExplorerInfoHeight(state, layout) > 0 {
		visible -= diskExplorerInfoHeight(state, layout) + 1
	}
	return max(0, visible)
}

func diskExplorerInfoHeight(state ViewState, layout rect) int {
	return len(diskExplorerVisibleInfoLines(state, layout))
}

func diskExplorerVisibleInfoLines(state ViewState, layout rect) []string {
	lines := diskExplorerInfoLines(state)
	if len(lines) == 0 {
		return nil
	}
	height := min(len(lines), min(6, max(1, layout.H-7)))
	if len(lines) <= height {
		return lines
	}
	visible := make([]string, 0, height)
	visible = append(visible, lines[0])
	visible = append(visible, lines[len(lines)-height+1:]...)
	return visible
}

func diskExplorerInfoLines(state ViewState) []string {
	if len(state.Console) == 0 {
		return nil
	}
	switch {
	case strings.HasPrefix(state.Message, "disk info:"),
		strings.HasPrefix(state.Message, "disk info failed:"),
		strings.HasPrefix(state.Message, "disk not found:"):
		return state.Console
	default:
		return nil
	}
}
