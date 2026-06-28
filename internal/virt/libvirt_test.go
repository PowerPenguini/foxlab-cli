package virt

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"foxlab-cli/internal/hostnet"
	"foxlab-cli/internal/lab"
)

type recordingRunner struct {
	commands []string
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	r.commands = append(r.commands, strings.Join(append([]string{name}, args...), " "))
	return nil
}

func TestStopMissingVMDetachesHostNICs(t *testing.T) {
	l := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "vm1.qcow2",
			Networks: []lab.VMNetwork{{Switch: "lan"}},
		}},
	}
	l.Normalize()
	runner := &recordingRunner{}
	runtime := &LibvirtRuntime{Bridge: &hostnet.Bridge{Runner: runner}}

	if err := runtime.stopMissingVM(context.Background(), l, l.VMs[0]); err != nil {
		t.Fatalf("stopMissingVM returned error: %v", err)
	}

	want := []string{"ip link delete " + hostnet.VMTapName(l, l.VMs[0], 0)}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}
