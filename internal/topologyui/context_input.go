package topologyui

import "strings"

func (a *App) handleContextMenuMouse(event mouseEvent) bool {
	layout, node, hasNode, ok := a.currentContextMenuLayout()
	if !ok {
		return false
	}
	if layout.hasSub && xyInRect(event.x, event.y, layout.sub.rect) {
		row, rowOK := menuRowAt(layout.sub, event.x, event.y)
		if !rowOK {
			return false
		}
		a.State.ContextInSubmenu = true
		a.State.ContextSubSelected = row
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
			selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
			if len(subItems) > 0 {
				action := contextMenuAction(subItems[selected])
				if ok && a.State.ContextDeleteNIC {
					if index, deleteOK := nicDetailIndex(subItems[selected]); deleteOK {
						a.runMenuAction(node, "delete-nic:"+index)
						a.State.closeContextMenu()
						return false
					}
				}
				if ok && a.State.ContextDeleteDisk {
					entries := a.diskMenuEntries(node)
					entry := diskMenuEntry{}
					if len(entries) > 0 {
						entry = entries[normalizedMenuSelection(a.State.ContextSubSelected, len(entries))]
					}
					if selected < len(subItems) && entry.diskID != "" {
						a.deleteDiskMenuEntry(node, entry)
						a.State.closeContextMenu()
						return false
					}
				}
				if ok && a.State.ContextDetachDisk {
					entries := a.diskMenuEntries(node)
					entry := diskMenuEntry{}
					if len(entries) > 0 {
						entry = entries[normalizedMenuSelection(a.State.ContextSubSelected, len(entries))]
					}
					if selected < len(subItems) && entry.diskID != "" && entry.action == diskMenuActionNone {
						a.detachDiskFromNode(node)
						a.State.closeContextMenu()
						return false
					}
				}
				if ok && a.State.ContextAddDiskLayer {
					if selected < len(subItems) {
						entries := a.diskMenuEntries(node)
						if len(entries) > 0 {
							entry := entries[normalizedMenuSelection(a.State.ContextSubSelected, len(entries))]
							if a.diskMenuEntryKind(node, entry) == "base" && entry.action != diskMenuActionNone {
								a.startAddLayerInlineEdit(entry)
							}
						}
						return false
					}
				}
				if ok && a.State.ContextMergeDisk {
					entries := a.diskMenuEntries(node)
					entry := diskMenuEntry{}
					if len(entries) > 0 {
						entry = entries[normalizedMenuSelection(a.State.ContextSubSelected, len(entries))]
					}
					switch {
					case selected < len(subItems) && a.diskMenuEntryKind(node, entry) == "layer" && entry.action == diskMenuActionNone:
						a.mergeDiskForNode(node)
						a.State.closeContextMenu()
						return false
					case selected < len(subItems) && a.diskMenuEntryKind(node, entry) == "layer" && entry.action == diskMenuActionAttach:
						a.diskMerge(entry.diskID)
						a.State.closeContextMenu()
						return false
					}
				}
				if isContextGroup(action) {
					a.setContextGroup(action, node, ok)
					a.State.ContextSubSelected = 0
					return false
				}
				if ok && a.State.ContextGroup == "disk-menu" {
					entries := a.diskMenuEntries(node)
					if len(entries) > 0 {
						entry := entries[normalizedMenuSelection(a.State.ContextSubSelected, len(entries))]
						if entry.action == diskMenuActionCreate {
							a.startAddDiskInlineEdit()
							return false
						}
						a.selectDiskMenuEntry(node, entry)
					}
					a.State.closeContextMenu()
					return false
				}
				if ok && a.State.ContextGroup == "interface-menu" {
					a.selectExternalInterface(node, subItems[selected])
					return false
				}
				if ok && a.State.ContextGroup == "mode-menu" {
					a.selectExternalMode(node, subItems[selected])
					return false
				}
				if ok && isBoolContextItem(subItems[selected]) {
					a.applyContextEdit(node, subItems[selected], toggledBoolValue(contextItemValue(subItems[selected])))
					return false
				}
				if isContextInfoItem(subItems[selected]) {
					return false
				}
				if ok && action == "disk" {
					a.runMenuAction(node, action)
					a.State.closeContextMenu()
					return false
				}
				if ok && isExternalInterfaceField(node, subItems[selected]) {
					a.setContextGroup("interface-menu", node, ok)
					a.State.ContextSubSelected = 0
					return false
				}
				if ok && isExternalModeField(node, subItems[selected]) {
					a.setContextGroup("mode-menu", node, ok)
					a.State.ContextSubSelected = 0
					return false
				}
				if ok && isEditableContextItem(subItems[selected]) {
					a.State.ContextEdit = true
					a.State.ContextEditValue = contextItemValue(subItems[selected])
					a.State.ContextEditCursor = runeLen(a.State.ContextEditValue)
					return false
				}
				if ok {
					a.runMenuAction(node, action)
				} else {
					a.runGlobalMenuAction(action)
				}
				a.State.closeContextMenu()
				return false
			}
		} else {
			selected := normalizedMenuSelection(a.State.ContextSelected, len(rootItems))
			if len(rootItems) > 0 {
				action := contextMenuAction(rootItems[selected])
				if isContextGroup(action) {
					a.setContextGroup(action, node, ok)
					a.State.ContextInSubmenu = true
					a.State.ContextSubSelected = 0
					return false
				}
				if ok {
					a.runMenuAction(node, action)
				} else {
					a.runGlobalMenuAction(action)
				}
				a.State.closeContextMenu()
			} else {
				a.State.closeContextMenu()
			}
		}
	case "left", "right":
		if a.State.ContextInSubmenu && a.State.ContextGroup == "nic-menu" && len(subItems) > 0 {
			selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
			if isNICDetail(subItems[selected]) {
				if key == "right" {
					a.State.ContextDeleteNIC = true
					return false
				}
				if key == "left" && a.State.ContextDeleteNIC {
					a.State.ContextDeleteNIC = false
					return false
				}
			}
		}
		if a.State.ContextInSubmenu && a.State.ContextGroup == "disk-menu" && len(subItems) > 0 {
			selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
			entries := a.diskMenuEntries(node)
			entry := diskMenuEntry{}
			if len(entries) > 0 {
				entry = entries[normalizedMenuSelection(a.State.ContextSubSelected, len(entries))]
			}
			entryKind := a.diskMenuEntryKind(node, entry)
			if entryKind == "base" {
				if key == "right" {
					if entry.action == diskMenuActionNone {
						if a.State.ContextDetachDisk {
							a.State.ContextDetachDisk = false
							a.State.ContextDeleteDisk = true
							return false
						}
						a.State.ContextAddDiskLayer = false
						a.State.ContextMergeDisk = false
						a.State.ContextDetachDisk = true
						a.State.ContextDeleteDisk = false
						return false
					}
					if a.State.ContextAddDiskLayer {
						a.State.ContextAddDiskLayer = false
						a.State.ContextDeleteDisk = true
						return false
					}
					a.State.ContextAddDiskLayer = true
					a.State.ContextMergeDisk = false
					a.State.ContextDetachDisk = false
					a.State.ContextDeleteDisk = false
					return false
				}
				if key == "left" && a.State.ContextDeleteDisk {
					a.State.ContextDeleteDisk = false
					if entry.action == diskMenuActionNone {
						a.State.ContextDetachDisk = true
					} else {
						a.State.ContextAddDiskLayer = true
					}
					return false
				}
				if key == "left" && a.State.ContextDetachDisk {
					a.State.ContextDetachDisk = false
					return false
				}
				if key == "left" && a.State.ContextAddDiskLayer {
					a.State.ContextAddDiskLayer = false
					return false
				}
			}
			if entryKind == "data" {
				if key == "right" {
					if entry.action == diskMenuActionNone {
						if a.State.ContextDetachDisk {
							a.State.ContextDetachDisk = false
							a.State.ContextDeleteDisk = true
							return false
						}
						a.State.ContextAddDiskLayer = false
						a.State.ContextMergeDisk = false
						a.State.ContextDetachDisk = true
						a.State.ContextDeleteDisk = false
						return false
					}
					a.State.ContextAddDiskLayer = false
					a.State.ContextMergeDisk = false
					a.State.ContextDetachDisk = false
					a.State.ContextDeleteDisk = true
					return false
				}
				if key == "left" && a.State.ContextDeleteDisk {
					a.State.ContextDeleteDisk = false
					if entry.action == diskMenuActionNone {
						a.State.ContextDetachDisk = true
					}
					return false
				}
				if key == "left" && a.State.ContextDetachDisk {
					a.State.ContextDetachDisk = false
					return false
				}
			}
			if entryKind == "layer" || isDiskMenuDetail(subItems[selected]) {
				if key == "right" {
					if a.State.ContextMergeDisk {
						a.State.ContextMergeDisk = false
						a.State.ContextDetachDisk = false
						if entry.action == diskMenuActionNone {
							a.State.ContextDetachDisk = true
							a.State.ContextDeleteDisk = false
						} else {
							a.State.ContextDetachDisk = false
							a.State.ContextDeleteDisk = true
						}
						return false
					}
					if a.State.ContextDetachDisk {
						a.State.ContextDetachDisk = false
						a.State.ContextDeleteDisk = true
						return false
					}
					a.State.ContextAddDiskLayer = false
					a.State.ContextMergeDisk = true
					a.State.ContextDetachDisk = false
					a.State.ContextDeleteDisk = false
					return false
				}
				if key == "left" && a.State.ContextDeleteDisk {
					a.State.ContextDeleteDisk = false
					if entry.action == diskMenuActionNone {
						a.State.ContextDetachDisk = true
					} else {
						a.State.ContextMergeDisk = true
					}
					return false
				}
				if key == "left" && a.State.ContextDetachDisk {
					a.State.ContextDetachDisk = false
					a.State.ContextMergeDisk = true
					return false
				}
				if key == "left" && a.State.ContextMergeDisk {
					a.State.ContextMergeDisk = false
					a.State.ContextDetachDisk = false
					return false
				}
			}
		}
		if key == "left" && a.State.ContextInSubmenu && a.State.ContextGroup == "interface-menu" {
			a.setContextGroup("config-menu", node, ok)
			a.State.ContextSubSelected = externalInterfaceFieldIndex(node)
			return false
		}
		if key == "left" && a.State.ContextInSubmenu && a.State.ContextGroup == "mode-menu" {
			a.setContextGroup("config-menu", node, ok)
			a.State.ContextSubSelected = externalModeFieldIndex(node)
			return false
		}
		if key == "left" && a.State.ContextInSubmenu {
			a.State.closeContextSubmenu()
			return false
		}
		if key == "right" && !a.State.ContextInSubmenu {
			a.setContextGroup(activeRootContextGroup(rootItems, a.State.ContextSelected), node, ok)
			if a.State.ContextGroup == "" {
				return false
			}
			a.State.ContextInSubmenu = true
			return false
		}
		a.State.closeContextMenu()
		if ok {
			a.State.Selected = MoveSelection(a.Model, a.State.Selected, key)
		}
	}
	return false
}

