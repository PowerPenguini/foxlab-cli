package topologyui

type menuItemKind string

const (
	menuItemAction menuItemKind = "action"
	menuItemGroup  menuItemKind = "group"
	menuItemInfo   menuItemKind = "info"
)

type MenuItem struct {
	ID      string
	Label   string
	Action  string
	Kind    menuItemKind
	Enabled bool
	RowKind string
}

type menuColumnLayout struct {
	rect     rect
	start    int
	selected int
	items    []MenuItem
}

type menuLayout struct {
	root      menuColumnLayout
	sub       menuColumnLayout
	hasSub    bool
	selectBox menuColumnLayout
	hasSelect bool
}

func menuItemsFromLabels(labels []string) []MenuItem {
	items := make([]MenuItem, 0, len(labels))
	for _, label := range labels {
		action := contextMenuAction(label)
		kind := menuItemAction
		if isContextGroup(action) {
			kind = menuItemGroup
		}
		if isContextInfoItem(label) {
			kind = menuItemInfo
		}
		items = append(items, MenuItem{
			ID:      action,
			Label:   label,
			Action:  action,
			Kind:    kind,
			Enabled: kind != menuItemInfo,
		})
	}
	return items
}

func menuItemsWithMeta(labels, actions, kinds []string) []MenuItem {
	items := menuItemsFromLabels(labels)
	for i := range items {
		if i < len(actions) && actions[i] != "" {
			items[i].Action = actions[i]
			items[i].ID = actions[i]
		}
		if i < len(kinds) {
			items[i].RowKind = kinds[i]
		}
	}
	return items
}

func menuItemLabels(items []MenuItem) []string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func menuItemActions(items []MenuItem) []string {
	actions := make([]string, 0, len(items))
	for _, item := range items {
		actions = append(actions, item.Action)
	}
	return actions
}

func menuItemKinds(items []MenuItem) []string {
	kinds := make([]string, 0, len(items))
	for _, item := range items {
		kinds = append(kinds, item.RowKind)
	}
	return kinds
}

func menuItemEnabled(items []MenuItem) []bool {
	enabled := make([]bool, 0, len(items))
	for _, item := range items {
		enabled = append(enabled, item.Enabled)
	}
	return enabled
}

func layoutFloatingMenu(bounds, anchor rect, items []MenuItem, selected int) (menuColumnLayout, bool) {
	if len(items) == 0 {
		return menuColumnLayout{}, false
	}
	labels := menuItemLabels(items)
	menuH := min(len(items), max(1, bounds.H))
	active := normalizedMenuSelection(selected, len(items))
	start := contextMenuStart(active, len(items), menuH)
	menuW := contextMenuWidthWithKinds(labels, menuItemKinds(items))
	x := anchor.X + anchor.W + 1
	if x+menuW > bounds.X+bounds.W {
		x = anchor.X - menuW - 1
	}
	x = clamp(x, bounds.X, bounds.X+bounds.W-menuW)
	y := anchor.Y
	if y+menuH > bounds.Y+bounds.H {
		y = bounds.Y + bounds.H - menuH
	}
	y = clamp(y, bounds.Y, bounds.Y+bounds.H-menuH)
	return menuColumnLayout{
		rect:     rect{X: x, Y: y, W: menuW, H: menuH},
		start:    start,
		selected: active,
		items:    items,
	}, true
}

