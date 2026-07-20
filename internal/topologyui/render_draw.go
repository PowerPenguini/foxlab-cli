package topologyui

import (
	"fmt"
	"strings"
	"unicode"
)

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
	panelStyle := nodePanelStyle(node.Type, selected && graphFocused)
	stateStyleValue := panelStyle + nodeStateStyle(node.Type, node.State)
	fillRect(g, r, panelStyle)
	if selected && graphFocused {
		fillRow(g, r.X, r.Y, r.W, nodePanelStyle(node.Type, true)+ansiBold+ansiBrightCyan)
	}
	drawNodeAccent(g, node.Type, r, selected && graphFocused)
	kind := "[" + firstNonEmpty(node.Badge, NodeKind(node.Type)) + "]"
	fullLabel := kind + " " + node.Label
	contentX := r.X + 2
	contentWidth := r.W - 3
	labelWidth := contentWidth - runeLen(kind) - 1
	if labelWidth > 0 {
		g.Text(contentX, r.Y+1, kind, panelStyle+nodeBadgeStyle(node.Type))
		g.Text(contentX+runeLen(kind)+1, r.Y+1, fit(node.Label, labelWidth), panelStyle+nodeLabelStyle(node.Type))
	} else {
		g.Text(contentX, r.Y+1, fit(fullLabel, contentWidth), panelStyle+nodeLabelStyle(node.Type))
	}
	lines := nodeCardLines(node, frame, contentWidth)
	for i, line := range lines {
		y := r.Y + 2 + i
		if y >= r.Y+r.H {
			break
		}
		drawNodeCardLine(g, node, contentX, y, contentWidth, line, stateStyleValue, panelStyle)
	}
}

func drawNodeAccent(g *grid, nodeType string, r rect, selected bool) {
	style := nodeAccentStyle(nodeType, selected)
	for y := r.Y; y < r.Y+r.H; y++ {
		g.Set(r.X, y, '▌', style)
	}
}

func drawNodeCardLine(g *grid, node Node, x, y, width int, line, valueStyle, panelStyle string) {
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
		g.Text(x, y, fit(prefix, width), nodeDetailLabelStyle(node.Type, panelStyle))
		return
	}
	g.Text(x, y, fit(prefix, labelWidth), nodeDetailLabelStyle(node.Type, panelStyle))
	if padding := labelWidth - runeLen(prefix); padding > 0 {
		g.Text(x+runeLen(prefix), y, strings.Repeat(" ", padding), nodeDetailLabelStyle(node.Type, panelStyle))
	}
	g.Text(x+labelWidth, y, fit(value, width-labelWidth), valueStyle)
}

func nodeDetailLabelStyle(nodeType, panelStyle string) string {
	switch nodeType {
	case NodeSwitch:
		return panelStyle + ansiYellow + ansiDim
	case NodeExternal:
		return panelStyle + ansiBrightMagenta + ansiDim
	default:
		return panelStyle + ansiBrightBlack
	}
}

func nodeDetailLabelWidth(nodeType string) int {
	if nodeType == NodeExternal {
		return runeLen("Iface: ")
	}
	return runeLen("Mode: ")
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
	clearRect(g, column.rect)
	drawContextMenuItems(g, column.rect, menuItemLabels(column.items), menuItemActions(column.items), menuItemKinds(column.items), menuItemEnabled(column.items), column.selected, column.start, isActive, editing, editValue, editCursor, deleteButtonSelected, mergeButtonSelected, detachButtonSelected, addLayerButtonSelected, diskMenu)
}

