package topologyui

import (
	"strconv"
	"strings"
)

type mouseEvent struct {
	x      int
	y      int
	button int
	kind   string
}

const (
	mousePress   = "press"
	mouseDrag    = "drag"
	mouseRelease = "release"
)

func parseMouseEvent(key string) (mouseEvent, bool) {
	parts := strings.Split(key, ":")
	if len(parts) != 4 {
		return mouseEvent{}, false
	}
	kind := mousePress
	switch parts[0] {
	case "mouse":
	case "mouse-drag":
		kind = mouseDrag
	case "mouse-release":
		kind = mouseRelease
	default:
		return mouseEvent{}, false
	}
	x, errX := strconv.Atoi(parts[1])
	y, errY := strconv.Atoi(parts[2])
	button, errB := strconv.Atoi(parts[3])
	if errX != nil || errY != nil || errB != nil {
		return mouseEvent{}, false
	}
	return mouseEvent{x: x, y: y, button: button, kind: kind}, true
}

func (a *App) handleMouseKey(key string) bool {
	event, ok := parseMouseEvent(key)
	if !ok {
		return false
	}
	if a.tabs != nil && event.y > 0 && !a.shellPaletteOpen() {
		event.y -= tabBarHeight
	}
	if a.ViewWidth <= 0 {
		a.ViewWidth = 100
	}
	if a.ViewHeight <= 0 {
		a.ViewHeight = 30
	}
	switch event.kind {
	case mouseDrag:
		a.handleMouseDrag(event)
		return false
	case mouseRelease:
		a.handleMouseRelease(event)
		return false
	}
	if event.button != 0 {
		if a.State.DiskExplorerOpen && (event.button == 64 || event.button == 65) && a.handleDiskImportBrowserScroll(event) {
			return false
		}
		if a.State.InspectorCapOpen && (event.button == 64 || event.button == 65) {
			panel := inspectorBounds(a.ViewWidth, a.contentHeight())
			if xyInRect(event.x, event.y, panel) {
				node, nodeOK := selectedNode(a.Model, a.State.Selected)
				if nodeOK && a.handleInspectorPickerMouse(event, panel, node, a.inspectorFields(node)) {
					return false
				}
			}
		}
		return false
	}
	if notification, ok := notificationBoundsForState(a.State, a.ViewWidth, a.contentHeight()); ok && xyInRect(event.x, event.y, notification) {
		a.clearMouseDrag()
		a.clearMousePan()
		a.dismissNotification()
		return false
	}
	a.clearMouseDrag()
	a.clearMousePan()
	if a.State.PaletteOpen {
		return a.handlePaletteMouse(event)
	}
	if a.State.DiskExplorerOpen {
		return a.handleDiskExplorerMouse(event)
	}
	if a.State.ConnectTargetMenu {
		return a.handleConnectTargetMouse(event)
	}
	if event.y == 0 {
		return false
	}
	if a.State.TopMenuOpen {
		a.State.TopMenuOpen = false
	}
	if a.State.ContextMenu {
		if a.mouseInContextMenu(event) {
			return a.handleContextMenuMouse(event)
		}
		a.State.closeContextMenu()
	}
	if panel := inspectorBounds(a.ViewWidth, a.contentHeight()); xyInRect(event.x, event.y, panel) {
		return a.handleInspectorMouse(event, panel)
	}
	if a.State.InspectorCapOpen {
		a.closeInspectorCapabilityPicker()
	}
	if a.State.ConnectMode {
		if index, ok := a.nodeIndexAt(event.x, event.y); ok {
			a.State.Focus = FocusGraph
			a.State.Selected = index
			return a.handleConnectKey("enter")
		}
		return false
	}
	if index, ok := a.nodeIndexAt(event.x, event.y); ok {
		a.recordMouseNodePress(index, event)
		a.State.Focus = FocusGraph
		if a.State.Selected != index {
			a.clearInspectorEdit()
			a.closeInspectorCapabilityPicker()
			a.State.InspectorSelected = 0
		}
		a.State.Selected = index
		return false
	}
	if xyInRect(event.x, event.y, a.graphBounds()) {
		a.State.Focus = FocusGraph
		a.recordMousePanPress(event)
	}
	return false
}

