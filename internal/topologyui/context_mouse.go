package topologyui

func (a *App) handleContextMenuMouse(event mouseEvent) bool {
	layout, node, hasNode, ok := a.currentContextMenuLayout()
	if !ok {
		return false
	}
	if layout.hasSelect && xyInRect(event.x, event.y, layout.selectBox.rect) {
		row, rowOK := menuRowAt(layout.selectBox, event.x, event.y)
		if !rowOK {
			return false
		}
		a.State.ContextSelectSelected = row
		selectItems := a.contextMenuSelectItems(node, hasNode)
		a.handleContextSelectEnter(node, hasNode, selectItems)
		return false
	}
	if layout.hasSub && xyInRect(event.x, event.y, layout.sub.rect) {
		row, rowOK := menuRowAt(layout.sub, event.x, event.y)
		if !rowOK {
			return false
		}
		a.State.ContextInSubmenu = true
		a.State.ContextSubSelected = row
		a.State.closeContextSelectMenu()
		a.applyMouseButtonState(node, layout.sub.rect, event.x)
		return a.handleContextMenuKey("enter")
	}
	if xyInRect(event.x, event.y, layout.root.rect) {
		row, rowOK := menuRowAt(layout.root, event.x, event.y)
		if !rowOK {
			return false
		}
		a.State.ContextSelected = row
		action := ""
		items := layout.root.items
		if len(items) > 0 {
			action = items[normalizedMenuSelection(a.State.ContextSelected, len(items))].Action
		}
		if isContextGroup(action) {
			a.setContextGroup(action, node, hasNode)
			a.State.ContextInSubmenu = true
			a.State.ContextSubSelected = 0
			return false
		}
		return a.handleContextMenuKey("enter")
	}
	a.State.closeContextMenu()
	return false
}

func (a *App) applyMouseButtonState(node Node, menu rect, x int) {
	a.State.clearContextRowState()
	if a.State.ContextGroup == "nic-menu" && x >= menu.X+menu.W-3 {
		a.State.ContextDeleteNIC = true
		return
	}
	if a.State.ContextGroup != "disk-menu" {
		return
	}
	entries := a.diskMenuEntries(node)
	if len(entries) == 0 {
		return
	}
	entry := entries[normalizedMenuSelection(a.State.ContextSubSelected, len(entries))]
	kind := a.diskMenuEntryKind(node, entry)
	switch {
	case x >= menu.X+menu.W-3:
		a.State.ContextDeleteDisk = true
	case x >= menu.X+menu.W-6:
		switch kind {
		case "base":
			if entry.action == diskMenuActionNone {
				a.State.ContextDetachDisk = true
			} else {
				a.State.ContextAddDiskLayer = true
			}
		case "data":
			if entry.action == diskMenuActionNone {
				a.State.ContextDetachDisk = true
			} else {
				a.State.ContextDeleteDisk = true
			}
		case "layer":
			if entry.action == diskMenuActionNone {
				a.State.ContextDetachDisk = true
			} else {
				a.State.ContextMergeDisk = true
			}
		}
	case x >= menu.X+menu.W-9 && kind == "layer" && entry.action == diskMenuActionNone:
		a.State.ContextMergeDisk = true
	}
}

func (a *App) handleConnectTargetMouse(event mouseEvent) bool {
	menu, ok := a.connectTargetMenuLayout()
	if !ok || !xyInRect(event.x, event.y, menu.rect) {
		return false
	}
	row, rowOK := menuRowAt(menu, event.x, event.y)
	if !rowOK {
		return false
	}
	a.State.ConnectTargetIndex = row
	return a.handleConnectTargetMenuKey("enter")
}
