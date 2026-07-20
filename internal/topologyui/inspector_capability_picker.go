package topologyui

import "strings"

const inspectorCapabilityPickerMaxRows = 7

type inspectorCapabilityPickerLayout struct {
	rect       rect
	optionsY   int
	optionRows int
	start      int
}

func inspectorCapabilityOptions(node Node, query string) []string {
	query = strings.ToUpper(strings.TrimSpace(query))
	options := make([]string, 0, len(orderedContainerCapabilities()))
	for _, capability := range orderedContainerCapabilities() {
		if query == "" || strings.Contains(capability, query) {
			options = append(options, capability)
		}
	}
	return options
}

func inspectorCapabilityFieldIndex(fields []inspectorField) (int, bool) {
	for index, field := range fields {
		if field.kind == inspectorFieldCapabilityPicker {
			return index, true
		}
	}
	return 0, false
}

func inspectorCapabilityLayout(panel rect, state ViewState, fields []inspectorField, optionCount int) (inspectorCapabilityPickerLayout, bool) {
	fieldIndex, ok := inspectorCapabilityFieldIndex(fields)
	if !ok {
		return inspectorCapabilityPickerLayout{}, false
	}
	fieldY, ok := inspectorFieldY(panel, state, fields, fieldIndex)
	if !ok {
		return inspectorCapabilityPickerLayout{}, false
	}
	y := fieldY + 1
	bottom := panel.Y + panel.H - inspectorFooterRows
	height := bottom - y
	if height < 2 {
		return inspectorCapabilityPickerLayout{}, false
	}
	optionRows := min(inspectorCapabilityPickerMaxRows, height-1)
	height = optionRows + 1
	selected := normalizedMenuSelection(state.InspectorCapSelected, optionCount)
	return inspectorCapabilityPickerLayout{
		rect:       rect{X: panel.X + 2, Y: y, W: panel.W - 4, H: height},
		optionsY:   y + 1,
		optionRows: optionRows,
		start:      contextMenuStart(selected, optionCount, optionRows),
	}, true
}

func drawInspectorCapabilityPicker(g *grid, node Node, state ViewState, panel rect, fields []inspectorField) {
	options := inspectorCapabilityOptions(node, state.InspectorCapQuery)
	layout, ok := inspectorCapabilityLayout(panel, state, fields, len(options))
	if !ok {
		return
	}
	fillRect(g, layout.rect, themeMenuRow)
	fillRow(g, layout.rect.X, layout.rect.Y, layout.rect.W, themePaletteInput)
	query := state.InspectorCapQuery
	queryStyle := themePaletteInput
	if query == "" {
		query = "search capabilities…"
		queryStyle = themePaletteInputHint
	} else {
		query = contextEditText(query, runeLen(query))
	}
	g.Text(layout.rect.X+1, layout.rect.Y, fit("⌕ "+query, layout.rect.W-2), queryStyle)
	if len(options) == 0 {
		g.Text(layout.rect.X+1, layout.optionsY, fit("no matches", layout.rect.W-2), themeMenuMuted)
		return
	}
	enabled := containerCapabilityEnabledMap(node)
	selected := normalizedMenuSelection(state.InspectorCapSelected, len(options))
	for row := 0; row < layout.optionRows && layout.start+row < len(options); row++ {
		index := layout.start + row
		style := themeMenuRow
		if index == selected {
			style = themeMenuActive
		}
		fillRow(g, layout.rect.X, layout.optionsY+row, layout.rect.W, style)
		marker := "[ ]"
		if enabled[options[index]] {
			marker = "[X]"
		}
		g.Text(layout.rect.X+1, layout.optionsY+row, fit(marker+" "+options[index], layout.rect.W-2), style)
	}
	if layout.start > 0 {
		g.Set(layout.rect.X+layout.rect.W-2, layout.optionsY, '↑', themeMenuMuted)
	}
	if layout.start+layout.optionRows < len(options) {
		g.Set(layout.rect.X+layout.rect.W-2, layout.optionsY+layout.optionRows-1, '↓', themeMenuMuted)
	}
}

func (a *App) openInspectorCapabilityPicker() {
	a.State.InspectorCapOpen = true
	a.State.InspectorCapQuery = ""
	a.State.InspectorCapSelected = 0
}

func (a *App) closeInspectorCapabilityPicker() {
	a.State.InspectorCapOpen = false
	a.State.InspectorCapQuery = ""
	a.State.InspectorCapSelected = 0
}

func (a *App) handleInspectorCapabilityKey(key string) bool {
	node, ok := selectedNode(a.Model, a.State.Selected)
	if !ok || node.Type != NodeContainer {
		a.closeInspectorCapabilityPicker()
		return false
	}
	options := inspectorCapabilityOptions(node, a.State.InspectorCapQuery)
	a.State.InspectorCapSelected = normalizedMenuSelection(a.State.InspectorCapSelected, len(options))
	switch key {
	case "up":
		a.State.InspectorCapSelected = MoveContextSelection(a.State.InspectorCapSelected, len(options), "up")
	case "down":
		a.State.InspectorCapSelected = MoveContextSelection(a.State.InspectorCapSelected, len(options), "down")
	case "pageup", "shift-pageup":
		a.State.InspectorCapSelected = clamp(a.State.InspectorCapSelected-inspectorCapabilityPickerMaxRows, 0, max(0, len(options)-1))
	case "pagedown", "shift-pagedown":
		a.State.InspectorCapSelected = clamp(a.State.InspectorCapSelected+inspectorCapabilityPickerMaxRows, 0, max(0, len(options)-1))
	case "enter", "space":
		if len(options) > 0 {
			capability := options[a.State.InspectorCapSelected]
			enabled := containerCapabilityEnabledMap(node)[capability]
			a.containerCapabilitySet(node.ID, capability, !enabled)
		}
	case "backspace":
		runes := []rune(a.State.InspectorCapQuery)
		if len(runes) > 0 {
			a.State.InspectorCapQuery = string(runes[:len(runes)-1])
			a.State.InspectorCapSelected = 0
		}
	case "delete":
		a.State.InspectorCapQuery = ""
		a.State.InspectorCapSelected = 0
	case "escape", "left":
		a.closeInspectorCapabilityPicker()
	case "quit":
		return true
	default:
		if strings.HasPrefix(key, "char:") {
			a.State.InspectorCapQuery += strings.TrimPrefix(key, "char:")
			a.State.InspectorCapSelected = 0
		}
	}
	return false
}

func (a *App) handleInspectorCapabilityMouse(event mouseEvent, panel rect, node Node, fields []inspectorField) bool {
	options := inspectorCapabilityOptions(node, a.State.InspectorCapQuery)
	layout, ok := inspectorCapabilityLayout(panel, a.State, fields, len(options))
	if !ok || !xyInRect(event.x, event.y, layout.rect) {
		return false
	}
	if event.button == 64 || event.button == 65 {
		direction := "down"
		if event.button == 64 {
			direction = "up"
		}
		a.State.InspectorCapSelected = MoveContextSelection(a.State.InspectorCapSelected, len(options), direction)
		return true
	}
	if event.button != 0 {
		return true
	}
	if event.y == layout.rect.Y {
		return true
	}
	row := event.y - layout.optionsY
	index := layout.start + row
	if row < 0 || row >= layout.optionRows || index < 0 || index >= len(options) {
		return true
	}
	a.State.InspectorCapSelected = index
	capability := options[index]
	enabled := containerCapabilityEnabledMap(node)[capability]
	a.containerCapabilitySet(node.ID, capability, !enabled)
	return true
}