func (a *App) recordMouseNodePress(index int, event mouseEvent) {
	if index < 0 || index >= len(a.Model.Nodes) {
		return
	}
	node := a.Model.Nodes[index]
	a.inputState.mouse.downNodeID = node.ID
	a.inputState.mouse.downNodeType = node.Type
	a.inputState.mouse.downX = event.x
	a.inputState.mouse.downY = event.y
	a.inputState.mouse.dragStartX = node.X
	a.inputState.mouse.dragStartY = node.Y
	a.inputState.mouse.dragMoved = false
}

func (a *App) clearMouseDrag() {
	a.inputState.mouse.downNodeID = ""
	a.inputState.mouse.downNodeType = ""
	a.inputState.mouse.downX = 0
	a.inputState.mouse.downY = 0
	a.inputState.mouse.dragStartX = 0
	a.inputState.mouse.dragStartY = 0
	a.inputState.mouse.dragMoved = false
}

func (a *App) recordMousePanPress(event mouseEvent) {
	a.inputState.mouse.panActive = true
	a.inputState.mouse.panDownX = event.x
	a.inputState.mouse.panDownY = event.y
	a.inputState.mouse.panStartX = a.State.PanX
	a.inputState.mouse.panStartY = a.State.PanY
}

func (a *App) clearMousePan() {
	a.inputState.mouse.panActive = false
	a.inputState.mouse.panDownX = 0
	a.inputState.mouse.panDownY = 0
	a.inputState.mouse.panStartX = 0
	a.inputState.mouse.panStartY = 0
}

func (a *App) handleMouseDrag(event mouseEvent) {
	if event.button != 0 {
		return
	}
	if a.inputState.mouse.panActive {
		a.handleMousePanDrag(event)
		return
	}
	index, ok := a.mouseDragNodeIndex()
	if !ok {
		a.clearMouseDrag()
		return
	}
	if !a.State.MoveMode || a.State.MoveNodeID != a.inputState.mouse.downNodeID || a.State.MoveNodeType != a.inputState.mouse.downNodeType {
		a.State.MoveMode = true
		a.State.MoveNodeID = a.inputState.mouse.downNodeID
		a.State.MoveNodeType = a.inputState.mouse.downNodeType
		a.State.MoveStartX = a.inputState.mouse.dragStartX
		a.State.MoveStartY = a.inputState.mouse.dragStartY
	}
	dx := event.x - a.inputState.mouse.downX
	dy := event.y - a.inputState.mouse.downY
	maxX, maxY := a.moveBounds()
	nextX := clamp(a.inputState.mouse.dragStartX+dx, 0, maxX)
	nextY := clamp(a.inputState.mouse.dragStartY+dy, 0, maxY)
	if a.Model.Nodes[index].X != nextX || a.Model.Nodes[index].Y != nextY {
		a.Model.Nodes[index].X = nextX
		a.Model.Nodes[index].Y = nextY
		a.inputState.mouse.dragMoved = true
	}
	a.State.Focus = FocusGraph
	a.State.Selected = index
	a.State.Message = ""
	a.State.TopMenuOpen = false
	a.State.closeContextMenu()
}

func (a *App) handleMousePanDrag(event mouseEvent) {
	dx := event.x - a.inputState.mouse.panDownX
	dy := event.y - a.inputState.mouse.panDownY
	nextX, nextY := clampPanForModel(a.Model, a.graphBounds(), a.inputState.mouse.panStartX+dx, a.inputState.mouse.panStartY+dy)
	a.State.PanX = nextX
	a.State.PanY = nextY
	a.State.Focus = FocusGraph
	a.State.Message = ""
	a.State.TopMenuOpen = false
	a.State.closeContextMenu()
}

