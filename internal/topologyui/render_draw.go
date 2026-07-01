package topologyui

import "strings"

func layoutNodeRects(m Model, pane rect) map[string]rect {
	return layoutNodeRectsWithPan(m, pane, 0, 0)
}

func layoutNodeRectsWithPan(m Model, pane rect, panX, panY int) map[string]rect {
	out := make(map[string]rect, len(m.Nodes))
	if len(m.Nodes) == 0 {
		return out
	}
	for _, node := range m.Nodes {
		x := pane.X + node.X + panX
		y := pane.Y + node.Y + panY
		out[node.Key()] = rect{X: x, Y: y, W: nodeWidthForNode(node), H: nodeHeightForNode(node)}
	}
	return out
}

func rectFullyVisible(r rect, bounds rect) bool {
	return r.X >= bounds.X &&
		r.Y >= bounds.Y &&
		r.X+r.W <= bounds.X+bounds.W &&
		r.Y+r.H <= bounds.Y+bounds.H
}

func rectIntersects(r rect, bounds rect) bool {
	return r.W > 0 &&
		r.H > 0 &&
		bounds.W > 0 &&
		bounds.H > 0 &&
		r.X < bounds.X+bounds.W &&
		r.X+r.W > bounds.X &&
		r.Y < bounds.Y+bounds.H &&
		r.Y+r.H > bounds.Y
}

func drawNode(g *grid, node Node, r rect, selected, graphFocused bool, frame int) {
	stateStyleValue := nodeStateStyle(node.Type, node.State)
	clearRect(g, r)
	drawNodeBox(g, r, selectedBorderStyle(selected, graphFocused))
	kind := "[" + firstNonEmpty(node.Badge, NodeKind(node.Type)) + "]"
	fullLabel := kind + " " + node.Label
	contentX := r.X + 1
	contentWidth := r.W - 2
	labelWidth := contentWidth - runeLen(kind) - 1
	if labelWidth > 0 {
		g.Text(contentX, r.Y+1, kind, nodeBadgeStyle(node.Type))
		g.Text(contentX+runeLen(kind)+1, r.Y+1, fit(node.Label, labelWidth), nodeLabelStyle(node.Type))
	} else {
		g.Text(contentX, r.Y+1, fit(fullLabel, contentWidth), nodeLabelStyle(node.Type))
	}
	lines := nodeCardLines(node, frame, r.W-2)
	for i, line := range lines {
		y := r.Y + 2 + i
		if y >= r.Y+r.H-1 {
			break
		}
		drawNodeCardLine(g, node, r.X+1, y, r.W-2, line, stateStyleValue)
	}
}

func drawNodeCardLine(g *grid, node Node, x, y, width int, line, valueStyle string) {
	line = fit(line, width)
	if node.Type != NodeExternal && node.Type != NodeSwitch {
		g.Text(x, y, line, valueStyle)
		return
	}
	label, value, ok := strings.Cut(line, ": ")
	if !ok || label == "" || value == "" {
		g.Text(x, y, line, valueStyle)
		return
	}
	prefix := label + ":"
	labelWidth := nodeDetailLabelWidth(node.Type)
	if labelWidth >= width {
		g.Text(x, y, fit(prefix, width), nodeDetailLabelStyle(node.Type))
		return
	}
	g.Text(x, y, fit(prefix, labelWidth), nodeDetailLabelStyle(node.Type))
	if padding := labelWidth - runeLen(prefix); padding > 0 {
		g.Text(x+runeLen(prefix), y, strings.Repeat(" ", padding), nodeDetailLabelStyle(node.Type))
	}
	g.Text(x+labelWidth, y, fit(value, width-labelWidth), valueStyle)
}

func nodeDetailLabelStyle(nodeType string) string {
	switch nodeType {
	case NodeSwitch:
		return ansiYellow + ansiDim
	case NodeExternal:
		return ansiBrightMagenta + ansiDim
	default:
		return themeMuted
	}
}

