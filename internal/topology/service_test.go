package topology

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestServiceMutationsPersistAndRefreshLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		Switches: []lab.Switch{
			{ID: "sw1", Mode: "bridge"},
		},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	if got, want := service.VMCreate("vm1", map[string]string{"switch": "sw1", "memory": "4096"}), "created vm:vm1"; got != want {
		t.Fatalf("VMCreate() = %q, want %q", got, want)
	}
	vm, ok := service.LabVM("vm1")
	if !ok {
		t.Fatalf("vm1 was not present after create")
	}
	if vm.MemoryMB != 4096 || len(vm.Networks) != 1 || vm.Networks[0].Switch != "sw1" {
		t.Fatalf("vm1 was not refreshed from persisted lab: %+v", vm)
	}

	if got, want := service.SwitchDelete("sw1"), "deleted switch:sw1"; got != want {
		t.Fatalf("SwitchDelete() = %q, want %q", got, want)
	}
	vm, ok = service.LabVM("vm1")
	if !ok {
		t.Fatalf("vm1 disappeared after deleting switch")
	}
	if len(vm.Networks) != 1 || vm.Networks[0].Switch != "" {
		t.Fatalf("vm1 did not keep disconnected nic after deleting switch: %+v", vm.Networks)
	}
	if service.HasLabSwitch("sw1") {
		t.Fatalf("deleted switch still present in lab")
	}
}

func TestServiceVMNICDeleteReindexesDirectLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "disks/vm1.qcow2", Networks: []lab.VMNetwork{{}, {}, {}}},
			{ID: "vm2", MemoryMB: 512, CPUs: 1, Disk: "disks/vm2.qcow2", Networks: []lab.VMNetwork{{}, {}}},
		},
		NetworkLinks: []lab.NetworkLink{
			{From: lab.NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 1}, To: lab.NetworkEndpoint{Type: "vm", ID: "vm2", NIC: 0}},
			{From: lab.NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 2}, To: lab.NetworkEndpoint{Type: "vm", ID: "vm2", NIC: 1}},
		},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	if got, want := service.VMNICDelete("vm1", "1"), "deleted nic from vm:vm1 nic1"; got != want {
		t.Fatalf("VMNICDelete() = %q, want %q", got, want)
	}
	vm, ok := service.LabVM("vm1")
	if !ok {
		t.Fatal("vm1 missing after nic delete")
	}
	if len(vm.Networks) != 2 {
		t.Fatalf("vm1 networks = %#v, want 2 nics", vm.Networks)
	}
	if len(service.Lab.NetworkLinks) != 1 {
		t.Fatalf("network links = %#v, want only link for shifted nic", service.Lab.NetworkLinks)
	}
	link := service.Lab.NetworkLinks[0]
	if link.From.ID != "vm1" || link.From.NIC != 1 || link.To.ID != "vm2" || link.To.NIC != 1 {
		t.Fatalf("reindexed link = %#v", link)
	}
}

func TestServiceNICIndexArgumentsTrimWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm1.qcow2",
			Networks: []lab.VMNetwork{{}},
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	if got, want := service.VMNICConnect("vm1", " 0 ", map[string]string{"to": "lan"}), "connected nic to vm:vm1"; got != want {
		t.Fatalf("VMNICConnect() = %q, want %q", got, want)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Networks[0].Switch != "lan" {
		t.Fatalf("vm nic switch = %q, want lan", reloaded.VMs[0].Networks[0].Switch)
	}
}

func TestServiceContainerNICDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{{
			ID:       "web",
			Image:    "docker.io/library/nginx:latest",
			Networks: []lab.ContainerNetwork{{}, {}},
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	if got, want := service.ContainerNICDelete("web", "0"), "deleted nic from container:web nic0"; got != want {
		t.Fatalf("ContainerNICDelete() = %q, want %q", got, want)
	}
	ct, ok := service.LabContainer("web")
	if !ok {
		t.Fatal("container missing after nic delete")
	}
	if len(ct.Networks) != 1 {
		t.Fatalf("container networks = %#v, want one nic", ct.Networks)
	}
}

func TestServiceContainerSetClearsEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{{
			ID:    "web",
			Image: "nginx",
			Env: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	if got, want := service.ContainerSet("web", map[string]string{"env": ""}), "configured container:web"; got != want {
		t.Fatalf("ContainerSet() = %q, want %q", got, want)
	}
	ct, ok := service.LabContainer("web")
	if !ok {
		t.Fatal("container missing after env clear")
	}
	if len(ct.Env) != 0 {
		t.Fatalf("service env = %#v, want empty", ct.Env)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("reload lab: %v", err)
	}
	if len(reloaded.Containers[0].Env) != 0 {
		t.Fatalf("persisted env = %#v, want empty", reloaded.Containers[0].Env)
	}
}

func TestServiceDesiredStatePersistsAndRefreshesLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm1.qcow2",
		}},
		Containers: []lab.Container{{
			ID:    "web",
			Image: "nginx",
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	if got, want := service.VMDesiredState("vm1", lab.DesiredStateRunning), "desired vm:vm1 running"; got != want {
		t.Fatalf("VMDesiredState() = %q, want %q", got, want)
	}
	if got, want := service.ContainerDesiredState("web", lab.DesiredStateStopped), "desired container:web stopped"; got != want {
		t.Fatalf("ContainerDesiredState() = %q, want %q", got, want)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("reload lab: %v", err)
	}
	if reloaded.VMs[0].DesiredState != lab.DesiredStateRunning {
		t.Fatalf("vm desiredState = %q, want running", reloaded.VMs[0].DesiredState)
	}
	if reloaded.Containers[0].DesiredState != lab.DesiredStateStopped {
		t.Fatalf("container desiredState = %q, want stopped", reloaded.Containers[0].DesiredState)
	}
}

func TestServiceCreateRejectsEmptyIDWithoutMutatingLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "existing", Mode: "bridge"}},
		Layout: lab.Layout{
			Nodes: map[string]lab.Position{"existing": {X: 1, Y: 2}},
		},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	tests := []struct {
		name string
		run  func() string
		want string
	}{
		{
			name: "vm",
			run:  func() string { return service.VMCreate("", map[string]string{}) },
			want: "usage: vm create <id> [cpus=N] [memory=N] [switch=ID|uplink=ID]",
		},
		{
			name: "vm invalid",
			run:  func() string { return service.VMCreate("bad/id", map[string]string{}) },
			want: "invalid vm id: bad/id",
		},
		{
			name: "container",
			run:  func() string { return service.ContainerCreate("", map[string]string{}) },
			want: "usage: container create <id> [image=REF] [command=CMD] [switch=ID]",
		},
		{
			name: "container invalid",
			run:  func() string { return service.ContainerCreate("bad/id", map[string]string{}) },
			want: "invalid container id: bad/id",
		},
		{
			name: "switch",
			run:  func() string { return service.SwitchCreate("", map[string]string{}) },
			want: "usage: switch create <id> [mode=bridge|nat|macnat-bridge] [uplink=ID]",
		},
		{
			name: "switch invalid",
			run:  func() string { return service.SwitchCreate("bad/id", map[string]string{}) },
			want: "invalid switch id: bad/id",
		},
		{
			name: "external",
			run:  func() string { return service.ExternalCreate("", map[string]string{}) },
			want: "usage: uplink create <id> interface=IFACE [mode=nat|direct|macnat]",
		},
		{
			name: "external invalid",
			run:  func() string { return service.ExternalCreate("bad/id", map[string]string{"interface": "eth0"}) },
			want: "invalid uplink id: bad/id",
		},
		{
			name: "external interface",
			run:  func() string { return service.ExternalCreate("uplink", map[string]string{}) },
			want: "usage: uplink create <id> interface=IFACE [mode=nat|direct|macnat]",
		},
	}

	for _, tt := range tests {
		if got := tt.run(); got != tt.want {
			t.Fatalf("%s create = %q, want %q", tt.name, got, tt.want)
		}
	}
	if len(service.Lab.VMs) != 0 || len(service.Lab.Containers) != 0 || len(service.Lab.Switches) != 1 || len(service.Lab.ExternalLinks) != 0 {
		t.Fatalf("empty-id create mutated lab: %#v", service.Lab)
	}
	if service.Lab.Switches[0].ID != "existing" {
		t.Fatalf("empty-id create mutated existing switch: %#v", service.Lab.Switches)
	}
	if len(service.Lab.Layout.Nodes) != 1 || service.Lab.Layout.Nodes["existing"] != (lab.Position{X: 1, Y: 2}) {
		t.Fatalf("empty-id create mutated layout: %#v", service.Lab.Layout.Nodes)
	}
}