func (a *App) handleMouseRelease(event mouseEvent) {
	if event.button != 0 {
		return
	}
	if a.inputState.mouse.panActive {
		a.clearMousePan()
		return
	}
	if a.inputState.mouse.downNodeID == "" {
		return
	}
	moved := a.inputState.mouse.dragMoved
	a.clearMouseDrag()
	if moved && a.State.MoveMode {
		a.saveActiveMove()
	}
}

func (a *App) mouseDragNodeIndex() (int, bool) {
	if a.inputState.mouse.downNodeID == "" {
		return 0, false
	}
	for i, node := range a.Model.Nodes {
		if node.ID == a.inputState.mouse.downNodeID && node.Type == a.inputState.mouse.downNodeType {
			return i, true
		}
	}
	return 0, false
}

func (a *App) setMouseClickFeedback(r rect) {
	a.State.MouseClickActive = true
	a.State.MouseClickX = r.X
	a.State.MouseClickY = r.Y
	a.State.MouseClickW = r.W
	a.State.MouseClickH = r.H
}

func (a *App) clearMouseClickFeedback() {
	a.State.MouseClickActive = false
	a.State.MouseClickW = 0
	a.State.MouseClickH = 0
}

func (a *App) prepareMouseClickFeedback(key string) bool {
	event, ok := parseMouseEvent(key)
	if !ok || event.kind != mousePress || event.button != 0 {
		return false
	}
	if a.ViewWidth <= 0 {
		a.ViewWidth = 100
	}
	if a.ViewHeight <= 0 {
		a.ViewHeight = 30
	}
	if a.tabs != nil && event.y > 0 && !a.shellPaletteOpen() {
		event.y -= tabBarHeight
	}
	if r, ok := a.mouseClickFeedbackRect(event); ok {
		a.setMouseClickFeedback(r)
		return true
	}
	a.clearMouseClickFeedback()
	return false
}

func (a *App) mouseClickFeedbackRect(event mouseEvent) (rect, bool) {
	if notification, ok := notificationBoundsForState(a.State, a.ViewWidth, a.contentHeight()); ok && xyInRect(event.x, event.y, notification) {
		return notification, true
	}
	if a.State.PaletteOpen {
		return a.paletteFeedbackRect(event)
	}
	if a.State.DiskExplorerOpen {
		return a.diskExplorerFeedbackRect(event)
	}
	if a.State.ConnectTargetMenu {
		return a.connectTargetFeedbackRect(event)
	}
	if a.State.ContextMenu {
		if r, ok := a.contextMenuFeedbackRect(event); ok {
			return r, true
		}
	}
	if index, ok := a.nodeIndexAt(event.x, event.y); ok {
		nodeRects := layoutNodeRectsWithPan(a.Model, a.graphBounds(), a.State.PanX, a.State.PanY)
		if r, rectOK := nodeRects[a.Model.Nodes[index].Key()]; rectOK {
			return r, true
		}
	}
	return rect{}, false
}

func (a *App) topMenuFeedbackRect(event mouseEvent) (rect, bool) {
	rootItems := topRibbonRootItems()
	rootRects := topMenuButtonRects(rootItems, a.ViewWidth)
	for i, button := range rootRects {
		if xyInRect(event.x, event.y, button) {
			if !topRibbonRootEnabled(rootItems[i], a.State) {
				return rect{}, false
			}
			return button, true
		}
	}
	if !a.State.TopMenuOpen {
		return rect{}, false
	}
	menu, ok := a.topMenuDropdownLayout()
	if !ok || !xyInRect(event.x, event.y, menu.rect) {
		return rect{}, false
	}
	return rect{X: menu.rect.X, Y: event.y, W: menu.rect.W, H: 1}, true
}

func (a *App) topMenuDropdownLayout() (menuColumnLayout, bool) {
	items := topRibbonAddItems()
	if len(items) == 0 {
		return menuColumnLayout{}, false
	}
	rootRects := topMenuButtonRects(topRibbonRootItems(), a.ViewWidth)
	addIndex := topRibbonAddRootIndex(topRibbonRootItems())
	if addIndex < 0 || addIndex >= len(rootRects) {
		return menuColumnLayout{}, false
	}
	return layoutDropdownMenu(rect{X: 0, Y: 0, W: a.ViewWidth, H: a.contentHeight()}, rootRects[addIndex], menuItemsFromLabels(items), a.State.TopMenuSelected)
}