func drawPalette(g *grid, m Model, state ViewState, width, height int) {
	if !state.PaletteOpen {
		return
	}
	layout, ok := paletteLayout(width, height)
	if !ok {
		return
	}
	drawPaletteOverlay(g, layout)
	fillRect(g, layout, themePalette)
	drawPalettePrompt(g, m, state, layout)
	actions := filteredPaletteActions(m, state)
	if len(actions) == 0 {
		g.Text(layout.X+paletteRecordPaddingX, paletteEmptyY(layout), fit("no completions", layout.W-paletteRecordPaddingX*2), themePalette)
		return
	}
	start := paletteStart(state, len(actions), layout)
	selected := normalizedMenuSelection(state.PaletteSelected, len(actions))
	visible := paletteVisibleRows(layout)
	for row := 0; row < visible; row++ {
		index := start + row
		if index >= len(actions) {
			break
		}
		y := paletteRowsY(layout) + row
		action := actions[index]
		rowStyle := themePalette
		labelStyle := rowStyle
		hintStyle := themePaletteHint
		if index == selected {
			rowStyle = themePaletteActive
			labelStyle = rowStyle
			hintStyle = rowStyle
			fillRow(g, layout.X, y, layout.W, rowStyle)
		}
		if !action.Enabled {
			labelStyle = rowStyle + ansiBrightBlack
			hintStyle = rowStyle + ansiBrightBlack
		}
		labelX := layout.X + paletteRecordPaddingX
		hint := paletteActionHint(action)
		hintW := 0
		if hint != "" && layout.W > 48 {
			hintW = min(18, runeLen(hint)+2)
		}
		labelW := layout.W - paletteRecordPaddingX*2 - hintW
		g.Text(labelX, y, fit(paletteActionDisplay(action), labelW), labelStyle)
		if hintW > 0 {
			g.Text(layout.X+layout.W-paletteRecordPaddingX-hintW, y, fit(hint, hintW), hintStyle)
		}
	}
	if len(actions) > visible {
		scroll := paletteScrollText(start, visible, len(actions))
		if scroll != "" {
			g.Text(layout.X+layout.W-1-runeLen(scroll), layout.Y, scroll, themePaletteMuted)
		}
	}
}

func drawPaletteOverlay(g *grid, layout rect) {
	for y := 0; y < g.Height; y++ {
		for x := 0; x < g.Width; x++ {
			if x >= layout.X && x < layout.X+layout.W && y >= layout.Y && y < layout.Y+layout.H {
				continue
			}
			cell := &g.Cells[y*g.Width+x]
			cell.Style = paletteOverlayStyle(cell.Style)
		}
	}
}

func paletteOverlayStyle(style string) string {
	if style == "" {
		return themeTerminal + ansiDim
	}
	if strings.Contains(style, ansiDim) {
		return style
	}
	return style + ansiDim
}

func drawPalettePrompt(g *grid, m Model, state ViewState, layout rect) {
	actions := filteredPaletteActions(m, state)
	selected := normalizedMenuSelection(state.PaletteSelected, len(actions))
	query := state.PaletteQuery
	normalized := normalizedPaletteQuery(query)
	input := paletteInputRect(layout)
	fillRect(g, input, themePaletteInput)
	x := input.X + paletteInputPaddingX
	y := input.Y + paletteInputPaddingY
	maxW := input.W - paletteInputPaddingX*2
	prompt := ":" + query
	g.Text(x, y, fit(prompt, maxW), themePaletteInput+ansiBrightCyan+ansiBold)
	if len(actions) == 0 || selected >= len(actions) {
		return
	}
	suffix := paletteCompletionSuffix(normalized, actions[selected].Query)
	if suffix == "" {
		return
	}
	offset := runeLen(prompt)
	if offset >= maxW {
		return
	}
	g.Text(x+offset, y, fit(suffix, maxW-offset), themePaletteInputHint)
}

func paletteCompletionSuffix(query, completion string) string {
	if completion == "" || query == completion || !strings.HasPrefix(completion, query) {
		return ""
	}
	return string([]rune(completion)[runeLen(query):])
}

func paletteActionDisplay(action paletteAction) string {
	if action.Query != "" {
		return action.Query
	}
	return strings.ToLower(action.Label)
}

func paletteActionHint(action paletteAction) string {
	if !action.Enabled {
		if action.DisabledReason != "" {
			return "disabled"
		}
		return "unavailable"
	}
	if action.CompleteOnly {
		return "complete"
	}
	if action.Hint != "" {
		return action.Hint
	}
	return "run"
}

func paletteScrollText(start, visible, count int) string {
	if visible <= 0 || count <= visible {
		return ""
	}
	top := min(count, start+1)
	bottom := min(count, start+visible)
	return fmt.Sprintf("%d-%d/%d", top, bottom, count)
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
		if isActive && i == active {
			rowStyle = themeMenuActive
		}
		textStyle := rowStyle
		if isContextInfoItem(item) || !itemEnabled {
			textStyle = themeMenuMuted
			if rowStyle == themeMenuActive {
				textStyle = themeMenuMutedActive
			}
		}
		fillRow(g, menu.X, menu.Y+row, menu.W, rowStyle)
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
				lStyle = ansiBrightCyan + ansiBold
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
				mStyle = ansiGreen + ansiBold
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
	notification, ok := notificationFromState(state)
	if !ok {
		return
	}
	line := consoleLine(state)
	if line == "" || width <= 0 || height <= 0 {
		return
	}
	drawNotification(g, notificationDisplayLine(line), notificationThemeFor(notification, line), width, height)
}

