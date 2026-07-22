package topologyui

import (
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/workload"
)

func (a *App) runWorkload(typ, id string) {
	if a.currentLab() == nil {
		a.State.Message = "run needs a loaded .lab file"
		return
	}
	a.setWorkloadDesiredState(typ, id, lab.DesiredStateRunning)
}

func (a *App) stopWorkload(typ, id string) {
	if a.currentLab() == nil {
		a.State.Message = "stop needs a loaded .lab file"
		return
	}
	a.setWorkloadDesiredState(typ, id, lab.DesiredStateStopped)
}

func (a *App) setWorkloadDesiredState(typ, id, state string) {
	resolvedID := id
	result := a.runTopologyMutation(func(service *topology.Service) topology.Result {
		if value, ok := service.ResolveWorkloadID(typ, id); ok {
			resolvedID = value
		}
		switch typ {
		case NodeContainer:
			return service.ContainerDesiredState(resolvedID, state)
		case NodeVM:
			return service.VMDesiredState(resolvedID, state)
		default:
			return topology.Failure("desired state is available for vm and container nodes")
		}
	})
	message := result.Message
	if result.Changed {
		a.setPendingWorkloadStart(typ, resolvedID, state)
		a.ensureAppliedAfterDesiredState(message)
	}
}

func (a *App) setPendingWorkloadStart(typ, id, state string) {
	key := NodeKey(typ, id)
	if state != lab.DesiredStateRunning {
		delete(a.PendingStarts, key)
		if len(a.PendingStarts) == 0 {
			a.PendingStarts = nil
		}
		return
	}
	if a.PendingStarts == nil {
		a.PendingStarts = map[string]bool{}
	}
	a.PendingStarts[key] = true
}

func workloadRef(typ, id string) workload.Ref {
	switch typ {
	case NodeContainer:
		return workload.Ref{Type: workload.TypeContainer, ID: id}
	default:
		return workload.Ref{Type: workload.TypeVM, ID: id}
	}
}

func (a *App) shouldRefreshRuntimeAfterMutation() bool {
	return a.runtimeClient().configured() || a.WorkloadStates != nil || a.VNCPorts != nil
}
