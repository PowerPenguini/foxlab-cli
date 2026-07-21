package topologyui

import (
	"strconv"
	"strings"
)

const (
	inspectorButtonGreenStyle       = "\x1b[48;2;53;167;89m" + ansiBlack + ansiBold
	inspectorButtonGreenActiveStyle = "\x1b[48;2;75;199;115m" + ansiBlack + ansiBold
	inspectorButtonRedStyle         = "\x1b[48;2;194;55;69m" + ansiWhite + ansiBold
	inspectorButtonRedActiveStyle   = "\x1b[48;2;229;76;92m" + ansiWhite + ansiBold
	inspectorButtonCyanStyle        = "\x1b[48;2;65;161;194m" + ansiBlack + ansiBold
	inspectorButtonCyanActiveStyle  = "\x1b[48;2;86;190;224m" + ansiBlack + ansiBold
	inspectorButtonMutedStyle       = "\x1b[48;2;74;81;90m" + ansiWhite + ansiBold
	inspectorButtonMutedActiveStyle = "\x1b[48;2;105;115;126m" + ansiWhite + ansiBold
	inspectorPanelForeground        = "\x1b[38;5;233m"
)

func drawInspector(g *grid, m Model, state ViewState, panel rect) {
	if panel.W <= 0 || panel.H <= 0 {
		return
	}
	fillRect(g, panel, themePanelInspector)
	node, ok := selectedNode(m, state.Selected)
	if !ok {
		return
	}
	x := panel.X + 3
	w := panel.W - 6
	if w <= 0 {
		return
	}
	drawInspectorHeader(g, node, x, panel.Y+1, w)
	stateLine := displayNodeState(node.State, state.AnimationFrame)
	if node.DesiredState != "" {
		stateLine += "  /  want " + node.DesiredState
	}
	g.Text(x, panel.Y+2, fit(stateLine, w), themePanelInspector+nodeStateStyle(node.Type, node.State))
	drawInspectorActionButtons(g, node, state, panel)
	drawInspectorFields(g, node, state, panel)
	drawInspectorFooter(g, node, state, panel)
}

func drawInspectorActionButtons(g *grid, node Node, state ViewState, panel rect) {
	fields := inspectorFieldsForState(node, state)
	if index, field, ok := inspectorPowerField(fields); ok {
		style := inspectorButtonGreenStyle
		label := "▶  Start"
		if field.value == "stop" {
			style = inspectorButtonRedStyle
			label = "■  Stop"
		}
		drawInspectorActionButton(g, inspectorPowerButtonRect(panel), label, style, state.Focus == FocusInspector && normalizedMenuSelection(state.InspectorSelected, len(fields)) == index)
	}
	if index, _, ok := inspectorShellField(fields); ok {
		style := inspectorButtonMutedStyle
		if normalizeRuntimeState(node.State) == "running" {
			style = inspectorButtonCyanStyle
		}
		drawInspectorActionButton(g, inspectorShellButtonRectForFields(panel, fields), ">_ Shell", style, state.Focus == FocusInspector && normalizedMenuSelection(state.InspectorSelected, len(fields)) == index)
	}
	if index, _, ok := inspectorVNCField(fields); ok {
		viewerActive := state.VNCViewerActive[NodeKey(NodeVM, node.ID)]
		style := inspectorButtonMutedStyle
		label := "▣  VNC"
		if viewerActive {
			style = inspectorButtonRedStyle
			label = "■  Stop VNC"
		} else if normalizeRuntimeState(node.State) == "running" && strings.EqualFold(nodeDetailRawValue(node, "vnc"), "true") {
			style = inspectorButtonCyanStyle
		}
		drawInspectorActionButton(g, inspectorVNCButtonRect(panel), label, style, state.Focus == FocusInspector && normalizedMenuSelection(state.InspectorSelected, len(fields)) == index)
	}
}

func drawInspectorActionButton(g *grid, button rect, label, style string, selected bool) {
	if button.W <= 0 || button.H < 3 {
		return
	}
	if selected {
		style = inspectorActiveActionButtonStyle(style)
	}
	edgeStyle := style + inspectorPanelForeground
	for x := button.X; x < button.X+button.W; x++ {
		g.Set(x, button.Y, '▀', edgeStyle)
		g.Set(x, button.Y+button.H-1, '▄', edgeStyle)
	}
	textY := button.Y + button.H/2
	fillRow(g, button.X, textY, button.W, style)
	labelX := button.X + max(1, (button.W-runeLen(label))/2)
	g.Text(labelX, textY, fit(label, button.W-1), style)
}

