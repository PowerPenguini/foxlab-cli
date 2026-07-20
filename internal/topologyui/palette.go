package topologyui

import "strings"

type paletteAction struct {
	Label          string
	Hint           string
	Query          string
	Action         string
	Node           Node
	HasNode        bool
	Enabled        bool
	DisabledReason string
	CompleteOnly   bool
}

func (a *App) openPalette() {
	a.State.openOverlay(overlayPalette)
	a.State.PaletteSelected = 0
}

func (a *App) closePalette() {
	a.State.PaletteOpen = false
	a.State.PaletteQuery = ""
	a.State.PaletteSelected = 0
}

func (a *App) togglePalette() {
	if a.State.PaletteOpen {
		a.closePalette()
		return
	}
	a.openPalette()
}

func (a *App) handlePaletteKey(key string) bool {
	switch key {
	case "quit":
		return true
	case "ctrl+p", "escape":
		a.closePalette()
	case "tab":
		a.completeSelectedPaletteAction()
	case "enter":
		return a.runSelectedPaletteAction()
	case "up":
		a.State.PaletteSelected = MoveContextSelection(a.State.PaletteSelected, len(filteredPaletteActions(a.Model, a.State)), key)
	case "down":
		a.State.PaletteSelected = MoveContextSelection(a.State.PaletteSelected, len(filteredPaletteActions(a.Model, a.State)), key)
	case "backspace":
		if a.State.PaletteQuery != "" {
			runes := []rune(a.State.PaletteQuery)
			a.State.PaletteQuery = string(runes[:len(runes)-1])
			a.State.PaletteSelected = 0
		}
	default:
		if value, ok := strings.CutPrefix(key, "char:"); ok {
			a.State.PaletteQuery += value
			a.State.PaletteSelected = 0
		}
	}
	return false
}

func (a *App) runSelectedPaletteAction() bool {
	query := normalizedPaletteQuery(a.State.PaletteQuery)
	if strings.HasPrefix(query, "tabnext") || strings.HasPrefix(query, "tabprev") || strings.HasPrefix(query, "tabclose") || strings.HasPrefix(query, "tabrestart") {
		a.closePalette()
		a.executeCommand(query)
		return false
	}
	if action, ok := executablePaletteCommand(query, a.State); ok {
		return a.runPaletteAction(action)
	}
	action, ok := a.selectedPaletteAction()
	if ok && action.Action != "" {
		return a.runPaletteAction(action)
	}
	if ok && action.Query != "" && action.Query != query {
		a.State.PaletteQuery = action.Query
		a.State.PaletteSelected = 0
		return false
	}
	if !ok {
		return false
	}
	return a.runPaletteAction(action)
}

func (a *App) completeSelectedPaletteAction() {
	query := normalizedPaletteQuery(a.State.PaletteQuery)
	action, ok := a.selectedPaletteAction()
	if !ok || action.Query == "" || action.Query == query {
		return
	}
	a.State.PaletteQuery = action.Query
	a.State.PaletteSelected = 0
}

func (a *App) selectedPaletteAction() (paletteAction, bool) {
	actions := filteredPaletteActions(a.Model, a.State)
	if len(actions) == 0 {
		return paletteAction{}, false
	}
	return actions[normalizedMenuSelection(a.State.PaletteSelected, len(actions))], true
}

func (a *App) runPaletteAction(action paletteAction) bool {
	if !action.Enabled {
		if action.DisabledReason != "" {
			a.State.Message = action.DisabledReason
		}
		return false
	}
	a.closePalette()
	switch action.Action {
	case "exit":
		return true
	case "tab-close-active":
		a.ensureTabs()
		return a.closeActiveTab()
	case "apply-lab":
		a.applyOpenLab()
	case "disk-explorer":
		a.openDiskExplorer()
	default:
		if action.HasNode {
			if isContextGroup(action.Action) {
				a.openPaletteContextGroup(action.Node, action.Action)
				return false
			}
			a.runMenuAction(action.Node, action.Action)
			return false
		}
		a.runGlobalMenuAction(action.Action)
	}
	return false
}

