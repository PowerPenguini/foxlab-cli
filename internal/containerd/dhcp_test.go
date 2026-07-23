package containerd

import (
	"slices"
	"strings"
	"testing"

	"foxlab-cli/internal/hostnet"
	"foxlab-cli/internal/lab"
)

func TestPrepareDHCPContainerBuildsManagedDnsmasqProcess(t *testing.T) {
	l := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "nat"}},
	}
	ct := lab.Container{
		ID:       "dhcp",
		Service:  lab.ContainerServiceDHCP,
		Image:    lab.DefaultDHCPImage,
		Networks: []lab.ContainerNetwork{{Switch: "lan"}},
	}
	prepared, err := prepareContainerForStart(l, ct)
	if err != nil {
		t.Fatal(err)
	}
	if len(ct.Command) != 0 || ct.Capabilities != nil {
		t.Fatalf("prepare mutated desired container: %#v", ct)
	}
	if len(prepared.Command) == 0 || prepared.Command[0] != "/dnsmasq" {
		t.Fatalf("managed command = %#v", prepared.Command)
	}
	config := hostnet.NATSwitchConfiguration(l, l.Switches[0])
	command := strings.Join(prepared.Command, " ")
	for _, want := range []string{
		"--no-daemon",
		"--leasefile-ro",
		"--bind-dynamic",
		"--interface=eth0",
		"--dhcp-authoritative",
		config.DHCPRangeStart,
		config.DHCPRangeEnd,
		config.Gateway,
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("managed command %q does not contain %q", command, want)
		}
	}
	if prepared.Capabilities == nil || !slices.Contains(prepared.Capabilities.Add, "NET_ADMIN") {
		t.Fatalf("managed capabilities = %#v, want NET_ADMIN", prepared.Capabilities)
	}
	if opts := containerSpecOpts(nil, prepared, containerDiskMount{}, ""); len(opts) != 3 {
		t.Fatalf("scratch DHCP spec options = %d, want image, process, and capabilities without resolv.conf mount", len(opts))
	}
}

func TestPrepareDHCPContainerRejectsMissingSwitchAttachment(t *testing.T) {
	_, err := prepareContainerForStart(&lab.Lab{ID: "demo"}, lab.Container{ID: "dhcp", Service: lab.ContainerServiceDHCP})
	if err == nil || !strings.Contains(err.Error(), "exactly one switch") {
		t.Fatalf("prepare error = %v", err)
	}
}

func TestPrepareDHCPContainerRejectsDesiredManagedOverrides(t *testing.T) {
	l := &lab.Lab{ID: "demo", Switches: []lab.Switch{{ID: "lan", Mode: "nat"}}}
	ct := lab.Container{
		ID: "dhcp", Service: lab.ContainerServiceDHCP, Image: "alpine",
		Capabilities: &lab.ContainerCapabilities{Add: []string{"NET_ADMIN"}},
		Networks:     []lab.ContainerNetwork{{Switch: "lan"}},
	}
	_, err := prepareContainerForStart(l, ct)
	if err == nil || !strings.Contains(err.Error(), "image is managed") || !strings.Contains(err.Error(), "capabilities are managed") {
		t.Fatalf("prepare error = %v", err)
	}
}
