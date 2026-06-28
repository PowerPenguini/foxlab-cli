package topologyui

import (
	"strconv"
	"strings"
)

type mouseEvent struct {
	x      int
	y      int
	button int
}

func parseMouseEvent(key string) (mouseEvent, bool) {
	parts := strings.Split(key, ":")
	if len(parts) != 4 || parts[0] != "mouse" {
		return mouseEvent{}, false
	}
	x, errX := strconv.Atoi(parts[1])
	y, errY := strconv.Atoi(parts[2])
	button, errB := strconv.Atoi(parts[3])
	if errX != nil || errY != nil || errB != nil {
		return mouseEvent{}, false
	}
	return mouseEvent{x: x, y: y, button: button}, true
}

func (a *App) handleMouseKey(key string) bool {
	event, ok := parseMouseEvent(key)
	if !ok || event.button != 0 {
		return false
	}
	if a.ViewWidth <= 0 {
		a.ViewWidth = 100
	}
	if a.ViewHeight <= 0 {
		a.ViewHeight = 30
	}
	if a.State.ConnectTargetMenu {
		return a.handleConnectTargetMouse(event)
	}
	if event.y == 0 {
		return a.handleTopMenuMouse(event)
	}
	if a.State.TopMenuOpen && a.mouseInTopMenuDropdown(event) {
		return a.handleTopMenuMouse(event)
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
	if a.State.ConnectMode {
		if index, ok := a.nodeIndexAt(event.x, event.y); ok {
			a.State.Focus = FocusGraph
			a.State.Selected = index
			return a.handleConnectKey("enter")
		}
		return false
	}
	if index, ok := a.nodeIndexAt(event.x, event.y); ok {
		a.State.Focus = FocusGraph
		a.State.Selected = index
		a.State.ContextMenu = true
		a.State.ContextGroup = ""
		a.State.ContextInSubmenu = false
		a.State.ContextSelected = 0
		a.State.ContextSubSelected = 0
		a.State.closeContextSelectMenu()
		return false
	}
	if xyInRect(event.x, event.y, a.graphBounds()) {
		a.State.Focus = FocusGraph
	}
	return false
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
	if !ok || event.button != 0 {
		return false
	}
	if a.ViewWidth <= 0 {
		a.ViewWidth = 100
	}
	if a.ViewHeight <= 0 {
		a.ViewHeight = 30
	}
	if r, ok := a.mouseClickFeedbackRect(event); ok {
		a.setMouseClickFeedback(r)
		return true
	}
	a.clearMouseClickFeedback()
	return false
}

func (a *App) mouseClickFeedbackRect(event mouseEvent) (rect, bool) {
	if a.State.ConnectTargetMenu {
		return a.connectTargetFeedbackRect(event)
	}
	if event.y == 0 || a.State.TopMenuOpen {
		if r, ok := a.topMenuFeedbackRect(event); ok {
			return r, true
		}
	}
	if a.State.ContextMenu {
		if r, ok := a.contextMenuFeedbackRect(event); ok {
			return r, true
		}
	}
	if index, ok := a.nodeIndexAt(event.x, event.y); ok {
		nodeRects := layoutNodeRects(a.Model, a.graphBounds())
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
	return layoutDropdownMenu(rect{X: 0, Y: 0, W: a.ViewWidth, H: a.ViewHeight}, rootRects[addIndex], menuItemsFromLabels(items), a.State.TopMenuSelected)
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
	if a.State.ContextGroup == "nic-menu" && isNICDetail(item) && event.x >= layout.sub.rect.X+layout.sub.rect.W-3 {
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
	nodeRects := layoutNodeRects(a.Model, bounds)
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
	nodeRects := layoutNodeRects(a.Model, bounds)
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
	nodeRects := layoutNodeRects(a.Model, bounds)
	return contextMenuLayoutFor(a.Model, a.State, nodeRects, bounds)
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
	return graphBounds(a.ViewWidth, a.ViewHeight)
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