func (a *App) mouseInTopMenuDropdown(event mouseEvent) bool {
	menu, ok := a.topMenuDropdownLayout()
	return ok && xyInRect(event.x, event.y, menu.rect)
}

func (a *App) contextMenuFeedbackRect(event mouseEvent) (rect, bool) {
	layout, _, _, ok := a.currentContextMenuLayout()
	if !ok {
		return rect{}, false
	}
	if layout.hasSelect && xyInRect(event.x, event.y, layout.selectBox.rect) {
		return rect{X: layout.selectBox.rect.X, Y: event.y, W: layout.selectBox.rect.W, H: 1}, true
	}
	if layout.hasSub && xyInRect(event.x, event.y, layout.sub.rect) {
		if r, ok := a.contextSubmenuActionButtonRect(layout, event); ok {
			return r, true
		}
		return rect{X: layout.sub.rect.X, Y: event.y, W: layout.sub.rect.W, H: 1}, true
	}
	if xyInRect(event.x, event.y, layout.root.rect) {
		return rect{X: layout.root.rect.X, Y: event.y, W: layout.root.rect.W, H: 1}, true
	}
	return rect{}, false
}

func (a *App) contextSubmenuActionButtonRect(layout menuLayout, event mouseEvent) (rect, bool) {
	row, ok := menuRowAt(layout.sub, event.x, event.y)
	if !ok {
		return rect{}, false
	}
	item := layout.sub.items[row].Label
	action := layout.sub.items[row].Action
	if a.State.ContextGroup == "nic-menu" && isNICDetail(item) && event.x >= layout.sub.rect.X+layout.sub.rect.W-3 {
		return rect{X: layout.sub.rect.X + layout.sub.rect.W - 3, Y: event.y, W: 3, H: 1}, true
	}
	if a.State.ContextGroup == "uplink-menu" && isSwitchUplinkMenuDetail(action) && event.x >= layout.sub.rect.X+layout.sub.rect.W-3 {
		return rect{X: layout.sub.rect.X + layout.sub.rect.W - 3, Y: event.y, W: 3, H: 1}, true
	}
	if a.State.ContextGroup != "disk-menu" {
		return rect{}, false
	}
	kind := layout.sub.items[row].RowKind
	if kind == "" {
		if isDiskMenuDetail(item) {
			kind = "layer"
		} else if isDiskAttachMenuDetail(item) {
			kind = "base"
		}
	}
	if kind == "" {
		return rect{}, false
	}
	switch {
	case event.x >= layout.sub.rect.X+layout.sub.rect.W-3:
		return rect{X: layout.sub.rect.X + layout.sub.rect.W - 3, Y: event.y, W: 3, H: 1}, true
	case event.x >= layout.sub.rect.X+layout.sub.rect.W-6:
		return rect{X: layout.sub.rect.X + layout.sub.rect.W - 6, Y: event.y, W: 3, H: 1}, true
	case event.x >= layout.sub.rect.X+layout.sub.rect.W-9 && kind == "layer":
		return rect{X: layout.sub.rect.X + layout.sub.rect.W - 9, Y: event.y, W: 3, H: 1}, true
	default:
		return rect{}, false
	}
}

func (a *App) connectTargetFeedbackRect(event mouseEvent) (rect, bool) {
	menu, ok := a.connectTargetMenuLayout()
	if !ok || !xyInRect(event.x, event.y, menu.rect) {
		return rect{}, false
	}
	return rect{X: menu.rect.X, Y: event.y, W: menu.rect.W, H: 1}, true
}

