package hostnet

import (
	"context"
	"os"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/macnat"
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

func TestAttachContainerPlansExternalBridgeVethCommands(t *testing.T) {
	l := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "br0"}},
		Containers:    []lab.Container{{ID: "web", Image: "nginx", Networks: []lab.ContainerNetwork{{ExternalLink: "uplink1"}}}},
	}
	runner := &fakeRunner{}
	bridge := &Bridge{Runner: runner}

	if err := bridge.AttachContainer(context.Background(), l, l.Containers[0], 1234); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"ip link add",
		"master br0",
		"netns 1234",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command fragment %q in:\n%s", want, joined)
		}
	}
}

func TestAttachContainerPlansPhysicalExternalMacvlanCommands(t *testing.T) {
	l := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0"}},
		Containers:    []lab.Container{{ID: "web", Image: "nginx", Networks: []lab.ContainerNetwork{{ExternalLink: "uplink1", MAC: "02:00:00:00:00:10"}}}},
	}
	runner := &fakeRunner{}
	bridge := &Bridge{Runner: runner}

	if err := bridge.AttachContainer(context.Background(), l, l.Containers[0], 1234); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"ip link add link eth0 name",
		"type macvlan mode bridge",
		"netns 1234",
		"address 02:00:00:00:00:10",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command fragment %q in:\n%s", want, joined)
		}
	}
}

func TestAttachContainerPlansNATExternalCommands(t *testing.T) {
	l := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0", Mode: lab.ExternalModeNAT}},
		Containers:    []lab.Container{{ID: "web", Image: "nginx", Networks: []lab.ContainerNetwork{{ExternalLink: "uplink1"}}}},
	}
	runner := &fakeRunner{}
	bridge := &Bridge{Runner: runner}

	if err := bridge.AttachContainer(context.Background(), l, l.Containers[0], 1234); err != nil {
		t.Fatal(err)
	}

	managed := l.ManagedExternalBridgeName(l.ExternalLinks[0])
	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"ip link show " + managed,
		"ip addr replace 10.250.",
		"sysctl -w net.ipv4.ip_forward=1",
		"iptables -C FORWARD -i " + managed + " -o eth0 -j ACCEPT",
		"iptables -C FORWARD -i eth0 -o " + managed + " -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT",
		"iptables -t nat -C POSTROUTING -s 10.250.",
		"nsenter -t 1234 -n ip link delete eth0",
		"master " + managed,
		"nsenter -t 1234 -n ip addr replace 10.250.",
		"nsenter -t 1234 -n ip route replace default via 10.250.",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command fragment %q in:\n%s", want, joined)
		}
	}
}

func TestAttachContainerPlansMacNATExternalCommands(t *testing.T) {
	device, err := os.CreateTemp(t.TempDir(), "macnat")
	if err != nil {
		t.Fatal(err)
	}
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}
	l := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0", Mode: lab.ExternalModeMacNAT}},
		Containers:    []lab.Container{{ID: "web", Image: "nginx", Networks: []lab.ContainerNetwork{{ExternalLink: "uplink1"}}}},
	}
	runner := &fakeRunner{}
	ctrl := macnat.NewController(device.Name())
	bridge := &Bridge{Runner: runner, MacNAT: &ctrl}

	if err := bridge.AttachContainer(context.Background(), l, l.Containers[0], 1234); err != nil {
		t.Fatal(err)
	}

	managed := l.ManagedExternalBridgeName(l.ExternalLinks[0])
	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"ip link show " + managed,
		"master " + managed,
		"address " + l.GeneratedNICMAC("container", "web", 0),
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command fragment %q in:\n%s", want, joined)
		}
	}
	data, err := os.ReadFile(device.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"configure labID=demo switchID=external-uplink1 bridge=" + managed + " uplink=eth0",
		"mac=" + l.GeneratedNICMAC("container", "web", 0),
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("expected macnat command fragment %q in:\n%s", want, string(data))
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
