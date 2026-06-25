package topologyui

func (a *App) handleContextMenuKey(key string) bool {
	node, ok := selectedNode(a.Model, a.State.Selected)
	rootItems := a.contextMenuRootItems(node, ok)
	subItems := a.contextMenuSubmenuItems(node, ok)
	if a.State.ContextEdit {
		return a.handleContextEditKey(key, node, ok, subItems)
	}
	switch key {
	case "up", "down":
		a.State.clearContextRowState()
		if a.State.ContextInSubmenu {
			a.State.ContextSubSelected = MoveContextSelection(a.State.ContextSubSelected, len(subItems), key)
		} else {
			a.State.ContextSelected = MoveContextSelection(a.State.ContextSelected, len(rootItems), key)
			a.setContextGroup("", node, ok)
			a.State.ContextSubSelected = 0
		}
	case "space", "escape", "tab":
		a.State.closeContextMenu()
	case "enter":
		if a.State.ContextInSubmenu {
			a.handleContextSubmenuEnter(node, ok, subItems)
		} else {
			a.handleContextRootEnter(node, ok, rootItems)
		}
	case "left", "right":
		a.handleContextHorizontalKey(node, ok, key, rootItems, subItems)
	}
	return false
}

func (a *App) handleContextSubmenuEnter(node Node, ok bool, subItems []string) {
	selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
	if len(subItems) == 0 {
		return
	}
	action := contextMenuAction(subItems[selected])
	if ok && a.handleContextInlineActionEnter(node, subItems, selected) {
		return
	}
	if isContextGroup(action) {
		a.setContextGroup(action, node, ok)
		a.State.ContextSubSelected = 0
		return
	}
	if ok && a.State.ContextGroup == "disk-menu" {
		entries := a.diskMenuEntries(node)
		if len(entries) > 0 {
			entry := entries[normalizedMenuSelection(a.State.ContextSubSelected, len(entries))]
			if entry.action == diskMenuActionCreate {
				a.startAddDiskInlineEdit()
				return
			}
			a.selectDiskMenuEntry(node, entry)
		}
		a.State.closeContextMenu()
		return
	}
	if ok && a.State.ContextGroup == "interface-menu" {
		a.selectExternalInterface(node, subItems[selected])
		return
	}
	if ok && a.State.ContextGroup == "mode-menu" {
		a.selectExternalMode(node, subItems[selected])
		return
	}
	if ok && isBoolContextItem(subItems[selected]) {
		a.applyContextEdit(node, subItems[selected], toggledBoolValue(contextItemValue(subItems[selected])))
		return
	}
	if isContextInfoItem(subItems[selected]) {
		return
	}
	if ok && action == "disk" {
		a.runMenuAction(node, action)
		a.State.closeContextMenu()
		return
	}
	if ok && isExternalInterfaceField(node, subItems[selected]) {
		a.setContextGroup("interface-menu", node, ok)
		a.State.ContextSubSelected = 0
		return
	}
	if ok && isExternalModeField(node, subItems[selected]) {
		a.setContextGroup("mode-menu", node, ok)
		a.State.ContextSubSelected = 0
		return
	}
	if ok && isEditableContextItem(subItems[selected]) {
		a.State.ContextEdit = true
		a.State.ContextEditValue = contextItemValue(subItems[selected])
		a.State.ContextEditCursor = runeLen(a.State.ContextEditValue)
		return
	}
	if ok {
		a.runMenuAction(node, action)
	} else {
		a.runGlobalMenuAction(action)
	}
	a.State.closeContextMenu()
}

func (a *App) handleContextRootEnter(node Node, ok bool, rootItems []string) {
	selected := normalizedMenuSelection(a.State.ContextSelected, len(rootItems))
	if len(rootItems) == 0 {
		a.State.closeContextMenu()
		return
	}
	action := contextMenuAction(rootItems[selected])
	if isContextGroup(action) {
		a.setContextGroup(action, node, ok)
		a.State.ContextInSubmenu = true
		a.State.ContextSubSelected = 0
		return
	}
	if ok {
		a.runMenuAction(node, action)
	} else {
		a.runGlobalMenuAction(action)
	}
	a.State.closeContextMenu()
}

func (a *App) handleContextHorizontalKey(node Node, ok bool, key string, rootItems, subItems []string) {
	if a.handleContextHorizontalActionKey(node, key, subItems) {
		return
	}
	if key == "left" && a.State.ContextInSubmenu && a.State.ContextGroup == "interface-menu" {
		a.setContextGroup("config-menu", node, ok)
		a.State.ContextSubSelected = externalInterfaceFieldIndex(node)
		return
	}
	if key == "left" && a.State.ContextInSubmenu && a.State.ContextGroup == "mode-menu" {
		a.setContextGroup("config-menu", node, ok)
		a.State.ContextSubSelected = externalModeFieldIndex(node)
		return
	}
	if key == "left" && a.State.ContextInSubmenu {
		a.State.closeContextSubmenu()
		return
	}
	if key == "right" && !a.State.ContextInSubmenu {
		a.setContextGroup(activeRootContextGroup(rootItems, a.State.ContextSelected), node, ok)
		if a.State.ContextGroup == "" {
			return
		}
		a.State.ContextInSubmenu = true
		return
	}
	a.State.closeContextMenu()
	if ok {
		a.State.Selected = MoveSelection(a.Model, a.State.Selected, key)
	}
}
