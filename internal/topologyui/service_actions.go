package topologyui

import "foxlab-cli/internal/topology"

func (a *App) vmCreate(request topology.VMCreateRequest) {
	a.setOperationResult(a.ensureService().CreateVM(request))
	a.syncAfterServiceMutation()
}

func (a *App) vmSet(id string, update topology.VMUpdate) {
	a.setOperationResult(a.ensureService().UpdateVM(id, update))
	a.syncAfterServiceMutation()
	if update.VNC.Set && a.shouldRefreshRuntimeAfterMutation() {
		a.refreshWorkloadStates()
	}
}

func (a *App) vmNICAdd(id string, args map[string]string) topology.Result {
	result := a.ensureService().VMNICAdd(id, args)
	a.setOperationResult(result)
	a.syncAfterServiceMutation()
	return result
}

func (a *App) vmNICConnect(id, index string, args map[string]string) {
	a.setOperationResult(a.ensureService().VMNICConnect(id, index, args))
	a.syncAfterServiceMutation()
}

func (a *App) vmNICDelete(id, index string) {
	a.setOperationResult(a.ensureService().VMNICDelete(id, index))
	a.syncAfterServiceMutation()
}

func (a *App) vmDelete(id string) {
	a.setOperationResult(a.ensureService().VMDelete(id))
	a.syncAfterServiceMutation()
}

func (a *App) switchCreate(id string, args map[string]string) {
	a.setOperationResult(a.ensureService().SwitchCreate(id, args))
	a.syncAfterServiceMutation()
}

func (a *App) switchSet(id string, args map[string]string) {
	a.setOperationResult(a.ensureService().SwitchSet(id, args))
	a.syncAfterServiceMutation()
}

func (a *App) switchDisconnectExternal(id, externalID string) {
	a.setOperationResult(a.ensureService().SwitchDisconnectExternal(id, externalID))
	a.syncAfterServiceMutation()
}

func (a *App) switchDelete(id string) {
	a.setOperationResult(a.ensureService().SwitchDelete(id))
	a.syncAfterServiceMutation()
}

func (a *App) externalCreate(id string, args map[string]string) {
	a.setOperationResult(a.ensureService().ExternalCreate(id, args))
	a.syncAfterServiceMutation()
}

func (a *App) externalSet(id string, args map[string]string) {
	a.setOperationResult(a.ensureService().ExternalSet(id, args))
	a.syncAfterServiceMutation()
}

func (a *App) externalDelete(id string) {
	a.setOperationResult(a.ensureService().ExternalDelete(id))
	a.syncAfterServiceMutation()
}

func (a *App) containerCreate(request topology.ContainerCreateRequest) {
	a.setOperationResult(a.ensureService().CreateContainer(request))
	a.syncAfterServiceMutation()
}

func (a *App) containerSet(id string, update topology.ContainerUpdate) {
	a.setOperationResult(a.ensureService().UpdateContainer(id, update))
	a.syncAfterServiceMutation()
}

func (a *App) containerCapabilitySet(id, capability string, enabled bool) {
	a.setOperationResult(a.ensureService().ContainerCapabilitySet(id, capability, enabled))
	a.syncAfterServiceMutation()
}

func (a *App) containerNICAdd(id string, args map[string]string) topology.Result {
	result := a.ensureService().ContainerNICAdd(id, args)
	a.setOperationResult(result)
	a.syncAfterServiceMutation()
	return result
}

func (a *App) containerNICConnect(id, index string, args map[string]string) {
	a.setOperationResult(a.ensureService().ContainerNICConnect(id, index, args))
	a.syncAfterServiceMutation()
}

func (a *App) containerNICDelete(id, index string) {
	a.setOperationResult(a.ensureService().ContainerNICDelete(id, index))
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
	a.setOperationResult(a.ensureService().NICConnectDirect(sourceType, sourceID, index, targetType, targetID))
	a.syncAfterServiceMutation()
}

func (a *App) nicConnectDirectTo(sourceType, sourceID, index, targetType, targetID, targetIndex string) {
	a.setOperationResult(a.ensureService().NICConnectDirectTo(sourceType, sourceID, index, targetType, targetID, targetIndex))
	a.syncAfterServiceMutation()
}

func (a *App) nicDisconnect(sourceType, sourceID, index string) bool {
	result := a.ensureService().NICDisconnect(sourceType, sourceID, index)
	a.setOperationResult(result)
	a.syncAfterServiceMutation()
	return result.OK()
}

func (a *App) containerDelete(id string) {
	a.setOperationResult(a.ensureService().ContainerDelete(id))
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