func (a *App) setContextGroup(group string, node Node, ok bool) {
	a.State.ContextGroup = group
	a.State.clearContextRowState()
	if group == "disk-menu" && ok {
		a.State.DiskMenuItems = a.diskMenuItems(node)
		a.State.DiskMenuActions = a.diskMenuActions(node)
		a.State.DiskMenuKinds = a.diskMenuKinds(node)
		return
	}
	a.State.clearContextMenuCache()
}

func (a *App) selectExternalInterface(node Node, item string) {
	if node.Type != NodeExternal || isContextInfoItem(item) {
		return
	}
	iface := strings.TrimSpace(item)
	if iface == "" {
		return
	}
	a.externalSet(node.ID, map[string]string{"interface": iface})
	a.State.closeContextMenu()
}

func (a *App) selectExternalMode(node Node, item string) {
	if node.Type != NodeExternal || isContextInfoItem(item) {
		return
	}
	mode := strings.TrimSpace(item)
	if mode == "" {
		return
	}
	a.externalSet(node.ID, map[string]string{"mode": mode})
	a.State.closeContextMenu()
}

func isExternalInterfaceField(node Node, item string) bool {
	return node.Type == NodeExternal && contextItemKey(item) == "interface"
}

func isExternalModeField(node Node, item string) bool {
	return node.Type == NodeExternal && contextItemKey(item) == "mode"
}

func externalInterfaceFieldIndex(node Node) int {
	for i, item := range contextMenuItems(node, "config-menu") {
		if contextItemKey(item) == "interface" {
			return i
		}
	}
	return 0
}

func externalModeFieldIndex(node Node) int {
	for i, item := range contextMenuItems(node, "config-menu") {
		if contextItemKey(item) == "mode" {
			return i
		}
	}
	return 0
}