func (a *App) openPaletteContextGroup(node Node, group string) {
	a.State.openOverlay(overlayContextMenu)
	a.State.ContextGroup = group
	a.State.ContextInSubmenu = true
	a.State.ContextSubSelected = 0
	a.State.ContextSelectGroup = ""
	a.State.ContextSelectSelected = 0
	rootItems := contextMenuItems(node, "")
	for i, item := range rootItems {
		if contextMenuAction(item) == group {
			a.State.ContextSelected = i
			return
		}
	}
	a.State.ContextSelected = 0
}

func (a *App) handlePaletteMouse(event mouseEvent) bool {
	layout, ok := paletteLayout(a.ViewWidth, a.paletteViewportHeight())
	if !ok || !xyInRect(event.x, event.y, layout) {
		a.closePalette()
		return false
	}
	actions := filteredPaletteActions(a.Model, a.State)
	start := paletteStart(a.State, len(actions), layout)
	if index, ok := paletteRowAt(layout, event.x, event.y, start, len(actions)); ok {
		a.State.PaletteSelected = index
		return a.runSelectedPaletteAction()
	}
	return false
}

func (a *App) paletteFeedbackRect(event mouseEvent) (rect, bool) {
	layout, ok := paletteLayout(a.ViewWidth, a.paletteViewportHeight())
	if !ok || !xyInRect(event.x, event.y, layout) {
		return rect{}, false
	}
	actions := filteredPaletteActions(a.Model, a.State)
	start := paletteStart(a.State, len(actions), layout)
	if _, ok := paletteRowAt(layout, event.x, event.y, start, len(actions)); ok {
		return rect{X: layout.X + 1, Y: event.y, W: layout.W - 2, H: 1}, true
	}
	return rect{}, false
}

func (a *App) paletteViewportHeight() int {
	if a.shellPaletteOpen() {
		return a.ViewHeight
	}
	return a.contentHeight()
}

func paletteActions(_ Model, state ViewState) []paletteAction {
	return topLevelPaletteActions(state)
}

func topLevelPaletteActions(state ViewState) []paletteAction {
	return []paletteAction{
		{Label: "add", Hint: "create", Query: "add", Enabled: true, CompleteOnly: true},
		{Label: "apply", Hint: "lab", Query: "apply", Action: "apply-lab", Enabled: !state.ApplyLabDisabled, DisabledReason: "lab already applied"},
		{Label: "disk", Hint: "explorer", Query: "disk", Action: "disk-explorer", Enabled: true},
		{Label: "quit", Hint: "card", Query: "quit", Action: "tab-close-active", Enabled: true},
	}
}

func filteredPaletteActions(m Model, state ViewState) []paletteAction {
	query := normalizedPaletteQuery(state.PaletteQuery)
	if query == "" {
		return paletteActions(m, state)
	}
	if query == "qa" {
		return []paletteAction{{Label: "quit all", Query: "quit all", Action: "exit", Enabled: true}}
	}
	if strings.HasPrefix(query, "add") {
		if actions := filteredAddPaletteActions(query); actions != nil {
			return actions
		}
	}
	actions := paletteActions(m, state)
	out := make([]paletteAction, 0, len(actions))
	for _, action := range actions {
		if strings.HasPrefix(action.Query, query) || strings.HasPrefix(strings.ToLower(action.Label), query) {
			out = append(out, action)
		}
	}
	return out
}