func inspectorActiveActionButtonStyle(style string) string {
	switch style {
	case inspectorButtonGreenStyle:
		return inspectorButtonGreenActiveStyle
	case inspectorButtonRedStyle:
		return inspectorButtonRedActiveStyle
	case inspectorButtonCyanStyle:
		return inspectorButtonCyanActiveStyle
	case inspectorButtonMutedStyle:
		return inspectorButtonMutedActiveStyle
	default:
		return style
	}
}

func inspectorPowerField(fields []inspectorField) (int, inspectorField, bool) {
	return inspectorFieldByKind(fields, inspectorFieldPower)
}

func inspectorShellField(fields []inspectorField) (int, inspectorField, bool) {
	return inspectorFieldByKind(fields, inspectorFieldShellAction)
}

func inspectorVNCField(fields []inspectorField) (int, inspectorField, bool) {
	return inspectorFieldByKind(fields, inspectorFieldVNCAction)
}

func inspectorDeleteField(fields []inspectorField) (int, inspectorField, bool) {
	return inspectorFieldByKind(fields, inspectorFieldDeleteAction)
}

func inspectorMoveField(fields []inspectorField) (int, inspectorField, bool) {
	return inspectorFieldByKind(fields, inspectorFieldMoveAction)
}

func inspectorFieldByKind(fields []inspectorField, kind string) (int, inspectorField, bool) {
	for index, field := range fields {
		if field.kind == kind {
			return index, field, true
		}
	}
	return 0, inspectorField{}, false
}

func inspectorPowerButtonRect(panel rect) rect {
	return rect{X: panel.X + 3, Y: panel.Y + 4, W: max(0, panel.W-6), H: 3}
}

func inspectorShellButtonRect(panel rect) rect {
	contentWidth := max(0, panel.W-6)
	return rect{X: panel.X + 3, Y: panel.Y + 7, W: max(0, (contentWidth-1)/2), H: 3}
}

func inspectorShellButtonRectForFields(panel rect, fields []inspectorField) rect {
	if _, _, ok := inspectorVNCField(fields); ok {
		return inspectorShellButtonRect(panel)
	}
	return rect{X: panel.X + 3, Y: panel.Y + 7, W: max(0, panel.W-6), H: 3}
}

func inspectorVNCButtonRect(panel rect) rect {
	shell := inspectorShellButtonRect(panel)
	contentRight := panel.X + panel.W - 3
	x := shell.X + shell.W + 1
	return rect{X: x, Y: shell.Y, W: max(0, contentRight-x), H: shell.H}
}

func inspectorDeleteButtonRect(panel rect, state ViewState, fields []inspectorField) (rect, bool) {
	index, _, ok := inspectorDeleteField(fields)
	if !ok {
		return rect{}, false
	}
	y, ok := inspectorFieldY(panel, state, fields, index)
	if !ok {
		return rect{}, false
	}
	return rect{X: panel.X + 3, Y: y, W: max(0, panel.W-6), H: 3}, true
}

func inspectorMoveButtonRect(panel rect, state ViewState, fields []inspectorField) (rect, bool) {
	index, _, ok := inspectorMoveField(fields)
	if !ok {
		return rect{}, false
	}
	y, ok := inspectorFieldY(panel, state, fields, index)
	if !ok {
		return rect{}, false
	}
	return rect{X: panel.X + 3, Y: y, W: max(0, panel.W-6), H: 3}, true
}