func TestServiceSwitchAndExternalRejectUnsupportedArgs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		Switches: []lab.Switch{{
			ID:   "lan",
			Mode: "bridge",
		}},
		ExternalLinks: []lab.ExternalLink{{
			ID:        "uplink",
			Interface: "eth0",
			Mode:      lab.ExternalModeNAT,
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	tests := []struct {
		name string
		run  func() string
		want string
	}{
		{
			name: "switch create",
			run:  func() string { return service.SwitchCreate("wan", map[string]string{"mod": "nat"}) },
			want: "unsupported switch create argument: mod",
		},
		{
			name: "switch set",
			run:  func() string { return service.SwitchSet("lan", map[string]string{"mod": "nat"}) },
			want: "unsupported switch set argument: mod",
		},
		{
			name: "external create",
			run:  func() string { return service.ExternalCreate("lte", map[string]string{"iface": "wwan0"}) },
			want: "unsupported uplink create argument: iface",
		},
		{
			name: "external set",
			run:  func() string { return service.ExternalSet("uplink", map[string]string{"iface": "eth1"}) },
			want: "unsupported uplink set argument: iface",
		},
	}

	for _, tt := range tests {
		if got := tt.run(); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}
	if len(service.Lab.Switches) != 1 || service.Lab.Switches[0].ID != "lan" || service.Lab.Switches[0].Mode != "bridge" {
		t.Fatalf("unsupported switch args mutated lab: %#v", service.Lab.Switches)
	}
	if len(service.Lab.ExternalLinks) != 1 || service.Lab.ExternalLinks[0].ID != "uplink" || service.Lab.ExternalLinks[0].Interface != "eth0" {
		t.Fatalf("unsupported external args mutated lab: %#v", service.Lab.ExternalLinks)
	}
}

func TestServiceVMRejectsInvalidTypedArgsWithoutMutatingLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm1.qcow2",
			VNC:      true,
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	tests := []struct {
		name string
		run  func() string
		want string
	}{
		{
			name: "create cpus",
			run:  func() string { return service.VMCreate("bad-cpus", map[string]string{"cpus": "zero"}) },
			want: "invalid vm cpus: zero",
		},
		{
			name: "create memory",
			run:  func() string { return service.VMCreate("bad-memory", map[string]string{"memory": "0"}) },
			want: "invalid vm memory: 0",
		},
		{
			name: "create mem",
			run:  func() string { return service.VMCreate("bad-mem", map[string]string{"mem": "-1"}) },
			want: "invalid vm memory: -1",
		},
		{
			name: "set cpus",
			run:  func() string { return service.VMSet("vm1", map[string]string{"cpus": "zero"}) },
			want: "invalid vm cpus: zero",
		},
		{
			name: "set memory",
			run:  func() string { return service.VMSet("vm1", map[string]string{"memory": "0"}) },
			want: "invalid vm memory: 0",
		},
		{
			name: "set mem",
			run:  func() string { return service.VMSet("vm1", map[string]string{"mem": "-1"}) },
			want: "invalid vm memory: -1",
		},
		{
			name: "set vnc",
			run:  func() string { return service.VMSet("vm1", map[string]string{"vnc": "maybe"}) },
			want: "invalid vm vnc: maybe",
		},
		{
			name: "set mixed before invalid vnc",
			run: func() string {
				return service.VMSet("vm1", map[string]string{"name": "changed", "disk": "changed.qcow2", "vnc": "maybe"})
			},
			want: "invalid vm vnc: maybe",
		},
	}

	for _, tt := range tests {
		if got := tt.run(); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}
	if len(service.Lab.VMs) != 1 {
		t.Fatalf("invalid vm args created vms: %#v", service.Lab.VMs)
	}
	vm := service.Lab.VMs[0]
	if vm.ID != "vm1" || vm.Name != "" || vm.Disk != "disks/vm1.qcow2" || vm.CPUs != 1 || vm.MemoryMB != 512 || !vm.VNC {
		t.Fatalf("invalid vm args mutated vm: %#v", vm)
	}
}

