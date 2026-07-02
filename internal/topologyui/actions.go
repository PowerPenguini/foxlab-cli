package topologyui

import (
	"strings"

	"foxlab-cli/internal/lab"
)

func (a *App) runGlobalMenuAction(action string) {
	switch action {
	case "add vm", "create-vm":
		a.openCreateVMCommand(Node{})
	case "add cont", "create-container":
		a.openCreateContainerCommand(Node{})
	case "add sw", "create-switch":
		a.openCreateSwitchCommand(Node{})
	case "add disk", "create-disk":
		a.openCreateDiskCommand()
	case "add uplink", "create uplink", "create external", "create-external":
		a.openCreateExternalCommand()
	case "link", "create-link":
		a.openCreateLink()
	}
}

func (a *App) runMenuAction(node Node, action string) {
	if index, ok := strings.CutPrefix(action, "connect-nic:"); ok {
		a.startConnectNICIndex(node, index)
		return
	}
	if index, ok := strings.CutPrefix(action, "delete-nic:"); ok {
		a.deleteNIC(node, index)
		return
	}
	switch action {
	case "edit":
		a.openConfigCommand(node)
	case "rename":
		switch node.Type {
		case NodeVM:
			target := a.displayNodeName(node.Type, node.ID)
			if vm, ok := a.labVM(node.ID); ok {
				a.openCommand("vm set " + commandValue(target) + " name=" + commandValue(firstNonEmpty(vm.Name, vm.ID)))
			} else {
				a.openCommand("vm set " + commandValue(target) + " name=" + commandValue(node.Label))
			}
		case NodeContainer:
			a.openCommand("container set " + commandValue(a.displayNodeName(node.Type, node.ID)) + " name=" + commandValue(node.Label))
		case NodeExternal:
			a.openExternalNameCommand(node.ID)
		case NodeSwitch:
			a.openSwitchNameCommand(node.ID)
		default:
			a.openCommand("vm set " + commandValue(a.displayNodeName(node.Type, node.ID)) + " name=" + commandValue(node.Label))
		}
	case "name":
		switch node.Type {
		case NodeSwitch:
			a.openSwitchNameCommand(node.ID)
		case NodeExternal:
			a.openExternalNameCommand(node.ID)
		}
	case "interface":
		if node.Type == NodeExternal {
			a.openExternalInterfaceCommand(node.ID)
		}
	case "iso":
		target := a.displayNodeName(node.Type, node.ID)
		if vm, ok := a.labVM(node.ID); ok {
			a.openCommand("vm set " + commandValue(target) + " iso=" + commandValue(vm.ISO))
		} else {
			a.openCommand("vm set " + commandValue(target) + " iso=")
		}
	case "add-nic":
		a.openAddNICCommand(node)
	case "connect":
		a.startConnectEndpoint(node)
	case "attach-uplink":
		if node.Type == NodeSwitch {
			a.attachFirstAvailableUplink(node.ID)
		}
	case "add-disk":
		a.openAddDiskCommand(node)
	case "add vm", "create-vm":
		a.openCreateVMCommand(node)
	case "add cont", "create-container":
		a.openCreateContainerCommand(node)
	case "add sw", "create-switch":
		a.openCreateSwitchCommand(node)
	case "add disk", "create-disk":
		a.openCreateDiskCommand()
	case "add uplink", "create uplink", "create external", "create-external":
		a.openCreateExternalCommand()
	case "link", "create-link":
		a.openCreateLink()
	case "delete":
		switch node.Type {
		case NodeVM:
			a.vmDelete(node.ID)
		case NodeContainer:
			a.containerDelete(node.ID)
		case NodeSwitch:
			a.switchDelete(node.ID)
		case NodeExternal:
			a.externalDelete(node.ID)
		}
	case "move":
		a.startMove(node)
	case "shell":
		a.startShell(node)
	case "vnc":
		a.startVNC(node)
	case "run":
		a.runWorkload(node.Type, node.ID)
	case "stop":
		a.stopWorkload(node.Type, node.ID)
	}
}

func (a *App) openCreateVMCommand(node Node) {
	a.vmCreate(a.nextVMID(), a.createVMArgs(node))
}

func (a *App) openCreateContainerCommand(node Node) {
	a.containerCreate(a.nextContainerID(), a.createContainerArgs(node))
}

func (a *App) openCreateSwitchCommand(node Node) {
	id := a.nextSwitchID()
	args := map[string]string{}
	if node.Type == NodeExternal {
		args["external"] = node.ID
	}
	a.switchCreate(id, args)
}

func (a *App) openCreateExternalCommand() {
	id := a.nextExternalID()
	a.externalCreate(id, map[string]string{"interface": defaultExternalInterfaceName(), "name": id, "mode": lab.ExternalModeNAT})
}

func (a *App) attachFirstAvailableUplink(switchID string) bool {
	ids := a.attachableUplinkIDs()
	switch len(ids) {
	case 0:
		a.State.Message = "no uplink available"
		return false
	case 1:
		a.switchSet(switchID, map[string]string{"external": ids[0]})
	default:
		node, ok := nodeByKey(a.Model, NodeKey(NodeSwitch, switchID))
		if !ok {
			a.State.Message = "switch not found: " + switchID
			return false
		}
		a.startConnectEndpoint(node)
	}
	return true
}

func (a *App) selectSwitchUplinkMenuItem(node Node, item string) bool {
	if strings.TrimSpace(item) == attachUplinkMenuItem {
		return a.attachFirstAvailableUplink(node.ID)
	}
	externalID, ok := switchUplinkMenuExternalID(item)
	if !ok {
		a.State.Message = "select uplink"
		return false
	}
	a.switchSet(node.ID, map[string]string{"external": externalID})
	return true
}

func (a *App) openCreateLink() {
	a.openCreateExternalCommand()
}

func (a *App) openSwitchNameCommand(id string) {
	a.State.Message = "edit name from Configuration"
}

func (a *App) openExternalNameCommand(id string) {
	a.State.Message = "edit name from Configuration"
}

func (a *App) openExternalInterfaceCommand(id string) {
	a.State.Message = "choose interface from Configuration"
}

func (a *App) openExternalModeCommand(id string) {
	a.State.Message = "choose uplink mode from Configuration"
}

func (a *App) openExternalSwitchCommand(id string) {
	switchID := a.switchForExternal(id)
	if switchID == "" {
		switchID = a.firstSwitchID()
	}
	if switchID == "" {
		a.openCreateSwitchCommand(Node{ID: id, Type: NodeExternal})
		return
	}
	a.switchSet(switchID, map[string]string{"mode": "macnat-bridge", "external": id})
}

func (a *App) openAddNICCommand(node Node) {
	switch node.Type {
	case NodeVM:
		a.vmNICAdd(node.ID, nil)
	case NodeContainer:
		a.containerNICAdd(node.ID, nil)
	}
}

func (a *App) openConfigCommand(node Node) {
	a.State.Message = "edit fields from Configuration"
}