func drawInspectorFields(g *grid, node Node, state ViewState, panel rect) {
	fields := inspectorFieldsForState(node, state)
	if len(fields) == 0 {
		return
	}
	rows, start, visible := inspectorFieldWindow(panel, state, fields)
	selected := normalizedMenuSelection(state.InspectorSelected, len(fields))
	for visibleRow := 0; visibleRow < visible && start+visibleRow < len(rows); visibleRow++ {
		row := rows[start+visibleRow]
		y := panel.Y + inspectorFieldListY + visibleRow
		if row.fieldIndex < 0 {
			if !row.spacer {
				drawInspectorSectionBar(g, panel, y, row.section)
			}
			continue
		}
		index := row.fieldIndex
		field := fields[index]
		active := state.Focus == FocusInspector && index == selected
		if field.kind == inspectorFieldMoveAction {
			if row.buttonPart == -1 && visibleRow+2 < visible {
				button := rect{X: panel.X + 3, Y: y, W: max(0, panel.W-6), H: 3}
				drawInspectorActionButton(g, button, "↔  Move", inspectorButtonMutedStyle, active)
			}
			continue
		}
		if field.kind == inspectorFieldDeleteAction {
			if row.buttonPart == -1 && visibleRow+2 < visible {
				button := rect{X: panel.X + 3, Y: y, W: max(0, panel.W-6), H: 3}
				drawInspectorActionButton(g, button, "×  Delete", inspectorButtonRedStyle, active)
			}
			continue
		}
		rowStyle := themePanelInspector
		markerStyle := themePanelInspectorMuted
		if active {
			rowStyle = themePanelInspectorActive
			markerStyle = themePanelInspectorActive
			fillRow(g, panel.X, y, panel.W, rowStyle)
			g.Set(panel.X+1, y, '›', markerStyle)
		}
		x := panel.X + 4
		width := panel.W - 8
		keyWidth := min(14, max(10, width/3))
		if field.kind == inspectorFieldCapabilityPicker {
			keyWidth = min(14, max(12, width/3))
		}
		labelStyle := themePanelInspectorMuted
		if active {
			labelStyle = themePanelInspectorActive
		}
		g.Text(x, y, fit(field.label, keyWidth), labelStyle)
		valueX := x + keyWidth + 1
		valueRight := panel.X + panel.W - 3
		attachButton, hasAttachButton := inspectorDiskAttachButtonRect(field, panel, y)
		if hasAttachButton {
			valueRight = attachButton.X - 1
		}
		valueWidth := valueRight - valueX
		if valueWidth > 0 {
			valueStyle := inspectorFieldValueStyle(field, active)
			value := inspectorFieldDisplayValue(field)
			displayWidth := valueWidth - 1
			switch {
			case active && state.InspectorEditing && inspectorFieldSupportsInlineEdit(field):
				value = inspectorEditViewport(state.InspectorEditValue, state.InspectorEditCursor, displayWidth)
			case value == "":
				value = contextEditPlaceholder
				if !active {
					valueStyle = themePanelInspectorMuted
				}
				value = fit(value, displayWidth)
			case field.kind == inspectorFieldText || field.kind == inspectorFieldDisk:
				value = inspectorTailViewport(value, displayWidth)
			default:
				value = fit(value, displayWidth)
			}
			g.Text(valueX+1, y, value, valueStyle)
		}
		if hasAttachButton {
			drawInspectorInlineButton(g, attachButton, "Attach", inspectorButtonCyanStyle, active)
		}
		if field.kind == inspectorFieldNIC {
			valueStyle := inspectorFieldValueStyle(field, active)
			g.Set(panel.X+panel.W-4, y, '×', valueStyle)
		}
	}
	if start > 0 {
		g.Set(panel.X+panel.W-3, panel.Y+inspectorFieldListY, '↑', themePanelInspectorSection)
	}
	if start+visible < len(rows) {
		g.Set(panel.X+panel.W-3, panel.Y+inspectorFieldListY+visible-1, '↓', themePanelInspectorMuted+ansiBold)
	}
	if state.InspectorCapOpen {
		selected := normalizedMenuSelection(state.InspectorSelected, len(fields))
		switch fields[selected].kind {
		case inspectorFieldCapabilityPicker:
			drawInspectorCapabilityPicker(g, node, state, panel, fields)
		case inspectorFieldInterfacePicker:
			drawInspectorInterfacePicker(g, node, state, panel, fields)
		}
	}
}

func inspectorDiskAttachButtonRect(field inspectorField, panel rect, y int) (rect, bool) {
	if field.kind != inspectorFieldDisk || field.diskAction != diskMenuActionAttach || field.diskID == "" {
		return rect{}, false
	}
	const width = 10
	right := panel.X + panel.W - 3
	return rect{X: right - width, Y: y, W: width, H: 1}, true
}

func drawInspectorInlineButton(g *grid, button rect, label, style string, selected bool) {
	if button.W <= 0 || button.H <= 0 {
		return
	}
	if selected {
		style = inspectorActiveActionButtonStyle(style)
	}
	fillRow(g, button.X, button.Y, button.W, style)
	labelX := button.X + max(0, (button.W-runeLen(label))/2)
	g.Text(labelX, button.Y, fit(label, button.W), style)
}

func inspectorTailViewport(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return "…" + string(runes[len(runes)-width+1:])
}

func inspectorFieldSupportsInlineEdit(field inspectorField) bool {
	return field.kind == inspectorFieldText || field.kind == inspectorFieldDiskAdd || field.kind == inspectorFieldDisk
}

