package topologyui

import (
	"strings"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

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
	resolvedID := id
	if value, ok := service.ResolveWorkloadID(typ, id); ok {
		resolvedID = value
	}
	switch typ {
	case NodeContainer:
		a.State.Message = service.ContainerDesiredState(resolvedID, state)
	case NodeVM:
		a.State.Message = service.VMDesiredState(resolvedID, state)
	default:
		a.State.Message = "desired state is available for vm and container nodes"
	}
	message := a.State.Message
	a.syncFromService()
	if strings.HasPrefix(message, "desired ") {
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
	return a.Runtime != nil || a.WorkloadStates != nil || a.VNCPorts != nil
}
