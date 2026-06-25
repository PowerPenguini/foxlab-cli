package topologyui

func (a *App) setContextGroup(group string, node Node, ok bool) {
	a.State.ContextGroup = group
	a.State.clearContextRowState()
	if group == "disk-menu" && ok {
		a.State.DiskMenuItems = a.diskMenuItems(node)
		a.State.DiskMenuActions = a.diskMenuActions(node)
		a.State.DiskMenuKinds = a.diskMenuKinds(node)
		return
	}
	a.State.clearContextMenuCache()
}