func TestServiceRejectsInvalidConfigBeforeMutatingNewFileLab(t *testing.T) {
	base := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm1.qcow2",
		}},
		Containers: []lab.Container{{ID: "web", Image: "nginx"}},
		Switches: []lab.Switch{{
			ID:   "lan",
			Mode: "bridge",
		}},
		ExternalLinks: []lab.ExternalLink{{
			ID:        "uplink",
			Interface: "eth0",
			Mode:      lab.ExternalModeNAT,
		}},
		Layout: lab.Layout{Nodes: map[string]lab.Position{
			"vm1":    {X: 1, Y: 1},
			"web":    {X: 2, Y: 2},
			"lan":    {X: 3, Y: 3},
			"uplink": {X: 4, Y: 4},
		}},
	}
	tests := []struct {
		name string
		run  func(*Service) string
		want string
	}{
		{
			name: "switch create bad mode",
			run:  func(s *Service) string { return s.SwitchCreate("wan", map[string]string{"mode": "bad"}) },
			want: `switch create failed: switch "wan" uses unsupported mode "bad"; supported modes are bridge, nat and macnat-bridge`,
		},
		{
			name: "switch create missing external",
			run:  func(s *Service) string { return s.SwitchCreate("wan", map[string]string{"external": "missing"}) },
			want: `switch create failed: switch "wan" references missing uplink "missing"`,
		},
		{
			name: "switch set bad mode",
			run:  func(s *Service) string { return s.SwitchSet("lan", map[string]string{"mode": "bad"}) },
			want: `switch config failed: switch "lan" uses unsupported mode "bad"; supported modes are bridge, nat and macnat-bridge`,
		},
		{
			name: "external create bad mode",
			run: func(s *Service) string {
				return s.ExternalCreate("lte", map[string]string{"interface": "wwan0", "mode": "bad"})
			},
			want: `uplink create failed: uplink "lte" uses unsupported mode "bad"; supported modes are nat, direct and macnat`,
		},
		{
			name: "external set bad mode",
			run:  func(s *Service) string { return s.ExternalSet("uplink", map[string]string{"mode": "bad"}) },
			want: `uplink config failed: uplink "uplink" uses unsupported mode "bad"; supported modes are nat, direct and macnat`,
		},
		{
			name: "vm create missing switch",
			run:  func(s *Service) string { return s.VMCreate("vm2", map[string]string{"switch": "missing"}) },
			want: `create failed: vm "vm2" references missing switch "missing"`,
		},
		{
			name: "vm set switch and external",
			run: func(s *Service) string {
				return s.VMSet("vm1", map[string]string{"switch": "lan", "external": "uplink"})
			},
			want: `config failed: vm "vm1" network must not reference both switch and externalLink`,
		},
		{
			name: "vm set missing external",
			run:  func(s *Service) string { return s.VMSet("vm1", map[string]string{"external": "missing"}) },
			want: `config failed: vm "vm1" references missing uplink "missing"`,
		},
		{
			name: "container create switch and external",
			run: func(s *Service) string {
				return s.ContainerCreate("api", map[string]string{"image": "alpine", "switch": "lan", "external": "uplink"})
			},
			want: `container create failed: container "api" network must not reference both switch and externalLink`,
		},
		{
			name: "container create missing external",
			run: func(s *Service) string {
				return s.ContainerCreate("api", map[string]string{"image": "alpine", "external": "missing"})
			},
			want: `container create failed: container "api" references missing uplink "missing"`,
		},
		{
			name: "container set switch and external",
			run: func(s *Service) string {
				return s.ContainerSet("web", map[string]string{"switch": "lan", "external": "uplink"})
			},
			want: `container config failed: container "web" network must not reference both switch and externalLink`,
		},
		{
			name: "container set missing switch",
			run:  func(s *Service) string { return s.ContainerSet("web", map[string]string{"switch": "missing"}) },
			want: `container config failed: container "web" references missing switch "missing"`,
		},
	}

	for _, tt := range tests {
		initial := lab.Clone(base)
		path := filepath.Join(t.TempDir(), "new.lab")
		service := NewService(lab.Clone(initial), path)
		if got := tt.run(service); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
		if !reflect.DeepEqual(service.Lab, initial) {
			t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.Lab, initial)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s created lab file or stat failed: %v", tt.name, err)
		}
	}
}

func TestServiceSwitchSetAppendsExternalLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		Switches: []lab.Switch{{
			ID:            "lan",
			Mode:          "bridge",
			ExternalLinks: []string{"uplink1"},
		}},
		ExternalLinks: []lab.ExternalLink{
			{ID: "uplink1", Interface: "eth0", Mode: lab.ExternalModeNAT},
			{ID: "uplink2", Interface: "eth1", Mode: lab.ExternalModeNAT},
		},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.SwitchSet("lan", map[string]string{"external": "uplink2"}); got != "configured switch:lan" {
		t.Fatalf("SwitchSet = %q", got)
	}

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := lab.SwitchExternalLinks(reloaded.Switches[0]), []string{"uplink1", "uplink2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("switch externalLinks = %#v, want %#v", got, want)
	}
}

