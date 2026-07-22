package topologyui

import "foxlab-cli/internal/topology"

func (a *App) vmCreate(request topology.VMCreateRequest) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.CreateVM(request)
	})
}

func (a *App) vmSet(id string, update topology.VMUpdate) {
	result := a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.UpdateVM(id, update)
	})
	if result.Changed && update.VNC.Set && a.shouldRefreshRuntimeAfterMutation() {
		a.refreshWorkloadStates()
	}
}

func (a *App) vmNICAdd(id string, request topology.NICAddRequest) topology.Result {
	return a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.AddVMNIC(id, request)
	})
}

func (a *App) vmNICConnect(id string, request topology.NICConnectRequest) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.ConnectVMNIC(id, request)
	})
}

func (a *App) vmNICDelete(id string, index int) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.DeleteVMNIC(id, index)
	})
}

func (a *App) vmDelete(id string) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.VMDelete(id)
	})
}

func (a *App) switchCreate(request topology.SwitchCreateRequest) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.CreateSwitch(request)
	})
}

func (a *App) switchSet(id string, update topology.SwitchUpdate) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.UpdateSwitch(id, update)
	})
}

func (a *App) switchDisconnectExternal(id, externalID string) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.SwitchDisconnectExternal(id, externalID)
	})
}

func (a *App) switchDelete(id string) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.SwitchDelete(id)
	})
}

func (a *App) externalCreate(request topology.ExternalCreateRequest) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.CreateExternal(request)
	})
}

func (a *App) externalSet(id string, update topology.ExternalUpdate) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.UpdateExternal(id, update)
	})
}

func (a *App) externalDelete(id string) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.ExternalDelete(id)
	})
}

func (a *App) containerCreate(request topology.ContainerCreateRequest) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.CreateContainer(request)
	})
}

func (a *App) containerSet(id string, update topology.ContainerUpdate) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.UpdateContainer(id, update)
	})
}

func (a *App) containerCapabilitySet(id, capability string, enabled bool) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.ContainerCapabilitySet(id, capability, enabled)
	})
}

func (a *App) containerNICAdd(id string, request topology.NICAddRequest) topology.Result {
	return a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.AddContainerNIC(id, request)
	})
}

func (a *App) containerNICConnect(id string, request topology.NICConnectRequest) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.ConnectContainerNIC(id, request)
	})
}

func (a *App) containerNICDelete(id string, index int) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.DeleteContainerNIC(id, index)
	})
}

func (a *App) deleteNIC(node Node, indexValue string) {
	index, ok := parseNICIndex(indexValue)
	if !ok {
		a.State.Message = "nic not found: " + a.displayNodeName(node.Type, node.ID) + ":" + indexValue
		return
	}
	switch node.Type {
	case NodeVM:
		a.vmNICDelete(node.ID, index)
	case NodeContainer:
		a.containerNICDelete(node.ID, index)
	}
}

func (a *App) nicConnectDirect(source, target topology.NetworkEndpointRef) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.ConnectDirectNIC(source, target)
	})
}

func (a *App) nicDisconnect(source topology.NetworkEndpointRef) bool {
	result := a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.DisconnectNIC(source)
	})
	return result.OK()
}

func (a *App) containerDelete(id string) {
	a.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.ContainerDelete(id)
	})
}

func (a *App) saveAndRefresh() error {
	revision := a.ensureSession().Revision()
	service := a.ensureService()
	err := service.SaveAndRefresh()
	a.refreshAfterSessionRevision(revision)
	return err
}