func inspectorEditViewport(value string, cursor, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	cursor = clamp(cursor, 0, len(runes))
	if len(runes) == 0 {
		return fit("|"+contextEditPlaceholder, width)
	}
	if width == 1 {
		return "|"
	}
	textWidth := width - 1
	start := max(0, cursor-textWidth/2)
	if start+textWidth > len(runes) {
		start = max(0, len(runes)-textWidth)
	}
	end := min(len(runes), start+textWidth)
	cursorInView := clamp(cursor-start, 0, end-start)
	visible := runes[start:end]
	return string(visible[:cursorInView]) + "|" + string(visible[cursorInView:])
}

func inspectorFieldValueStyle(field inspectorField, active bool) string {
	if active {
		return themePanelInspectorActive
	}
	switch field.kind {
	case inspectorFieldBool:
		if strings.EqualFold(field.value, "true") {
			return themePanelInspector + ansiGreen + ansiBold
		}
		return themePanelInspectorMuted
	case inspectorFieldChoice:
		return themePanelInspector + ansiBrightCyan
	case inspectorFieldCapabilityPicker:
		return themePanelInspector + ansiBrightCyan
	case inspectorFieldInterfacePicker:
		return themePanelInspector + ansiBrightCyan
	case inspectorFieldNICAdd, inspectorFieldDiskAdd:
		return themePanelInspector + ansiBrightCyan + ansiBold
	case inspectorFieldPower:
		if field.value == "stop" {
			return themePanelInspector + ansiOrange + ansiBold
		}
		return themePanelInspector + ansiGreen + ansiBold
	default:
		return themePanelInspector
	}
}

func drawInspectorSectionBar(g *grid, panel rect, y int, title string) {
	fillRow(g, panel.X, y, panel.W, themePanelInspectorSection)
	g.Text(panel.X+4, y, fit("▾ "+title, panel.W-8), themePanelInspectorSection)
}

func drawInspectorFooter(g *grid, node Node, state ViewState, panel rect) {
	y := panel.Y + panel.H - 1
	fillRow(g, panel.X, y, panel.W, themePanelInspectorHeader)
	hint := "TAB  edit properties"
	if state.InspectorEditing {
		hint = "Enter save · Esc cancel"
	} else if state.InspectorCapOpen {
		hint = "type search · Enter select · Esc"
		fields := inspectorFieldsForState(node, state)
		selected := normalizedMenuSelection(state.InspectorSelected, len(fields))
		if len(fields) > 0 && fields[selected].kind == inspectorFieldCapabilityPicker {
			hint = "type search · Space toggle · Esc"
		}
	} else if state.Focus == FocusInspector {
		hint = "↑↓ select · Enter edit · Tab back"
		fields := inspectorFieldsForState(node, state)
		selected := normalizedMenuSelection(state.InspectorSelected, len(fields))
		if len(fields) > 0 {
			field := fields[selected]
			switch field.kind {
			case inspectorFieldPower:
				hint = "Enter start/stop · Tab back"
			case inspectorFieldShellAction:
				hint = "Enter open Shell · Tab back"
			case inspectorFieldVNCAction:
				if state.VNCViewerActive[NodeKey(NodeVM, node.ID)] {
					hint = "Enter stop VNC · Tab back"
				} else {
					hint = "Enter open VNC · Tab back"
				}
			case inspectorFieldNICAdd:
				hint = "Enter add NIC · Tab back"
			case inspectorFieldNIC:
				hint = "Enter connect · X delete · Tab back"
			case inspectorFieldDiskAdd:
				hint = "Enter add disk · Tab back"
			case inspectorFieldDisk:
				hint = inspectorDiskFooterHint(field)
			case inspectorFieldMoveAction:
				hint = "Enter move · arrows position · Esc"
			case inspectorFieldDeleteAction:
				hint = "Enter delete · Tab back"
			}
		}
	}
	g.Text(panel.X+3, y, fit(hint, panel.W-6), themePanelInspectorHeader)
}

func inspectorDiskFooterHint(field inspectorField) string {
	if field.diskAction == diskMenuActionNone {
		if field.diskKind == "layer" {
			return "M merge · D detach · Tab back"
		}
		return "D detach · Tab back"
	}
	if field.diskKind == "base" {
		return "Enter attach · A layer · Tab back"
	}
	if field.diskKind == "layer" {
		return "Enter attach · M merge · Tab back"
	}
	return "Enter attach · Tab back"
}

