package topologyui

import "strings"

func layoutNodeRects(m Model, pane rect) map[string]rect {
	out := make(map[string]rect, len(m.Nodes))
	if len(m.Nodes) == 0 {
		return out
	}
	for _, node := range m.Nodes {
		x := pane.X + node.X
		y := pane.Y + node.Y
		out[node.Key()] = rect{X: x, Y: y, W: nodeWidth, H: nodeHeight}
	}
	return out
}

func rectFullyVisible(r rect, bounds rect) bool {
	return r.X >= bounds.X &&
		r.Y >= bounds.Y &&
		r.X+r.W <= bounds.X+bounds.W &&
		r.Y+r.H <= bounds.Y+bounds.H
}

func drawNode(g *grid, node Node, r rect, selected, graphFocused bool) {
	stateStyleValue := stateStyle(node.State)
	clearRect(g, r)
	drawBox(g, r, "", selectedBorderStyle(selected, graphFocused))
	kind := "[" + firstNonEmpty(node.Badge, NodeKind(node.Type)) + "]"
	g.Text(r.X+1, r.Y+1, fit(kind+" "+node.Label, r.W-2), "")
	g.Text(r.X+1, r.Y+2, fit(node.State, r.W-2), stateStyleValue)
}

func selectedBorderStyle(selected, graphFocused bool) string {
	if !selected || !graphFocused {
		return ""
	}
	return ansiBold + ansiBrightCyan
}

func styleBoxBorder(g *grid, r rect, style string) {
	if r.W < 2 || r.H < 2 {
		return
	}
	for x := r.X; x < r.X+r.W; x++ {
		g.SetStyle(x, r.Y, style)
		g.SetStyle(x, r.Y+r.H-1, style)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		g.SetStyle(r.X, y, style)
		g.SetStyle(r.X+r.W-1, y, style)
	}
}

func drawContextMenu(g *grid, m Model, state ViewState, nodeRects map[string]rect, bounds rect) {
	if !state.ContextMenu {
		return
	}
	layout, _, _, ok := contextMenuLayoutFor(m, state, nodeRects, bounds)
	if !ok {
		return
	}
	drawMenuColumn(g, layout.root, state.ContextInSubmenu == false, false, "", 0, false, false, false, false, false)
	if !layout.hasSub {
		return
	}
	contextGroup := state.ContextGroup
	deleteActionSelected := (contextGroup == "nic-menu" && state.ContextDeleteNIC) || (contextGroup == "disk-menu" && state.ContextDeleteDisk)
	mergeActionSelected := contextGroup == "disk-menu" && state.ContextMergeDisk
	detachActionSelected := contextGroup == "disk-menu" && state.ContextDetachDisk
	addLayerActionSelected := contextGroup == "disk-menu" && state.ContextAddDiskLayer
	drawMenuColumn(g, layout.sub, state.ContextInSubmenu, state.ContextEdit, state.ContextEditValue, state.ContextEditCursor, deleteActionSelected, mergeActionSelected, detachActionSelected, addLayerActionSelected, contextGroup == "disk-menu")
}

func drawMenuColumn(g *grid, column menuColumnLayout, isActive bool, editing bool, editValue string, editCursor int, deleteButtonSelected bool, mergeButtonSelected bool, detachButtonSelected bool, addLayerButtonSelected bool, diskMenu bool) {
	drawContextMenuItems(g, column.rect, menuItemLabels(column.items), menuItemActions(column.items), menuItemKinds(column.items), column.selected, column.start, isActive, editing, editValue, editCursor, deleteButtonSelected, mergeButtonSelected, detachButtonSelected, addLayerButtonSelected, diskMenu)
}

func drawTopRibbon(g *grid, bounds rect, state ViewState) {
	items := topRibbonRootItems()
	buttons := topMenuButtonRects(items, bounds.W)
	fillRow(g, 0, 0, g.Width, ansiBgGray+ansiWhite)
	activeRoot := normalizedMenuSelection(state.TopMenuRootSelected, len(items))
	addRoot := topRibbonAddRootIndex(items)
	for i, button := range buttons {
		style := ansiBgGray + ansiWhite
		enabled := topRibbonRootEnabled(items[i], state)
		if !enabled {
			style = ansiBgGray + ansiWhite + ansiDim
		} else if state.Focus == FocusTop && i == activeRoot {
			style = ansiBgCyan + ansiWhite + ansiBold
		}
		if state.TopMenuOpen && i == addRoot {
			style = ansiBgCyan + ansiWhite + ansiBold
		}
		g.Text(button.X, button.Y, fit(" "+topMenuLabel(items[i])+" ", button.W), style)
	}
	if !state.TopMenuOpen || len(buttons) == 0 {
		return
	}
	dropdownItems := topRibbonAddItems()
	if len(dropdownItems) == 0 {
		return
	}
	if addRoot < 0 || addRoot >= len(buttons) {
		return
	}
	menu, ok := layoutDropdownMenu(bounds, buttons[addRoot], menuItemsFromLabels(dropdownItems), state.TopMenuSelected)
	if !ok {
		return
	}
	drawMenuColumn(g, menu, true, false, "", 0, false, false, false, false, false)
}