func consoleLine(state ViewState) string {
	if notification, ok := notificationFromState(state); ok {
		if notification.Busy || animatedStateFromMessage(notification.Text) {
			return spinner(state.AnimationFrame) + " " + notification.Text
		}
		return notification.Text
	}
	return ""
}

type notificationTheme struct {
	body string
	bar  string
}

func notificationThemeForLine(line string) notificationTheme {
	return notificationThemeFor(Notification{}, line)
}

func notificationThemeFor(notification Notification, line string) notificationTheme {
	theme := notificationTheme{body: themeNotification, bar: themeNotificationBar}
	if notification.Revision != 0 {
		switch notification.Level {
		case NotificationInfo:
			theme.bar = themeNotificationInfoBar
		case NotificationSuccess:
			theme.bar = themeNotificationSuccessBar
		}
	} else if isInfoNotification(line) {
		theme.bar = themeNotificationInfoBar
	} else if isSuccessNotification(line) {
		theme.bar = themeNotificationSuccessBar
	}
	return theme
}

func isInfoNotification(line string) bool {
	line = strings.TrimSpace(strings.ToLower(line))
	return strings.HasPrefix(line, "configured ")
}

func isSuccessNotification(line string) bool {
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return false
	}
	for _, marker := range []string{" failed", " failed:", " not found", " unavailable", " denied", " missing", " invalid", " unsupported"} {
		if strings.Contains(line, marker) {
			return false
		}
	}
	for _, prefix := range []string{
		"applied lab ",
		"attached disk:",
		"created disk:",
		"created disk layer:",
		"deleted disk:",
		"detached disk from ",
		"merged disk layer:",
		"renamed disk:",
		"resized disk:",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func notificationDisplayLine(line string) string {
	line = strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(line, "applied lab "):
		return "Lab applied: " + strings.TrimSpace(strings.TrimPrefix(line, "applied lab "))
	case strings.HasPrefix(line, "lab already applied "):
		return "Lab already applied: " + strings.TrimSpace(strings.TrimPrefix(line, "lab already applied "))
	case strings.HasPrefix(line, "created disk layer:"):
		return "Disk layer created: " + strings.TrimSpace(strings.TrimPrefix(line, "created disk layer:"))
	case strings.HasPrefix(line, "created disk:"):
		return "Disk created: " + strings.TrimSpace(strings.TrimPrefix(line, "created disk:"))
	case strings.HasPrefix(line, "deleted disk:"):
		return "Disk deleted: " + strings.TrimSpace(strings.TrimPrefix(line, "deleted disk:"))
	case strings.HasPrefix(line, "resized disk:"):
		return "Disk resized: " + strings.TrimSpace(strings.TrimPrefix(line, "resized disk:"))
	case strings.HasPrefix(line, "merged disk layer:"):
		return "Disk layer merged: " + strings.TrimSpace(strings.TrimPrefix(line, "merged disk layer:"))
	case strings.HasPrefix(line, "attached disk:"):
		return formatDiskTargetNotification(line, "attached disk:", "Disk attached")
	case strings.HasPrefix(line, "renamed disk:"):
		return formatRenamedDiskNotification(line)
	case strings.HasPrefix(line, "detached disk from "):
		return "Disk detached from: " + strings.TrimSpace(strings.TrimPrefix(line, "detached disk from "))
	default:
		return capitalizeMessage(line)
	}
}

func formatDiskTargetNotification(line, prefix, label string) string {
	value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	id, target, ok := strings.Cut(value, " to ")
	if !ok {
		return label + ": " + value
	}
	return label + ": " + strings.TrimSpace(id) + " -> " + strings.TrimSpace(target)
}

