package hostnet

import (
	"context"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

type fakeRunner struct {
	commands []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	f.commands = append(f.commands, name+" "+strings.Join(args, " "))
	return nil
}

func TestAttachContainerPlansBridgeAndVethCommands(t *testing.T) {
	l := &lab.Lab{
		ID:         "demo",
		Switches:   []lab.Switch{{ID: "lan", Mode: "bridge"}},
		Containers: []lab.Container{{ID: "web", Image: "nginx", Networks: []lab.ContainerNetwork{{Switch: "lan", MAC: "02:00:00:00:00:10"}}}},
	}
	runner := &fakeRunner{}
	bridge := &Bridge{Runner: runner}

	if err := bridge.AttachContainer(context.Background(), l, l.Containers[0], 1234); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"ip link add",
		"ip link set",
		"master " + l.ManagedSwitchBridgeName(l.Switches[0]),
		"netns 1234",
		"nsenter -t 1234 -n ip link set",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command fragment %q in:\n%s", want, joined)
		}
	}
}

func TestAttachVMNICsPlansBridgeAndTapCommands(t *testing.T) {
	l := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     "labs/demo/disks/vm1.qcow2",
			Networks: []lab.VMNetwork{{Switch: "lan", MAC: "02:00:00:00:00:20"}},
		}},
	}
	runner := &fakeRunner{}
	bridge := &Bridge{Runner: runner}

	if err := bridge.AttachVMNICs(context.Background(), l, l.VMs[0]); err != nil {
		t.Fatal(err)
	}

	tap := VMTapName(l, l.VMs[0], 0)
	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"ip link show " + l.ManagedSwitchBridgeName(l.Switches[0]),
		"ip link delete " + tap,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command fragment %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "ip tuntap add") || strings.Contains(joined, " master "+l.ManagedSwitchBridgeName(l.Switches[0])) {
		t.Fatalf("vm attach should let libvirt create tap interfaces:\n%s", joined)
	}
}

func TestAttachVMNICsPlansDirectLinkBridgeCommands(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2", Networks: []lab.VMNetwork{{}}},
			{ID: "vm2", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm2.qcow2", Networks: []lab.VMNetwork{{}}},
		},
		NetworkLinks: []lab.NetworkLink{{
			From: lab.NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 0},
			To:   lab.NetworkEndpoint{Type: "vm", ID: "vm2", NIC: 0},
		}},
	}
	runner := &fakeRunner{}
	bridge := &Bridge{Runner: runner}

	if err := bridge.AttachVMNICs(context.Background(), l, l.VMs[0]); err != nil {
		t.Fatal(err)
	}

	tap := VMTapName(l, l.VMs[0], 0)
	linkBridge := l.ManagedNetworkLinkBridgeName(l.NetworkLinks[0])
	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"ip link show " + linkBridge,
		"ip link delete " + tap,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command fragment %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "ip tuntap add") || strings.Contains(joined, " master "+linkBridge) {
		t.Fatalf("vm attach should let libvirt create tap interfaces:\n%s", joined)
	}
}
