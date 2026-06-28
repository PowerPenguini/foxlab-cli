package workload

import (
	"context"
	"errors"
	"testing"

	"foxlab-cli/internal/lab"
)

type fakeRuntime struct {
	states  map[string]string
	started []string
	stopped []string
}

func (f *fakeRuntime) States(context.Context, *lab.Lab) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range f.states {
		out[key] = value
	}
	return out, nil
}

func (f *fakeRuntime) Start(_ context.Context, _ *lab.Lab, ref Ref) error {
	f.started = append(f.started, Key(ref))
	return nil
}

func (f *fakeRuntime) Stop(_ context.Context, _ *lab.Lab, ref Ref) error {
	f.stopped = append(f.stopped, Key(ref))
	return nil
}

func (f *fakeRuntime) Close() error { return nil }

type nilStatesRuntime struct {
	fakeRuntime
}

func (f *nilStatesRuntime) States(context.Context, *lab.Lab) (map[string]string, error) {
	return nil, nil
}

type directStateRuntime struct {
	fakeRuntime
}

func (f *directStateRuntime) States(context.Context, *lab.Lab) (map[string]string, error) {
	return f.states, nil
}

type cancelingRuntime struct {
	fakeRuntime
	cancel context.CancelFunc
}

func (f *cancelingRuntime) States(context.Context, *lab.Lab) (map[string]string, error) {
	f.cancel()
	return map[string]string{
		Key(Ref{Type: TypeContainer, ID: "web"}): "missing",
	}, nil
}

func TestReconcilerStartsDesiredRunningWorkloads(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", DesiredState: lab.DesiredStateRunning, MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2"},
			{ID: "vm2", DesiredState: lab.DesiredStateRunning, MemoryMB: 512, CPUs: 1, Disk: "vm2.qcow2"},
		},
		Containers: []lab.Container{{ID: "web", DesiredState: lab.DesiredStateRunning, Image: "nginx"}},
	}
	runtime := &fakeRuntime{states: map[string]string{
		Key(Ref{Type: TypeVM, ID: "vm1"}):        "running",
		Key(Ref{Type: TypeVM, ID: "vm2"}):        "shutoff",
		Key(Ref{Type: TypeContainer, ID: "web"}): "missing",
	}}

	result := (&Reconciler{Runtime: runtime}).Step(context.Background(), l)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected reconcile errors: %v", result.Errors)
	}
	wantStarted := []string{"vm:vm1", "vm:vm2", "container:web"}
	if len(runtime.started) != len(wantStarted) {
		t.Fatalf("started = %#v, want %#v", runtime.started, wantStarted)
	}
	for i, want := range wantStarted {
		if runtime.started[i] != want {
			t.Fatalf("started[%d] = %q, want %q", i, runtime.started[i], want)
		}
	}
	if len(runtime.stopped) != 0 {
		t.Fatalf("stopped = %#v, want none", runtime.stopped)
	}
	wantActions := []string{"started vm:vm2", "started container:web"}
	if len(result.Actions) != len(wantActions) {
		t.Fatalf("actions = %#v, want %#v", result.Actions, wantActions)
	}
	for i, want := range wantActions {
		if result.Actions[i] != want {
			t.Fatalf("actions[%d] = %q, want %q", i, result.Actions[i], want)
		}
	}
}

func TestReconcilerCallsStartForAlreadyRunningDesiredWorkload(t *testing.T) {
	l := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", DesiredState: lab.DesiredStateRunning, Image: "nginx"}},
	}
	runtime := &fakeRuntime{states: map[string]string{
		Key(Ref{Type: TypeContainer, ID: "web"}): "running",
	}}

	result := (&Reconciler{Runtime: runtime}).Step(context.Background(), l)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected reconcile errors: %v", result.Errors)
	}
	if len(runtime.started) != 1 || runtime.started[0] != "container:web" {
		t.Fatalf("started = %#v, want container:web for idempotent config reconciliation", runtime.started)
	}
	if len(result.Actions) != 0 {
		t.Fatalf("actions = %#v, want no visible action for already running workload", result.Actions)
	}
}

