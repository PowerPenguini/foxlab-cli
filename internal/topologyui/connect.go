package topologyui

import (
	"strconv"
	"strings"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
)

func (a *App) startConnectNICIndex(node Node, index string) {
	if !a.hasNIC(node) {
		a.State.Message = "add nic first"
		return
	}
	if !a.hasNICIndex(node, index) {
		a.State.Message = "nic not found: " + a.displayNodeName(node.Type, node.ID) + ":" + index
		return
	}
	managedDHCP := a.isManagedDHCPNode(node)
	if !managedDHCP {
		source, ok := directNetworkEndpoint(node.Type, node.ID, index)
		if !ok || !a.nicDisconnect(source) {
			return
		}
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
	a.State.Message = "connect " + a.displayNodeKey(node.Type, node.ID) + " nic" + a.State.ConnectNICIndex + ": select endpoint"
	a.State.Selected = endpointIndex
}

func (a *App) startConnectEndpoint(node Node) {
	switch node.Type {
	case NodeSwitch, NodeExternal:
	default:
		a.State.Message = "connect needs switch or uplink"
		return
	}
	if node.Type == NodeExternal && a.externalConnected(node.ID) {
		a.State.Message = "uplink already connected: " + a.displayNodeName(node.Type, node.ID)
		return
	}
	a.State.ConnectNodeID = node.ID
	a.State.ConnectNodeType = node.Type
	a.State.ConnectNICIndex = ""
	endpointIndex, ok := a.firstConnectEndpointIndex(node.Type)
	if !ok {
		a.clearConnectMode()
		a.State.Message = "no " + a.connectEndpointLabel() + " available"
		return
	}
	a.State.ConnectMode = true
	a.State.Message = "connect " + a.displayNodeKey(node.Type, node.ID) + ": select " + a.connectEndpointLabel()
	a.State.Selected = endpointIndex
}

func (a *App) externalConnected(id string) bool {
	if a.currentLab() == nil {
		return externalConnectedInModel(a.Model, id)
	}
	for _, sw := range a.currentLab().Switches {
		if lab.SwitchHasExternalLink(sw, id) {
			return true
		}
	}
	for _, vm := range a.currentLab().VMs {
		for _, nic := range vm.Networks {
			if nic.ExternalLink == id {
				return true
			}
		}
	}
	for _, ct := range a.currentLab().Containers {
		for _, nic := range ct.Networks {
			if nic.ExternalLink == id {
				return true
			}
		}
	}
	return false
}

func (a *App) firstAttachableUplinkID() string {
	ids := a.attachableUplinkIDs()
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func (a *App) attachableUplinkIDs() []string {
	if a.currentLab() == nil {
		return attachableUplinkIDsInModel(a.Model)
	}
	ids := []string{}
	for _, link := range a.currentLab().ExternalLinks {
		if link.ID != "" && !a.externalConnected(link.ID) {
			ids = append(ids, link.ID)
		}
	}
	return ids
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
		switch {
		case sourceType == NodeVM || sourceType == NodeContainer:
			nicIndex, ok := parseNICIndex(index)
			if !ok {
				a.State.Message = "nic not found: " + a.displayNodeName(sourceType, sourceID) + ":" + index
				return
			}
			endpointType := topology.NetworkEndpointSwitch
			if endpoint.Type == NodeExternal {
				endpointType = topology.NetworkEndpointUplink
			}
			request := topology.NICConnectRequest{
				NIC:      nicIndex,
				Endpoint: topology.NetworkEndpointRef{Type: endpointType, ID: endpoint.ID},
			}
			if sourceType == NodeVM {
				a.vmNICConnect(sourceID, request)
			} else {
				a.containerNICConnect(sourceID, request)
			}
			a.clearConnectMode()
		case sourceType == NodeSwitch || sourceType == NodeExternal:
			a.connectSwitchExternal(sourceType, sourceID, endpoint.Type, endpoint.ID)
			a.clearConnectMode()
		}
	case NodeVM, NodeContainer:
		a.openConnectTargetMenu(endpoint)
	}
}

func (a *App) connectSwitchExternal(sourceType, sourceID, targetType, targetID string) {
	switchID := ""
	externalID := ""
	switch {
	case sourceType == NodeSwitch && targetType == NodeExternal:
		switchID = sourceID
		externalID = targetID
	case sourceType == NodeExternal && targetType == NodeSwitch:
		switchID = targetID
		externalID = sourceID
	default:
		a.State.Message = "select " + a.connectEndpointLabel()
		return
	}
	a.switchSet(switchID, topology.SwitchUpdate{AttachUplink: topology.SetField(externalID)})
}

func (a *App) openConnectTargetMenu(endpoint Node) {
	a.State.openOverlay(overlayConnectTarget)
	a.State.ConnectTargetID = endpoint.ID
	a.State.ConnectTargetType = endpoint.Type
	a.State.ConnectTargetIndex = 0
	a.State.Message = "connect to " + a.displayNodeKey(endpoint.Type, endpoint.ID) + ": select target nic"
}

func (a *App) connectSelectedTargetNIC(target Node, item string) {
	targetIndex := ""
	if strings.TrimSpace(item) == "New NIC" {
		targetIndex = strconv.Itoa(a.nicCount(target.Type, target.ID))
		result := topology.Failure("target must be vm or container")
		switch target.Type {
		case NodeVM:
			result = a.vmNICAdd(target.ID, topology.NICAddRequest{})
		case NodeContainer:
			result = a.containerNICAdd(target.ID, topology.NICAddRequest{})
		default:
			a.State.Message = "target must be vm or container"
			return
		}
		if !result.OK() {
			return
		}
		if !a.hasNICIndex(target, targetIndex) {
			a.State.Message = "target nic create failed: " + a.displayNodeName(target.Type, target.ID)
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
	source, sourceOK := directNetworkEndpoint(sourceType, sourceID, sourceIndex)
	targetRef, targetOK := directNetworkEndpoint(target.Type, target.ID, targetIndex)
	if !sourceOK || !targetOK {
		a.State.Message = "select target nic"
		return
	}
	a.nicConnectDirect(source, targetRef)
	a.clearConnectMode()
}

func (a *App) connectTargetNode() (Node, bool) {
	return nodeByKey(a.Model, NodeKey(a.State.ConnectTargetType, a.State.ConnectTargetID))
}

func (a *App) canConnectToEndpoint(node Node) bool {
	if node.Type == a.State.ConnectNodeType && node.ID == a.State.ConnectNodeID {
		return false
	}
	source := Node{Type: a.State.ConnectNodeType, ID: a.State.ConnectNodeID}
	if a.isManagedDHCPNode(source) {
		return node.Type == NodeSwitch && a.isDHCPCompatibleSwitch(node.ID, source.ID)
	}
	if a.isManagedDHCPNode(node) {
		return false
	}
	switch a.State.ConnectNodeType {
	case NodeVM:
		if node.Type == NodeExternal {
			return !a.externalConnected(node.ID)
		}
		return node.Type == NodeSwitch || node.Type == NodeVM || node.Type == NodeContainer
	case NodeContainer:
		if node.Type == NodeExternal {
			return !a.externalConnected(node.ID)
		}
		return node.Type == NodeSwitch || node.Type == NodeVM || node.Type == NodeContainer
	case NodeSwitch:
		return node.Type == NodeExternal && !a.externalConnected(node.ID)
	case NodeExternal:
		return node.Type == NodeSwitch
	default:
		return false
	}
}

func (a *App) connectEndpointLabel() string {
	switch a.State.ConnectNodeType {
	case NodeSwitch:
		return "uplink endpoint"
	case NodeExternal:
		return "switch endpoint"
	case NodeContainer:
		if a.isManagedDHCPNode(Node{Type: a.State.ConnectNodeType, ID: a.State.ConnectNodeID}) {
			return "NAT switch endpoint"
		}
		return "switch, uplink or workload endpoint"
	default:
		return "switch, uplink or workload endpoint"
	}
}

func (a *App) isManagedDHCPNode(node Node) bool {
	if node.Type != NodeContainer {
		return false
	}
	ct, ok := a.labContainer(node.ID)
	return ok && lab.IsDHCPContainer(ct)
}

func (a *App) isDHCPCompatibleSwitch(switchID, containerID string) bool {
	sw, ok := a.labSwitch(switchID)
	if !ok || sw.Mode != "nat" || a.currentLab() == nil {
		return false
	}
	for _, externalID := range lab.SwitchExternalLinks(sw) {
		link, ok := lab.FindExternalLink(a.currentLab(), externalID)
		if ok && link.Mode == lab.ExternalModeMacNAT {
			return false
		}
	}
	for _, ct := range a.currentLab().Containers {
		if ct.ID == containerID {
			continue
		}
		if attached, ok := lab.DHCPContainerSwitch(ct); ok && attached == switchID {
			return false
		}
	}
	return true
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
