package topologyui

import (
	"strconv"
	"strings"
)

func (a *App) startConnectNICIndex(node Node, index string) {
	if !a.hasNIC(node) {
		a.State.Message = "add nic first"
		return
	}
	if !a.hasNICIndex(node, index) {
		a.State.Message = "nic not found: " + node.ID + ":" + index
		return
	}
	if !a.nicDisconnect(node.Type, node.ID, index) {
		return
	}
	a.State.ConnectNodeID = node.ID
	a.State.ConnectNodeType = node.Type
	a.State.ConnectNICIndex = index
	endpointIndex, ok := a.firstConnectEndpointIndex(node.Type)
	if !ok {
		a.clearConnectMode()
		a.State.Message = "no " + a.connectEndpointLabel() + " available"
		return
	}
	a.State.ConnectMode = true
	a.State.Message = "connect " + node.Key() + " nic" + a.State.ConnectNICIndex + ": select endpoint"
	a.State.Selected = endpointIndex
}

func (a *App) handleConnectTargetMenuKey(key string) bool {
	node, ok := a.connectTargetNode()
	items := []string{}
	if ok {
		items = connectTargetNICMenuItems(node)
	}
	switch key {
	case "quit":
		return true
	case "up", "down":
		a.State.ConnectTargetIndex = MoveContextSelection(a.State.ConnectTargetIndex, len(items), key)
	case "enter":
		if ok && len(items) > 0 {
			a.connectSelectedTargetNIC(node, items[normalizedMenuSelection(a.State.ConnectTargetIndex, len(items))])
		}
	case "escape":
		a.clearConnectMode()
		a.State.Message = ""
	}
	return false
}

func (a *App) handleConnectKey(key string) bool {
	switch key {
	case "quit":
		return true
	case "left", "right", "up", "down":
		a.State.Selected = MoveSelection(a.Model, a.State.Selected, key)
	case "enter":
		a.connectSelectedEndpoint()
	case "escape":
		a.clearConnectMode()
		a.State.Message = ""
	}
	return false
}

func (a *App) connectSelectedEndpoint() {
	endpoint, ok := selectedNode(a.Model, a.State.Selected)
	if !ok {
		a.clearConnectMode()
		return
	}
	if !a.canConnectToEndpoint(endpoint) {
		a.State.Message = "select " + a.connectEndpointLabel()
		return
	}
	sourceID := a.State.ConnectNodeID
	sourceType := a.State.ConnectNodeType
	index := a.State.ConnectNICIndex
	switch endpoint.Type {
	case NodeSwitch, NodeExternal:
		switch sourceType {
		case NodeVM:
			a.vmNICConnect(sourceID, index, map[string]string{"to": endpoint.ID})
		case NodeContainer:
			a.containerNICConnect(sourceID, index, map[string]string{"to": endpoint.ID})
		}
		a.clearConnectMode()
	case NodeVM, NodeContainer:
		a.openConnectTargetMenu(endpoint)
	}
}

func (a *App) openConnectTargetMenu(endpoint Node) {
	a.State.ConnectTargetMenu = true
	a.State.ConnectTargetID = endpoint.ID
	a.State.ConnectTargetType = endpoint.Type
	a.State.ConnectTargetIndex = 0
	a.State.Message = "connect to " + endpoint.Key() + ": select target nic"
}

func (a *App) connectSelectedTargetNIC(target Node, item string) {
	targetIndex := ""
	if strings.TrimSpace(item) == "New NIC" {
		targetIndex = strconv.Itoa(a.nicCount(target.Type, target.ID))
		switch target.Type {
		case NodeVM:
			a.vmNICAdd(target.ID, nil)
		case NodeContainer:
			a.containerNICAdd(target.ID, nil)
		default:
			a.State.Message = "target must be vm or container"
			return
		}
		if !strings.HasPrefix(a.State.Message, "added nic to ") {
			return
		}
		if !a.hasNICIndex(target, targetIndex) {
			a.State.Message = "target nic create failed: " + target.ID
			return
		}
	} else if index, ok := nicDetailIndex(item); ok {
		targetIndex = index
	} else {
		a.State.Message = "select target nic"
		return
	}
	sourceID := a.State.ConnectNodeID
	sourceType := a.State.ConnectNodeType
	sourceIndex := a.State.ConnectNICIndex
	a.nicConnectDirectTo(sourceType, sourceID, sourceIndex, target.Type, target.ID, targetIndex)
	a.clearConnectMode()
}

func (a *App) connectTargetNode() (Node, bool) {
	return nodeByKey(a.Model, NodeKey(a.State.ConnectTargetType, a.State.ConnectTargetID))
}

func (a *App) canConnectToEndpoint(node Node) bool {
	if node.Type == a.State.ConnectNodeType && node.ID == a.State.ConnectNodeID {
		return false
	}
	switch a.State.ConnectNodeType {
	case NodeVM:
		return node.Type == NodeSwitch || node.Type == NodeExternal || node.Type == NodeVM || node.Type == NodeContainer
	case NodeContainer:
		return node.Type == NodeSwitch || node.Type == NodeExternal || node.Type == NodeVM || node.Type == NodeContainer
	default:
		return false
	}
}

func (a *App) connectEndpointLabel() string {
	if a.State.ConnectNodeType == NodeContainer {
		return "switch, external or workload endpoint"
	}
	return "switch, external or workload endpoint"
}

func (a *App) firstConnectEndpointIndex(sourceType string) (int, bool) {
	for i, node := range a.Model.Nodes {
		if node.Type == a.State.ConnectNodeType && node.ID == a.State.ConnectNodeID {
			continue
		}
		if a.canConnectToEndpoint(node) {
			return i, true
		}
	}
	return 0, false
}

func (a *App) hasNIC(node Node) bool {
	switch node.Type {
	case NodeVM:
		if vm, ok := a.labVM(node.ID); ok {
			return len(vm.Networks) > 0
		}
	case NodeContainer:
		if ct, ok := a.labContainer(node.ID); ok {
			return len(ct.Networks) > 0
		}
	}
	return false
}

func (a *App) hasNICIndex(node Node, index string) bool {
	nicIndex, err := strconv.Atoi(index)
	if err != nil || nicIndex < 0 {
		return false
	}
	switch node.Type {
	case NodeVM:
		if vm, ok := a.labVM(node.ID); ok {
			return nicIndex < len(vm.Networks)
		}
	case NodeContainer:
		if ct, ok := a.labContainer(node.ID); ok {
			return nicIndex < len(ct.Networks)
		}
	}
	return false
}

func (a *App) nicCount(typ, id string) int {
	switch typ {
	case NodeVM:
		if vm, ok := a.labVM(id); ok {
			return len(vm.Networks)
		}
	case NodeContainer:
		if ct, ok := a.labContainer(id); ok {
			return len(ct.Networks)
		}
	}
	return 0
}

func (a *App) clearConnectMode() {
	a.State.ConnectMode = false
	a.State.ConnectNodeID = ""
	a.State.ConnectNodeType = ""
	a.State.ConnectNICIndex = ""
	a.State.ConnectTargetMenu = false
	a.State.ConnectTargetID = ""
	a.State.ConnectTargetType = ""
	a.State.ConnectTargetIndex = 0
}
