package workload

import (
	"context"
	"fmt"

	"foxlab-cli/internal/lab"
)

type ReconcileResult struct {
	States  map[string]string
	Actions []string
	Errors  []error
}

type Reconciler struct {
	Runtime Runtime
}

func (r *Reconciler) Step(ctx context.Context, l *lab.Lab) ReconcileResult {
	result := ReconcileResult{States: map[string]string{}}
	if r.Runtime == nil || l == nil {
		return result
	}
	states, err := r.Runtime.States(ctx, l)
	if err != nil {
		result.Errors = append(result.Errors, err)
		return result
	}
	result.States = states
	for _, vm := range l.VMs {
		ref := Ref{Type: TypeVM, ID: vm.ID}
		r.reconcileRef(ctx, l, ref, lab.DesiredState(vm.DesiredState), states[Key(ref)], &result)
	}
	for _, ct := range l.Containers {
		ref := Ref{Type: TypeContainer, ID: ct.ID}
		r.reconcileRef(ctx, l, ref, lab.DesiredState(ct.DesiredState), states[Key(ref)], &result)
	}
	return result
}

func (r *Reconciler) reconcileRef(ctx context.Context, l *lab.Lab, ref Ref, desired, actual string, result *ReconcileResult) {
	switch desired {
	case lab.DesiredStateRunning:
		if actual == "running" {
			return
		}
		if err := r.Runtime.Start(ctx, l, ref); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("start %s: %w", Key(ref), err))
			return
		}
		result.Actions = append(result.Actions, "started "+Key(ref))
		result.States[Key(ref)] = "running"
	case lab.DesiredStateStopped:
		if actualStopped(actual) {
			return
		}
		if err := r.Runtime.Stop(ctx, l, ref); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("stop %s: %w", Key(ref), err))
			return
		}
		result.Actions = append(result.Actions, "stopped "+Key(ref))
		result.States[Key(ref)] = "stopped"
	}
}

func actualStopped(state string) bool {
	switch state {
	case "", "missing", "shutoff", "stopped":
		return true
	default:
		return false
	}
}
