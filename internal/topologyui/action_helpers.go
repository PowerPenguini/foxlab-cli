package topologyui

import (
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
)

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

func (a *App) nextDHCPID() string {
	return a.ensureService().NextDHCPID()
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
		return " switch=" + commandValue(a.displayNodeName(node.Type, node.ID))
	case NodeExternal:
		return " uplink=" + commandValue(a.displayNodeName(node.Type, node.ID))
	default:
		if a.currentLab() != nil && len(a.currentLab().Switches) > 0 {
			return " switch=" + commandValue(displayNodeNameFromLab(a.currentLab(), NodeSwitch, a.currentLab().Switches[0].ID))
		}
		return ""
	}
}

func (a *App) createContainerHint(node Node) string {
	switch node.Type {
	case NodeSwitch:
		return " switch=" + commandValue(a.displayNodeName(node.Type, node.ID))
	default:
		if a.currentLab() != nil && len(a.currentLab().Switches) > 0 {
			return " switch=" + commandValue(displayNodeNameFromLab(a.currentLab(), NodeSwitch, a.currentLab().Switches[0].ID))
		}
		return ""
	}
}

func (a *App) createVMRequest(node Node) topology.VMCreateRequest {
	request := topology.VMCreateRequest{Name: a.nextVMID()}
	switch node.Type {
	case NodeSwitch:
		request.Network.Switch = node.ID
	case NodeExternal:
		request.Network.Uplink = node.ID
	}
	return request
}

func (a *App) createContainerRequest(node Node) topology.ContainerCreateRequest {
	request := topology.ContainerCreateRequest{Name: a.nextContainerID()}
	if node.Type == NodeSwitch {
		request.Network.Switch = node.ID
	}
	return request
}

func (a *App) createDHCPRequest(node Node) topology.DHCPCreateRequest {
	request := topology.DHCPCreateRequest{Name: a.nextDHCPID()}
	if node.Type == NodeSwitch {
		request.Switch = node.ID
	}
	return request
}