func TestServiceRollsBackWhenSaveFailsForNewPath(t *testing.T) {
	restore := stubDiskCommands(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	base := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "disks/vm1.qcow2", Networks: []lab.VMNetwork{{}}},
			{ID: "vm2", MemoryMB: 512, CPUs: 1, Disk: "disks/vm2.qcow2", Networks: []lab.VMNetwork{{}}},
		},
		Containers: []lab.Container{
			{ID: "web", Image: "nginx", Networks: []lab.ContainerNetwork{{}}},
			{ID: "api", Image: "alpine", Networks: []lab.ContainerNetwork{{}}},
		},
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink", Interface: "eth0", Mode: lab.ExternalModeNAT}},
		NetworkLinks: []lab.NetworkLink{{
			From: lab.NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 0},
			To:   lab.NetworkEndpoint{Type: "container", ID: "web", NIC: 0},
		}},
		Disks: []lab.Disk{{
			ID:           "data",
			Path:         "disks/data.qcow2",
			Format:       "qcow2",
			Kind:         "base",
			AttachedType: "vm",
			AttachedTo:   "vm1",
		}, {
			ID:     "free",
			Path:   "disks/free.qcow2",
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	tests := []struct {
		name string
		run  func(*Service) string
	}{
		{name: "vm desired", run: func(s *Service) string { return s.VMDesiredState("vm1", lab.DesiredStateRunning) }},
		{name: "container desired", run: func(s *Service) string { return s.ContainerDesiredState("web", lab.DesiredStateRunning) }},
		{name: "vm create", run: func(s *Service) string { return s.VMCreate("vm3", map[string]string{"switch": "lan"}) }},
		{name: "vm set", run: func(s *Service) string { return s.VMSet("vm1", map[string]string{"name": "changed"}) }},
		{name: "vm delete", run: func(s *Service) string { return s.VMDelete("vm2") }},
		{name: "container create", run: func(s *Service) string {
			return s.ContainerCreate("db", map[string]string{"image": "postgres", "switch": "lan"})
		}},
		{name: "container set", run: func(s *Service) string { return s.ContainerSet("web", map[string]string{"image": "redis"}) }},
		{name: "container delete", run: func(s *Service) string { return s.ContainerDelete("api") }},
		{name: "switch create", run: func(s *Service) string { return s.SwitchCreate("wan", map[string]string{"mode": "bridge"}) }},
		{name: "switch set", run: func(s *Service) string { return s.SwitchSet("lan", map[string]string{"name": "LAN"}) }},
		{name: "switch delete", run: func(s *Service) string { return s.SwitchDelete("lan") }},
		{name: "external create", run: func(s *Service) string { return s.ExternalCreate("lte", map[string]string{"interface": "wwan0"}) }},
		{name: "external set", run: func(s *Service) string { return s.ExternalSet("uplink", map[string]string{"name": "WAN"}) }},
		{name: "external delete", run: func(s *Service) string { return s.ExternalDelete("uplink") }},
		{name: "vm nic add", run: func(s *Service) string { return s.VMNICAdd("vm1", nil) }},
		{name: "vm nic connect", run: func(s *Service) string { return s.VMNICConnect("vm2", "0", map[string]string{"switch": "lan"}) }},
		{name: "vm nic delete", run: func(s *Service) string { return s.VMNICDelete("vm2", "0") }},
		{name: "container nic add", run: func(s *Service) string { return s.ContainerNICAdd("web", nil) }},
		{name: "container nic connect", run: func(s *Service) string { return s.ContainerNICConnect("api", "0", map[string]string{"switch": "lan"}) }},
		{name: "container nic delete", run: func(s *Service) string { return s.ContainerNICDelete("api", "0") }},
		{name: "direct connect", run: func(s *Service) string { return s.NICConnectDirect("vm", "vm2", "0", "container", "api") }},
		{name: "direct connect to", run: func(s *Service) string { return s.NICConnectDirectTo("vm", "vm2", "0", "container", "api", "0") }},
		{name: "direct disconnect", run: func(s *Service) string { return s.NICDisconnect("vm", "vm1", "0") }},
		{name: "disk detach", run: func(s *Service) string { return s.DiskDetach("vm1", map[string]string{"type": "vm"}) }},
		{name: "disk attach base", run: func(s *Service) string { return s.DiskAttach("free", map[string]string{"to": "vm:vm2"}) }},
	}

	for _, tt := range tests {
		initial := lab.Clone(base)
		blocker := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocker, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		service := NewService(lab.Clone(initial), filepath.Join(blocker, "demo.lab"))
		got := tt.run(service)
		if !strings.Contains(got, "failed:") {
			t.Fatalf("%s = %q, want save failure", tt.name, got)
		}
		if !reflect.DeepEqual(service.Lab, initial) {
			t.Fatalf("%s failed save mutated lab:\ngot  %#v\nwant %#v", tt.name, service.Lab, initial)
		}
	}
}

func TestServiceMutationsRequireSavePathBeforeMutatingLab(t *testing.T) {
	service := NewService(&lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "disks/vm1.qcow2", Networks: []lab.VMNetwork{{}}},
			{ID: "vm2", MemoryMB: 512, CPUs: 1, Disk: "disks/vm2.qcow2"},
		},
		Containers: []lab.Container{{
			ID:       "web",
			Image:    "nginx",
			Networks: []lab.ContainerNetwork{{}},
		}},
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink", Interface: "eth0", Mode: lab.ExternalModeNAT}},
		NetworkLinks: []lab.NetworkLink{{
			From: lab.NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 0},
			To:   lab.NetworkEndpoint{Type: "container", ID: "web", NIC: 0},
		}},
	}, "")

	tests := []struct {
		name string
		run  func() string
		want string
	}{
		{
			name: "vm set",
			run:  func() string { return service.VMSet("vm1", map[string]string{"cpus": "2"}) },
			want: "config failed: missing lab path",
		},
		{
			name: "vm delete",
			run:  func() string { return service.VMDelete("vm1") },
			want: "delete failed: missing lab path",
		},
		{
			name: "container delete",
			run:  func() string { return service.ContainerDelete("web") },
			want: "container delete failed: missing lab path",
		},
		{
			name: "switch delete",
			run:  func() string { return service.SwitchDelete("lan") },
			want: "switch delete failed: missing lab path",
		},
		{
			name: "external delete",
			run:  func() string { return service.ExternalDelete("uplink") },
			want: "uplink delete failed: missing lab path",
		},
		{
			name: "nic delete",
			run:  func() string { return service.VMNICDelete("vm1", "0") },
			want: "nic delete failed: missing lab path",
		},
		{
			name: "direct disconnect",
			run:  func() string { return service.NICDisconnect("vm", "vm1", "0") },
			want: "nic disconnect failed: missing lab path",
		},
		{
			name: "desired state",
			run:  func() string { return service.VMDesiredState("vm1", lab.DesiredStateRunning) },
			want: "desired state failed: missing lab path",
		},
	}

	for _, tt := range tests {
		if got := tt.run(); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}
	if len(service.Lab.VMs) != 2 || service.Lab.VMs[0].ID != "vm1" || service.Lab.VMs[0].CPUs != 1 || service.Lab.VMs[1].ID != "vm2" {
		t.Fatalf("missing-path operations mutated vms: %#v", service.Lab.VMs)
	}
	if len(service.Lab.Containers) != 1 || service.Lab.Containers[0].ID != "web" {
		t.Fatalf("missing-path operations mutated containers: %#v", service.Lab.Containers)
	}
	if len(service.Lab.Switches) != 1 || service.Lab.Switches[0].ID != "lan" {
		t.Fatalf("missing-path operations mutated switches: %#v", service.Lab.Switches)
	}
	if len(service.Lab.ExternalLinks) != 1 || service.Lab.ExternalLinks[0].ID != "uplink" {
		t.Fatalf("missing-path operations mutated externals: %#v", service.Lab.ExternalLinks)
	}
	if len(service.Lab.NetworkLinks) != 1 {
		t.Fatalf("missing-path operations mutated network links: %#v", service.Lab.NetworkLinks)
	}
	if len(service.Lab.VMs[0].Networks) != 1 {
		t.Fatalf("missing-path operations mutated vm nics: %#v", service.Lab.VMs[0].Networks)
	}
}