func (a *App) connectTargetMenuLayout() (menuColumnLayout, bool) {
	bounds := a.graphBounds()
	nodeRects := layoutNodeRectsWithPan(a.Model, bounds, a.State.PanX, a.State.PanY)
	node, ok := a.connectTargetNode()
	if !ok {
		return menuColumnLayout{}, false
	}
	nodeRect, ok := nodeRects[node.Key()]
	if !ok {
		return menuColumnLayout{}, false
	}
	items := connectTargetNICMenuItems(node)
	if len(items) == 0 {
		return menuColumnLayout{}, false
	}
	return layoutFloatingMenu(bounds, nodeRect, menuItemsFromLabels(items), a.State.ConnectTargetIndex)
}

func (a *App) nodeIndexAt(x, y int) (int, bool) {
	bounds := a.graphBounds()
	nodeRects := layoutNodeRectsWithPan(a.Model, bounds, a.State.PanX, a.State.PanY)
	for i, node := range a.Model.Nodes {
		r, ok := nodeRects[node.Key()]
		if ok && xyInRect(x, y, r) {
			return i, true
		}
	}
	return 0, false
}

func xyInRect(x, y int, r rect) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

func (a *App) currentContextMenuLayout() (menuLayout, Node, bool, bool) {
	bounds := a.graphBounds()
	nodeRects := layoutNodeRectsWithPan(a.Model, bounds, a.State.PanX, a.State.PanY)
	return contextMenuLayoutFor(a.Model, a.State, nodeRects, bounds, inspectorBounds(a.ViewWidth, a.contentHeight()).W > 0)
}

func (a *App) mouseInContextMenu(event mouseEvent) bool {
	layout, _, _, ok := a.currentContextMenuLayout()
	if !ok {
		return false
	}
	return xyInRect(event.x, event.y, layout.root.rect) ||
		(layout.hasSub && xyInRect(event.x, event.y, layout.sub.rect)) ||
		(layout.hasSelect && xyInRect(event.x, event.y, layout.selectBox.rect))
}

func (a *App) graphBounds() rect {
	return graphBounds(a.ViewWidth, a.contentHeight())
}

func (a *App) contentHeight() int {
	height := a.ViewHeight
	if a.tabs != nil {
		height -= tabBarHeight
	}
	return max(0, height)
}

func (a *App) handleTopMenuMouse(event mouseEvent) bool {
	if event.y == 0 {
		a.State.Focus = FocusTop
	}
	rootItems := topRibbonRootItems()
	rootRects := topMenuButtonRects(rootItems, a.ViewWidth)
	for i, button := range rootRects {
		if !xyInRect(event.x, event.y, button) {
			continue
		}
		a.State.Focus = FocusTop
		a.State.TopMenuRootSelected = i
		action := contextMenuAction(rootItems[i])
		if !topRibbonRootEnabled(rootItems[i], a.State) {
			a.State.TopMenuOpen = false
			return false
		}
		if action == "exit" {
			return true
		}
		if action == "apply-lab" {
			a.State.TopMenuOpen = false
			a.applyOpenLab()
			return false
		}
		if action == "disk-explorer" {
			a.openDiskExplorer()
			return false
		}
		if action == "create-menu" {
			a.State.TopMenuOpen = !a.State.TopMenuOpen
			a.State.TopMenuSelected = 0
		}
		return false
	}
	if !a.State.TopMenuOpen {
		return false
	}
	items := topRibbonAddItems()
	actions := topRibbonAddActions()
	if len(items) == 0 || len(actions) != len(items) {
		a.State.TopMenuOpen = false
		return false
	}
	menu, ok := a.topMenuDropdownLayout()
	if !ok || !xyInRect(event.x, event.y, menu.rect) {
		a.State.TopMenuOpen = false
		return false
	}
	selected, rowOK := menuRowAt(menu, event.x, event.y)
	if !rowOK {
		a.State.TopMenuOpen = false
		return false
	}
	if selected < 0 || selected >= len(actions) {
		a.State.TopMenuOpen = false
		return false
	}
	a.State.TopMenuSelected = selected
	a.State.closeContextMenu()
	a.runGlobalMenuAction(actions[selected])
	a.State.TopMenuOpen = false
	return false
}