func layoutSubmenu(bounds rect, root menuColumnLayout, items []MenuItem, selected int, editWidth int) (menuColumnLayout, bool) {
	if len(items) == 0 {
		return menuColumnLayout{}, false
	}
	labels := menuItemLabels(items)
	menuH := min(len(items), max(1, bounds.H))
	active := normalizedMenuSelection(selected, len(items))
	start := contextMenuStart(active, len(items), menuH)
	menuW := contextMenuWidthWithKinds(labels, menuItemKinds(items))
	if editWidth > 0 {
		menuW = max(menuW, editWidth)
	}
	x := root.rect.X + root.rect.W
	if x+menuW > bounds.X+bounds.W {
		x = root.rect.X - menuW
	}
	x = clamp(x, bounds.X, bounds.X+bounds.W-menuW)
	y := root.rect.Y + (root.selected - root.start)
	if y < bounds.Y {
		y = bounds.Y
	}
	if y+menuH > bounds.Y+bounds.H {
		y = bounds.Y + bounds.H - menuH
	}
	y = clamp(y, bounds.Y, bounds.Y+bounds.H-menuH)
	return menuColumnLayout{
		rect:     rect{X: x, Y: y, W: menuW, H: menuH},
		start:    start,
		selected: active,
		items:    items,
	}, true
}

func layoutSelectMenu(bounds rect, anchor menuColumnLayout, items []MenuItem, selected int) (menuColumnLayout, bool) {
	if len(items) == 0 {
		return menuColumnLayout{}, false
	}
	labels := menuItemLabels(items)
	menuH := min(len(items), max(1, bounds.H))
	active := normalizedMenuSelection(selected, len(items))
	start := contextMenuStart(active, len(items), menuH)
	menuW := contextMenuWidthWithKinds(labels, menuItemKinds(items))
	x := anchor.rect.X + anchor.rect.W
	if x+menuW > bounds.X+bounds.W {
		x = anchor.rect.X - menuW
	}
	x = clamp(x, bounds.X, bounds.X+bounds.W-menuW)
	y := anchor.rect.Y + (anchor.selected - anchor.start)
	if y < bounds.Y {
		y = bounds.Y
	}
	if y+menuH > bounds.Y+bounds.H {
		y = bounds.Y + bounds.H - menuH
	}
	y = clamp(y, bounds.Y, bounds.Y+bounds.H-menuH)
	return menuColumnLayout{
		rect:     rect{X: x, Y: y, W: menuW, H: menuH},
		start:    start,
		selected: active,
		items:    items,
	}, true
}

func layoutDropdownMenu(bounds, anchor rect, items []MenuItem, selected int) (menuColumnLayout, bool) {
	if len(items) == 0 {
		return menuColumnLayout{}, false
	}
	labels := menuItemLabels(items)
	menuY := anchor.Y + anchor.H
	availableH := max(1, bounds.Y+bounds.H-menuY)
	menuH := min(len(items), availableH)
	active := normalizedMenuSelection(selected, len(items))
	start := contextMenuStart(active, len(items), menuH)
	menuW := contextMenuWidthWithKinds(labels, menuItemKinds(items))
	menuX := anchor.X
	if menuX+menuW > bounds.X+bounds.W {
		menuX = bounds.X + bounds.W - menuW
	}
	menuX = clamp(menuX, bounds.X, bounds.X+bounds.W-menuW)
	return menuColumnLayout{
		rect:     rect{X: menuX, Y: menuY, W: menuW, H: menuH},
		start:    start,
		selected: active,
		items:    items,
	}, true
}

func menuRowAt(column menuColumnLayout, x, y int) (int, bool) {
	if !xyInRect(x, y, column.rect) {
		return 0, false
	}
	row := column.start + y - column.rect.Y
	if row < 0 || row >= len(column.items) {
		return 0, false
	}
	return row, true
}

