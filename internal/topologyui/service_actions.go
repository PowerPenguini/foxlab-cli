package topologyui

import "strings"

func (a *App) vmCreate(id string, args map[string]string) {
	a.State.Message = a.ensureService().VMCreate(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) vmSet(id string, args map[string]string) {
	a.State.Message = a.ensureService().VMSet(id, args)
	a.syncAfterServiceMutation()
	if _, ok := args["vnc"]; ok && a.shouldRefreshRuntimeAfterMutation() {
		a.refreshWorkloadStates()
	}
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
	if strings.HasPrefix(a.State.Message, "connected nic to container:") {
		a.reconcileRunningWorkload(NodeContainer, id)
	}
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
