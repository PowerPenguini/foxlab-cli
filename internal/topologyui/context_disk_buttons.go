package topologyui

func (a *App) handleContextInlineActionEnter(node Node, subItems []string, selected int) bool {
	if a.State.ContextDeleteNIC {
		if index, deleteOK := nicDetailIndex(subItems[selected]); deleteOK {
			a.runMenuAction(node, "delete-nic:"+index)
			a.State.closeContextMenu()
			return true
		}
	}
	if a.State.ContextDeleteUplink {
		if isSwitchUplinkMenuDetail(subItems[selected]) {
			a.switchDisconnectExternal(node.ID)
			a.State.closeContextMenu()
			return true
		}
	}
	if a.State.ContextDeleteDisk {
		entry := a.contextSelectedDiskEntry(node)
		if selected < len(subItems) && entry.diskID != "" {
			a.deleteDiskMenuEntry(node, entry)
			a.State.closeContextMenu()
			return true
		}
	}
	if a.State.ContextDetachDisk {
		entry := a.contextSelectedDiskEntry(node)
		if selected < len(subItems) && entry.diskID != "" && entry.action == diskMenuActionNone {
			a.detachDiskFromNode(node)
			a.State.closeContextMenu()
			return true
		}
	}
	if a.State.ContextAddDiskLayer {
		if selected < len(subItems) {
			entry := a.contextSelectedDiskEntry(node)
			if entry.diskID != "" && a.diskMenuEntryKind(node, entry) == "base" && entry.action != diskMenuActionNone {
				a.startAddLayerInlineEdit(entry)
			}
			return true
		}
	}
	if a.State.ContextMergeDisk {
		entry := a.contextSelectedDiskEntry(node)
		switch {
		case selected < len(subItems) && a.diskMenuEntryKind(node, entry) == "layer" && entry.action == diskMenuActionNone:
			a.mergeDiskForNode(node)
			a.State.closeContextMenu()
			return true
		case selected < len(subItems) && a.diskMenuEntryKind(node, entry) == "layer" && entry.action == diskMenuActionAttach:
			a.diskMerge(entry.diskID)
			a.State.closeContextMenu()
			return true
		}
	}
	return false
}

func (a *App) contextSelectedDiskEntry(node Node) diskMenuEntry {
	entries := a.diskMenuEntries(node)
	if len(entries) == 0 {
		return diskMenuEntry{}
	}
	return entries[normalizedMenuSelection(a.State.ContextSubSelected, len(entries))]
}

func (a *App) handleContextHorizontalActionKey(node Node, key string, subItems []string) bool {
	if a.State.ContextInSubmenu && a.State.ContextGroup == "nic-menu" && len(subItems) > 0 {
		selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
		if isNICDetail(subItems[selected]) {
			if key == "right" {
				a.State.ContextDeleteNIC = true
				return true
			}
			if key == "left" && a.State.ContextDeleteNIC {
				a.State.ContextDeleteNIC = false
				return true
			}
		}
	}
	if a.State.ContextInSubmenu && a.State.ContextGroup == "uplink-menu" && len(subItems) > 0 {
		selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
		if isSwitchUplinkMenuDetail(subItems[selected]) {
			if key == "right" {
				a.State.ContextDeleteUplink = true
				return true
			}
			if key == "left" && a.State.ContextDeleteUplink {
				a.State.ContextDeleteUplink = false
				return true
			}
		}
	}
	if a.State.ContextInSubmenu && a.State.ContextGroup == "disk-menu" && len(subItems) > 0 {
		selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
		entry := a.contextSelectedDiskEntry(node)
		entryKind := a.diskMenuEntryKind(node, entry)
		if entryKind == "base" {
			return a.handleBaseDiskActionKey(key, entry)
		}
		if entryKind == "data" {
			return a.handleDataDiskActionKey(key, entry)
		}
		if entryKind == "layer" || isDiskMenuDetail(subItems[selected]) {
			return a.handleLayerDiskActionKey(key, entry)
		}
	}
	return false
}

func (a *App) handleBaseDiskActionKey(key string, entry diskMenuEntry) bool {
	if key == "right" {
		if entry.action == diskMenuActionNone {
			if a.State.ContextDetachDisk {
				a.State.ContextDetachDisk = false
				a.State.ContextDeleteDisk = true
				return true
			}
			a.State.ContextAddDiskLayer = false
			a.State.ContextMergeDisk = false
			a.State.ContextDetachDisk = true
			a.State.ContextDeleteDisk = false
			return true
		}
		if a.State.ContextAddDiskLayer {
			a.State.ContextAddDiskLayer = false
			a.State.ContextDeleteDisk = true
			return true
		}
		a.State.ContextAddDiskLayer = true
		a.State.ContextMergeDisk = false
		a.State.ContextDetachDisk = false
		a.State.ContextDeleteDisk = false
		return true
	}
	if key == "left" && a.State.ContextDeleteDisk {
		a.State.ContextDeleteDisk = false
		if entry.action == diskMenuActionNone {
			a.State.ContextDetachDisk = true
		} else {
			a.State.ContextAddDiskLayer = true
		}
		return true
	}
	if key == "left" && a.State.ContextDetachDisk {
		a.State.ContextDetachDisk = false
		return true
	}
	if key == "left" && a.State.ContextAddDiskLayer {
		a.State.ContextAddDiskLayer = false
		return true
	}
	return false
}

func (a *App) handleDataDiskActionKey(key string, entry diskMenuEntry) bool {
	if key == "right" {
		if entry.action == diskMenuActionNone {
			if a.State.ContextDetachDisk {
				a.State.ContextDetachDisk = false
				a.State.ContextDeleteDisk = true
				return true
			}
			a.State.ContextAddDiskLayer = false
			a.State.ContextMergeDisk = false
			a.State.ContextDetachDisk = true
			a.State.ContextDeleteDisk = false
			return true
		}
		a.State.ContextAddDiskLayer = false
		a.State.ContextMergeDisk = false
		a.State.ContextDetachDisk = false
		a.State.ContextDeleteDisk = true
		return true
	}
	if key == "left" && a.State.ContextDeleteDisk {
		a.State.ContextDeleteDisk = false
		if entry.action == diskMenuActionNone {
			a.State.ContextDetachDisk = true
		}
		return true
	}
	if key == "left" && a.State.ContextDetachDisk {
		a.State.ContextDetachDisk = false
		return true
	}
	return false
}

func (a *App) handleLayerDiskActionKey(key string, entry diskMenuEntry) bool {
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
			return true
		}
		if a.State.ContextDetachDisk {
			a.State.ContextDetachDisk = false
			a.State.ContextDeleteDisk = true
			return true
		}
		a.State.ContextAddDiskLayer = false
		a.State.ContextMergeDisk = true
		a.State.ContextDetachDisk = false
		a.State.ContextDeleteDisk = false
		return true
	}
	if key == "left" && a.State.ContextDeleteDisk {
		a.State.ContextDeleteDisk = false
		if entry.action == diskMenuActionNone {
			a.State.ContextDetachDisk = true
		} else {
			a.State.ContextMergeDisk = true
		}
		return true
	}
	if key == "left" && a.State.ContextDetachDisk {
		a.State.ContextDetachDisk = false
		a.State.ContextMergeDisk = true
		return true
	}
	if key == "left" && a.State.ContextMergeDisk {
		a.State.ContextMergeDisk = false
		a.State.ContextDetachDisk = false
		return true
	}
	return false
}