func formatRenamedDiskNotification(line string) string {
	value := strings.TrimSpace(strings.TrimPrefix(line, "renamed disk:"))
	id, next, ok := strings.Cut(value, " to ")
	if !ok {
		return "Disk renamed: " + value
	}
	return "Disk renamed: " + strings.TrimSpace(id) + " -> " + strings.TrimSpace(next)
}

func capitalizeMessage(line string) string {
	if line == "" {
		return ""
	}
	runes := []rune(line)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func drawNotification(g *grid, line string, theme notificationTheme, width, height int) {
	layout, ok := notificationLayoutFor(line, width, height)
	if !ok {
		return
	}
	for row := 0; row < layout.bounds.H; row++ {
		y := layout.bounds.Y + row
		g.Set(layout.bounds.X, y, ' ', theme.bar)
		fillRow(g, layout.bounds.X+1, y, layout.bodyW, theme.body)
	}
	for i, line := range layout.lines {
		y := layout.bounds.Y + i + layout.paddingY
		g.Text(layout.bounds.X+1+layout.horizontalPadding, y, fit(line, layout.bodyW-layout.horizontalPadding*2), theme.body)
	}
}

type notificationLayout struct {
	bounds            rect
	lines             []string
	bodyW             int
	paddingY          int
	horizontalPadding int
}

func notificationLayoutFor(line string, width, height int) (notificationLayout, bool) {
	if width <= 3 || height <= 0 {
		return notificationLayout{}, false
	}
	const horizontalPadding = 2
	const verticalPadding = 1
	const maxNotificationLines = 4
	maxW := min(width-2, max(24, width/2))
	textW := maxW - 1 - horizontalPadding*2
	if textW <= 0 {
		return notificationLayout{}, false
	}
	x := 1
	bottomY := height - 1
	availableRows := bottomY + 1
	paddingY := 0
	if availableRows > verticalPadding*2 {
		paddingY = verticalPadding
	}
	maxLines := min(maxNotificationLines, availableRows-paddingY*2)
	lines := notificationLines(line, textW, maxLines)
	if len(lines) == 0 {
		return notificationLayout{}, false
	}
	longest := 0
	for _, line := range lines {
		longest = max(longest, runeLen(line))
	}
	bodyW := min(maxW-1, longest+horizontalPadding*2)
	boxH := len(lines) + paddingY*2
	startY := bottomY - boxH + 1
	if startY < 0 {
		startY = 0
	}
	return notificationLayout{
		bounds:            rect{X: x, Y: startY, W: bodyW + 1, H: boxH},
		lines:             lines,
		bodyW:             bodyW,
		paddingY:          paddingY,
		horizontalPadding: horizontalPadding,
	}, true
}

func notificationBoundsForState(state ViewState, width, height int) (rect, bool) {
	line := consoleLine(state)
	if line == "" {
		return rect{}, false
	}
	layout, ok := notificationLayoutFor(notificationDisplayLine(line), width, height)
	return layout.bounds, ok
}

func notificationLines(text string, width, maxLines int) []string {
	if width <= 0 || maxLines <= 0 {
		return nil
	}
	lines := []string{}
	for _, raw := range strings.Split(text, "\n") {
		words := strings.Fields(raw)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		current := ""
		for _, word := range words {
			for runeLen(word) > width {
				if current != "" {
					lines = append(lines, current)
					current = ""
				}
				prefix, rest := splitRunes(word, width)
				lines = append(lines, prefix)
				word = rest
			}
			if current == "" {
				current = word
				continue
			}
			next := current + " " + word
			if runeLen(next) <= width {
				current = next
				continue
			}
			lines = append(lines, current)
			current = word
		}
		if current != "" {
			lines = append(lines, current)
		}
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = fit(lines[maxLines-1]+" ...", width)
	}
	return lines
}

func splitRunes(value string, width int) (string, string) {
	runes := []rune(value)
	if width <= 0 || width >= len(runes) {
		return value, ""
	}
	return string(runes[:width]), string(runes[width:])
}

func drawMouseClickFeedback(g *grid, state ViewState) {
	if !state.MouseClickActive {
		return
	}
	style := ansiInverse
	w := max(1, state.MouseClickW)
	h := max(1, state.MouseClickH)
	for y := state.MouseClickY; y < state.MouseClickY+h; y++ {
		for x := state.MouseClickX; x < state.MouseClickX+w; x++ {
			g.SetStyle(x, y, style)
		}
	}
}
