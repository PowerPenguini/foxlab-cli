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
	if l.ExternalLinks[0].Mode != ExternalModeDirect {
		t.Fatalf("legacy external mode = %q, want direct", l.ExternalLinks[0].Mode)
	}
}

func TestValidateExternalLinkModes(t *testing.T) {
	for _, mode := range []string{ExternalModeNAT, ExternalModeDirect, ExternalModeMacNAT} {
		l := &Lab{
			ID:            "demo",
			ExternalLinks: []ExternalLink{{ID: "uplink1", Interface: "eth0", Mode: mode}},
		}
		l.Normalize()
		if err := l.Validate(); err != nil {
			t.Fatalf("mode %q should validate: %v", mode, err)
		}
	}
	l := &Lab{
		ID:            "demo",
		ExternalLinks: []ExternalLink{{ID: "uplink1", Interface: "eth0", Mode: "wifi"}},
	}
	l.Normalize()
	if err := l.Validate(); err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("expected unsupported mode error, got %v", err)
	}
}

func TestValidateAcceptsUnconnectedNICs(t *testing.T) {
	l := &Lab{
		ID: "demo",
		VMs: []VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm1.qcow2",
			Networks: []VMNetwork{{}},
		}},
		Containers: []Container{{
			ID:       "web",
			Image:    "nginx",
			Networks: []ContainerNetwork{{}},
		}},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected unconnected NICs to validate, got %v", err)
	}
}

func TestValidateAcceptsNICMACAddresses(t *testing.T) {
	l := &Lab{
		ID:       "demo",
		Switches: []Switch{{ID: "lan", Mode: "bridge"}},
		VMs: []VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Networks: []VMNetwork{{Switch: "lan", MAC: "02-00-00-00-00-10"}},
		}},
		Containers: []Container{{
			ID:       "web",
			Image:    "nginx",
			Networks: []ContainerNetwork{{Switch: "lan", MAC: "02:00:00:00:00:11"}},
		}},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected valid MAC addresses to validate, got %v", err)
	}
	if got := l.VMs[0].Networks[0].MAC; got != "02:00:00:00:00:10" {
		t.Fatalf("vm MAC normalized to %q, want colon-separated runtime MAC", got)
	}
	if got := l.Containers[0].Networks[0].MAC; got != "02:00:00:00:00:11" {
		t.Fatalf("container MAC normalized to %q, want colon-separated runtime MAC", got)
	}
}

func TestValidateRejectsInvalidNICMACAddresses(t *testing.T) {
	l := &Lab{
		ID:       "demo",
		Switches: []Switch{{ID: "lan", Mode: "bridge"}},
		VMs: []VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Networks: []VMNetwork{{Switch: "lan", MAC: "not-a-mac"}},
		}},
		Containers: []Container{{
			ID:       "web",
			Image:    "nginx",
			Networks: []ContainerNetwork{{Switch: "lan", MAC: "02:00:00:00:00:11:12"}},
		}},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil {
		t.Fatal("expected invalid MAC validation errors")
	}
	for _, want := range []string{
		`vm "vm1" network mac "not-a-mac" is invalid`,
		`container "web" network mac "02:00:00:00:00:11:12" is invalid`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in validation error, got %v", want, err)
		}
	}
}

func TestValidateAcceptsVMWithoutDisk(t *testing.T) {
	l := &Lab{
		ID: "demo",
		VMs: []VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
		}},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected vm without disk to validate, got %v", err)
	}
}

func TestValidateAcceptsContainerDataDisk(t *testing.T) {
	l := &Lab{
		ID:         "demo",
		Containers: []Container{{ID: "web", Image: "nginx", Disk: "disks/web.qcow2"}},
		Disks: []Disk{{
			ID:           "web-data",
			Path:         "disks/web.qcow2",
			Format:       "qcow2",
			Kind:         "data",
			AttachedType: "container",
			AttachedTo:   "web",
		}},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected container data disk to validate, got %v", err)
	}
}

