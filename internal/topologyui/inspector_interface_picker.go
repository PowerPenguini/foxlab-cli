package topologyui

import (
	"sort"
	"strings"
)

type inspectorInterfacePickerLayout struct {
	rect       rect
	optionsY   int
	optionRows int
	start      int
}

func inspectorInterfaceOptions(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	seen := map[string]bool{}
	options := make([]string, 0)
	for _, name := range hostInterfaceNames() {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || (query != "" && !strings.Contains(strings.ToLower(name), query)) {
			continue
		}
		seen[name] = true
		options = append(options, name)
	}
	sort.Slice(options, func(i, j int) bool {
		return strings.ToLower(options[i]) < strings.ToLower(options[j])
	})
	return options
}

func inspectorInterfaceFieldIndex(fields []inspectorField) (int, bool) {
	for index, field := range fields {
		if field.kind == inspectorFieldInterfacePicker {
			return index, true
		}
	}
	return 0, false
}

func inspectorInterfaceLayout(panel rect, state ViewState, fields []inspectorField, optionCount int) (inspectorInterfacePickerLayout, bool) {
	fieldIndex, ok := inspectorInterfaceFieldIndex(fields)
	if !ok {
		return inspectorInterfacePickerLayout{}, false
	}
	fieldY, ok := inspectorFieldY(panel, state, fields, fieldIndex)
	if !ok {
		return inspectorInterfacePickerLayout{}, false
	}
	y := fieldY + 1
	bottom := panel.Y + panel.H - inspectorFooterRows
	height := bottom - y
	if height < 2 {
		return inspectorInterfacePickerLayout{}, false
	}
	optionRows := min(inspectorCapabilityPickerMaxRows, height-1)
	selected := normalizedMenuSelection(state.InspectorCapSelected, optionCount)
	return inspectorInterfacePickerLayout{
		rect:       rect{X: panel.X + 2, Y: y, W: panel.W - 4, H: optionRows + 1},
		optionsY:   y + 1,
		optionRows: optionRows,
		start:      contextMenuStart(selected, optionCount, optionRows),
	}, true
}

func drawInspectorInterfacePicker(g *grid, node Node, state ViewState, panel rect, fields []inspectorField) {
	options := inspectorInterfaceOptions(state.InspectorCapQuery)
	layout, ok := inspectorInterfaceLayout(panel, state, fields, len(options))
	if !ok {
		return
	}
	fillRect(g, layout.rect, themeMenuRow)
	fillRow(g, layout.rect.X, layout.rect.Y, layout.rect.W, themePaletteInput)
	query := state.InspectorCapQuery
	queryStyle := themePaletteInput
	if query == "" {
		query = "search interfaces…"
		queryStyle = themePaletteInputHint
	} else {
		query = contextEditText(query, runeLen(query))
	}
	g.Text(layout.rect.X+1, layout.rect.Y, fit("⌕ "+query, layout.rect.W-2), queryStyle)
	if len(options) == 0 {
		g.Text(layout.rect.X+1, layout.optionsY, fit("no interfaces", layout.rect.W-2), themeMenuMuted)
		return
	}
	current := nodeDetailRawValue(node, "interface")
	selected := normalizedMenuSelection(state.InspectorCapSelected, len(options))
	for row := 0; row < layout.optionRows && layout.start+row < len(options); row++ {
		index := layout.start + row
		style := themeMenuRow
		if index == selected {
			style = themeMenuActive
		}
		fillRow(g, layout.rect.X, layout.optionsY+row, layout.rect.W, style)
		marker := "[ ]"
		if options[index] == current {
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

func (a *App) openInspectorInterfacePicker() {
	a.State.InspectorCapOpen = true
	a.State.InspectorCapQuery = ""
	a.State.InspectorCapSelected = 0
}

func (a *App) handleInspectorInterfaceKey(key string) bool {
	fields := a.selectedInspectorFields()
	fieldIndex, ok := inspectorInterfaceFieldIndex(fields)
	if !ok {
		a.closeInspectorCapabilityPicker()
		return false
	}
	options := inspectorInterfaceOptions(a.State.InspectorCapQuery)
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
			a.applyInspectorField(fields[fieldIndex], options[a.State.InspectorCapSelected])
			a.closeInspectorCapabilityPicker()
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

func (a *App) handleInspectorInterfaceMouse(event mouseEvent, panel rect, _ Node, fields []inspectorField) bool {
	fieldIndex, ok := inspectorInterfaceFieldIndex(fields)
	if !ok {
		return false
	}
	options := inspectorInterfaceOptions(a.State.InspectorCapQuery)
	layout, ok := inspectorInterfaceLayout(panel, a.State, fields, len(options))
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
	if event.button != 0 || event.y == layout.rect.Y {
		return true
	}
	row := event.y - layout.optionsY
	index := layout.start + row
	if row < 0 || row >= layout.optionRows || index < 0 || index >= len(options) {
		return true
	}
	a.State.InspectorCapSelected = index
	a.applyInspectorField(fields[fieldIndex], options[index])
	a.closeInspectorCapabilityPicker()
	return true
}
