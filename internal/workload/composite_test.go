package workload

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

type fakeTerminalSession struct {
	bytes.Buffer
}

func (s *fakeTerminalSession) Close() error               { return nil }
func (s *fakeTerminalSession) Resize(columns, rows int)   {}
func (s *fakeTerminalSession) Wait(context.Context) error { return nil }

type sessionRuntime struct {
	fakeRuntime
	opened Ref
	size   TerminalSize
}

func (r *sessionRuntime) OpenTerminalSession(_ context.Context, _ *lab.Lab, ref Ref, size TerminalSize) (OpenedTerminalSession, error) {
	r.opened = ref
	r.size = size
	return OpenedTerminalSession{Session: &fakeTerminalSession{}, Endpoint: ref.ID}, nil
}

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

type fileTransferRuntime struct {
	fakeRuntime
	put []string
	get []string
}

func (r *fileTransferRuntime) PutFile(_ context.Context, _ *lab.Lab, ref Ref, hostPath, guestPath string) error {
	r.put = append(r.put, Key(ref)+" "+hostPath+" "+guestPath)
	return nil
}

func (r *fileTransferRuntime) GetFile(_ context.Context, _ *lab.Lab, ref Ref, guestPath, hostPath string) error {
	r.get = append(r.get, Key(ref)+" "+guestPath+" "+hostPath)
	return nil
}

func TestCompositeFileTransferDispatchesByWorkloadType(t *testing.T) {
	vm := &fileTransferRuntime{}
	ct := &fileTransferRuntime{}
	composite := &Composite{VM: vm, Container: ct}

	if err := composite.PutFile(context.Background(), &lab.Lab{}, Ref{Type: TypeVM, ID: "vm1"}, "/host", "/guest"); err != nil {
		t.Fatalf("PutFile vm returned error: %v", err)
	}
	if err := composite.GetFile(context.Background(), &lab.Lab{}, Ref{Type: TypeContainer, ID: "web"}, "/guest", "/host"); err != nil {
		t.Fatalf("GetFile container returned error: %v", err)
	}
	if len(vm.put) != 1 || vm.put[0] != "vm:vm1 /host /guest" {
		t.Fatalf("vm put = %#v", vm.put)
	}
	if len(ct.get) != 1 || ct.get[0] != "container:web /guest /host" {
		t.Fatalf("container get = %#v", ct.get)
	}
	if len(vm.get) != 0 || len(ct.put) != 0 {
		t.Fatalf("wrong runtime used: vm get=%#v ct put=%#v", vm.get, ct.put)
	}
}

func TestCompositeFileTransferReportsUnsupportedRuntime(t *testing.T) {
	err := (&Composite{VM: &fakeRuntime{}}).PutFile(context.Background(), &lab.Lab{}, Ref{Type: TypeVM, ID: "vm1"}, "/host", "/guest")
	if err == nil || !strings.Contains(err.Error(), `file transfer not configured for workload type "vm"`) {
		t.Fatalf("PutFile error = %v, want file transfer not configured", err)
	}
}

func TestCompositeTerminalSessionDispatchesByWorkloadType(t *testing.T) {
	vm := &sessionRuntime{}
	container := &sessionRuntime{}
	composite := &Composite{VM: vm, Container: container}
	ref := Ref{Type: TypeContainer, ID: "web"}
	size := TerminalSize{Columns: 120, Rows: 40}

	opened, err := composite.OpenTerminalSession(context.Background(), &lab.Lab{}, ref, size)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Session.Close()
	if container.opened != ref || container.size != size {
		t.Fatalf("container session = %#v at %#v", container.opened, container.size)
	}
	if opened.Endpoint != "web" {
		t.Fatalf("terminal endpoint = %q, want web", opened.Endpoint)
	}
	if vm.opened != (Ref{}) {
		t.Fatalf("vm runtime unexpectedly opened %#v", vm.opened)
	}
}

func TestCompositeTerminalSessionReportsUnsupportedBackend(t *testing.T) {
	_, err := (&Composite{VM: &fakeRuntime{}}).OpenTerminalSession(
		context.Background(), &lab.Lab{}, Ref{Type: TypeVM, ID: "vm1"}, TerminalSize{},
	)
	if err == nil || !strings.Contains(err.Error(), `terminal session not configured for workload type "vm"`) {
		t.Fatalf("OpenTerminalSession error = %v", err)
	}
}
