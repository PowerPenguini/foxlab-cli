package lab

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRejectsMissingSwitchReference(t *testing.T) {
	l := &Lab{
		ID: "demo",
		VMs: []VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm1.qcow2",
			Networks: []VMNetwork{{Switch: "missing"}},
		}},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil || !strings.Contains(err.Error(), "missing switch") {
		t.Fatalf("expected missing switch error, got %v", err)
	}
}

func TestValidateAcceptsExternalLinks(t *testing.T) {
	l := &Lab{
		ID: "demo",
		ExternalLinks: []ExternalLink{{
			ID:        "uplink1",
			Interface: "br0",
		}},
		Switches: []Switch{{
			ID:           "sw1",
			Mode:         "bridge",
			ExternalLink: "uplink1",
		}},
		VMs: []VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm1.qcow2",
			Networks: []VMNetwork{{ExternalLink: "uplink1"}},
		}},
		Layout: Layout{
			Nodes: map[string]Position{
				"vm1":     {X: 100, Y: 100},
				"sw1":     {X: 300, Y: 100},
				"uplink1": {X: 500, Y: 100},
			},
			Links: []LayoutLink{
				{From: LayoutEndpoint{Type: "vm", ID: "vm1"}, To: LayoutEndpoint{Type: "external", ID: "uplink1"}},
				{From: LayoutEndpoint{Type: "switch", ID: "sw1"}, To: LayoutEndpoint{Type: "external", ID: "uplink1"}},
			},
		},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected external link config to validate, got %v", err)
	}
}

func TestValidateAcceptsNATSwitch(t *testing.T) {
	l := &Lab{
		ID:            "demo",
		ExternalLinks: []ExternalLink{{ID: "uplink1", Interface: "wlp0s20f3"}},
		Switches: []Switch{{
			ID:           "sw1",
			Mode:         "nat",
			ExternalLink: "uplink1",
		}},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected NAT switch config to validate, got %v", err)
	}
}

func TestValidateAcceptsMACNATBridgeSwitch(t *testing.T) {
	l := &Lab{
		ID:            "demo",
		ExternalLinks: []ExternalLink{{ID: "uplink1", Interface: "wlp0s20f3"}},
		Switches: []Switch{{
			ID:           "sw1",
			Mode:         "macnat-bridge",
			ExternalLink: "uplink1",
		}},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected macnat-bridge switch config to validate, got %v", err)
	}
}

func TestValidateAcceptsSwitchWithoutExternalLink(t *testing.T) {
	for _, mode := range []string{"bridge", "nat"} {
		l := &Lab{
			ID:       "demo",
			Switches: []Switch{{ID: "sw1", Mode: mode}},
		}
		l.Normalize()
		if err := l.Validate(); err != nil {
			t.Fatalf("expected %s switch without external link to validate, got %v", mode, err)
		}
	}
}

