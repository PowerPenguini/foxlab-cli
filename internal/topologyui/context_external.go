package topologyui

import "strings"

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