func nodeDetailLabelWidth(nodeType string) int {
	if nodeType == NodeExternal {
		return runeLen("Iface: ")
	}
	return runeLen("Mode: ")
}

func drawNodeBox(g *grid, r rect, style string) {
	if r.W < 2 || r.H < 2 {
		return
	}
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		g.Set(x, r.Y, lineHorizontal, style)
		g.Set(x, r.Y+r.H-1, lineHorizontal, style)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		g.Set(r.X, y, lineVertical, style)
		g.Set(r.X+r.W-1, y, lineVertical, style)
	}
	g.Set(r.X, r.Y, '╭', style)
	g.Set(r.X+r.W-1, r.Y, '╮', style)
	g.Set(r.X, r.Y+r.H-1, '╰', style)
	g.Set(r.X+r.W-1, r.Y+r.H-1, '╯', style)
}

func displayNodeState(state string, frame int) string {
	if animatedState(state) {
		return spinner(frame) + " " + state
	}
	if glyph := stateGlyph(state); glyph != "" {
		return glyph + " " + state
	}
	return modeDisplayLabel(state)
}

func stateGlyph(state string) string {
	switch state {
	case "running", "link":
		return "●"
	case "defined", "stopped", "shutoff", "created":
		return "◌"
	case "missing", "error", "failed":
		return "!"
	default:
		return ""
	}
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
	drawMenuColumn(g, layout.root, !state.ContextInSubmenu, false, "", 0, false, false, false, false, false)
	if !layout.hasSub {
		return
	}
	contextGroup := state.ContextGroup
	deleteActionSelected := (contextGroup == "nic-menu" && state.ContextDeleteNIC) ||
		(contextGroup == "uplink-menu" && state.ContextDeleteUplink) ||
		(contextGroup == "disk-menu" && state.ContextDeleteDisk)
	mergeActionSelected := contextGroup == "disk-menu" && state.ContextMergeDisk
	detachActionSelected := contextGroup == "disk-menu" && state.ContextDetachDisk
	addLayerActionSelected := contextGroup == "disk-menu" && state.ContextAddDiskLayer
	drawMenuColumn(g, layout.sub, state.ContextInSubmenu && !layout.hasSelect, state.ContextEdit, state.ContextEditValue, state.ContextEditCursor, deleteActionSelected, mergeActionSelected, detachActionSelected, addLayerActionSelected, contextGroup == "disk-menu")
	if layout.hasSelect {
		drawMenuColumn(g, layout.selectBox, true, false, "", 0, false, false, false, false, false)
	}
}

func drawMenuColumn(g *grid, column menuColumnLayout, isActive bool, editing bool, editValue string, editCursor int, deleteButtonSelected bool, mergeButtonSelected bool, detachButtonSelected bool, addLayerButtonSelected bool, diskMenu bool) {
	drawContextMenuItems(g, column.rect, menuItemLabels(column.items), menuItemActions(column.items), menuItemKinds(column.items), menuItemEnabled(column.items), column.selected, column.start, isActive, editing, editValue, editCursor, deleteButtonSelected, mergeButtonSelected, detachButtonSelected, addLayerButtonSelected, diskMenu)
}