func TestValidateRejectsMACNATBridgeWithoutExternalLink(t *testing.T) {
	l := &Lab{
		ID:       "demo",
		Switches: []Switch{{ID: "sw1", Mode: "macnat-bridge"}},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil || !strings.Contains(err.Error(), "macnat-bridge mode requires externalLink") {
		t.Fatalf("expected macnat-bridge externalLink validation error, got %v", err)
	}
}

func TestValidateRejectsInvalidExternalLinkReferences(t *testing.T) {
	l := &Lab{
		ID: "demo",
		ExternalLinks: []ExternalLink{
			{ID: "uplink1"},
			{ID: "uplink1", Interface: "br1"},
		},
		Switches: []Switch{{
			ID:           "sw1",
			Mode:         "bridge",
			ExternalLink: "missing",
		}},
		VMs: []VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm1.qcow2",
			Networks: []VMNetwork{{Switch: "sw1", ExternalLink: "uplink1"}},
		}},
		Layout: Layout{
			Nodes: map[string]Position{"ghost": {X: 100, Y: 100}},
			Links: []LayoutLink{{From: LayoutEndpoint{Type: "external", ID: "missing"}, To: LayoutEndpoint{Type: "vm", ID: "vm1"}}},
		},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil {
		t.Fatal("expected external link validation errors")
	}
	for _, want := range []string{
		`external link "uplink1" interface is required`,
		`duplicate external link id "uplink1"`,
		`switch "sw1" references missing external link "missing"`,
		`vm "vm1" network must reference exactly one switch or externalLink`,
		`layout link references missing external link "missing"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in validation error, got %v", want, err)
		}
	}
}

func TestManagedNamesAreStable(t *testing.T) {
	l := &Lab{ID: "Demo_Lab"}
	if got := l.ManagedDomainName(VM{ID: "Router1"}); got != "foxlab-demo_lab-router1" {
		t.Fatalf("unexpected domain name %q", got)
	}
	if got := l.ManagedNetworkName(Switch{ID: "SW1"}); got != "foxlab-demo_lab-sw1" {
		t.Fatalf("unexpected network name %q", got)
	}
	if got := l.ManagedContainerName(Container{ID: "Web1"}); got != "foxlab-demo_lab-web1" {
		t.Fatalf("unexpected container name %q", got)
	}
	if got := l.ManagedSwitchBridgeName(Switch{ID: "SW1"}); got != "flfoxlabdemolab" {
		t.Fatalf("unexpected bridge name %q", got)
	}
}

func TestValidateAcceptsContainers(t *testing.T) {
	l := &Lab{
		ID:       "demo",
		Switches: []Switch{{ID: "lan", Mode: "bridge"}},
		Containers: []Container{{
			ID:      "web",
			Image:   "docker.io/library/nginx:latest",
			Command: []string{"nginx", "-g", "daemon off;"},
			Env:     map[string]string{"ENV": "test"},
			Networks: []ContainerNetwork{{
				Switch: "lan",
				MAC:    "02:00:00:00:00:10",
			}},
		}},
		Layout: Layout{
			Nodes: map[string]Position{"web": {X: 100, Y: 100}},
			Links: []LayoutLink{{From: LayoutEndpoint{Type: "container", ID: "web"}, To: LayoutEndpoint{Type: "switch", ID: "lan"}}},
		},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected container config to validate, got %v", err)
	}
}

func TestValidateRejectsInvalidContainers(t *testing.T) {
	l := &Lab{
		ID: "demo",
		Containers: []Container{
			{ID: "web", Networks: []ContainerNetwork{{Switch: "missing"}}},
			{ID: "web", Image: "docker.io/library/nginx:latest"},
		},
		Layout: Layout{
			Links: []LayoutLink{{From: LayoutEndpoint{Type: "container", ID: "missing"}, To: LayoutEndpoint{Type: "switch", ID: "lan"}}},
		},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil {
		t.Fatal("expected container validation errors")
	}
	for _, want := range []string{
		`container "web" image is required`,
		`container "web" references missing switch "missing"`,
		`duplicate container id "web"`,
		`layout link references missing container "missing"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in validation error, got %v", want, err)
		}
	}
}

func TestLoadFileKnownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lab")
	writeTestFile(t, path, "id: demo\nunknown: true\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected known-fields validation error")
	}
}

func TestLoadFileAllowsDisksField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lab")
	writeTestFile(t, path, `id: demo
disks:
  - id: vm1
    path: labs/demo/disks/vm1.qcow2
    sizeGB: 20
    format: qcow2
vms:
  - id: vm1
    memoryMB: 2048
    cpus: 2
    disk: labs/demo/disks/vm1.qcow2
    networks:
      - switch: sw1
switches:
  - id: sw1
    mode: bridge
externalLinks:
  - id: link1
    interface: br0
`)

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Disks) != 1 {
		t.Fatalf("loaded disks = %d, want 1", len(loaded.Disks))
	}
}

func TestLoadFileRejectsTopLevelName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lab")
	writeTestFile(t, path, "id: demo\nname: Demo\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected top-level name to be rejected")
	}
}

func TestListFilesIncludesOnlyLabExtension(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "demo.lab"), "id: demo\n")
	writeTestFile(t, filepath.Join(dir, "legacy.yaml"), "id: legacy\n")
	writeTestFile(t, filepath.Join(dir, "old.yml"), "id: old\n")
	writeTestFile(t, filepath.Join(dir, "notes.txt"), "id: notes\n")

	files, err := ListFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, path := range files {
		got[filepath.Base(path)] = true
	}
	if !got["demo.lab"] {
		t.Fatalf("ListFiles missing demo.lab in %#v", files)
	}
	for _, unwanted := range []string{"legacy.yaml", "old.yml", "notes.txt"} {
		if got[unwanted] {
			t.Fatalf("ListFiles included non-lab file %s: %#v", unwanted, files)
		}
	}
}

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
