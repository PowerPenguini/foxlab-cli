package topologyui

func (a *App) handleContextMenuKey(key string) bool {
	node, ok := selectedNode(a.Model, a.State.Selected)
	rootItems := a.contextMenuRootItems(node, ok)
	subItems := a.contextMenuSubmenuItems(node, ok)
	selectItems := a.contextMenuSelectItems(node, ok)
	if a.State.ContextEdit {
		return a.handleContextEditKey(key, node, ok, subItems)
	}
	switch key {
	case "up", "down":
		a.State.clearContextRowState()
		if a.State.ContextSelectGroup != "" {
			a.State.ContextSelectSelected = MoveContextSelection(a.State.ContextSelectSelected, len(selectItems), key)
		} else if a.State.ContextInSubmenu {
			a.State.ContextSubSelected = MoveContextSelection(a.State.ContextSubSelected, len(subItems), key)
			a.State.closeContextSelectMenu()
		} else {
			a.State.ContextSelected = MoveContextSelection(a.State.ContextSelected, len(rootItems), key)
			a.setContextGroup("", node, ok)
			a.State.ContextSubSelected = 0
		}
	case "space", "escape", "tab":
		a.State.closeContextMenu()
	case "enter":
		if a.State.ContextSelectGroup != "" {
			a.handleContextSelectEnter(node, ok, selectItems)
		} else if a.State.ContextInSubmenu {
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
	if ok && a.State.ContextGroup == "uplink-menu" && node.Type == NodeSwitch {
		if a.selectSwitchUplinkMenuItem(node, subItems[selected]) {
			a.State.closeContextMenu()
		}
		return
	}
	if ok && a.State.ContextGroup == "permissions-menu" && node.Type == NodeContainer {
		if capability, enabled, parsed := permissionCapabilityState(subItems[selected]); parsed {
			a.containerCapabilitySet(node.ID, capability, !enabled)
		}
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
		a.setContextSelectGroup("interface-menu")
		return
	}
	if ok && isModeField(node, subItems[selected]) {
		a.setContextSelectGroup("mode-menu")
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

func (a *App) handleContextSelectEnter(node Node, ok bool, selectItems []string) {
	if !ok || len(selectItems) == 0 {
		return
	}
	selected := normalizedMenuSelection(a.State.ContextSelectSelected, len(selectItems))
	switch a.State.ContextSelectGroup {
	case "interface-menu":
		a.selectExternalInterface(node, selectItems[selected])
	case "mode-menu":
		a.selectNodeMode(node, selectItems[selected])
	}
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
	if !a.contextRootActionEnabled(node, ok, action) {
		return
	}
	if ok {
		a.runMenuAction(node, action)
	} else {
		a.runGlobalMenuAction(action)
	}
	a.State.closeContextMenu()
}

func (a *App) contextRootActionEnabled(node Node, ok bool, action string) bool {
	if !ok {
		return true
	}
	if node.Type == NodeExternal && action == "connect" && a.externalConnected(node.ID) {
		a.State.Message = "uplink already connected: " + a.displayNodeName(node.Type, node.ID)
		return false
	}
	return true
}

func (a *App) handleContextHorizontalKey(node Node, ok bool, key string, rootItems, subItems []string) {
	if key == "left" && a.State.ContextSelectGroup != "" {
		a.State.closeContextSelectMenu()
		return
	}
	if a.handleContextHorizontalActionKey(node, key, subItems) {
		return
	}
	if key == "left" && a.State.ContextInSubmenu {
		a.State.closeContextSubmenu()
		return
	}
	if key == "right" && a.State.ContextInSubmenu && len(subItems) > 0 {
		selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
		if ok && isExternalInterfaceField(node, subItems[selected]) {
			a.setContextSelectGroup("interface-menu")
			return
		}
		if ok && isModeField(node, subItems[selected]) {
			a.setContextSelectGroup("mode-menu")
			return
		}
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

func (a *App) contextMenuSelectItems(node Node, ok bool) []string {
	if !ok || a.State.ContextSelectGroup == "" {
		return nil
	}
	return contextMenuItems(node, a.State.ContextSelectGroup)
}
