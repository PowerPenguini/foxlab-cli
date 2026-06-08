package virt

import (
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestDomainXMLUsesManagedNetworkAndDomainNames(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     "labs/demo/disks/vm1.qcow2",
			VNC:      true,
			Networks: []lab.VMNetwork{{
				Switch: "sw1",
			}},
		}},
		Switches: []lab.Switch{{ID: "sw1", Mode: "bridge"}},
	}
	l.Normalize()

	xmlText, err := domainXML(l, l.VMs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<name>foxlab-demo-vm1</name>",
		`<source file="labs/demo/disks/vm1.qcow2"/>`,
		`<source network="foxlab-demo-sw1"/>`,
		`<graphics type="vnc"`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("domain XML missing %q:\n%s", want, xmlText)
		}
	}
}

func TestNetworkXMLUsesManagedMetadata(t *testing.T) {
	l := &lab.Lab{ID: "demo", Switches: []lab.Switch{{ID: "sw1", Mode: "nat"}}}
	l.Normalize()

	xmlText, err := networkXML(l, l.Switches[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<name>foxlab-demo-sw1</name>",
		`lab="demo" id="sw1" kind="network"`,
		`<forward mode="nat"/>`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("network XML missing %q:\n%s", want, xmlText)
		}
	}
}