func drawTopRibbon(g *grid, m Model, bounds rect, state ViewState) {
	items := topRibbonRootItems()
	buttons := topMenuButtonRects(items, bounds.W)
	fillRow(g, 0, 0, g.Width, themeChrome)
	activeRoot := normalizedMenuSelection(state.TopMenuRootSelected, len(items))
	addRoot := topRibbonAddRootIndex(items)
	for i, button := range buttons {
		style := themeChrome
		enabled := topRibbonRootEnabled(items[i], state)
		if !enabled {
			style = themeChrome + themeMuted
		} else if state.Focus == FocusTop && i == activeRoot {
			style = themeChromeActive
		}
		if state.TopMenuOpen && i == addRoot {
			style = themeChromeActive
		}
		g.Text(button.X, button.Y, fit(" "+topMenuLabel(items[i])+" ", button.W), style)
	}
	drawTopRibbonContext(g, m, bounds, state, buttons)
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

func drawTopRibbonContext(g *grid, m Model, bounds rect, state ViewState, buttons []rect) {
	if bounds.W < 72 {
		return
	}
	leftLimit := 0
	if len(buttons) > 0 {
		last := buttons[len(buttons)-1]
		leftLimit = last.X + last.W + 1
	}
	context := topRibbonContext(m, state)
	if state.StatusRefreshing {
		context = spinner(state.AnimationFrame) + " " + context
	}
	if context == "" {
		return
	}
	width := runeLen(context) + 2
	x := bounds.X + bounds.W - width
	if x <= leftLimit {
		return
	}
	g.Text(x, bounds.Y, fit(" "+context+" ", width), themeChrome+themeMuted)
}

func topRibbonContext(m Model, state ViewState) string {
	mode := "graph"
	switch {
	case state.CommandMode:
		mode = "command"
	case state.MoveMode:
		mode = "move"
	case state.ConnectMode:
		mode = "connect"
	case state.ContextMenu:
		mode = "menu"
	case state.Focus == FocusTop:
		mode = "top"
	}
	parts := []string{}
	if m.ID != "" {
		parts = append(parts, "lab "+m.ID)
	}
	if node, ok := selectedNode(m, state.Selected); ok {
		parts = append(parts, NodeKind(node.Type)+" "+firstNonEmpty(node.Label, node.ID))
	}
	parts = append(parts, "mode:"+mode)
	return strings.Join(parts, " | ")
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
	if !ok || !rectIntersects(from, bounds) {
		return
	}
	to, ok := nodeRects[targetKey]
	if !ok || !rectIntersects(to, bounds) {
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

func drawContextMenuItems(g *grid, menu rect, items []string, actions []string, kinds []string, enabled []bool, active, start int, isActive bool, editing bool, editValue string, editCursor int, deleteButtonSelected bool, mergeButtonSelected bool, detachButtonSelected bool, addLayerButtonSelected bool, diskMenu bool) {
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
		itemEnabled := true
		if i < len(enabled) {
			itemEnabled = enabled[i]
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
		rowStyle := themeMenuRow
		textStyle := rowStyle
		if isContextInfoItem(item) || !itemEnabled {
			textStyle += themeMuted
		}
		if isActive && i == active {
			rowStyle = themeMenuActive
			textStyle = rowStyle
		}
		fillRow(g, menu.X, menu.Y+row, menu.W, rowStyle)
		if isActive && i == active {
			g.Set(menu.X, menu.Y+row, ' ', ansiBgCyan)
		}
		textWidth := menu.W - 3
		if isNICDetail(item) || kind == "uplink" || layerRow {
			textWidth = max(0, menu.W-6)
		}
		if baseRow || layerRow || dataRow {
			textWidth = max(0, menu.W-9)
		}
		if activeLayerRow {
			textWidth = max(0, menu.W-12)
		}
		renderedItem := fit(item, textWidth)
		g.Text(menu.X+2, menu.Y+row, renderedItem, textStyle)
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
		if isNICDetail(item) || kind == "uplink" {
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
	line := consoleLine(state)
	if line == "" {
		return
	}
	drawConsoleLines(g, []string{line}, width, height, state.CommandMode)
}

func consoleLine(state ViewState) string {
	if state.Message != "" {
		if animatedStateFromMessage(state.Message) {
			return spinner(state.AnimationFrame) + " " + state.Message
		}
		return state.Message
	}
	return ""
}

func drawConsoleLines(g *grid, lines []string, width, height int, commandMode bool) {
	maxLines := min(len(lines), 1)
	y := height - maxLines
	for i := 0; i < maxLines; i++ {
		line := lines[len(lines)-maxLines+i]
		style := themeFooter
		if commandMode && i == maxLines-1 {
			style = themeFooterActive
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