func inspectorFieldDisplayValue(field inspectorField) string {
	switch field.kind {
	case inspectorFieldBool:
		if strings.EqualFold(field.value, "true") {
			return "[X]"
		}
		return "[ ]"
	case inspectorFieldChoice:
		return "[" + modeDisplayLabel(field.value) + "]"
	case inspectorFieldCapabilityPicker, inspectorFieldInterfacePicker:
		return "[" + field.value + " ▾]"
	case inspectorFieldNICAdd, inspectorFieldDiskAdd:
		return "[+]"
	case inspectorFieldPower:
		if field.value == "stop" {
			return "[Stop]"
		}
		return "[Run]"
	default:
		return field.value
	}
}

func drawInspectorHeader(g *grid, node Node, x, y, width int) {
	if width <= 0 {
		return
	}
	badge := "[" + firstNonEmpty(node.Badge, NodeKind(node.Type)) + "]"
	name := firstNonEmpty(node.Label, node.ID)
	if runeLen(badge) >= width {
		g.Text(x, y, fit(badge, width), themePanelInspectorHeader+nodeBadgeStyle(node.Type))
		return
	}
	g.Text(x, y, badge, themePanelInspectorHeader+nodeBadgeStyle(node.Type))
	nameWidth := width - runeLen(badge) - 1
	if nameWidth <= 0 {
		return
	}
	g.Text(x+runeLen(badge)+1, y, fit(name, nameWidth), themePanelInspectorHeader+nodeLabelStyle(node.Type))
}

type inspectorKV struct {
	Key   string
	Value string
	Style string
}

func drawInspectorSection(g *grid, x, y, width int, title string, rows []inspectorKV) int {
	if width <= 0 || len(rows) == 0 {
		return y
	}
	g.Text(x, y, fit(title, width), themePanelInspectorHeader)
	y++
	for _, row := range rows {
		if row.Key == "" && row.Value == "" {
			continue
		}
		key := fit(row.Key, min(10, max(4, width/3)))
		keyW := runeLen(key)
		g.Text(x, y, key, themePanelInspectorMuted)
		if width-keyW-1 > 0 {
			g.Text(x+keyW+1, y, fit(row.Value, width-keyW-1), row.Style)
		}
		y++
	}
	return y + 1
}

func inspectorLines(node Node) []string {
	switch node.Type {
	case NodeVM:
		return compactDetailLines(node, []string{"cpu", "mem", "vnc", "disk", "iso"}, 7)
	case NodeContainer:
		return compactDetailLines(node, []string{"image", "command", "disk"}, 7)
	case NodeSwitch:
		links := len(nicDetails(node.Details))
		configNode := node
		configNode.Details = switchConfigurationDetails(node.Details)
		lines := compactDetailLines(configNode, []string{"mode"}, 5)
		lines = append(lines, "links  "+strconv.Itoa(links))
		return lines
	case NodeExternal:
		return compactDetailLines(node, []string{"interface", "mode"}, 5)
	default:
		return compactDetailLines(node, nil, 7)
	}
}

func inspectorLineParts(line string) (string, string) {
	line = strings.TrimSpace(line)
	key, value, ok := strings.Cut(line, " ")
	if !ok {
		return line, ""
	}
	return strings.TrimSpace(key), strings.TrimSpace(value)
}

func compactDetailLines(node Node, keys []string, limit int) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, key := range keys {
		if value := nodeDetailRawValue(node, key); value != "" {
			out = append(out, detailLine(key, value))
			seen[key] = struct{}{}
		}
	}
	for _, detail := range node.Details {
		key, value, ok := strings.Cut(detail, "=")
		if !ok || value == "" {
			continue
		}
		if _, ok := seen[key]; ok || isRuntimeDetail(detail) {
			continue
		}
		out = append(out, detailLine(key, value))
		seen[key] = struct{}{}
		if len(out) >= limit {
			break
		}
	}
	if len(out) > limit {
		return out[:limit]
	}
	return out
}

func detailLine(key, value string) string {
	label := key
	switch key {
	case "cpu", "cpus":
		label = "cpu"
	case "mem", "memory":
		label = "mem"
	}
	if runeLen(label) < 6 {
		label += strings.Repeat(" ", 6-runeLen(label))
	}
	return label + " " + shortDetailValue(key, value)
}

func inspectorLineStyle(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "links") {
		return themePanelInspectorMuted
	}
	return themePanelInspector
}

func drawPanelBox(g *grid, r rect, style string) {
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
