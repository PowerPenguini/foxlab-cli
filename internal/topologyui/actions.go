package topologyui

import (
	"fmt"
	"strings"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

func (a *App) runGlobalMenuAction(action string) {
	switch action {
	case "add vm", "create-vm":
		a.openCreateVMCommand(Node{})
	case "add cont", "create-container":
		a.openCreateContainerCommand(Node{})
	case "add sw", "create-switch":
		a.openCreateSwitchCommand(Node{})
	case "create external", "create-external":
		a.openCreateExternalCommand()
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
			if vm, ok := a.labVM(node.ID); ok {
				a.openCommand("vm set " + node.ID + " name=" + commandValue(firstNonEmpty(vm.Name, vm.ID)))
			} else {
				a.openCommand("vm set " + node.ID + " name=" + commandValue(node.Label))
			}
		case NodeContainer:
			a.openCommand("container set " + node.ID + " name=" + commandValue(node.Label))
		case NodeExternal:
			a.openExternalNameCommand(node.ID)
		case NodeSwitch:
			a.openSwitchNameCommand(node.ID)
		default:
			a.openCommand("vm set " + node.ID + " name=" + commandValue(node.Label))
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
	case "disk":
		a.openDiskCommand(node.ID)
	case "iso":
		if vm, ok := a.labVM(node.ID); ok {
			a.openCommand("vm set " + node.ID + " iso=" + commandValue(vm.ISO))
		} else {
			a.openCommand("vm set " + node.ID + " iso=")
		}
	case "add-nic":
		a.openAddNICCommand(node)
	case "add vm", "create-vm":
		a.openCreateVMCommand(node)
	case "add cont", "create-container":
		a.openCreateContainerCommand(node)
	case "add sw", "create-switch":
		a.openCreateSwitchCommand(node)
	case "create external", "create-external":
		a.openCreateExternalCommand()
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
	case "run":
		a.runWorkload(node.Type, node.ID)
	case "stop":
		a.stopWorkload(node.Type, node.ID)
	}
}

func (a *App) runWorkload(typ, id string) {
	if a.Lab == nil {
		a.State.Message = "run needs a loaded .lab file"
		return
	}
	a.setWorkloadDesiredState(typ, id, lab.DesiredStateRunning)
}

func (a *App) stopWorkload(typ, id string) {
	if a.Lab == nil {
		a.State.Message = "stop needs a loaded .lab file"
		return
	}
	a.setWorkloadDesiredState(typ, id, lab.DesiredStateStopped)
}

func (a *App) setWorkloadDesiredState(typ, id, state string) {
	service := a.ensureService()
	switch typ {
	case NodeContainer:
		a.State.Message = service.ContainerDesiredState(id, state)
	case NodeVM:
		a.State.Message = service.VMDesiredState(id, state)
	default:
		a.State.Message = "desired state is available for vm and container nodes"
	}
	a.syncFromService()
}

func workloadRef(typ, id string) workload.Ref {
	switch typ {
	case NodeContainer:
		return workload.Ref{Type: workload.TypeContainer, ID: id}
	default:
		return workload.Ref{Type: workload.TypeVM, ID: id}
	}
}

func (a *App) openCreateVMCommand(node Node) {
	a.openCommand("add vm " + a.nextVMID() + a.createVMHint(node))
}

func (a *App) openCreateContainerCommand(node Node) {
	a.openCommand("add cont " + a.nextContainerID() + a.createContainerHint(node))
}

func (a *App) openCreateSwitchCommand(node Node) {
	id := a.nextSwitchID()
	cmd := "add sw " + id
	if node.Type == NodeExternal {
		cmd = "add sw " + id + " external=" + node.ID
	}
	a.openCommand(cmd)
}

func (a *App) openCreateExternalCommand() {
	id := a.nextExternalID()
	a.openCommand("external create " + id + " interface= name=" + id)
}

func (a *App) openSwitchNameCommand(id string) {
	name := id
	if sw, ok := a.labSwitch(id); ok {
		name = firstNonEmpty(sw.Name, sw.ID)
	}
	a.openCommand("switch set " + id + " name=" + commandValue(name))
}

func (a *App) openExternalNameCommand(id string) {
	name := id
	if link, ok := a.labExternal(id); ok {
		name = firstNonEmpty(link.Name, link.ID)
	}
	a.openCommand("external set " + id + " name=" + commandValue(name))
}

func (a *App) openExternalInterfaceCommand(id string) {
	if link, ok := a.labExternal(id); ok {
		a.openCommand("external set " + id + " interface=" + commandValue(link.Interface))
		return
	}
	a.openCommand("external set " + id + " interface=")
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
	a.openCommand("switch set " + switchID + " mode=macnat-bridge external=" + id)
}

func (a *App) openAddNICCommand(node Node) {
	switch node.Type {
	case NodeVM:
		a.openCommand("vm nic add " + node.ID)
	case NodeContainer:
		a.openCommand("container nic add " + node.ID)
	}
}

func (a *App) openDiskCommand(id string) {
	vm, ok := a.labVM(id)
	if !ok {
		a.openCommand("vm set " + id + " disk=")
		return
	}
	cmd := "vm set " + id + " disk=" + commandValue(vm.Disk)
	a.openCommand(cmd)
}

func (a *App) openConfigCommand(node Node) {
	switch node.Type {
	case NodeVM:
		if vm, ok := a.labVM(node.ID); ok {
			a.openCommand(fmt.Sprintf("vm set %s name=%s cpus=%d memory=%d vnc=%t", node.ID, commandValue(firstNonEmpty(vm.Name, vm.ID)), vm.CPUs, vm.MemoryMB, vm.VNC))
		} else {
			a.openCommand("vm set " + node.ID + " cpus=2 memory=2048")
		}
	case NodeSwitch:
		if sw, ok := a.labSwitch(node.ID); ok {
			cmd := fmt.Sprintf("switch set %s mode=%s", node.ID, firstNonEmpty(sw.Mode, "bridge"))
			if sw.ExternalLink != "" {
				cmd += " external=" + sw.ExternalLink
			}
			a.openCommand(cmd)
		} else {
			a.openCommand("switch set " + node.ID + " mode=bridge")
		}
	case NodeContainer:
		if ct, ok := a.labContainer(node.ID); ok {
			cmd := fmt.Sprintf("container set %s name=%s image=%s", node.ID, commandValue(firstNonEmpty(ct.Name, ct.ID)), commandValue(ct.Image))
			if len(ct.Command) > 0 {
				cmd += " command=" + commandValue(strings.Join(ct.Command, " "))
			}
			a.openCommand(cmd)
		} else {
			a.openCommand("container set " + node.ID + " image=")
		}
	case NodeExternal:
		if link, ok := a.labExternal(node.ID); ok {
			a.openCommand(fmt.Sprintf("external set %s interface=%s name=%s", node.ID, commandValue(link.Interface), commandValue(firstNonEmpty(link.Name, node.ID))))
		} else {
			a.openCommand("external set " + node.ID + " interface=")
		}
	}
}

func (a *App) vmCreate(id string, args map[string]string) {
	a.State.Message = a.ensureService().VMCreate(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) vmSet(id string, args map[string]string) {
	a.State.Message = a.ensureService().VMSet(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) vmNICAdd(id string, args map[string]string) {
	a.State.Message = a.ensureService().VMNICAdd(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) vmNICConnect(id, index string, args map[string]string) {
	a.State.Message = a.ensureService().VMNICConnect(id, index, args)
	a.syncAfterServiceMutation()
}

func (a *App) vmNICDelete(id, index string) {
	a.State.Message = a.ensureService().VMNICDelete(id, index)
	a.syncAfterServiceMutation()
}

func (a *App) vmDelete(id string) {
	a.State.Message = a.ensureService().VMDelete(id)
	a.syncAfterServiceMutation()
}

func (a *App) switchCreate(id string, args map[string]string) {
	a.State.Message = a.ensureService().SwitchCreate(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) switchSet(id string, args map[string]string) {
	a.State.Message = a.ensureService().SwitchSet(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) switchDelete(id string) {
	a.State.Message = a.ensureService().SwitchDelete(id)
	a.syncAfterServiceMutation()
}

func (a *App) externalCreate(id string, args map[string]string) {
	a.State.Message = a.ensureService().ExternalCreate(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) externalSet(id string, args map[string]string) {
	a.State.Message = a.ensureService().ExternalSet(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) externalDelete(id string) {
	a.State.Message = a.ensureService().ExternalDelete(id)
	a.syncAfterServiceMutation()
}

func (a *App) containerCreate(id string, args map[string]string) {
	a.State.Message = a.ensureService().ContainerCreate(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) containerSet(id string, args map[string]string) {
	a.State.Message = a.ensureService().ContainerSet(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) containerNICAdd(id string, args map[string]string) {
	a.State.Message = a.ensureService().ContainerNICAdd(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) containerNICConnect(id, index string, args map[string]string) {
	a.State.Message = a.ensureService().ContainerNICConnect(id, index, args)
	a.syncAfterServiceMutation()
}

func (a *App) containerNICDelete(id, index string) {
	a.State.Message = a.ensureService().ContainerNICDelete(id, index)
	a.syncAfterServiceMutation()
}

func (a *App) deleteNIC(node Node, index string) {
	switch node.Type {
	case NodeVM:
		a.vmNICDelete(node.ID, index)
	case NodeContainer:
		a.containerNICDelete(node.ID, index)
	}
}

func (a *App) nicConnectDirect(sourceType, sourceID, index, targetType, targetID string) {
	a.State.Message = a.ensureService().NICConnectDirect(sourceType, sourceID, index, targetType, targetID)
	a.syncAfterServiceMutation()
}

func (a *App) nicConnectDirectTo(sourceType, sourceID, index, targetType, targetID, targetIndex string) {
	a.State.Message = a.ensureService().NICConnectDirectTo(sourceType, sourceID, index, targetType, targetID, targetIndex)
	a.syncAfterServiceMutation()
}

func (a *App) nicDisconnect(sourceType, sourceID, index string) bool {
	a.State.Message = a.ensureService().NICDisconnect(sourceType, sourceID, index)
	a.syncAfterServiceMutation()
	return strings.HasPrefix(a.State.Message, "disconnected nic")
}

func (a *App) containerDelete(id string) {
	a.State.Message = a.ensureService().ContainerDelete(id)
	a.syncAfterServiceMutation()
}

func (a *App) syncAfterServiceMutation() {
	a.syncFromService()
	if a.State.Selected >= len(a.Model.Nodes) {
		a.State.Selected = max(0, len(a.Model.Nodes)-1)
	}
}

func (a *App) saveAndRefresh() error {
	service := a.ensureService()
	if err := service.SaveAndRefresh(); err != nil {
		return err
	}
	a.syncFromService()
	if a.State.Selected >= len(a.Model.Nodes) {
		a.State.Selected = max(0, len(a.Model.Nodes)-1)
	}
	return nil
}

func (a *App) hasVM(id string) bool {
	return a.ensureService().HasVM(id)
}

func (a *App) hasLabVM(id string) bool {
	return a.ensureService().HasLabVM(id)
}

func (a *App) labVM(id string) (lab.VM, bool) {
	return a.ensureService().LabVM(id)
}

func (a *App) hasLabSwitch(id string) bool {
	return a.ensureService().HasLabSwitch(id)
}

func (a *App) labSwitch(id string) (lab.Switch, bool) {
	return a.ensureService().LabSwitch(id)
}

func (a *App) hasLabExternal(id string) bool {
	return a.ensureService().HasLabExternal(id)
}

func (a *App) labExternal(id string) (lab.ExternalLink, bool) {
	return a.ensureService().LabExternal(id)
}

func (a *App) labContainer(id string) (lab.Container, bool) {
	return a.ensureService().LabContainer(id)
}

func (a *App) nextVMID() string {
	return a.ensureService().NextVMID()
}

func (a *App) nextSwitchID() string {
	return a.ensureService().NextSwitchID()
}

func (a *App) nextExternalID() string {
	return a.ensureService().NextExternalID()
}

func (a *App) nextContainerID() string {
	return a.ensureService().NextContainerID()
}

func (a *App) firstExternalID() string {
	return a.ensureService().FirstExternalID()
}

func (a *App) firstSwitchID() string {
	return a.ensureService().FirstSwitchID()
}

func (a *App) switchForExternal(id string) string {
	return a.ensureService().SwitchForExternal(id)
}

func (a *App) createVMHint(node Node) string {
	switch node.Type {
	case NodeSwitch:
		return " switch=" + node.ID
	case NodeExternal:
		return " external=" + node.ID
	default:
		if a.Lab != nil && len(a.Lab.Switches) > 0 {
			return " switch=" + a.Lab.Switches[0].ID
		}
		return ""
	}
}

func (a *App) createContainerHint(node Node) string {
	switch node.Type {
	case NodeSwitch:
		return " switch=" + node.ID
	default:
		if a.Lab != nil && len(a.Lab.Switches) > 0 {
			return " switch=" + a.Lab.Switches[0].ID
		}
		return ""
	}
}