func TestReconcilerTreatsNilStateMapAsEmpty(t *testing.T) {
	l := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", DesiredState: lab.DesiredStateRunning, Image: "nginx"}},
	}
	runtime := &nilStatesRuntime{}

	result := (&Reconciler{Runtime: runtime}).Step(context.Background(), l)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected reconcile errors: %v", result.Errors)
	}
	if len(runtime.started) != 1 || runtime.started[0] != "container:web" {
		t.Fatalf("started = %#v, want container:web", runtime.started)
	}
	if result.States[Key(Ref{Type: TypeContainer, ID: "web"})] != "running" {
		t.Fatalf("result states = %#v, want container running", result.States)
	}
	if len(result.Actions) != 1 || result.Actions[0] != "started container:web" {
		t.Fatalf("actions = %#v, want started action", result.Actions)
	}
}

func TestReconcilerContinuesWithPartialStatesAfterStateError(t *testing.T) {
	stateErr := errors.New("vm states failed")
	ctKey := Key(Ref{Type: TypeContainer, ID: "web"})
	l := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:           "vm1",
			MemoryMB:     512,
			CPUs:         1,
			Disk:         "vm1.qcow2",
			DesiredState: lab.DesiredStateStopped,
		}},
		Containers: []lab.Container{{ID: "web", DesiredState: lab.DesiredStateRunning, Image: "nginx"}},
	}
	runtime := &stateErrorRuntime{
		fakeRuntime: fakeRuntime{states: map[string]string{
			ctKey: "missing",
		}},
		err: stateErr,
	}

	result := (&Reconciler{Runtime: runtime}).Step(context.Background(), l)

	if len(result.Errors) != 1 || !errors.Is(result.Errors[0], stateErr) {
		t.Fatalf("errors = %#v, want state error", result.Errors)
	}
	if len(runtime.started) != 1 || runtime.started[0] != "container:web" {
		t.Fatalf("started = %#v, want container:web despite state error", runtime.started)
	}
	if result.States[ctKey] != "running" {
		t.Fatalf("container state = %q, want running", result.States[ctKey])
	}
	if len(result.Actions) != 1 || result.Actions[0] != "started container:web" {
		t.Fatalf("actions = %#v, want container start action", result.Actions)
	}
}

func TestReconcilerDoesNotMutateRuntimeStateMap(t *testing.T) {
	key := Key(Ref{Type: TypeContainer, ID: "web"})
	l := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", DesiredState: lab.DesiredStateRunning, Image: "nginx"}},
	}
	runtime := &directStateRuntime{
		fakeRuntime: fakeRuntime{states: map[string]string{key: "missing"}},
	}

	result := (&Reconciler{Runtime: runtime}).Step(context.Background(), l)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected reconcile errors: %v", result.Errors)
	}
	if runtime.states[key] != "missing" {
		t.Fatalf("runtime states mutated to %q, want missing", runtime.states[key])
	}
	if result.States[key] != "running" {
		t.Fatalf("result state = %q, want running", result.States[key])
	}
}

func TestReconcilerStopsOnlyRunningWorkloadsWithStoppedDesiredState(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2"},
			{ID: "vm2", DesiredState: lab.DesiredStateStopped, MemoryMB: 512, CPUs: 1, Disk: "vm2.qcow2"},
			{ID: "vm3", DesiredState: lab.DesiredStateStopped, MemoryMB: 512, CPUs: 1, Disk: "vm3.qcow2"},
		},
		Containers: []lab.Container{{ID: "web", DesiredState: lab.DesiredStateStopped, Image: "nginx"}},
	}
	runtime := &fakeRuntime{states: map[string]string{
		Key(Ref{Type: TypeVM, ID: "vm1"}):        "running",
		Key(Ref{Type: TypeVM, ID: "vm2"}):        "shutoff",
		Key(Ref{Type: TypeVM, ID: "vm3"}):        "missing",
		Key(Ref{Type: TypeContainer, ID: "web"}): "created",
	}}

	result := (&Reconciler{Runtime: runtime}).Step(context.Background(), l)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected reconcile errors: %v", result.Errors)
	}
	if len(runtime.started) != 0 {
		t.Fatalf("started = %#v, want none", runtime.started)
	}
	if len(runtime.stopped) != 1 || runtime.stopped[0] != "vm:vm1" {
		t.Fatalf("stopped = %#v, want vm:vm1", runtime.stopped)
	}
}

