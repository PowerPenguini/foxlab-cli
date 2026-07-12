package hostnet

import (
	"fmt"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestNATContainerAddressesAreUniqueWithinEachNetwork(t *testing.T) {
	l := &lab.Lab{
		ID:            "demo",
		Switches:      []lab.Switch{{ID: "lan", Mode: "nat"}},
		ExternalLinks: []lab.ExternalLink{{ID: "wan", Interface: "eth0", Mode: lab.ExternalModeNAT}},
	}
	for i := 0; i < 80; i++ {
		l.Containers = append(l.Containers, lab.Container{
			ID:    fmt.Sprintf("ct-%d", i),
			Image: "alpine",
			Networks: []lab.ContainerNetwork{
				{Switch: "lan"},
				{ExternalLink: "wan"},
			},
		})
	}

	switchAddresses := map[string]string{}
	externalAddresses := map[string]string{}
	for _, ct := range l.Containers {
		switchAddress, err := switchNATContainerAddress(l, l.Switches[0], ct, 0)
		if err != nil {
			t.Fatal(err)
		}
		if previous := switchAddresses[switchAddress]; previous != "" {
			t.Fatalf("switch NAT address %s collides for %s and %s", switchAddress, previous, ct.ID)
		}
		switchAddresses[switchAddress] = ct.ID

		externalAddress, err := externalNATContainerAddress(l, l.ExternalLinks[0], ct, 1)
		if err != nil {
			t.Fatal(err)
		}
		if previous := externalAddresses[externalAddress]; previous != "" {
			t.Fatalf("external NAT address %s collides for %s and %s", externalAddress, previous, ct.ID)
		}
		externalAddresses[externalAddress] = ct.ID
	}
}

func TestNATContainerAddressRejectsExhaustedPool(t *testing.T) {
	l := &lab.Lab{ID: "demo", Switches: []lab.Switch{{ID: "lan", Mode: "nat"}}}
	for i := 0; i < 81; i++ {
		l.Containers = append(l.Containers, lab.Container{
			ID:       fmt.Sprintf("ct-%d", i),
			Image:    "alpine",
			Networks: []lab.ContainerNetwork{{Switch: "lan"}},
		})
	}
	if _, err := switchNATContainerAddress(l, l.Switches[0], l.Containers[0], 0); err == nil {
		t.Fatal("expected exhausted NAT address pool error")
	}
}