func drawConnectTargetMenu(g *grid, m Model, state ViewState, nodeRects map[string]rect, bounds rect) {
	if !state.ConnectTargetMenu {
		return
	}
	node, ok := nodeByKey(m, NodeKey(state.ConnectTargetType, state.ConnectTargetID))
	if !ok {
		return
	}
	items := connectTargetNICMenuItems(node)
	if len(items) == 0 {
		return
	}
	nodeRect, ok := nodeRects[node.Key()]
	if !ok {
		return
	}
	menu, ok := layoutFloatingMenu(bounds, nodeRect, menuItemsFromLabels(items), state.ConnectTargetIndex)
	if !ok {
		return
	}
	drawMenuColumn(g, menu, true, false, "", 0, false, false, false, false, false)
}

func drawConnectPreview(g *grid, m Model, state ViewState, nodeRects map[string]rect, bounds rect, planner *routePlanner) {
	if !state.ConnectMode {
		return
	}
	sourceKey := NodeKey(state.ConnectNodeType, state.ConnectNodeID)
	targetKey := connectPreviewTargetKey(m, state)
	if targetKey == "" || targetKey == sourceKey {
		return
	}
	from, ok := nodeRects[sourceKey]
	if !ok || !rectFullyVisible(from, bounds) {
		return
	}
	to, ok := nodeRects[targetKey]
	if !ok || !rectFullyVisible(to, bounds) {
		return
	}
	route, ok := planner.planRoute(from, to)
	if !ok {
		return
	}
	drawDashedRoute(g, route)
}

func connectPreviewTargetKey(m Model, state ViewState) string {
	if state.ConnectTargetMenu {
		return NodeKey(state.ConnectTargetType, state.ConnectTargetID)
	}
	node, ok := selectedNode(m, state.Selected)
	if !ok {
		return ""
	}
	return node.Key()
}

