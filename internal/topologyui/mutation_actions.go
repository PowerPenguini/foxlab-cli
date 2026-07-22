package topologyui

import "foxlab-cli/internal/topology"

func (a *App) runTopologyMutation(run func(*topology.Service) topology.Result) topology.Result {
	revision := a.ensureSession().Revision()
	result := run(a.ensureService())
	a.setOperationResult(result)
	a.refreshAfterSessionRevision(revision)
	return result
}

func (a *App) refreshAfterSessionRevision(revision uint64) bool {
	if a.ensureSession().Revision() == revision {
		return false
	}
	a.refreshModelAfterMutation()
	return true
}

func (a *App) refreshModelAfterMutation() {
	a.refreshModelFromSession()
	if a.State.Selected >= len(a.Model.Nodes) {
		a.State.Selected = max(0, len(a.Model.Nodes)-1)
	}
}
