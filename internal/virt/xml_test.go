package virt

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestDomainXMLUsesManagedNetworkAndDomainNames(t *testing.T) {
	diskPath := filepath.Join(t.TempDir(), "vm1.qcow2")
	if err := writeEmptyFile(diskPath); err != nil {
		t.Fatal(err)
	}
	l := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     diskPath,
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
		`<source file="` + diskPath + `"/>`,
		`<interface type="bridge">`,
		`<source bridge="flfoxlabdemosw1"/>`,
		`<graphics type="vnc"`,
		`<model type="virtio" heads="1" primary="yes"/>`,
		`<serial type="pty">`,
		`<target type="isa-serial" port="0"/>`,
		`<console type="pty">`,
		`<target type="serial" port="0"/>`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("domain XML missing %q:\n%s", want, xmlText)
		}
	}
	if strings.Contains(xmlText, `<source network="foxlab-demo-sw1"/>`) {
		t.Fatalf("domain XML still uses libvirt network for switch NIC:\n%s", xmlText)
	}
}

func TestDomainXMLOmitsMissingDisk(t *testing.T) {
	l := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     "labs/demo/disks/missing.qcow2",
		}},
	}
	l.Normalize()

	xmlText, err := domainXML(l, l.VMs[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(xmlText, `<disk type="file" device="disk">`) {
		t.Fatalf("domain XML included missing disk:\n%s", xmlText)
	}
	if strings.Contains(xmlText, `<boot dev="hd"/>`) {
		t.Fatalf("domain XML requested disk boot without disk:\n%s", xmlText)
	}
}

func TestDomainXMLIncludesDirectLinkedNIC(t *testing.T) {
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
	l.Normalize()

	xmlText, err := domainXML(l, l.VMs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<interface type="bridge">`,
		`<source bridge="flp2p`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("domain XML missing direct NIC %q:\n%s", want, xmlText)
		}
	}
}

func TestDomainXMLUsesManagedBridgeForNATExternalLink(t *testing.T) {
	l := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0", Mode: lab.ExternalModeNAT}},
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     "labs/demo/disks/vm1.qcow2",
			Networks: []lab.VMNetwork{{ExternalLink: "uplink1"}},
		}},
	}
	l.Normalize()

	xmlText, err := domainXML(l, l.VMs[0])
	if err != nil {
		t.Fatal(err)
	}
	bridge := l.ManagedExternalBridgeName(l.ExternalLinks[0])
	for _, want := range []string{
		`<interface type="bridge">`,
		`<source bridge="` + bridge + `"/>`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("domain XML missing %q:\n%s", want, xmlText)
		}
	}
}

func TestDomainXMLUsesGeneratedMACForMacNATExternalLink(t *testing.T) {
	l := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0", Mode: lab.ExternalModeMacNAT}},
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     "labs/demo/disks/vm1.qcow2",
			Networks: []lab.VMNetwork{{ExternalLink: "uplink1"}},
		}},
	}
	l.Normalize()

	xmlText, err := domainXML(l, l.VMs[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<source bridge="` + l.ManagedExternalBridgeName(l.ExternalLinks[0]) + `"/>`,
		`<mac address="` + l.GeneratedNICMAC("vm", "vm1", 0) + `"/>`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("domain XML missing %q:\n%s", want, xmlText)
		}
	}
}

func TestDomainXMLEscapesAttributeValues(t *testing.T) {
	diskPath := filepath.Join(t.TempDir(), `disk & "quote".qcow2`)
	if err := writeEmptyFile(diskPath); err != nil {
		t.Fatal(err)
	}
	l := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: `eth0&"lab"`}},
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     diskPath,
			Networks: []lab.VMNetwork{{ExternalLink: "uplink1"}},
		}},
	}
	l.Normalize()

	xmlText, err := domainXML(l, l.VMs[0])
	if err != nil {
		t.Fatal(err)
	}
	assertWellFormedXML(t, xmlText)
	for _, want := range []string{
		`disk &amp; &quot;quote&quot;.qcow2`,
		`<source dev="eth0&amp;&quot;lab&quot;" mode="bridge"/>`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("domain XML missing escaped value %q:\n%s", want, xmlText)
		}
	}
}

func TestParseVNCPortReturnsAssignedPort(t *testing.T) {
	xmlText := `<domain><devices><graphics type="vnc" port="5903" autoport="yes" listen="127.0.0.1"/></devices></domain>`

	if got := parseVNCPort(xmlText); got != 5903 {
		t.Fatalf("VNC port = %d, want 5903", got)
	}
}

func TestParseVNCPortIgnoresAutoportPlaceholder(t *testing.T) {
	xmlText := `<domain><devices><graphics type="vnc" port="-1" autoport="yes" listen="127.0.0.1"/></devices></domain>`

	if got := parseVNCPort(xmlText); got != 0 {
		t.Fatalf("VNC port = %d, want 0", got)
	}
}

func writeEmptyFile(path string) error {
	return os.WriteFile(path, nil, 0o644)
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

func TestNetworkXMLEscapesAttributeValues(t *testing.T) {
	l := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: `eth0&"wan"`}},
		Switches:      []lab.Switch{{ID: "sw1", Mode: "nat", ExternalLink: "uplink1"}},
	}
	l.Normalize()

	xmlText, err := networkXML(l, l.Switches[0])
	if err != nil {
		t.Fatal(err)
	}
	assertWellFormedXML(t, xmlText)
	if want := `<forward mode="nat" dev="eth0&amp;&quot;wan&quot;"/>`; !strings.Contains(xmlText, want) {
		t.Fatalf("network XML missing escaped uplink %q:\n%s", want, xmlText)
	}
}

func assertWellFormedXML(t *testing.T, xmlText string) {
	t.Helper()
	var root struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal([]byte(xmlText), &root); err != nil {
		t.Fatalf("generated XML is not well-formed: %v\n%s", err, xmlText)
	}
}