func filteredAddPaletteActions(query string) []paletteAction {
	fields := strings.Fields(query)
	if len(fields) == 0 {
		return nil
	}
	if fields[0] != "add" && !strings.HasPrefix("add", fields[0]) {
		return nil
	}
	if len(fields) > 2 {
		return nil
	}
	if len(fields) == 1 && fields[0] != "add" {
		return []paletteAction{{Label: "add", Hint: "create", Query: "add", Enabled: true, CompleteOnly: true}}
	}
	prefix := ""
	if len(fields) == 2 {
		prefix = fields[1]
	}
	all := []paletteAction{
		{Label: "vm", Hint: "add", Query: "add vm", Action: "add vm", Enabled: true},
		{Label: "sw", Hint: "add", Query: "add sw", Action: "add sw", Enabled: true},
		{Label: "ct", Hint: "add", Query: "add ct", Action: "add cont", Enabled: true},
		{Label: "disk", Hint: "add", Query: "add disk", Action: "add disk", Enabled: true},
		{Label: "uplink", Hint: "add", Query: "add uplink", Action: "add uplink", Enabled: true},
	}
	if prefix == "" {
		return all
	}
	out := make([]paletteAction, 0, len(all))
	for _, action := range all {
		if strings.HasPrefix(action.Label, prefix) {
			out = append(out, action)
		}
	}
	return out
}

func executablePaletteCommand(query string, state ViewState) (paletteAction, bool) {
	switch query {
	case "q", "quit":
		return paletteAction{Label: "q", Query: query, Action: "tab-close-active", Enabled: true}, true
	case "qa", "quit all", "exit":
		return paletteAction{Label: "quit all", Query: query, Action: "exit", Enabled: true}, true
	case "apply":
		return paletteAction{Label: "apply", Query: query, Action: "apply-lab", Enabled: !state.ApplyLabDisabled, DisabledReason: "lab already applied"}, true
	case "disk", "disks":
		return paletteAction{Label: "disk", Query: query, Action: "disk-explorer", Enabled: true}, true
	case "add vm":
		return paletteAction{Label: "vm", Query: query, Action: "add vm", Enabled: true}, true
	case "add sw", "add switch":
		return paletteAction{Label: "sw", Query: query, Action: "add sw", Enabled: true}, true
	case "add ct", "add cont", "add container":
		return paletteAction{Label: "ct", Query: query, Action: "add cont", Enabled: true}, true
	case "add disk":
		return paletteAction{Label: "disk", Query: query, Action: "add disk", Enabled: true}, true
	case "add uplink", "add up":
		return paletteAction{Label: "uplink", Query: query, Action: "add uplink", Enabled: true}, true
	default:
		return paletteAction{}, false
	}
}

func normalizedPaletteQuery(query string) string {
	return strings.ToLower(strings.Join(strings.Fields(query), " "))
}

const paletteInputPaddingX = 1
const paletteInputPaddingY = 1
const paletteRecordPaddingX = 1

func paletteLayout(width, height int) (rect, bool) {
	if width < minWidth || height < minHeight {
		return rect{}, false
	}
	w := min(width-6, max(44, min(72, width*2/3)))
	h := min(height-4, 12)
	if w < 36 || h < 7 {
		return rect{}, false
	}
	return rect{X: (width - w) / 2, Y: (height - h) / 2, W: w, H: h}, true
}

func paletteInputRect(layout rect) rect {
	return rect{X: layout.X, Y: layout.Y, W: layout.W, H: 3}
}

func paletteRowsY(layout rect) int {
	input := paletteInputRect(layout)
	return input.Y + input.H
}

func paletteEmptyY(layout rect) int {
	return paletteRowsY(layout) + 1
}

func paletteVisibleRows(layout rect) int {
	return max(0, layout.H-paletteInputRect(layout).H)
}

func paletteStart(state ViewState, count int, layout rect) int {
	return contextMenuStart(normalizedMenuSelection(state.PaletteSelected, count), count, paletteVisibleRows(layout))
}

func paletteRowAt(layout rect, x, y, start, count int) (int, bool) {
	if x < layout.X || x >= layout.X+layout.W {
		return 0, false
	}
	firstY := paletteRowsY(layout)
	lastY := firstY + min(max(0, count-start), paletteVisibleRows(layout))
	if y < firstY || y >= lastY {
		return 0, false
	}
	return start + y - firstY, true
}
