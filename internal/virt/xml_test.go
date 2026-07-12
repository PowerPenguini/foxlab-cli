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
		"<name>" + l.ManagedDomainName(l.VMs[0]) + "</name>",
		`<source file="` + diskPath + `"/>`,
		`<interface type="bridge">`,
		`<source bridge="` + l.ManagedSwitchBridgeName(l.Switches[0]) + `"/>`,
		`<graphics type="vnc"`,
		`<model type="virtio" heads="1" primary="yes"/>`,
		`<serial type="pty">`,
		`<target type="isa-serial" port="0"/>`,
		`<console type="pty">`,
		`<target type="serial" port="0"/>`,
		`<target type="virtio" name="org.qemu.guest_agent.0"/>`,
		`configSHA256="`,
	} {
		if !strings.Contains(xmlText, want) {
			t.Fatalf("domain XML missing %q:\n%s", want, xmlText)
		}
	}
	if strings.Contains(xmlText, `<source network="`+l.ManagedNetworkName(l.Switches[0])+`"/>`) {
		t.Fatalf("domain XML still uses libvirt network for switch NIC:\n%s", xmlText)
	}
}

func TestDomainXMLWithUUIDPreservesExistingIdentity(t *testing.T) {
	l := &lab.Lab{ID: "demo"}
	vm := lab.VM{ID: "victim-a", MemoryMB: 2048, CPUs: 2}
	const uuid = "918ec8fd-ecd1-4c30-b632-fc7906efbdb0"

	xmlText, err := domainXMLWithUUID(l, vm, uuid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xmlText, "<uuid>"+uuid+"</uuid>") {
		t.Fatalf("redefined domain XML does not preserve UUID:\n%s", xmlText)
	}
	baseXML, err := domainXML(l, vm)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(baseXML, "<uuid>") {
		t.Fatalf("new domain XML unexpectedly pins UUID:\n%s", baseXML)
	}
	_, _, redefinedHash, _ := managedDomainMetadata(xmlText)
	_, _, baseHash, _ := managedDomainMetadata(baseXML)
	if redefinedHash == "" || redefinedHash != baseHash {
		t.Fatalf("UUID changed configuration hash: redefined=%q base=%q", redefinedHash, baseHash)
	}
	matches, err := domainConfigMatches(l, vm, xmlText)
	if err != nil || !matches {
		t.Fatalf("UUID-preserving XML config match = %t, err = %v", matches, err)
	}
}

func TestDomainConfigMatchesFingerprintAndLegacyXML(t *testing.T) {
	diskPath := filepath.Join(t.TempDir(), "vm.qcow2")
	if err := writeEmptyFile(diskPath); err != nil {
		t.Fatal(err)
	}
	l := &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 2, Disk: diskPath, VNC: true}}}
	l.Normalize()
	xmlText, err := domainXML(l, l.VMs[0])
	if err != nil {
		t.Fatal(err)
	}
	matched, err := domainConfigMatches(l, l.VMs[0], xmlText)
	if err != nil || !matched {
		t.Fatalf("fingerprinted match = %t, err = %v", matched, err)
	}
	externallyChangedXML := strings.Replace(xmlText, "<vcpu>2</vcpu>", "<vcpu>3</vcpu>", 1)
	matched, err = domainConfigMatches(l, l.VMs[0], externallyChangedXML)
	if err != nil || matched {
		t.Fatalf("externally changed live XML match = %t, err = %v", matched, err)
	}
	changed := l.VMs[0]
	changed.CPUs++
	matched, err = domainConfigMatches(l, changed, xmlText)
	if err != nil || matched {
		t.Fatalf("fingerprinted drift match = %t, err = %v", matched, err)
	}
	_, _, hash, ok := managedDomainMetadata(xmlText)
	if !ok || hash == "" {
		t.Fatalf("managed metadata missing hash: ok=%t hash=%q", ok, hash)
	}
	legacyXML := strings.Replace(xmlText, ` configSHA256="`+hash+`"`, "", 1)
	matched, err = domainConfigMatches(l, l.VMs[0], legacyXML)
	if err != nil || !matched {
		t.Fatalf("legacy match = %t, err = %v", matched, err)
	}
	matched, err = domainConfigMatches(l, changed, legacyXML)
	if err != nil || matched {
		t.Fatalf("legacy drift match = %t, err = %v", matched, err)
	}
}

func TestOrphanManagedDomainID(t *testing.T) {
	l := &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1}}}
	l.Normalize()
	xmlText, err := domainXML(l, l.VMs[0])
	if err != nil {
		t.Fatal(err)
	}
	desired := map[string]bool{l.VMs[0].ID: true}
	if id, orphan := orphanManagedDomainID(l, desired, xmlText); orphan || id != l.VMs[0].ID {
		t.Fatalf("desired domain orphan = %t id=%q", orphan, id)
	}
	if id, orphan := orphanManagedDomainID(l, map[string]bool{}, xmlText); !orphan || id != l.VMs[0].ID {
		t.Fatalf("absent managed domain orphan = %t id=%q", orphan, id)
	}
	unmanaged := strings.Replace(xmlText, `kind="domain"`, `kind="external"`, 1)
	if _, orphan := orphanManagedDomainID(l, map[string]bool{}, unmanaged); orphan {
		t.Fatal("unmanaged domain classified as orphan")
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
		`<mac address="` + l.GeneratedNICMAC("vm", l.VMs[0].ID, 0) + `"/>`,
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
		"<name>" + l.ManagedNetworkName(l.Switches[0]) + "</name>",
		`lab="demo" id="` + l.Switches[0].ID + `" kind="network"`,
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
