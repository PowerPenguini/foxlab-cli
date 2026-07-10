package workload

import (
	"context"
	"fmt"
	"strings"

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
	}
	if states == nil {
		states = map[string]string{}
	} else {
		states = cloneStates(states)
	}
	result.States = states
	if cleaner, ok := r.Runtime.(OrphanCleaner); ok {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, err)
			return result
		}
		actions, err := cleaner.CleanupOrphans(ctx, l)
		result.Actions = append(result.Actions, actions...)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("cleanup orphans: %w", err))
		}
	}
	for _, vm := range l.VMs {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, err)
			return result
		}
		ref := Ref{Type: TypeVM, ID: vm.ID}
		r.reconcileRef(ctx, l, ref, lab.DesiredState(vm.DesiredState), states[Key(ref)], &result)
	}
	for _, ct := range l.Containers {
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, err)
			return result
		}
		ref := Ref{Type: TypeContainer, ID: ct.ID}
		r.reconcileRef(ctx, l, ref, lab.DesiredState(ct.DesiredState), states[Key(ref)], &result)
	}
	return result
}

func (r *Reconciler) reconcileRef(ctx context.Context, l *lab.Lab, ref Ref, desired, actual string, result *ReconcileResult) {
	actual = normalizeActualState(actual)
	switch desired {
	case lab.DesiredStateRunning:
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, err)
			return
		}
		outcome := StartOutcome{}
		var err error
		if outcomeRuntime, ok := r.Runtime.(StartOutcomeRuntime); ok {
			outcome, err = outcomeRuntime.StartWithOutcome(ctx, l, ref)
		} else {
			err = r.Runtime.Start(ctx, l, ref)
		}
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("start %s: %w", Key(ref), err))
			return
		}
		if outcome.Action != "" {
			result.Actions = append(result.Actions, outcome.Action)
		} else if actual != "running" {
			result.Actions = append(result.Actions, "started "+Key(ref))
		}
		result.States[Key(ref)] = "running"
	case lab.DesiredStateStopped:
		cleanup := stoppedCleanupNeeded(l, ref, actual)
		if actualStopped(actual) && !cleanup {
			return
		}
		if err := ctx.Err(); err != nil {
			result.Errors = append(result.Errors, err)
			return
		}
		if err := r.Runtime.Stop(ctx, l, ref); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("stop %s: %w", Key(ref), err))
			return
		}
		if !actualStopped(actual) {
			result.Actions = append(result.Actions, "stopped "+Key(ref))
		}
		result.States[Key(ref)] = "stopped"
	}
}

func stoppedCleanupNeeded(l *lab.Lab, ref Ref, actual string) bool {
	if normalizeActualState(actual) != "created" || ref.Type != TypeContainer || l == nil {
		return false
	}
	for _, ct := range l.Containers {
		if ct.ID == ref.ID {
			return strings.TrimSpace(ct.Disk) != ""
		}
	}
	return false
}

func actualStopped(state string) bool {
	switch normalizeActualState(state) {
	case "", "created", "missing", "shutoff", "stopped":
		return true
	default:
		return false
	}
}

func normalizeActualState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}

func cloneStates(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = normalizeActualState(value)
	}
	return out
}