func drawContextMenuItems(g *grid, menu rect, items []string, actions []string, kinds []string, active, start int, isActive bool, editing bool, editValue string, editCursor int, deleteButtonSelected bool, mergeButtonSelected bool, detachButtonSelected bool, addLayerButtonSelected bool, diskMenu bool) {
	for row := 0; row < menu.H; row++ {
		i := start + row
		item := items[i]
		action := ""
		if i < len(actions) {
			action = actions[i]
		}
		kind := ""
		if i < len(kinds) {
			kind = kinds[i]
		}
		layerRow := diskMenu && (kind == "layer" || isDiskMenuDetail(item))
		baseRow := diskMenu && (kind == "base" || isDiskAttachMenuDetail(item))
		dataRow := diskMenu && kind == "data"
		activeLayerRow := layerRow && action == diskMenuActionNone
		activeBaseRow := baseRow && action == diskMenuActionNone
		if editing && i == active {
			if kind == "base" && addLayerButtonSelected {
				item = contextDiskLayerEditLabel(item, editValue, editCursor)
			} else {
				item = contextEditLabel(item, editValue, editCursor)
			}
		}
		rowStyle := ansiBgGray + ansiWhite
		indicatorStyle := rowStyle
		if isActive && i == active {
			rowStyle += ansiBold
			indicatorStyle = ansiBgCyan
		}
		fillRow(g, menu.X, menu.Y+row, menu.W, rowStyle)
		g.Set(menu.X, menu.Y+row, ' ', indicatorStyle)
		textWidth := menu.W - 3
		if isNICDetail(item) || layerRow {
			textWidth = max(0, menu.W-6)
		}
		if baseRow || layerRow || dataRow {
			textWidth = max(0, menu.W-9)
		}
		if activeLayerRow {
			textWidth = max(0, menu.W-12)
		}
		renderedItem := fit(item, textWidth)
		g.Text(menu.X+2, menu.Y+row, renderedItem, rowStyle)
		if editing && i == active && editValue == "" {
			drawContextEditPlaceholder(g, menu.X+2, menu.Y+row, renderedItem, rowStyle)
		} else if isContextPlaceholderItem(renderedItem) {
			drawContextPlaceholder(g, menu.X+2, menu.Y+row, renderedItem, rowStyle)
		}
		if baseRow {
			if activeBaseRow {
				dStyle := rowStyle
				if isActive && i == active && detachButtonSelected {
					dStyle = ansiBgYellow + ansiBlack + ansiBold
				}
				xStyle := rowStyle
				if isActive && i == active && deleteButtonSelected {
					xStyle = ansiBgRed + ansiWhite + ansiBold
				}
				g.Text(menu.X+menu.W-6, menu.Y+row, " D ", dStyle)
				g.Text(menu.X+menu.W-3, menu.Y+row, " X ", xStyle)
				continue
			}
			lStyle := rowStyle
			if isActive && i == active && addLayerButtonSelected {
				lStyle = ansiBgCyan + ansiWhite + ansiBold
			}
			xStyle := rowStyle
			if isActive && i == active && deleteButtonSelected {
				xStyle = ansiBgRed + ansiWhite + ansiBold
			}
			g.Text(menu.X+menu.W-6, menu.Y+row, " L ", lStyle)
			g.Text(menu.X+menu.W-3, menu.Y+row, " X ", xStyle)
			continue
		}
		if dataRow {
			xStyle := rowStyle
			if isActive && i == active && deleteButtonSelected {
				xStyle = ansiBgRed + ansiWhite + ansiBold
			}
			if action == diskMenuActionNone {
				dStyle := rowStyle
				if isActive && i == active && detachButtonSelected {
					dStyle = ansiBgYellow + ansiBlack + ansiBold
				}
				g.Text(menu.X+menu.W-6, menu.Y+row, " D ", dStyle)
			}
			g.Text(menu.X+menu.W-3, menu.Y+row, " X ", xStyle)
			continue
		}
		if layerRow {
			mStyle := rowStyle
			if isActive && i == active && mergeButtonSelected {
				mStyle = ansiBgGreen + ansiWhite + ansiBold
			}
			dStyle := rowStyle
			if isActive && i == active && detachButtonSelected {
				dStyle = ansiBgYellow + ansiBlack + ansiBold
			}
			xStyle := rowStyle
			if isActive && i == active && deleteButtonSelected {
				xStyle = ansiBgRed + ansiWhite + ansiBold
			}
			if activeLayerRow {
				g.Text(menu.X+menu.W-9, menu.Y+row, " M ", mStyle)
				g.Text(menu.X+menu.W-6, menu.Y+row, " D ", dStyle)
				g.Text(menu.X+menu.W-3, menu.Y+row, " X ", xStyle)
				continue
			}
			g.Text(menu.X+menu.W-6, menu.Y+row, " M ", mStyle)
			g.Text(menu.X+menu.W-3, menu.Y+row, " X ", xStyle)
			continue
		}
		if isNICDetail(item) {
			xStyle := rowStyle
			if isActive && i == active && deleteButtonSelected {
				xStyle = ansiBgRed + ansiWhite + ansiBold
			}
			g.Text(menu.X+menu.W-3, menu.Y+row, " X ", xStyle)
		}
	}
}

func drawContextEditPlaceholder(g *grid, x, y int, item, rowStyle string) {
	start := strings.Index(item, "|"+contextEditPlaceholder)
	if start < 0 {
		return
	}
	g.Text(x+start+1, y, contextEditPlaceholder, rowStyle+ansiDim)
}

func isContextPlaceholderItem(item string) bool {
	value, ok := contextDisplayValue(item)
	return ok && value == contextEditPlaceholder
}

func drawContextPlaceholder(g *grid, x, y int, item, rowStyle string) {
	start := strings.Index(item, contextEditPlaceholder)
	if start < 0 {
		return
	}
	g.Text(x+start, y, contextEditPlaceholder, rowStyle+ansiDim)
}

func drawConsole(g *grid, state ViewState, width, height int) {
	line := state.Message
	if line == "" {
		line = "Space/menu click opens actions"
	}
	drawConsoleLines(g, []string{line}, width, height, false)
}

func drawConsoleLines(g *grid, lines []string, width, height int, commandMode bool) {
	maxLines := min(len(lines), 1)
	y := height - maxLines
	for i := 0; i < maxLines; i++ {
		line := lines[len(lines)-maxLines+i]
		style := ansiBgGray + ansiWhite
		if commandMode && i == maxLines-1 {
			style += ansiBold
		}
		fillRow(g, 0, y+i, width, style)
		g.Text(1, y+i, fit(line, width-2), style)
	}
}

func drawMouseClickFeedback(g *grid, state ViewState) {
	if !state.MouseClickActive {
		return
	}
	style := ansiBgCyan + ansiWhite + ansiBold
	w := max(1, state.MouseClickW)
	h := max(1, state.MouseClickH)
	for y := state.MouseClickY; y < state.MouseClickY+h; y++ {
		for x := state.MouseClickX; x < state.MouseClickX+w; x++ {
			g.SetStyle(x, y, style)
		}
	}
}

func stateStyle(state string) string {
	switch state {
	case "running", "link":
		return ansiCyan
	case "nat", "bridge", "direct", "macnat", "macnat-bridge":
		return ansiYellow
	default:
		return ansiDim
	}
}
