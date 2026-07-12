package hostnet

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/macnat"
)

type fakeRunner struct {
	commands      []string
	failAt        string
	existingLinks map[string]bool
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	command := name + " " + strings.Join(args, " ")
	f.commands = append(f.commands, command)
	if strings.HasPrefix(command, "nsenter ") {
		tokens := strings.Fields(command)
		if len(tokens) >= 2 && tokens[len(tokens)-2] == "show" {
			if !f.existingLinks[tokens[len(tokens)-1]] {
				return fmt.Errorf("missing link: %s", tokens[len(tokens)-1])
			}
		}
	}
	if f.failAt != "" && strings.Contains(command, f.failAt) {
		return fmt.Errorf("forced failure: %s", command)
	}
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

func TestAttachContainerDoesNotRecreateExistingContainerNIC(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		ExternalLinks: []lab.ExternalLink{
			{ID: "uplink1", Interface: "eth0", Mode: lab.ExternalModeNAT},
		},
		Switches:   []lab.Switch{{ID: "lan", Mode: "nat", ExternalLinks: []string{"uplink1"}}},
		Containers: []lab.Container{{ID: "web", Image: "nginx", Networks: []lab.ContainerNetwork{{Switch: "lan"}}}},
	}
	runner := &fakeRunner{existingLinks: map[string]bool{"eth0": true}}
	bridge := &Bridge{Runner: runner}

	if err := bridge.AttachContainer(context.Background(), l, l.Containers[0], 1234); err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(runner.commands, "\n")
	for _, forbidden := range []string{
		"ip link delete",
		"ip link add",
		"ip link set v",
		"netns 1234",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("unexpected recreate command fragment %q in:\n%s", forbidden, joined)
		}
	}
	gateway, _ := switchNATGatewayCIDR(l, l.Switches[0])
	address, err := switchNATContainerAddress(l, l.Switches[0], l.Containers[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"nsenter -t 1234 -n ip link show eth0",
		"nsenter -t 1234 -n ip link set eth0 up",
		"nsenter -t 1234 -n ip addr replace " + address + " dev eth0",
		"nsenter -t 1234 -n ip route replace default via " + gateway + " dev eth0",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected command fragment %q in:\n%s", want, joined)
		}
	}
}

func TestAttachContainerCleansCreatedVethsAfterAttachFailure(t *testing.T) {
	l := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
		Containers: []lab.Container{{
			ID:       "web",
			Image:    "nginx",
			Networks: []lab.ContainerNetwork{{Switch: "lan"}, {Switch: "lan"}},
		}},
	}
	firstHostIf, _ := vethNames(l, l.Containers[0], 0)
	secondHostIf, _ := vethNames(l, l.Containers[0], 1)
	runner := &fakeRunner{failAt: "ip link set " + secondHostIf + " master"}
	bridge := &Bridge{Runner: runner}

	if err := bridge.AttachContainer(context.Background(), l, l.Containers[0], 1234); err == nil {
		t.Fatal("AttachContainer returned nil error")
	}
	for _, hostIf := range []string{firstHostIf, secondHostIf} {
		deleteHostIf := "ip link delete " + hostIf
		if got := commandCount(runner.commands, deleteHostIf); got != 2 {
			t.Fatalf("host interface cleanup count for %s = %d, want 2; commands=%#v", hostIf, got, runner.commands)
		}
	}
}

func commandCount(commands []string, want string) int {
	count := 0
	for _, command := range commands {
		if command == want {
			count++
		}
	}
	return count
}

func TestGeneratedInterfaceNamesAreShortAndUniqueWhenTruncated(t *testing.T) {
	l := &lab.Lab{ID: "demo"}
	vmA := lab.VM{ID: "very-long-workload-alpha"}
	vmB := lab.VM{ID: "very-long-workload-beta"}
	ctA := lab.Container{ID: "very-long-container-alpha"}
	ctB := lab.Container{ID: "very-long-container-beta"}

	ctAHost, ctAGuest := vethNames(l, ctA, 0)
	ctBHost, ctBGuest := vethNames(l, ctB, 0)
	names := []string{
		VMTapName(l, vmA, 0),
		VMTapName(l, vmB, 0),
		ctAHost,
		ctAGuest,
		ctBHost,
		ctBGuest,
	}
	seen := map[string]bool{}
	for _, name := range names {
		if len(name) > 15 {
			t.Fatalf("generated interface name %q exceeds Linux IFNAMSIZ limit", name)
		}
		if seen[name] {
			t.Fatalf("generated interface name %q was reused for distinct endpoints: %#v", name, names)
		}
		seen[name] = true
	}
}