func TestReconcilerStopsCreatedDiskBackedContainerForCleanup(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{{
			ID:           "web",
			Image:        "nginx",
			Disk:         "layers/web.qcow2",
			DesiredState: lab.DesiredStateStopped,
		}},
	}
	runtime := &fakeRuntime{states: map[string]string{
		Key(Ref{Type: TypeContainer, ID: "web"}): "created",
	}}

	result := (&Reconciler{Runtime: runtime}).Step(context.Background(), l)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected reconcile errors: %v", result.Errors)
	}
	if len(runtime.stopped) != 1 || runtime.stopped[0] != "container:web" {
		t.Fatalf("stopped = %#v, want cleanup stop for disk-backed container", runtime.stopped)
	}
	if len(result.Actions) != 0 {
		t.Fatalf("actions = %#v, want no visible action for cleanup-only stop", result.Actions)
	}
	if got := result.States[Key(Ref{Type: TypeContainer, ID: "web"})]; got != "stopped" {
		t.Fatalf("state = %q, want stopped after cleanup", got)
	}
}

func TestReconcilerNormalizesActualStates(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", DesiredState: lab.DesiredStateRunning, MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2"},
			{ID: "vm2", DesiredState: lab.DesiredStateStopped, MemoryMB: 512, CPUs: 1, Disk: "vm2.qcow2"},
		},
	}
	runtime := &fakeRuntime{states: map[string]string{
		Key(Ref{Type: TypeVM, ID: "vm1"}): " Running ",
		Key(Ref{Type: TypeVM, ID: "vm2"}): " STOPPED ",
	}}

	result := (&Reconciler{Runtime: runtime}).Step(context.Background(), l)

	if len(result.Errors) != 0 {
		t.Fatalf("unexpected reconcile errors: %v", result.Errors)
	}
	if len(runtime.started) != 1 || runtime.started[0] != "vm:vm1" {
		t.Fatalf("started = %#v, want vm:vm1 for idempotent config reconciliation", runtime.started)
	}
	if len(runtime.stopped) != 0 {
		t.Fatalf("stopped = %#v, want none", runtime.stopped)
	}
	if len(result.Actions) != 0 {
		t.Fatalf("actions = %#v, want no visible actions for normalized states", result.Actions)
	}
	if got := result.States[Key(Ref{Type: TypeVM, ID: "vm2"})]; got != "stopped" {
		t.Fatalf("normalized state = %q, want stopped", got)
	}
}

func TestReconcilerDoesNotActAfterContextCanceledDuringStateFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &cancelingRuntime{cancel: cancel}
	l := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", DesiredState: lab.DesiredStateRunning, Image: "nginx"}},
	}

	result := (&Reconciler{Runtime: runtime}).Step(ctx, l)

	if len(runtime.started) != 0 {
		t.Fatalf("started = %#v, want none after cancellation", runtime.started)
	}
	if len(runtime.stopped) != 0 {
		t.Fatalf("stopped = %#v, want none after cancellation", runtime.stopped)
	}
	if len(result.Errors) != 1 || !errors.Is(result.Errors[0], context.Canceled) {
		t.Fatalf("errors = %#v, want context canceled", result.Errors)
	}
	if got := result.States[Key(Ref{Type: TypeContainer, ID: "web"})]; got != "missing" {
		t.Fatalf("state = %q, want missing state snapshot preserved", got)
	}
}
