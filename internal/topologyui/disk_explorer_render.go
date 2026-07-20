package topologyui

import (
	"fmt"
)

type diskExplorerActionButton struct {
	label  string
	action string
}

type diskExplorerColumns struct {
	Marker   rect
	ID       rect
	Kind     rect
	Size     rect
	Format   rect
	Relation rect
	Path     rect
}

func diskExplorerActionButtons() []diskExplorerActionButton {
	return []diskExplorerActionButton{
		{label: " N create ", action: diskExplorerActionCreate},
		{label: " L layer ", action: diskExplorerActionLayer},
		{label: " E rename ", action: diskExplorerActionRename},
		{label: " R resize ", action: diskExplorerActionResize},
		{label: " M merge ", action: diskExplorerActionMerge},
		{label: " X delete ", action: diskExplorerActionDelete},
	}
}

func drawDiskExplorer(g *grid, _ Model, state ViewState, width, height int) {
	if !state.DiskExplorerOpen {
		return
	}
	layout, ok := diskExplorerLayout(width, height)
	if !ok {
		return
	}
	fillRect(g, layout, themePanelDisk)
	drawDiskExplorerTableHeader(g, layout)
	rows := state.DiskExplorerRows
	if len(rows) == 0 {
		g.Text(layout.X+1, diskExplorerRowsY(layout), fit("No disks. Press N to create one.", layout.W-2), themePanelDiskMuted)
	} else {
		drawDiskExplorerRows(g, state, layout)
	}
	drawDiskExplorerActions(g, state, layout)
}

func (a *App) renderDiskExplorerTab(width, height int) *grid {
	width = max(0, width)
	height = max(0, height)
	contentHeight := max(0, height-tabBarHeight)
	state := a.diskExplorerRenderState()
	content := newGrid(width, contentHeight)
	drawDiskExplorer(content, a.Model, state, width, contentHeight)
	drawConsole(content, state, width, contentHeight)
	drawMouseClickFeedback(content, state)
	drawPalette(content, a.Model, state, width, contentHeight)
	applyTerminalBackground(content)

	g := newGrid(width, height)
	copyCanvas(g, content, 0, tabBarHeight)
	a.drawTabBar(g)
	applyTerminalBackground(g)
	return g
}

func drawDiskExplorerRows(g *grid, state ViewState, layout rect) {
	visible := diskExplorerVisibleRowsForState(state, layout)
	start := clamp(state.DiskExplorerScroll, 0, max(0, len(state.DiskExplorerRows)-1))
	selected := normalizedMenuSelection(state.DiskExplorerSelected, len(state.DiskExplorerRows))
	columns := diskExplorerTableColumns(layout)
	for row := 0; row < visible; row++ {
		index := start + row
		if index >= len(state.DiskExplorerRows) {
			break
		}
		y := diskExplorerRowsY(layout) + row
		style := themePanelDisk
		if index == selected {
			style = themePanelDiskSelected
			fillRow(g, layout.X+1, y, layout.W-2, style)
		}
		if index < len(state.DiskExplorerRowViews) {
			drawDiskExplorerStructuredRow(g, columns, y, state.DiskExplorerRowViews[index], style, index == selected, state)
			continue
		}
		label := state.DiskExplorerRows[index]
		if state.DiskExplorerEdit != "" && index == selected {
			label = diskExplorerEditLabel(label, state.DiskExplorerEdit, state.DiskExplorerEditValue, state.DiskExplorerEditCursor)
		}
		g.Text(layout.X+1, y, fit(label, layout.W-2), style)
	}
}

func drawDiskExplorerTableHeader(g *grid, layout rect) {
	columns := diskExplorerTableColumns(layout)
	y := layout.Y + 1
	style := themePanelDiskMuted + ansiBold
	g.Text(columns.ID.X, y, fit("DISK", columns.ID.W), style)
	g.Text(columns.Kind.X, y, fit("TYPE", columns.Kind.W), style)
	g.Text(columns.Size.X, y, fit("SIZE", columns.Size.W), style)
	g.Text(columns.Format.X, y, fit("FMT", columns.Format.W), style)
	g.Text(columns.Relation.X, y, fit("ATTACHED/BASE", columns.Relation.W), style)
	g.Text(columns.Path.X, y, fit("PATH", columns.Path.W), style)
}