func TestWorkloadNetworkRefsValidateBeforeSavePath(t *testing.T) {
	base := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Networks: []lab.VMNetwork{{}},
		}},
		Containers: []lab.Container{{
			ID:       "web",
			Image:    "nginx",
			Networks: []lab.ContainerNetwork{{}},
		}},
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink", Interface: "eth0", Mode: lab.ExternalModeNAT}},
		Layout: lab.Layout{Nodes: map[string]lab.Position{
			"vm1": {X: 1, Y: 1},
			"web": {X: 2, Y: 2},
		}},
	}
	tests := []struct {
		name string
		run  func(*Service) string
		want string
	}{
		{
			name: "vm create",
			run:  func(s *Service) string { return s.VMCreate("vm2", map[string]string{"switch": "missing"}) },
			want: `create failed: vm "vm2" references missing switch "missing"`,
		},
		{
			name: "vm set",
			run:  func(s *Service) string { return s.VMSet("vm1", map[string]string{"external": "missing"}) },
			want: `config failed: vm "vm1" references missing uplink "missing"`,
		},
		{
			name: "container create",
			run: func(s *Service) string {
				return s.ContainerCreate("api", map[string]string{"image": "alpine", "external": "missing"})
			},
			want: `container create failed: container "api" references missing uplink "missing"`,
		},
		{
			name: "container set",
			run:  func(s *Service) string { return s.ContainerSet("web", map[string]string{"switch": "missing"}) },
			want: `container config failed: container "web" references missing switch "missing"`,
		},
	}

	for _, tt := range tests {
		initial := lab.Clone(base)
		service := NewService(lab.Clone(initial), "")
		if got := tt.run(service); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
		if !reflect.DeepEqual(service.Lab, initial) {
			t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.Lab, initial)
		}
	}
}