func TestGeneratedInterfaceNamesStayShortWithLargeNICIndex(t *testing.T) {
	l := &lab.Lab{ID: "demo"}
	vm := lab.VM{ID: "router"}
	ct := lab.Container{ID: "web"}
	hostIf, guestIf := vethNames(l, ct, 123456789012345)
	names := []string{
		VMTapName(l, vm, 123456789012345),
		hostIf,
		guestIf,
	}
	seen := map[string]bool{}
	for _, name := range names {
		if len(name) > 15 {
			t.Fatalf("generated interface name %q exceeds Linux IFNAMSIZ limit", name)
		}
		if name == "" {
			t.Fatal("generated empty interface name")
		}
		if seen[name] {
			t.Fatalf("generated interface name %q was reused: %#v", name, names)
		}
		seen[name] = true
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

func TestAttachContainerSwitchNATPlansAddressAndUplinkRules(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		ExternalLinks: []lab.ExternalLink{
			{ID: "uplink1", Interface: "eth0", Mode: lab.ExternalModeNAT},
			{ID: "uplink2", Interface: "tun0", Mode: lab.ExternalModeNAT},
		},
		Switches:   []lab.Switch{{ID: "lan", Mode: "nat", ExternalLinks: []string{"uplink1", "uplink2"}}},
		Containers: []lab.Container{{ID: "web", Image: "nginx", Networks: []lab.ContainerNetwork{{Switch: "lan"}}}},
	}
	runner := &fakeRunner{}
	bridge := &Bridge{Runner: runner}

	if err := bridge.AttachContainer(context.Background(), l, l.Containers[0], 1234); err != nil {
		t.Fatal(err)
	}

	managed := l.ManagedSwitchBridgeName(l.Switches[0])
	gateway, cidr := switchNATGatewayCIDR(l, l.Switches[0])
	address, err := switchNATContainerAddress(l, l.Switches[0], l.Containers[0], 0)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"ip link show " + managed,
		"ip addr replace " + gateway + "/24 dev " + managed,
		"iptables -C FORWARD -i " + managed + " -o eth0 -j ACCEPT",
		"iptables -C FORWARD -i " + managed + " -o tun0 -j ACCEPT",
		"iptables -t nat -C POSTROUTING -s " + cidr + " -o eth0 -j MASQUERADE",
		"iptables -t nat -C POSTROUTING -s " + cidr + " -o tun0 -j MASQUERADE",
		"master " + managed,
		"nsenter -t 1234 -n ip addr replace " + address + " dev eth0",
		"nsenter -t 1234 -n ip route replace default via " + gateway + " dev eth0",
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

func TestAttachContainerSwitchMacNATUsesGeneratedMAC(t *testing.T) {
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
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge", ExternalLink: "uplink1"}},
		Containers:    []lab.Container{{ID: "web", Image: "nginx", Networks: []lab.ContainerNetwork{{Switch: "lan"}}}},
	}
	runner := &fakeRunner{}
	ctrl := macnat.NewController(device.Name())
	bridge := &Bridge{Runner: runner, MacNAT: &ctrl}

	if err := bridge.AttachContainer(context.Background(), l, l.Containers[0], 1234); err != nil {
		t.Fatal(err)
	}

	generatedMAC := l.GeneratedNICMAC("container", "web", 0)
	joined := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"master " + l.ManagedSwitchBridgeName(l.Switches[0]),
		"address " + generatedMAC,
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
		"configure labID=demo switchID=lan bridge=" + l.ManagedSwitchBridgeName(l.Switches[0]) + " uplink=eth0",
		"mac=" + generatedMAC,
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
