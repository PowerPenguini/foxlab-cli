package topologyui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

func TestNewAppBuildsCompositionRoot(t *testing.T) {
	loaded := &lab.Lab{ID: "demo"}
	runtime := &fakeVMRuntime{}
	factoryCalls := 0
	app := NewApp(Model{}, loaded, AppConfig{
		LabPath:               "/tmp/demo.lab",
		LibvirtURI:            "qemu:///system",
		ContainerdAddress:     "/tmp/containerd.sock",
		VNCViewer:             "remote-viewer",
		StatusSocket:          "/tmp/foxlabd.sock",
		StatusRefreshInterval: 2 * time.Second,
	}, AppDeps{
		RuntimeFactory: func(got *lab.Lab) (workload.Runtime, func(), error) {
			factoryCalls++
			if got != loaded {
				t.Fatalf("runtime lab = %p, want %p", got, loaded)
			}
			return runtime, func() {}, nil
		},
	})

	if app.Lab != loaded || app.Service == nil || app.Service.Lab != loaded || app.Service.Path != "/tmp/demo.lab" {
		t.Fatalf("composition root did not wire lab service: %#v", app)
	}
	if app.State.Focus != FocusGraph || app.LibvirtURI != "qemu:///system" || app.ContainerdAddress != "/tmp/containerd.sock" {
		t.Fatalf("composition root config = %#v", app)
	}
	if app.runtimeAccess == nil || app.runtimeAccess.statusSocket != "/tmp/foxlabd.sock" {
		t.Fatalf("runtime access config = %#v", app.runtimeAccess)
	}
	gotRuntime, closeRuntime, err := app.runtimeClient().open(app.Lab)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRuntime()
	if gotRuntime != runtime || factoryCalls != 1 {
		t.Fatalf("runtime = %p, calls = %d", gotRuntime, factoryCalls)
	}
}

func TestRuntimeAccessRequiresFactory(t *testing.T) {
	_, closeRuntime, err := (&App{}).runtimeClient().open(&lab.Lab{ID: "demo"})
	closeRuntime()
	if err == nil || err.Error() != "runtime factory is not configured" {
		t.Fatalf("runtime access error = %v", err)
	}
}

func TestManagedTerminalSessionReleasesRuntimeOnce(t *testing.T) {
	releases := 0
	runtime := &fakeVMRuntime{}
	app := &App{
		Lab: &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1"}}},
		runtimeAccess: newRuntimeAccess(func(*lab.Lab) (workload.Runtime, func(), error) {
			return runtime, func() { releases++ }, nil
		}, "", nil),
	}
	opened, err := app.openTerminalSession(context.Background(), workload.Ref{Type: workload.TypeVM, ID: "vm1"}, workload.TerminalSize{})
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.Session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := opened.Session.Close(); err != nil {
		t.Fatal(err)
	}
	if releases != 1 {
		t.Fatalf("runtime releases = %d, want 1", releases)
	}
}

func TestOpenTerminalSessionReleasesRuntimeOnBackendFailure(t *testing.T) {
	releases := 0
	wantErr := errors.New("console unavailable")
	runtime := &fakeVMRuntime{openTerminal: func(context.Context, *lab.Lab, workload.Ref, workload.TerminalSize) (workload.OpenedTerminalSession, error) {
		return workload.OpenedTerminalSession{}, wantErr
	}}
	app := &App{
		Lab: &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1"}}},
		runtimeAccess: newRuntimeAccess(func(*lab.Lab) (workload.Runtime, func(), error) {
			return runtime, func() { releases++ }, nil
		}, "", nil),
	}
	_, err := app.openTerminalSession(context.Background(), workload.Ref{Type: workload.TypeVM, ID: "vm1"}, workload.TerminalSize{})
	if !errors.Is(err, wantErr) || releases != 1 {
		t.Fatalf("open error = %v, releases = %d", err, releases)
	}
}

func TestOpenTerminalSessionRejectsEmptySessionAndReleasesRuntime(t *testing.T) {
	releases := 0
	runtime := &fakeVMRuntime{openTerminal: func(context.Context, *lab.Lab, workload.Ref, workload.TerminalSize) (workload.OpenedTerminalSession, error) {
		return workload.OpenedTerminalSession{Endpoint: "vm1"}, nil
	}}
	app := &App{
		Lab: &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1"}}},
		runtimeAccess: newRuntimeAccess(func(*lab.Lab) (workload.Runtime, func(), error) {
			return runtime, func() { releases++ }, nil
		}, "", nil),
	}
	_, err := app.openTerminalSession(context.Background(), workload.Ref{Type: workload.TypeVM, ID: "vm1"}, workload.TerminalSize{})
	if err == nil || !strings.Contains(err.Error(), "empty terminal session") || releases != 1 {
		t.Fatalf("open error = %v, releases = %d", err, releases)
	}
}