func drawDiskExplorerStructuredRow(g *grid, columns diskExplorerColumns, y int, row DiskExplorerRowView, rowStyle string, selected bool, state ViewState) {
	marker := " "
	if selected {
		marker = ">"
	}
	if row.Missing {
		marker = "!"
	}
	id := row.ID
	if row.Depth > 0 {
		id = "  " + id
	}
	if selected {
		switch state.DiskExplorerEdit {
		case diskExplorerActionRename:
			id = diskExplorerEditLabel(id, state.DiskExplorerEdit, state.DiskExplorerEditValue, state.DiskExplorerEditCursor)
		case diskExplorerActionResize:
			row.Size = contextEditText(state.DiskExplorerEditValue, state.DiskExplorerEditCursor) + "G"
		}
	}
	g.Text(columns.Marker.X, y, fit(marker, columns.Marker.W), rowStyle+ansiBrightCyan+ansiBold)
	g.Text(columns.ID.X, y, fit(id, columns.ID.W), rowStyle+ansiWhite+ansiBold)
	g.Text(columns.Kind.X, y, fit(row.Kind, columns.Kind.W), diskExplorerKindStyle(row.Kind, rowStyle))
	g.Text(columns.Size.X, y, fit(row.Size, columns.Size.W), rowStyle+ansiWhite)
	g.Text(columns.Format.X, y, fit(row.Format, columns.Format.W), rowStyle+ansiWhite)
	g.Text(columns.Relation.X, y, fit(row.Relation, columns.Relation.W), diskExplorerRelationStyle(row, rowStyle))
	g.Text(columns.Path.X, y, fit(row.Path, columns.Path.W), rowStyle)
}

func diskExplorerTableColumns(layout rect) diskExplorerColumns {
	x := layout.X + 1
	contentW := layout.W - 2
	markerW := 2
	kindW := 7
	sizeW := 6
	formatW := 7
	gap := 1
	fixed := markerW + kindW + sizeW + formatW + 5*gap
	remaining := max(0, contentW-fixed)
	idW := min(20, max(8, remaining/3))
	relationW := min(22, max(10, remaining/3))
	pathW := max(0, remaining-idW-relationW)
	columns := diskExplorerColumns{}
	columns.Marker = rect{X: x, W: markerW}
	columns.ID = rect{X: columns.Marker.X + markerW, W: idW}
	columns.Kind = rect{X: columns.ID.X + idW + gap, W: kindW}
	columns.Size = rect{X: columns.Kind.X + kindW + gap, W: sizeW}
	columns.Format = rect{X: columns.Size.X + sizeW + gap, W: formatW}
	columns.Relation = rect{X: columns.Format.X + formatW + gap, W: relationW}
	columns.Path = rect{X: columns.Relation.X + relationW + gap, W: pathW}
	return columns
}

func diskExplorerKindStyle(kind, rowStyle string) string {
	switch kind {
	case "base":
		return rowStyle + ansiBrightCyan + ansiBold
	case "layer":
		return rowStyle + ansiGreen + ansiBold
	default:
		return rowStyle + ansiWhite
	}
}

func diskExplorerRelationStyle(row DiskExplorerRowView, rowStyle string) string {
	if row.Missing {
		return rowStyle + ansiRed + ansiBold
	}
	if row.Relation == "-" {
		return rowStyle + ansiBrightBlack
	}
	return rowStyle + ansiWhite
}

func diskExplorerEditLabel(label, edit, value string, cursor int) string {
	if edit == diskExplorerActionRename {
		return contextEditText(value, cursor)
	}
	return label + "  " + edit + "=" + contextEditText(value, cursor)
}

func drawDiskExplorerActions(g *grid, state ViewState, layout rect) {
	y := layout.Y + layout.H - 1
	fillRow(g, layout.X, y, layout.W, themePanelDiskActions)
	x := layout.X + 1
	for _, action := range diskExplorerActionButtons() {
		if x >= layout.X+layout.W-1 {
			break
		}
		g.Text(x, y, fit(action.label, layout.X+layout.W-1-x), themePanelDiskActions)
		x += runeLen(action.label)
	}
	footer := "Esc close"
	if state.DiskExplorerEdit != "" {
		footer = "Enter apply  Esc cancel"
	}
	if x+1 < layout.X+layout.W-1 {
		g.Text(x+1, y, fit(footer, layout.X+layout.W-2-x), themePanelDiskActions)
	}
	if len(state.DiskExplorerRows) > diskExplorerVisibleRowsForState(state, layout) {
		pos := diskExplorerScrollText(state, layout)
		g.Text(layout.X+layout.W-1-runeLen(pos), layout.Y+1, pos, themePanelDiskMuted)
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

func diskExplorerVisibleRowsForState(_ ViewState, layout rect) int {
	return diskExplorerVisibleRows(layout)
}