func contextMenuLayoutFor(m Model, state ViewState, nodeRects map[string]rect, bounds rect) (menuLayout, Node, bool, bool) {
	nodeRect := rect{X: bounds.X + 2, Y: bounds.Y + 1, W: 0, H: 0}
	hasNode := false
	node := Node{}
	rootLabels := globalContextMenuItems("")
	if len(m.Nodes) > 0 {
		selected := normalizedSelected(m, state.Selected)
		node = m.Nodes[selected]
		hasNode = true
		if r, ok := nodeRects[node.Key()]; ok {
			nodeRect = r
			rootLabels = contextMenuItems(node, "")
		}
	}
	rootItems := menuItemsFromLabels(rootLabels)
	if hasNode {
		applyContextMenuItemState(m, node, rootItems)
	}
	root, ok := layoutFloatingMenu(bounds, nodeRect, rootItems, state.ContextSelected)
	if !ok {
		return menuLayout{}, node, hasNode, false
	}
	layout := menuLayout{root: root}
	rootContextGroup := activeRootContextGroup(rootLabels, root.selected)
	if rootContextGroup == "" || !state.ContextInSubmenu {
		return layout, node, hasNode, true
	}
	contextGroup := state.ContextGroup
	if !contextGroupBelongsToRoot(rootContextGroup, contextGroup) {
		return layout, node, hasNode, true
	}
	subLabels := contextMenuSubmenuItems(node, hasNode, contextGroup)
	subActions := []string(nil)
	subKinds := []string(nil)
	if contextGroup == "disk-menu" {
		subLabels = state.DiskMenuItems
		subActions = state.DiskMenuActions
		subKinds = state.DiskMenuKinds
	}
	if contextGroup == "uplink-menu" && node.Type == NodeSwitch {
		subLabels = switchUplinkMenuItems(node)
		subKinds = switchUplinkMenuKinds(subLabels)
	}
	editWidth := contextMenuEditWidth(state, contextGroup, subLabels, subKinds)
	subItems := menuItemsWithMeta(subLabels, subActions, subKinds)
	if contextGroup == "uplink-menu" && node.Type == NodeSwitch {
		for i := range subItems {
			subItems[i].Enabled = switchUplinkMenuItemEnabled(m, subItems[i].Label)
		}
	}
	sub, ok := layoutSubmenu(bounds, root, subItems, state.ContextSubSelected, editWidth)
	if !ok {
		return layout, node, hasNode, true
	}
	layout.sub = sub
	layout.hasSub = true
	if state.ContextSelectGroup == "" || !contextSelectGroupBelongsToSub(node, subLabels, sub.selected, state.ContextSelectGroup) {
		return layout, node, hasNode, true
	}
	selectLabels := contextMenuItems(node, state.ContextSelectGroup)
	selectBox, ok := layoutSelectMenu(bounds, sub, menuItemsFromLabels(selectLabels), state.ContextSelectSelected)
	if !ok {
		return layout, node, hasNode, true
	}
	layout.selectBox = selectBox
	layout.hasSelect = true
	return layout, node, hasNode, true
}

func applyContextMenuItemState(m Model, node Node, items []MenuItem) {
	if node.Type != NodeExternal || !externalConnectedInModel(m, node.ID) {
		return
	}
	for i := range items {
		if items[i].Action == "connect" {
			items[i].Enabled = false
		}
	}
}

func contextSelectGroupBelongsToSub(node Node, subItems []string, selected int, selectGroup string) bool {
	if len(subItems) == 0 {
		return false
	}
	item := subItems[normalizedMenuSelection(selected, len(subItems))]
	switch selectGroup {
	case "interface-menu":
		return isExternalInterfaceField(node, item)
	case "mode-menu":
		return isExternalModeField(node, item)
	default:
		return false
	}
}

func contextMenuEditWidth(state ViewState, contextGroup string, subItems, subKinds []string) int {
	if !state.ContextEdit || len(subItems) == 0 {
		return 0
	}
	active := normalizedMenuSelection(state.ContextSubSelected, len(subItems))
	editLabel := contextEditLabel(subItems[active], state.ContextEditValue, state.ContextEditCursor)
	if contextGroup == "disk-menu" && state.ContextAddDiskLayer && active < len(subKinds) && subKinds[active] == "base" {
		editLabel = contextDiskLayerEditLabel(subItems[active], state.ContextEditValue, state.ContextEditCursor)
	}
	return runeLen(editLabel) + 3
}