func TestValidateRejectsDataDiskForVM(t *testing.T) {
	l := &Lab{
		ID:  "demo",
		VMs: []VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "disks/data.qcow2"}},
		Disks: []Disk{{
			ID:           "data",
			Path:         "disks/data.qcow2",
			Format:       "qcow2",
			Kind:         "data",
			AttachedType: "vm",
			AttachedTo:   "vm1",
		}},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil || !strings.Contains(err.Error(), `disk "data" data disk cannot attach to vm`) {
		t.Fatalf("expected vm data disk validation error, got %v", err)
	}
}

func TestValidateRejectsDataDiskWithBase(t *testing.T) {
	l := &Lab{
		ID: "demo",
		Disks: []Disk{
			{ID: "base", Path: "disks/base.qcow2", Format: "qcow2", Kind: "base"},
			{ID: "data", Path: "disks/data.qcow2", Format: "qcow2", Kind: "data", Base: "base"},
		},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil || !strings.Contains(err.Error(), `disk "data" data disk must not reference base`) {
		t.Fatalf("expected data base validation error, got %v", err)
	}
}

func TestValidateAllowsRootDiskMountPathForContainerLayer(t *testing.T) {
	l := &Lab{
		ID:         "demo",
		Containers: []Container{{ID: "web", Image: "nginx", Disk: "disks/data.qcow2"}},
		Disks: []Disk{{
			ID:           "data",
			Path:         "disks/data.qcow2",
			Format:       "qcow2",
			Kind:         "data",
			AttachedType: "container",
			AttachedTo:   "web",
			MountPath:    "/",
		}},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestValidateRejectsInvalidDiskLayerBase(t *testing.T) {
	tests := []struct {
		name  string
		disks []Disk
		want  string
	}{
		{
			name: "self base",
			disks: []Disk{
				{ID: "self", Path: "disks/self.qcow2", Format: "qcow2", Kind: "layer", Base: "self"},
			},
			want: `disk "self" must not use itself as base`,
		},
		{
			name: "layer base",
			disks: []Disk{
				{ID: "base", Path: "disks/base.qcow2", Format: "qcow2", Kind: "base"},
				{ID: "parent-layer", Path: "layers/parent.qcow2", Format: "qcow2", Kind: "layer", Base: "base"},
				{ID: "child-layer", Path: "layers/child.qcow2", Format: "qcow2", Kind: "layer", Base: "parent-layer"},
			},
			want: `disk "child-layer" base disk "parent-layer" must be a base disk`,
		},
		{
			name: "data base",
			disks: []Disk{
				{ID: "data", Path: "disks/data.qcow2", Format: "qcow2", Kind: "data"},
				{ID: "layer", Path: "layers/layer.qcow2", Format: "qcow2", Kind: "layer", Base: "data"},
			},
			want: `disk "layer" base disk "data" must be a base disk`,
		},
		{
			name: "base with base field",
			disks: []Disk{
				{ID: "base", Path: "disks/base.qcow2", Format: "qcow2", Kind: "base"},
				{ID: "bad-base", Path: "disks/bad-base.qcow2", Format: "qcow2", Kind: "base", Base: "base"},
			},
			want: `disk "bad-base" base disk must not reference base`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &Lab{ID: "demo", Disks: tt.disks}
			l.Normalize()
			err := l.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateRejectsAmbiguousDiskAttachments(t *testing.T) {
	l := &Lab{
		ID:         "demo",
		Containers: []Container{{ID: "web", Image: "nginx", Disk: "disks/data1.qcow2"}},
		Disks: []Disk{
			{ID: "data1", Path: "disks/data1.qcow2", Format: "qcow2", Kind: "data", AttachedType: "container", AttachedTo: "web"},
			{ID: "data2", Path: "disks/data2.qcow2", Format: "qcow2", Kind: "data", AttachedType: "container", AttachedTo: "web"},
			{ID: "dangling", Path: "disks/dangling.qcow2", Format: "qcow2", Kind: "base", AttachedTo: "web"},
		},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil {
		t.Fatal("expected ambiguous attachment validation error")
	}
	for _, want := range []string{
		`disk "dangling" attachedTo requires attachedType`,
		`disks "data1" and "data2" are both attached to container:web`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("validation error %q missing %q", err, want)
		}
	}
}

func TestValidateRejectsAttachedDiskPathMismatch(t *testing.T) {
	l := &Lab{
		ID:  "demo",
		VMs: []VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "disks/other.qcow2"}},
		Disks: []Disk{{
			ID:           "data",
			Path:         "disks/data.qcow2",
			Format:       "qcow2",
			Kind:         "layer",
			Base:         "base",
			AttachedType: "vm",
			AttachedTo:   "vm1",
		}, {
			ID:     "base",
			Path:   "disks/base.qcow2",
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil || !strings.Contains(err.Error(), `disk "data" attachment path does not match vm:vm1 disk`) {
		t.Fatalf("expected attachment path validation error, got %v", err)
	}
}

func TestValidateRejectsAttachedDiskWithEmptyWorkloadDisk(t *testing.T) {
	l := &Lab{
		ID:  "demo",
		VMs: []VM{{ID: "vm1", MemoryMB: 512, CPUs: 1}},
		Disks: []Disk{{
			ID:           "data",
			Path:         "disks/data.qcow2",
			Format:       "qcow2",
			Kind:         "base",
			AttachedType: "vm",
			AttachedTo:   "vm1",
		}},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil || !strings.Contains(err.Error(), `disk "data" is attached to vm:vm1 but workload disk is empty`) {
		t.Fatalf("expected empty workload disk validation error, got %v", err)
	}
}

func TestFoxlabHomeUsesSudoUserHomeWhenRunningAsRoot(t *testing.T) {
	oldHomeDir := userHomeDir
	oldEffectiveUserID := effectiveUserID
	oldLookupUserHome := lookupUserHome
	t.Cleanup(func() {
		userHomeDir = oldHomeDir
		effectiveUserID = oldEffectiveUserID
		lookupUserHome = oldLookupUserHome
	})

	t.Setenv("SUDO_USER", "alice")
	userHomeDir = func() (string, error) { return "/root", nil }
	effectiveUserID = func() int { return 0 }
	lookupUserHome = func(name string) (string, error) {
		if name != "alice" {
			t.Fatalf("lookup user = %q, want alice", name)
		}
		return "/home/alice", nil
	}

	got, err := FoxlabHome()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/home/alice/.foxlab" {
		t.Fatalf("FoxlabHome = %q, want user home", got)
	}
}

func TestFoxlabHomeUsesProcessHomeWhenNotSudoRoot(t *testing.T) {
	oldHomeDir := userHomeDir
	oldEffectiveUserID := effectiveUserID
	oldLookupUserHome := lookupUserHome
	t.Cleanup(func() {
		userHomeDir = oldHomeDir
		effectiveUserID = oldEffectiveUserID
		lookupUserHome = oldLookupUserHome
	})

	t.Setenv("SUDO_USER", "alice")
	userHomeDir = func() (string, error) { return "/tmp/home", nil }
	effectiveUserID = func() int { return 1000 }
	lookupUserHome = func(name string) (string, error) {
		t.Fatalf("unexpected sudo user lookup for %q", name)
		return "", nil
	}

	got, err := FoxlabHome()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/home/.foxlab" {
		t.Fatalf("FoxlabHome = %q, want process home", got)
	}
}

func TestManagedStoragePathsStayUnderFoxlabHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")

	l := &Lab{ID: "demo"}
	root, err := l.StorageRoot()
	if err != nil {
		t.Fatalf("StorageRoot returned error: %v", err)
	}
	if root != filepath.Join(home, ".foxlab", "labs", "demo") {
		t.Fatalf("StorageRoot = %q, want managed lab root", root)
	}
	diskPath, err := l.DiskStoragePath("data", "raw")
	if err != nil {
		t.Fatalf("DiskStoragePath returned error: %v", err)
	}
	if diskPath != filepath.Join(root, "disks", "data.img") {
		t.Fatalf("DiskStoragePath = %q, want managed raw disk", diskPath)
	}
	layerPath, err := l.LayerStoragePath("container", "web", "data-layer")
	if err != nil {
		t.Fatalf("LayerStoragePath returned error: %v", err)
	}
	if layerPath != filepath.Join(root, "layers", "container-web-data-layer.qcow2") {
		t.Fatalf("LayerStoragePath = %q, want managed layer", layerPath)
	}
}

func TestManagedStoragePathsRejectUnsafeInputs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")

	for _, tt := range []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "lab id traversal",
			run: func() error {
				_, err := (&Lab{ID: "../escape"}).StorageRoot()
				return err
			},
			want: "lab name",
		},
		{
			name: "disk id traversal",
			run: func() error {
				_, err := (&Lab{ID: "demo"}).DiskStoragePath("../escape", "qcow2")
				return err
			},
			want: "disk id",
		},
		{
			name: "disk format",
			run: func() error {
				_, err := (&Lab{ID: "demo"}).DiskStoragePath("data", "vmdk")
				return err
			},
			want: "disk format",
		},
		{
			name: "layer workload type",
			run: func() error {
				_, err := (&Lab{ID: "demo"}).LayerStoragePath("pod", "web", "data")
				return err
			},
			want: "workload type",
		},
		{
			name: "layer workload id",
			run: func() error {
				_, err := (&Lab{ID: "demo"}).LayerStoragePath("vm", "../web", "data")
				return err
			},
			want: "workload id",
		},
		{
			name: "layer disk id",
			run: func() error {
				_, err := (&Lab{ID: "demo"}).LayerStoragePath("vm", "web", "../data")
				return err
			},
			want: "disk id",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("storage error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateDesiredState(t *testing.T) {
	l := &Lab{
		ID: "demo",
		VMs: []VM{{
			ID:           "vm1",
			DesiredState: " Running ",
			MemoryMB:     512,
			CPUs:         1,
			Disk:         "disks/vm1.qcow2",
		}},
		Containers: []Container{{
			ID:           "web",
			DesiredState: "STOPPED",
			Image:        "nginx",
		}},
	}
	l.Normalize()
	if l.VMs[0].DesiredState != DesiredStateRunning {
		t.Fatalf("vm desiredState = %q, want running", l.VMs[0].DesiredState)
	}
	if l.Containers[0].DesiredState != DesiredStateStopped {
		t.Fatalf("container desiredState = %q, want stopped", l.Containers[0].DesiredState)
	}
	if err := l.Validate(); err != nil {
		t.Fatalf("expected desired states to validate, got %v", err)
	}
	if got := DesiredState(""); got != DesiredStateStopped {
		t.Fatalf("empty desired state default = %q, want stopped", got)
	}
}

func TestValidateRejectsInvalidDesiredState(t *testing.T) {
	l := &Lab{
		ID: "demo",
		VMs: []VM{{
			ID:           "vm1",
			DesiredState: "paused",
			MemoryMB:     512,
			CPUs:         1,
			Disk:         "disks/vm1.qcow2",
		}},
		Containers: []Container{{
			ID:           "web",
			DesiredState: "starting",
			Image:        "nginx",
		}},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil {
		t.Fatal("expected invalid desired state errors")
	}
	for _, want := range []string{
		`vm "vm1" desiredState must be running or stopped`,
		`container "web" desiredState must be running or stopped`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in validation error, got %v", want, err)
		}
	}
}

func TestValidateAcceptsDirectNetworkLinks(t *testing.T) {
	l := &Lab{
		ID: "demo",
		VMs: []VM{
			{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "disks/vm1.qcow2", Networks: []VMNetwork{{}}},
			{ID: "vm2", MemoryMB: 512, CPUs: 1, Disk: "disks/vm2.qcow2", Networks: []VMNetwork{{}}},
		},
		NetworkLinks: []NetworkLink{{
			From: NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 0},
			To:   NetworkEndpoint{Type: "vm", ID: "vm2", NIC: 0},
		}},
	}
	l.Normalize()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected direct network link to validate, got %v", err)
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

func TestNormalizeMigratesSwitchExternalLinkToExternalLinks(t *testing.T) {
	l := &Lab{
		ID: "demo",
		ExternalLinks: []ExternalLink{
			{ID: "uplink1", Interface: "eth0"},
			{ID: "uplink2", Interface: "eth1"},
		},
		Switches: []Switch{{
			ID:            "sw1",
			Mode:          "bridge",
			ExternalLink:  " uplink1 ",
			ExternalLinks: []string{" uplink2 ", "uplink1"},
		}},
	}
	l.Normalize()
	if l.Switches[0].ExternalLink != "" {
		t.Fatalf("legacy externalLink kept after normalize: %#v", l.Switches[0])
	}
	if got, want := SwitchExternalLinks(l.Switches[0]), []string{"uplink2", "uplink1"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("switch externalLinks = %#v, want %#v", got, want)
	}
	if err := l.Validate(); err != nil {
		t.Fatalf("expected multi-uplink switch config to validate, got %v", err)
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
		`vm "vm1" network must not reference both switch and externalLink`,
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
	if got := l.ManagedSwitchBridgeName(Switch{ID: "SW1"}); got != "flfoxlabdbb6ab2" {
		t.Fatalf("unexpected bridge name %q", got)
	}
}

func TestManagedBridgeNamesAreShortAndUniqueWhenTruncated(t *testing.T) {
	l := &Lab{ID: "demo"}
	switchA := l.ManagedSwitchBridgeName(Switch{ID: "very-long-switch-alpha"})
	switchB := l.ManagedSwitchBridgeName(Switch{ID: "very-long-switch-beta"})
	externalA := l.ManagedExternalBridgeName(ExternalLink{ID: "very-long-uplink-alpha"})
	externalB := l.ManagedExternalBridgeName(ExternalLink{ID: "very-long-uplink-beta"})

	names := []string{switchA, switchB, externalA, externalB}
	seen := map[string]bool{}
	for _, name := range names {
		if len(name) > 15 {
			t.Fatalf("managed bridge name %q is longer than Linux IFNAMSIZ limit", name)
		}
		if seen[name] {
			t.Fatalf("managed bridge name %q was reused for distinct topology resources", name)
		}
		seen[name] = true
	}
}

func TestValidateAcceptsContainers(t *testing.T) {
	l := &Lab{
		ID:            "demo",
		Switches:      []Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []ExternalLink{{ID: "uplink1", Interface: "br0"}},
		Containers: []Container{{
			ID:      "web",
			Image:   "docker.io/library/nginx:latest",
			Command: []string{"nginx", "-g", "daemon off;"},
			Env:     map[string]string{"ENV": "test"},
			Networks: []ContainerNetwork{{
				Switch: "lan",
				MAC:    "02:00:00:00:00:10",
			}, {
				ExternalLink: "uplink1",
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
			{ID: "web", Networks: []ContainerNetwork{{Switch: "missing"}, {Switch: "lan", ExternalLink: "uplink1"}, {ExternalLink: "missing-uplink"}}},
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
		`container "web" network must not reference both switch and externalLink`,
		`container "web" references missing external link "missing-uplink"`,
		`duplicate container id "web"`,
		`layout link references missing container "missing"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q in validation error, got %v", want, err)
		}
	}
}

func TestValidateRejectsCrossTypeNodeIDCollision(t *testing.T) {
	l := &Lab{
		ID:         "demo",
		VMs:        []VM{{ID: "web", MemoryMB: 512, CPUs: 1}},
		Containers: []Container{{ID: "web", Image: "nginx"}},
	}
	l.Normalize()
	err := l.Validate()
	if err == nil || !strings.Contains(err.Error(), `node id "web" is used by both vm and container`) {
		t.Fatalf("expected cross-type node id collision error, got %v", err)
	}
}

func TestLoadFileKnownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lab")
	writeTestFile(t, path, "name: demo\nunknown: true\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected known-fields validation error")
	}
}

func TestLoadFileRejectsMultipleYAMLDocuments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lab")
	writeTestFile(t, path, "name: demo\n---\nname: ignored\n")
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("LoadFile error = %v, want multiple documents error", err)
	}
}

func TestLoadFileRejectsDuplicateYAMLKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lab")
	writeTestFile(t, path, "name: demo\nname: ignored\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected duplicate key error")
	}
}

func TestLoadFileAllowsDisksField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lab")
	writeTestFile(t, path, `name: demo
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

func TestLoadFileAllowsManagedDiskFieldsAndContainerDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lab")
	writeTestFile(t, path, `name: demo
containers:
  - id: web
    image: nginx
    disk: /tmp/web-layer.qcow2
disks:
  - id: data
    path: /tmp/data.qcow2
    sizeGB: 10
    format: qcow2
    kind: base
  - id: container-web-data
    path: /tmp/web-layer.qcow2
    format: qcow2
    kind: layer
    base: data
    attachedType: container
    attachedTo: web
`)
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if loaded.Containers[0].Disk != "/tmp/web-layer.qcow2" {
		t.Fatalf("container disk = %q", loaded.Containers[0].Disk)
	}
	if got := loaded.Disks[1]; got.Kind != "layer" || got.Base != "data" || got.AttachedType != "container" || got.AttachedTo != "web" {
		t.Fatalf("layer disk = %#v", got)
	}
}

func TestLoadFileNormalizesLayoutLinkEndpoints(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lab")
	writeTestFile(t, path, `name: demo
vms:
  - id: vm1
    memoryMB: 512
    cpus: 1
switches:
  - id: lan
    mode: bridge
layout:
  links:
    - from:
        type: " VM "
        id: " vm1 "
      to:
        type: " SWITCH "
        id: " lan "
`)

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if got := loaded.Layout.Links[0].From; got != (LayoutEndpoint{Type: "vm", ID: "vm1"}) {
		t.Fatalf("from endpoint = %#v, want normalized vm endpoint", got)
	}
	if got := loaded.Layout.Links[0].To; got != (LayoutEndpoint{Type: "switch", ID: "lan"}) {
		t.Fatalf("to endpoint = %#v, want normalized switch endpoint", got)
	}
}

func TestLoadFileAcceptsLegacyTopLevelID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.lab")
	writeTestFile(t, path, "id: demo\n")
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if loaded.ID != "demo" {
		t.Fatalf("loaded name = %q, want demo", loaded.ID)
	}
}

func TestLoadFileRejectsTopLevelNameAndID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.lab")
	writeTestFile(t, path, "name: demo\nid: legacy\n")
	if _, err := LoadFile(path); err == nil {
		t.Fatal("expected mixed top-level name/id to be rejected")
	}
}

func TestListFilesIncludesOnlyLabExtension(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "demo.lab"), "name: demo\n")
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

func TestCloneDeepCopiesMutableLabState(t *testing.T) {
	original := &Lab{
		ID: "demo",
		VMs: []VM{{
			ID:       "vm1",
			Networks: []VMNetwork{{Switch: "lan"}},
		}},
		Containers: []Container{{
			ID:       "web",
			Command:  []string{"nginx"},
			Env:      map[string]string{"MODE": "prod"},
			Networks: []ContainerNetwork{{Switch: "lan"}},
		}},
		Switches:      []Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []ExternalLink{{ID: "uplink", Interface: "eth0"}},
		NetworkLinks:  []NetworkLink{{From: NetworkEndpoint{Type: "vm", ID: "vm1"}, To: NetworkEndpoint{Type: "container", ID: "web"}}},
		Disks:         []Disk{{ID: "disk1", Path: "disks/disk1.qcow2"}},
		Layout: Layout{
			Nodes: map[string]Position{"vm1": {X: 1, Y: 2}},
			Links: []LayoutLink{{From: LayoutEndpoint{Type: "vm", ID: "vm1"}, To: LayoutEndpoint{Type: "switch", ID: "lan"}}},
		},
		Meta: map[string]string{"containerd.address": "/run/containerd/containerd.sock"},
		path: "/tmp/demo.lab",
		root: "/tmp",
	}

	cloned := Clone(original)
	if cloned == original {
		t.Fatal("Clone returned original pointer")
	}
	if cloned.Path() != original.Path() || cloned.Root() != original.Root() {
		t.Fatalf("clone path/root = %q/%q, want %q/%q", cloned.Path(), cloned.Root(), original.Path(), original.Root())
	}

	original.VMs[0].Networks[0].Switch = "changed"
	original.Containers[0].Command[0] = "changed"
	original.Containers[0].Env["MODE"] = "dev"
	original.Containers[0].Networks[0].Switch = "changed"
	original.Layout.Nodes["vm1"] = Position{X: 9, Y: 9}
	original.Meta["containerd.address"] = "changed"

	if cloned.VMs[0].Networks[0].Switch != "lan" {
		t.Fatalf("clone VM network mutated with original: %#v", cloned.VMs[0].Networks[0])
	}
	if cloned.Containers[0].Command[0] != "nginx" {
		t.Fatalf("clone command mutated with original: %#v", cloned.Containers[0].Command)
	}
	if cloned.Containers[0].Env["MODE"] != "prod" {
		t.Fatalf("clone env mutated with original: %#v", cloned.Containers[0].Env)
	}
	if cloned.Containers[0].Networks[0].Switch != "lan" {
		t.Fatalf("clone container network mutated with original: %#v", cloned.Containers[0].Networks[0])
	}
	if cloned.Layout.Nodes["vm1"] != (Position{X: 1, Y: 2}) {
		t.Fatalf("clone layout nodes mutated with original: %#v", cloned.Layout.Nodes)
	}
	if cloned.Meta["containerd.address"] != "/run/containerd/containerd.sock" {
		t.Fatalf("clone meta mutated with original: %#v", cloned.Meta)
	}
}

func TestSaveFileDoesNotMutateInputLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	original := &Lab{
		ID: "demo",
		VMs: []VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     " disks/vm1.qcow2 ",
			Networks: []VMNetwork{{Switch: " lan "}},
		}},
		Containers: []Container{{
			ID:      "web",
			Image:   " docker.io/library/nginx:latest ",
			Command: []string{" nginx ", " -g ", " daemon off; "},
			Networks: []ContainerNetwork{{
				ExternalLink: " uplink ",
				MAC:          " 02:00:00:00:00:01 ",
			}},
		}},
		Switches: []Switch{{ID: "lan", Mode: " bridge "}},
		ExternalLinks: []ExternalLink{{
			ID:        "uplink",
			Interface: " eth0 ",
		}},
		Layout: Layout{
			Nodes: map[string]Position{"vm1": {X: 1, Y: 2}},
		},
		Meta: map[string]string{"containerd.address": " /run/containerd/containerd.sock "},
	}

	if err := SaveFile(path, original); err != nil {
		t.Fatalf("SaveFile returned error: %v", err)
	}

	if original.VMs[0].Disk != " disks/vm1.qcow2 " {
		t.Fatalf("SaveFile mutated VM disk to %q", original.VMs[0].Disk)
	}
	if original.VMs[0].Networks[0].Switch != " lan " {
		t.Fatalf("SaveFile mutated VM network switch to %q", original.VMs[0].Networks[0].Switch)
	}
	if original.Containers[0].Image != " docker.io/library/nginx:latest " {
		t.Fatalf("SaveFile mutated container image to %q", original.Containers[0].Image)
	}
	if original.Containers[0].Command[0] != " nginx " {
		t.Fatalf("SaveFile mutated container command to %#v", original.Containers[0].Command)
	}
	if original.Containers[0].Networks[0].ExternalLink != " uplink " {
		t.Fatalf("SaveFile mutated container network to %#v", original.Containers[0].Networks[0])
	}
	if original.Switches[0].Mode != " bridge " {
		t.Fatalf("SaveFile mutated switch mode to %q", original.Switches[0].Mode)
	}
	if original.ExternalLinks[0].Interface != " eth0 " {
		t.Fatalf("SaveFile mutated external interface to %q", original.ExternalLinks[0].Interface)
	}
	if original.Meta["containerd.address"] != " /run/containerd/containerd.sock " {
		t.Fatalf("SaveFile mutated meta to %#v", original.Meta)
	}
}

func TestSaveFileAtomicallyReplacesExistingLab(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lab")
	if err := SaveFile(path, &Lab{ID: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveFile(path, &Lab{ID: "new"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != "new" {
		t.Fatalf("loaded lab name = %q, want new", loaded.ID)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("saved mode = %v, want preserved 0600", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".demo.lab.tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary save files left behind: %#v", matches)
	}
}

func TestSaveFileRejectsNilLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")

	err := SaveFile(path, nil)
	if err == nil || !strings.Contains(err.Error(), "missing lab") {
		t.Fatalf("SaveFile error = %v, want missing lab", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("SaveFile created path for nil lab: %v", statErr)
	}
}

func writeTestFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
