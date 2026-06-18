package workload

import (
	"context"
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

func TestReconcilerStartsOnlyWorkloadsNeedingRun(t *testing.T) {
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
	wantStarted := []string{"vm:vm2", "container:web"}
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
}

func TestReconcilerStopsOnlyRunningWorkloadsWithStoppedDesiredState(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2"},
			{ID: "vm2", DesiredState: lab.DesiredStateStopped, MemoryMB: 512, CPUs: 1, Disk: "vm2.qcow2"},
			{ID: "vm3", DesiredState: lab.DesiredStateStopped, MemoryMB: 512, CPUs: 1, Disk: "vm3.qcow2"},
		},
	}
	runtime := &fakeRuntime{states: map[string]string{
		Key(Ref{Type: TypeVM, ID: "vm1"}): "running",
		Key(Ref{Type: TypeVM, ID: "vm2"}): "shutoff",
		Key(Ref{Type: TypeVM, ID: "vm3"}): "missing",
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
