package topologyui

import (
	"context"

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

func (a *App) reconcileRunningWorkload(typ, id string) {
	if a.Runtime == nil || a.Lab == nil {
		return
	}
	if !a.labWorkloadDesiredRunning(typ, id) {
		return
	}
	ref := workloadRef(typ, id)
	if err := a.Runtime.Start(context.Background(), a.Lab, ref); err != nil {
		a.State.Message = "reconcile failed: start " + workload.Key(ref) + ": " + err.Error()
		return
	}
	a.refreshWorkloadStates()
}

func (a *App) labWorkloadDesiredRunning(typ, id string) bool {
	switch typ {
	case NodeContainer:
		ct, ok := a.labContainer(id)
		return ok && lab.DesiredState(ct.DesiredState) == lab.DesiredStateRunning
	case NodeVM:
		vm, ok := a.labVM(id)
		return ok && lab.DesiredState(vm.DesiredState) == lab.DesiredStateRunning
	default:
		return false
	}
}

func (a *App) shouldRefreshRuntimeAfterMutation() bool {
	return a.Runtime != nil || a.WorkloadStates != nil || a.VNCPorts != nil
}
