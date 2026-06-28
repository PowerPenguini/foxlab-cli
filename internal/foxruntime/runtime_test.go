package foxruntime

import (
	"context"
	"errors"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

func TestNewSkipsLibvirtForEmptyLab(t *testing.T) {
	runtime, err := New("invalid:///uri", "", &lab.Lab{ID: "demo"})
	if err != nil {
		t.Fatalf("New returned error for empty lab: %v", err)
	}
	if runtime.VM != nil || runtime.Container != nil {
		t.Fatalf("runtime = %#v, want no backends for empty lab", runtime)
	}
}

func TestNewSkipsLibvirtForContainerOnlyLab(t *testing.T) {
	runtime, err := New("invalid:///uri", "", &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest"}},
	})
	if err != nil {
		t.Fatalf("New returned error for container-only lab: %v", err)
	}
	if runtime.VM != nil {
		t.Fatalf("VM runtime configured for container-only lab: %#v", runtime.VM)
	}
	if runtime.Container == nil {
		t.Fatal("container runtime not configured")
	}
}

func TestNewDefersLibvirtErrorToVMRuntimeForVMLab(t *testing.T) {
	runtime, err := New("invalid:///uri", "", &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2"}},
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if runtime.VM == nil {
		t.Fatal("VM runtime not configured")
	}
	_, err = runtime.VM.States(context.Background(), &lab.Lab{})
	if err == nil || !strings.Contains(err.Error(), "libvirt states unavailable") {
		t.Fatalf("VM runtime States error = %v, want libvirt unavailable error", err)
	}
}

func TestFailingRuntimeDefersLibvirtErrorToVNCPortRuntime(t *testing.T) {
	libvirtErr := errors.New("connect failed")
	runtime := workload.Composite{
		VM: failingRuntime{kind: "libvirt", err: libvirtErr},
	}
	_, err := runtime.VNCPorts(context.Background(), &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2", VNC: true}},
	})
	if err == nil || !strings.Contains(err.Error(), "libvirt vnc ports unavailable") {
		t.Fatalf("VNCPorts error = %v, want libvirt unavailable error", err)
	}
	if !errors.Is(err, libvirtErr) {
		t.Fatalf("VNCPorts error = %v, want wrapped libvirt error", err)
	}
}

func TestNewKeepsContainerRuntimeWhenLibvirtFailsForMixedLab(t *testing.T) {
	runtime, err := New("invalid:///uri", "", &lab.Lab{
		ID:         "demo",
		VMs:        []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2"}},
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest"}},
	})
	if err != nil {
		t.Fatalf("New returned error for mixed lab: %v", err)
	}
	if runtime.VM == nil {
		t.Fatal("VM runtime not configured")
	}
	if runtime.Container == nil {
		t.Fatal("container runtime not configured after libvirt failure")
	}
}