func TestNICMACArgsValidateBeforeSavePath(t *testing.T) {
	base := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Networks: []lab.VMNetwork{{}},
		}},
		Containers: []lab.Container{{
			ID:       "web",
			Image:    "nginx",
			Networks: []lab.ContainerNetwork{{}},
		}},
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	tests := []struct {
		name string
		run  func(*Service) string
		want string
	}{
		{
			name: "vm nic add",
			run:  func(s *Service) string { return s.VMNICAdd("vm1", map[string]string{"mac": "not-a-mac"}) },
			want: "invalid vm nic mac: not-a-mac",
		},
		{
			name: "vm nic connect",
			run: func(s *Service) string {
				return s.VMNICConnect("vm1", "0", map[string]string{"to": "lan", "mac": "not-a-mac"})
			},
			want: "invalid vm nic mac: not-a-mac",
		},
		{
			name: "container create",
			run: func(s *Service) string {
				return s.ContainerCreate("api", map[string]string{"image": "alpine", "switch": "lan", "mac": "not-a-mac"})
			},
			want: "invalid container nic mac: not-a-mac",
		},
		{
			name: "container set",
			run: func(s *Service) string {
				return s.ContainerSet("web", map[string]string{"switch": "lan", "mac": "not-a-mac"})
			},
			want: "invalid container nic mac: not-a-mac",
		},
		{
			name: "container nic add",
			run:  func(s *Service) string { return s.ContainerNICAdd("web", map[string]string{"mac": "not-a-mac"}) },
			want: "invalid container nic mac: not-a-mac",
		},
		{
			name: "container nic connect",
			run: func(s *Service) string {
				return s.ContainerNICConnect("web", "0", map[string]string{"to": "lan", "mac": "not-a-mac"})
			},
			want: "invalid container nic mac: not-a-mac",
		},
	}

	for _, tt := range tests {
		initial := lab.Clone(base)
		service := NewService(lab.Clone(initial), "")
		if got := tt.run(service); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
		if !reflect.DeepEqual(service.Lab, initial) {
			t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.Lab, initial)
		}
	}
}

func TestCreateRejectsCrossTypeNodeIDBeforeSavePath(t *testing.T) {
	base := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
		}},
		Containers:    []lab.Container{{ID: "web", Image: "nginx"}},
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink", Interface: "eth0", Mode: lab.ExternalModeNAT}},
		Layout: lab.Layout{Nodes: map[string]lab.Position{
			"vm1":    {X: 1, Y: 1},
			"web":    {X: 2, Y: 2},
			"lan":    {X: 3, Y: 3},
			"uplink": {X: 4, Y: 4},
		}},
	}
	tests := []struct {
		name string
		run  func(*Service) string
		want string
	}{
		{
			name: "vm collides with container",
			run:  func(s *Service) string { return s.VMCreate("web", nil) },
			want: "node id already exists as container: web",
		},
		{
			name: "container collides with vm",
			run:  func(s *Service) string { return s.ContainerCreate("vm1", map[string]string{"image": "alpine"}) },
			want: "node id already exists as vm: vm1",
		},
		{
			name: "switch collides with external",
			run:  func(s *Service) string { return s.SwitchCreate("uplink", nil) },
			want: "node id already exists as uplink: uplink",
		},
		{
			name: "external collides with switch",
			run:  func(s *Service) string { return s.ExternalCreate("lan", map[string]string{"interface": "eth0"}) },
			want: "node id already exists as switch: lan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initial := lab.Clone(base)
			service := NewService(lab.Clone(initial), "")
			if got := tt.run(service); got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
			}
			if !reflect.DeepEqual(service.Lab, initial) {
				t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.Lab, initial)
			}
		})
	}
}

