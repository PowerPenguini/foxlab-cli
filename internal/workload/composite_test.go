package workload

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

type stateErrorRuntime struct {
	fakeRuntime
	err error
}

func (r *stateErrorRuntime) States(ctx context.Context, l *lab.Lab) (map[string]string, error) {
	states, _ := r.fakeRuntime.States(ctx, l)
	return states, r.err
}

func TestCompositeStatesReturnsPartialStatesWithBackendError(t *testing.T) {
	vmErr := errors.New("libvirt unavailable")
	ctKey := Key(Ref{Type: TypeContainer, ID: "web"})
	composite := &Composite{
		VM: &stateErrorRuntime{err: vmErr},
		Container: &fakeRuntime{states: map[string]string{
			ctKey: "missing",
		}},
	}
	states, err := composite.States(context.Background(), &lab.Lab{
		ID:         "demo",
		VMs:        []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2"}},
		Containers: []lab.Container{{ID: "web", Image: "nginx"}},
	})

	if !errors.Is(err, vmErr) {
		t.Fatalf("States error = %v, want libvirt error", err)
	}
	if states[ctKey] != "missing" {
		t.Fatalf("container state = %q, want missing in partial states", states[ctKey])
	}
}

func TestCompositeStatesReportsMissingBackendsForPresentWorkloads(t *testing.T) {
	states, err := (&Composite{}).States(context.Background(), &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:           "vm1",
			MemoryMB:     512,
			CPUs:         1,
			DesiredState: lab.DesiredStateStopped,
		}},
		Containers: []lab.Container{{
			ID:    "web",
			Image: "nginx",
		}},
	})

	if len(states) != 0 {
		t.Fatalf("states = %#v, want empty states with missing backends", states)
	}
	for _, want := range []string{
		`runtime not configured for workload type "vm"`,
		`runtime not configured for workload type "container"`,
	} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("States error = %v, want %q", err, want)
		}
	}
}

func TestCompositeStatesAllowsNoBackendsForEmptyLab(t *testing.T) {
	states, err := (&Composite{}).States(context.Background(), &lab.Lab{ID: "demo"})
	if err != nil {
		t.Fatalf("States returned error for empty lab: %v", err)
	}
	if len(states) != 0 {
		t.Fatalf("states = %#v, want empty", states)
	}
}

type closeErrorRuntime struct {
	fakeRuntime
	err error
}

func (r *closeErrorRuntime) Close() error {
	return r.err
}

func TestCompositeCloseJoinsBackendErrors(t *testing.T) {
	vmErr := errors.New("close vm")
	containerErr := errors.New("close container")
	err := (&Composite{
		VM:        &closeErrorRuntime{err: vmErr},
		Container: &closeErrorRuntime{err: containerErr},
	}).Close()

	if !errors.Is(err, vmErr) {
		t.Fatalf("Close error = %v, want vm close error", err)
	}
	if !errors.Is(err, containerErr) {
		t.Fatalf("Close error = %v, want container close error", err)
	}
}
