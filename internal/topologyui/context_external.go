package topologyui

import (
	"strings"

	"foxlab-cli/internal/topology"
)

func (a *App) selectExternalInterface(node Node, item string) {
	if node.Type != NodeExternal || isContextInfoItem(item) {
		return
	}
	iface := strings.TrimSpace(item)
	if iface == "" {
		return
	}
	a.externalSet(node.ID, topology.ExternalUpdate{Interface: topology.SetField(iface)})
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
	a.externalSet(node.ID, topology.ExternalUpdate{Mode: topology.SetField(mode)})
	a.State.closeContextMenu()
}

func (a *App) selectNodeMode(node Node, item string) {
	if isContextInfoItem(item) {
		return
	}
	mode := modeValueForNode(node.Type, item)
	if mode == "" {
		return
	}
	switch node.Type {
	case NodeSwitch:
		a.switchSet(node.ID, topology.SwitchUpdate{Mode: topology.SetField(mode)})
	case NodeExternal:
		a.externalSet(node.ID, topology.ExternalUpdate{Mode: topology.SetField(mode)})
	default:
		return
	}
	a.State.closeContextMenu()
}

func isExternalInterfaceField(node Node, item string) bool {
	return node.Type == NodeExternal && contextItemKey(item) == "interface"
}

func isExternalModeField(node Node, item string) bool {
	return node.Type == NodeExternal && contextItemKey(item) == "mode"
}

func isModeField(node Node, item string) bool {
	switch node.Type {
	case NodeSwitch, NodeExternal:
		return contextItemKey(item) == "mode"
	default:
		return false
	}
}

func externalInterfaceFieldIndex(node Node) int {
	for i, item := range contextMenuItems(node, "config-menu") {
		if contextItemKey(item) == "interface" {
			return i
		}
	}
	return 0
}

func switchModeFieldIndex(node Node) int {
	for i, item := range contextMenuItems(node, "config-menu") {
		if contextItemKey(item) == "mode" {
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
