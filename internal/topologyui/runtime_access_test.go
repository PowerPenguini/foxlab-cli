package topologyui

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

func TestRuntimeAccessReadStatusPrefersMatchingDaemonSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	key := NodeKey(NodeVM, "vm1")
	factoryCalls := 0
	access := newRuntimeAccess(
		func(*lab.Lab) (workload.Runtime, func(), error) {
			factoryCalls++
			return &fakeVMRuntime{}, func() {}, nil
		},
		"/tmp/foxlabd-test.sock",
		func(_ context.Context, socket string) (daemonstatus.Snapshot, error) {
			if socket != "/tmp/foxlabd-test.sock" {
				t.Fatalf("status socket = %q", socket)
			}
			return daemonstatus.Snapshot{
				LabPath:  path,
				States:   map[string]string{key: " Running "},
				VNCPorts: map[string]int{key: 5901},
				Actions:  []string{"started vm:vm1"},
			}, nil
		},
	)

	snapshot := access.readStatus(context.Background(), &lab.Lab{ID: "demo"}, path)

	if snapshot.source != runtimeSnapshotDaemon || factoryCalls != 0 {
		t.Fatalf("source = %v, factory calls = %d", snapshot.source, factoryCalls)
	}
	if snapshot.states[key] != "running" || snapshot.vncPorts[key] != 5901 || !snapshot.statesConfirmed {
		t.Fatalf("daemon snapshot = %#v", snapshot)
	}
	if snapshot.applyStatus == nil || !snapshot.applyStatus.Active || !sameLabPath(snapshot.applyStatus.LabPath, path) {
		t.Fatalf("apply status = %#v", snapshot.applyStatus)
	}
}

func TestRuntimeAccessReadStatusFallsBackForAnotherLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	key := NodeKey(NodeContainer, "web")
	releases := 0
	access := newRuntimeAccess(
		func(*lab.Lab) (workload.Runtime, func(), error) {
			return &fakeVMRuntime{states: map[string]string{key: "running"}}, func() { releases++ }, nil
		},
		"",
		func(context.Context, string) (daemonstatus.Snapshot, error) {
			return daemonstatus.Snapshot{LabPath: filepath.Join(t.TempDir(), "other.lab")}, nil
		},
	)

	snapshot := access.readStatus(context.Background(), &lab.Lab{ID: "demo"}, path)

	if snapshot.source != runtimeSnapshotDirect || snapshot.states[key] != "running" {
		t.Fatalf("fallback snapshot = %#v", snapshot)
	}
	if releases != 1 {
		t.Fatalf("runtime releases = %d, want 1", releases)
	}
}

func TestRuntimeAccessKeepsVNCPortsWhenStatesFail(t *testing.T) {
	key := NodeKey(NodeVM, "vm1")
	wantErr := errors.New("libvirt unavailable")
	access := testRuntimeAccess(&fakeVMRuntime{
		statesErr: wantErr,
		vncPorts:  map[string]int{key: 5902},
	})

	snapshot := access.readLiveStatus(context.Background(), &lab.Lab{ID: "demo"}, liveStatusOptions{includeVNC: true})

	if !errors.Is(snapshot.statesErr, wantErr) {
		t.Fatalf("states error = %v", snapshot.statesErr)
	}
	if !snapshot.vncReceived || snapshot.vncPorts[key] != 5902 {
		t.Fatalf("VNC snapshot = %#v", snapshot)
	}
}

func TestRuntimeAccessReportsFactoryFailure(t *testing.T) {
	wantErr := errors.New("containerd unavailable")
	access := newRuntimeAccess(func(*lab.Lab) (workload.Runtime, func(), error) {
		return nil, func() {}, wantErr
	}, "", func(context.Context, string) (daemonstatus.Snapshot, error) {
		return daemonstatus.Snapshot{}, errors.New("daemon unavailable")
	})

	snapshot := access.readStatus(context.Background(), &lab.Lab{ID: "demo"}, "/tmp/demo.lab")

	if !errors.Is(snapshot.runtimeErr, wantErr) || snapshot.statesReceived {
		t.Fatalf("factory failure snapshot = %#v", snapshot)
	}
}

func TestRuntimeAccessRejectsNilRuntimeAndReleasesFactory(t *testing.T) {
	releases := 0
	access := newRuntimeAccess(func(*lab.Lab) (workload.Runtime, func(), error) {
		return nil, func() { releases++ }, nil
	}, "", nil)

	snapshot := access.readLiveStatus(context.Background(), &lab.Lab{ID: "demo"}, liveStatusOptions{})

	if snapshot.runtimeErr == nil || snapshot.runtimeErr.Error() != "runtime factory returned nil runtime" {
		t.Fatalf("runtime error = %v", snapshot.runtimeErr)
	}
	if releases != 1 {
		t.Fatalf("runtime releases = %d, want 1", releases)
	}
}