func TestNextNodeIDsSkipCrossTypeCollisions(t *testing.T) {
	tests := []struct {
		name string
		lab  *lab.Lab
		got  func(*Service) string
		want string
	}{
		{
			name: "vm skips container id",
			lab:  &lab.Lab{ID: "demo", Containers: []lab.Container{{ID: "vm2", Image: "nginx"}}},
			got:  func(s *Service) string { return s.NextVMID() },
			want: "vm3",
		},
		{
			name: "container skips vm id",
			lab:  &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "ct2", MemoryMB: 512, CPUs: 1}}},
			got:  func(s *Service) string { return s.NextContainerID() },
			want: "ct3",
		},
		{
			name: "switch skips external id",
			lab:  &lab.Lab{ID: "demo", ExternalLinks: []lab.ExternalLink{{ID: "sw2", Interface: "eth0", Mode: lab.ExternalModeNAT}}},
			got:  func(s *Service) string { return s.NextSwitchID() },
			want: "sw3",
		},
		{
			name: "external skips switch id",
			lab:  &lab.Lab{ID: "demo", Switches: []lab.Switch{{ID: "uplink2", Mode: "bridge"}}},
			got:  func(s *Service) string { return s.NextExternalID() },
			want: "uplink3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewService(tt.lab, "")
			if got := tt.got(service); got != tt.want {
				t.Fatalf("next id = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestServiceEmptySetArgsAreNoOpBeforeSavePath(t *testing.T) {
	base := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			Name:     "VM 1",
			MemoryMB: 512,
			CPUs:     1,
		}},
		Containers: []lab.Container{{
			ID:    "web",
			Name:  "Web",
			Image: "nginx",
		}},
		Switches: []lab.Switch{{
			ID:   "lan",
			Name: "LAN",
			Mode: "bridge",
		}},
		ExternalLinks: []lab.ExternalLink{{
			ID:        "uplink",
			Name:      "WAN",
			Interface: "eth0",
			Mode:      lab.ExternalModeNAT,
		}},
	}
	tests := []struct {
		name string
		run  func(*Service) string
		want string
	}{
		{
			name: "vm set",
			run:  func(s *Service) string { return s.VMSet("vm1", nil) },
			want: "configured vm:vm1",
		},
		{
			name: "container set",
			run:  func(s *Service) string { return s.ContainerSet("web", nil) },
			want: "configured container:web",
		},
		{
			name: "switch set",
			run:  func(s *Service) string { return s.SwitchSet("lan", nil) },
			want: "configured switch:lan",
		},
		{
			name: "external set",
			run:  func(s *Service) string { return s.ExternalSet("uplink", nil) },
			want: "configured uplink:uplink",
		},
	}

	for _, tt := range tests {
		initial := lab.Clone(base)
		service := NewService(lab.Clone(initial), "")
		if got := tt.run(service); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
		if !reflect.DeepEqual(service.Lab, initial) {
			t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.Lab, initial)
		}
	}
}

func TestNICDisconnectValidatesEndpointBeforeSavePath(t *testing.T) {
	service := NewService(&lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Networks: []lab.VMNetwork{{}}}},
		NetworkLinks: []lab.NetworkLink{{
			From: lab.NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 0},
			To:   lab.NetworkEndpoint{Type: "container", ID: "web", NIC: 0},
		}},
	}, "")

	if got := service.NICDisconnect("pod", "vm1", "0"); got != "direct link target must be vm or container" {
		t.Fatalf("NICDisconnect invalid type = %q, want endpoint validation", got)
	}
	if len(service.Lab.NetworkLinks) != 1 {
		t.Fatalf("invalid disconnect mutated network links: %#v", service.Lab.NetworkLinks)
	}
}

func TestSaveAndRefreshFailureReloadsPersistedLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatalf("save initial lab: %v", err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("load initial lab: %v", err)
	}

	service := NewService(loaded, path)
	got := service.SwitchCreate("badmode", map[string]string{"mode": "bad"})
	if !strings.Contains(got, "unsupported mode") {
		t.Fatalf("SwitchCreate = %q, want validation failure", got)
	}
	if len(service.Lab.Switches) != 1 || service.Lab.Switches[0].ID != "lan" || service.Lab.Switches[0].Mode != "bridge" {
		t.Fatalf("service did not reload persisted lab after failed save: %#v", service.Lab.Switches)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("reload lab: %v", err)
	}
	if len(reloaded.Switches) != 1 || reloaded.Switches[0].ID != "lan" || reloaded.Switches[0].Mode != "bridge" {
		t.Fatalf("persisted lab changed after failed save: %#v", reloaded.Switches)
	}
}
