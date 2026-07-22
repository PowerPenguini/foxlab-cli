package topology

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
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
	request := VMCreateRequest{
		Name:     "vm1",
		MemoryMB: SetField(4096),
		Network:  WorkloadNetworkInput{Switch: "sw1"},
	}
	if got, want := service.CreateVM(request).Message, "created vm:vm1"; got != want {
		t.Fatalf("CreateVM() = %q, want %q", got, want)
	}
	vm, ok := service.LabVM("vm1")
	if !ok {
		t.Fatalf("vm1 was not present after create")
	}
	if vm.MemoryMB != 4096 || len(vm.Networks) != 1 || vm.Networks[0].Switch != service.CurrentLab().Switches[0].ID {
		t.Fatalf("vm1 was not refreshed from persisted lab: %+v", vm)
	}

	if got, want := service.SwitchDelete("sw1").Message, "deleted switch:sw1"; got != want {
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

func TestWorkloadMutationResultsCarryOutcomeMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	failed := service.CreateVM(VMCreateRequest{})
	if failed.OK() || failed.Err == nil || failed.Kind != ResultError || failed.Changed {
		t.Fatalf("failed result = %#v", failed)
	}
	info := service.UpdateVM("vm1", VMUpdate{})
	if !info.OK() || info.Err != nil || info.Kind != ResultInfo || info.Changed {
		t.Fatalf("info result = %#v", info)
	}
	changedInfo := service.UpdateVM("vm1", VMUpdate{CPUs: SetField(2)})
	if !changedInfo.OK() || changedInfo.Err != nil || changedInfo.Kind != ResultInfo || !changedInfo.Changed {
		t.Fatalf("changed info result = %#v", changedInfo)
	}
	success := service.CreateVM(VMCreateRequest{Name: "vm2"})
	if !success.OK() || success.Err != nil || success.Kind != ResultSuccess || !success.Changed {
		t.Fatalf("success result = %#v", success)
	}
}

func TestNodeDeleteRemovesRelatedLayoutLinks(t *testing.T) {
	tests := []struct {
		name          string
		deletedType   string
		deletedID     string
		wantRemaining int
		delete        func(*Service) string
	}{
		{name: "vm", deletedType: "vm", deletedID: "vm1", wantRemaining: 2, delete: func(s *Service) string { return s.VMDelete("vm1").Message }},
		{name: "container", deletedType: "container", deletedID: "ct1", wantRemaining: 2, delete: func(s *Service) string { return s.ContainerDelete("ct1").Message }},
		{name: "switch", deletedType: "switch", deletedID: "sw1", wantRemaining: 1, delete: func(s *Service) string { return s.SwitchDelete("sw1").Message }},
		{name: "uplink", deletedType: "external", deletedID: "up1", wantRemaining: 1, delete: func(s *Service) string { return s.ExternalDelete("up1").Message }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "demo.lab")
			initial := &lab.Lab{
				ID:            "demo",
				VMs:           []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1}},
				Containers:    []lab.Container{{ID: "ct1", Image: "alpine"}},
				Switches:      []lab.Switch{{ID: "sw1", Mode: "bridge"}},
				ExternalLinks: []lab.ExternalLink{{ID: "up1", Interface: "eth0", Mode: lab.ExternalModeNAT}},
				Layout: lab.Layout{
					Nodes: map[string]lab.Position{
						"vm1": {}, "ct1": {}, "sw1": {}, "up1": {},
					},
					Links: []lab.LayoutLink{
						{From: lab.LayoutEndpoint{Type: "vm", ID: "vm1"}, To: lab.LayoutEndpoint{Type: "switch", ID: "sw1"}},
						{From: lab.LayoutEndpoint{Type: "container", ID: "ct1"}, To: lab.LayoutEndpoint{Type: "external", ID: "up1"}},
						{From: lab.LayoutEndpoint{Type: "switch", ID: "sw1"}, To: lab.LayoutEndpoint{Type: "external", ID: "up1"}},
					},
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
			if got := tt.delete(service); strings.Contains(got, "failed:") {
				t.Fatalf("delete failed: %s", got)
			}
			reloaded, err := lab.LoadFile(path)
			if err != nil {
				t.Fatalf("reload lab after delete: %v", err)
			}
			if got := len(reloaded.Layout.Links); got != tt.wantRemaining {
				t.Fatalf("layout links after delete = %#v, want %d remaining", reloaded.Layout.Links, tt.wantRemaining)
			}
			for _, link := range reloaded.Layout.Links {
				for _, endpoint := range []lab.LayoutEndpoint{link.From, link.To} {
					if endpoint.Type == tt.deletedType && endpoint.ID == tt.deletedID {
						t.Fatalf("stale layout endpoint after delete: %#v", endpoint)
					}
				}
			}
		})
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
	if got, want := service.DeleteVMNIC("vm1", 1).Message, "deleted nic from vm:vm1 nic1"; got != want {
		t.Fatalf("DeleteVMNIC() = %q, want %q", got, want)
	}
	vm, ok := service.LabVM("vm1")
	if !ok {
		t.Fatal("vm1 missing after nic delete")
	}
	if len(vm.Networks) != 2 {
		t.Fatalf("vm1 networks = %#v, want 2 nics", vm.Networks)
	}
	if len(service.CurrentLab().NetworkLinks) != 1 {
		t.Fatalf("network links = %#v, want only link for shifted nic", service.CurrentLab().NetworkLinks)
	}
	link := service.CurrentLab().NetworkLinks[0]
	vm1, _ := service.LabVM("vm1")
	vm2, _ := service.LabVM("vm2")
	if link.From.ID != vm1.ID || link.From.NIC != 1 || link.To.ID != vm2.ID || link.To.NIC != 1 {
		t.Fatalf("reindexed link = %#v", link)
	}
}

func TestServiceConnectVMNIC(t *testing.T) {
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
	request := NICConnectRequest{NIC: 0, Endpoint: NetworkEndpointRef{Type: NetworkEndpointAuto, ID: "lan"}}
	if got, want := service.ConnectVMNIC("vm1", request).Message, "connected nic to vm:vm1"; got != want {
		t.Fatalf("ConnectVMNIC() = %q, want %q", got, want)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Networks[0].Switch != reloaded.Switches[0].ID {
		t.Fatalf("vm nic switch = %q, want %q", reloaded.VMs[0].Networks[0].Switch, reloaded.Switches[0].ID)
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
	if got, want := service.DeleteContainerNIC("web", 0).Message, "deleted nic from container:web nic0"; got != want {
		t.Fatalf("DeleteContainerNIC() = %q, want %q", got, want)
	}
	ct, ok := service.LabContainer("web")
	if !ok {
		t.Fatal("container missing after nic delete")
	}
	if len(ct.Networks) != 1 {
		t.Fatalf("container networks = %#v, want one nic", ct.Networks)
	}
}

func TestServiceContainerSetUpdatesShellAndClearsEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{{
			ID:    "web",
			Image: "nginx",
			Shell: "/bin/sh",
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
	update := ContainerUpdate{Env: SetField(map[string]string(nil)), Shell: SetField("/bin/bash")}
	if got, want := service.UpdateContainer("web", update).Message, "configured container:web"; got != want {
		t.Fatalf("UpdateContainer() = %q, want %q", got, want)
	}
	ct, ok := service.LabContainer("web")
	if !ok {
		t.Fatal("container missing after env clear")
	}
	if len(ct.Env) != 0 {
		t.Fatalf("service env = %#v, want empty", ct.Env)
	}
	if ct.Shell != "/bin/bash" {
		t.Fatalf("service shell = %q, want /bin/bash", ct.Shell)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("reload lab: %v", err)
	}
	if len(reloaded.Containers[0].Env) != 0 {
		t.Fatalf("persisted env = %#v, want empty", reloaded.Containers[0].Env)
	}
	if reloaded.Containers[0].Shell != "/bin/bash" {
		t.Fatalf("persisted shell = %q, want /bin/bash", reloaded.Containers[0].Shell)
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
	if got, want := service.VMDesiredState("vm1", lab.DesiredStateRunning).Message, "desired vm:vm1 running"; got != want {
		t.Fatalf("VMDesiredState() = %q, want %q", got, want)
	}
	if got, want := service.ContainerDesiredState("web", lab.DesiredStateStopped).Message, "desired container:web stopped"; got != want {
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
			run:  func() string { return service.CreateVM(VMCreateRequest{}).Message },
			want: "usage: vm create <name> [cpus=N] [memory=N] [switch=NAME|uplink=NAME]",
		},
		{
			name: "vm duplicate name",
			run:  func() string { return service.CreateVM(VMCreateRequest{Name: "existing"}).Message },
			want: "node id already exists as switch: existing",
		},
		{
			name: "container",
			run:  func() string { return service.CreateContainer(ContainerCreateRequest{}).Message },
			want: "usage: container create <name> [image=REF] [command=CMD] [switch=NAME|uplink=NAME]",
		},
		{
			name: "container duplicate name",
			run:  func() string { return service.CreateContainer(ContainerCreateRequest{Name: "existing"}).Message },
			want: "node id already exists as switch: existing",
		},
		{
			name: "switch",
			run:  func() string { return service.CreateSwitch(SwitchCreateRequest{}).Message },
			want: "usage: switch create <name> [mode=bridge|nat|macnat-bridge] [uplink=NAME]",
		},
		{
			name: "switch duplicate name",
			run:  func() string { return service.CreateSwitch(SwitchCreateRequest{Name: "existing"}).Message },
			want: "node id already exists as switch: existing",
		},
		{
			name: "external",
			run:  func() string { return service.CreateExternal(ExternalCreateRequest{}).Message },
			want: "usage: uplink create <name> interface=IFACE [mode=nat|direct|macnat]",
		},
		{
			name: "external duplicate name",
			run: func() string {
				return service.CreateExternal(ExternalCreateRequest{Name: "existing", Interface: "eth0"}).Message
			},
			want: "node id already exists as switch: existing",
		},
		{
			name: "external interface",
			run:  func() string { return service.CreateExternal(ExternalCreateRequest{Name: "uplink"}).Message },
			want: "usage: uplink create <name> interface=IFACE [mode=nat|direct|macnat]",
		},
	}

	for _, tt := range tests {
		if got := tt.run(); got != tt.want {
			t.Fatalf("%s create = %q, want %q", tt.name, got, tt.want)
		}
	}
	if len(service.CurrentLab().VMs) != 0 || len(service.CurrentLab().Containers) != 0 || len(service.CurrentLab().Switches) != 1 || len(service.CurrentLab().ExternalLinks) != 0 {
		t.Fatalf("empty-id create mutated lab: %#v", service.CurrentLab())
	}
	if service.CurrentLab().Switches[0].ID != "existing" || service.CurrentLab().Switches[0].Name != "" {
		t.Fatalf("empty-id create mutated existing switch: %#v", service.CurrentLab().Switches)
	}
	if len(service.CurrentLab().Layout.Nodes) != 1 {
		t.Fatalf("empty-id create mutated layout: %#v", service.CurrentLab().Layout.Nodes)
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
			run: func() string {
				return service.CreateVM(VMCreateRequest{Name: "bad-cpus", CPUs: SetField(0)}).Message
			},
			want: "invalid vm cpus: 0",
		},
		{
			name: "create memory",
			run: func() string {
				return service.CreateVM(VMCreateRequest{Name: "bad-memory", MemoryMB: SetField(0)}).Message
			},
			want: "invalid vm memory: 0",
		},
		{
			name: "set cpus",
			run:  func() string { return service.UpdateVM("vm1", VMUpdate{CPUs: SetField(0)}).Message },
			want: "invalid vm cpus: 0",
		},
		{
			name: "set memory",
			run:  func() string { return service.UpdateVM("vm1", VMUpdate{MemoryMB: SetField(0)}).Message },
			want: "invalid vm memory: 0",
		},
		{
			name: "set negative memory",
			run:  func() string { return service.UpdateVM("vm1", VMUpdate{MemoryMB: SetField(-1)}).Message },
			want: "invalid vm memory: -1",
		},
	}

	for _, tt := range tests {
		if got := tt.run(); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}
	if len(service.CurrentLab().VMs) != 1 {
		t.Fatalf("invalid vm args created vms: %#v", service.CurrentLab().VMs)
	}
	vm := service.CurrentLab().VMs[0]
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
			run:  func(s *Service) string { return s.CreateSwitch(SwitchCreateRequest{Name: "wan", Mode: "bad"}).Message },
			want: `switch create failed: switch "wan" uses unsupported mode "bad"; supported modes are bridge, nat and macnat-bridge`,
		},
		{
			name: "switch create missing external",
			run: func(s *Service) string {
				return s.CreateSwitch(SwitchCreateRequest{Name: "wan", Uplink: "missing"}).Message
			},
			want: `switch create failed: uplink not found: missing`,
		},
		{
			name: "switch set bad mode",
			run:  func(s *Service) string { return s.UpdateSwitch("lan", SwitchUpdate{Mode: SetField("bad")}).Message },
			want: `switch config failed: switch "lan" uses unsupported mode "bad"; supported modes are bridge, nat and macnat-bridge`,
		},
		{
			name: "external create bad mode",
			run: func(s *Service) string {
				return s.CreateExternal(ExternalCreateRequest{Name: "lte", Interface: "wwan0", Mode: "bad"}).Message
			},
			want: `uplink create failed: uplink "lte" uses unsupported mode "bad"; supported modes are nat, direct and macnat`,
		},
		{
			name: "external set bad mode",
			run: func(s *Service) string {
				return s.UpdateExternal("uplink", ExternalUpdate{Mode: SetField("bad")}).Message
			},
			want: `uplink config failed: uplink "uplink" uses unsupported mode "bad"; supported modes are nat, direct and macnat`,
		},
		{
			name: "vm create missing switch",
			run: func(s *Service) string {
				return s.CreateVM(VMCreateRequest{Name: "vm2", Network: WorkloadNetworkInput{Switch: "missing"}}).Message
			},
			want: `create failed: switch not found: missing`,
		},
		{
			name: "vm set switch and external",
			run: func(s *Service) string {
				return s.UpdateVM("vm1", VMUpdate{Network: WorkloadNetworkInput{Switch: "lan", Uplink: "uplink"}}).Message
			},
			want: `config failed: vm "vm1" network must not reference both switch and externalLink`,
		},
		{
			name: "vm set missing external",
			run: func(s *Service) string {
				return s.UpdateVM("vm1", VMUpdate{Network: WorkloadNetworkInput{Uplink: "missing"}}).Message
			},
			want: `config failed: uplink not found: missing`,
		},
		{
			name: "container create switch and external",
			run: func(s *Service) string {
				request := ContainerCreateRequest{Name: "api", Image: "alpine"}
				request.Network = WorkloadNetworkInput{Switch: "lan", Uplink: "uplink"}
				return s.CreateContainer(request).Message
			},
			want: `container create failed: container "api" network must not reference both switch and externalLink`,
		},
		{
			name: "container create missing external",
			run: func(s *Service) string {
				request := ContainerCreateRequest{Name: "api", Image: "alpine", Network: WorkloadNetworkInput{Uplink: "missing"}}
				return s.CreateContainer(request).Message
			},
			want: `container create failed: uplink not found: missing`,
		},
		{
			name: "container set switch and external",
			run: func(s *Service) string {
				return s.UpdateContainer("web", ContainerUpdate{Network: WorkloadNetworkInput{Switch: "lan", Uplink: "uplink"}}).Message
			},
			want: `container config failed: container "web" network must not reference both switch and externalLink`,
		},
		{
			name: "container set missing switch",
			run: func(s *Service) string {
				return s.UpdateContainer("web", ContainerUpdate{Network: WorkloadNetworkInput{Switch: "missing"}}).Message
			},
			want: `container config failed: switch not found: missing`,
		},
	}

	for _, tt := range tests {
		initial := lab.Clone(base)
		path := filepath.Join(t.TempDir(), "new.lab")
		service := NewService(lab.Clone(initial), path)
		if got := tt.run(service); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
		if !reflect.DeepEqual(service.CurrentLab(), initial) {
			t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.CurrentLab(), initial)
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

	update := SwitchUpdate{AttachUplink: SetField("uplink2")}
	if got := service.UpdateSwitch("lan", update).Message; got != "configured switch:lan" {
		t.Fatalf("UpdateSwitch = %q", got)
	}

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := lab.SwitchExternalLinks(reloaded.Switches[0]), []string{reloaded.ExternalLinks[0].ID, reloaded.ExternalLinks[1].ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("switch externalLinks = %#v, want %#v", got, want)
	}
}

func TestServiceRollsBackWhenSaveFailsForNewPath(t *testing.T) {
	diskCommands := stubDiskCommands(t)
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
		{name: "vm desired", run: func(s *Service) string { return s.VMDesiredState("vm1", lab.DesiredStateRunning).Message }},
		{name: "container desired", run: func(s *Service) string { return s.ContainerDesiredState("web", lab.DesiredStateRunning).Message }},
		{name: "vm create", run: func(s *Service) string {
			return s.CreateVM(VMCreateRequest{Name: "vm3", Network: WorkloadNetworkInput{Switch: "lan"}}).Message
		}},
		{name: "vm set", run: func(s *Service) string {
			return s.UpdateVM("vm1", VMUpdate{Name: SetField("changed")}).Message
		}},
		{name: "vm delete", run: func(s *Service) string { return s.VMDelete("vm2").Message }},
		{name: "container create", run: func(s *Service) string {
			request := ContainerCreateRequest{Name: "db", Image: "postgres", Network: WorkloadNetworkInput{Switch: "lan"}}
			return s.CreateContainer(request).Message
		}},
		{name: "container set", run: func(s *Service) string {
			return s.UpdateContainer("web", ContainerUpdate{Image: SetField("redis")}).Message
		}},
		{name: "container delete", run: func(s *Service) string { return s.ContainerDelete("api").Message }},
		{name: "switch create", run: func(s *Service) string {
			return s.CreateSwitch(SwitchCreateRequest{Name: "wan", Mode: "bridge"}).Message
		}},
		{name: "switch set", run: func(s *Service) string {
			return s.UpdateSwitch("lan", SwitchUpdate{Name: SetField("LAN")}).Message
		}},
		{name: "switch delete", run: func(s *Service) string { return s.SwitchDelete("lan").Message }},
		{name: "external create", run: func(s *Service) string {
			return s.CreateExternal(ExternalCreateRequest{Name: "lte", Interface: "wwan0"}).Message
		}},
		{name: "external set", run: func(s *Service) string {
			return s.UpdateExternal("uplink", ExternalUpdate{Name: SetField("WAN")}).Message
		}},
		{name: "external delete", run: func(s *Service) string { return s.ExternalDelete("uplink").Message }},
		{name: "vm nic add", run: func(s *Service) string { return s.AddVMNIC("vm1", NICAddRequest{}).Message }},
		{name: "vm nic connect", run: func(s *Service) string {
			request := NICConnectRequest{NIC: 0, Endpoint: NetworkEndpointRef{Type: NetworkEndpointSwitch, ID: "lan"}}
			return s.ConnectVMNIC("vm2", request).Message
		}},
		{name: "vm nic delete", run: func(s *Service) string { return s.DeleteVMNIC("vm2", 0).Message }},
		{name: "container nic add", run: func(s *Service) string { return s.AddContainerNIC("web", NICAddRequest{}).Message }},
		{name: "container nic connect", run: func(s *Service) string {
			request := NICConnectRequest{NIC: 0, Endpoint: NetworkEndpointRef{Type: NetworkEndpointSwitch, ID: "lan"}}
			return s.ConnectContainerNIC("api", request).Message
		}},
		{name: "container nic delete", run: func(s *Service) string { return s.DeleteContainerNIC("api", 0).Message }},
		{name: "direct connect", run: func(s *Service) string {
			source := NetworkEndpointRef{Type: NetworkEndpointVM, ID: "vm2", NIC: SetField(0)}
			target := NetworkEndpointRef{Type: NetworkEndpointContainer, ID: "api"}
			return s.ConnectDirectNIC(source, target).Message
		}},
		{name: "direct connect to", run: func(s *Service) string {
			source := NetworkEndpointRef{Type: NetworkEndpointVM, ID: "vm2", NIC: SetField(0)}
			target := NetworkEndpointRef{Type: NetworkEndpointContainer, ID: "api", NIC: SetField(0)}
			return s.ConnectDirectNIC(source, target).Message
		}},
		{name: "direct disconnect", run: func(s *Service) string {
			return s.DisconnectNIC(NetworkEndpointRef{Type: NetworkEndpointVM, ID: "vm1", NIC: SetField(0)}).Message
		}},
		{name: "disk detach", run: func(s *Service) string {
			return s.DetachDisk(DiskDetachRequest{Target: workload.Ref{Type: workload.TypeVM, ID: "vm1"}}).Message
		}},
		{name: "disk attach base", run: func(s *Service) string {
			request := DiskAttachRequest{DiskID: "free", Target: workload.Ref{Type: workload.TypeVM, ID: "vm2"}}
			return s.AttachDisk(request).Message
		}},
	}

	for _, tt := range tests {
		initial := lab.Clone(base)
		blocker := filepath.Join(t.TempDir(), "blocked")
		if err := os.WriteFile(blocker, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		service := NewService(lab.Clone(initial), filepath.Join(blocker, "demo.lab"))
		service.DiskCommands = diskCommands
		got := tt.run(service)
		if !strings.Contains(got, "failed:") {
			t.Fatalf("%s = %q, want save failure", tt.name, got)
		}
		if !reflect.DeepEqual(service.CurrentLab(), initial) {
			t.Fatalf("%s failed save mutated lab:\ngot  %#v\nwant %#v", tt.name, service.CurrentLab(), initial)
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
			run:  func() string { return service.UpdateVM("vm1", VMUpdate{CPUs: SetField(2)}).Message },
			want: "config failed: missing lab path",
		},
		{
			name: "vm delete",
			run:  func() string { return service.VMDelete("vm1").Message },
			want: "delete failed: missing lab path",
		},
		{
			name: "container delete",
			run:  func() string { return service.ContainerDelete("web").Message },
			want: "container delete failed: missing lab path",
		},
		{
			name: "switch delete",
			run:  func() string { return service.SwitchDelete("lan").Message },
			want: "switch delete failed: missing lab path",
		},
		{
			name: "external delete",
			run:  func() string { return service.ExternalDelete("uplink").Message },
			want: "uplink delete failed: missing lab path",
		},
		{
			name: "nic delete",
			run:  func() string { return service.DeleteVMNIC("vm1", 0).Message },
			want: "nic delete failed: missing lab path",
		},
		{
			name: "direct disconnect",
			run: func() string {
				return service.DisconnectNIC(NetworkEndpointRef{Type: NetworkEndpointVM, ID: "vm1", NIC: SetField(0)}).Message
			},
			want: "nic disconnect failed: missing lab path",
		},
		{
			name: "desired state",
			run:  func() string { return service.VMDesiredState("vm1", lab.DesiredStateRunning).Message },
			want: "desired state failed: missing lab path",
		},
	}

	for _, tt := range tests {
		if got := tt.run(); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
	}
	if len(service.CurrentLab().VMs) != 2 || service.CurrentLab().VMs[0].ID != "vm1" || service.CurrentLab().VMs[0].CPUs != 1 || service.CurrentLab().VMs[1].ID != "vm2" {
		t.Fatalf("missing-path operations mutated vms: %#v", service.CurrentLab().VMs)
	}
	if len(service.CurrentLab().Containers) != 1 || service.CurrentLab().Containers[0].ID != "web" {
		t.Fatalf("missing-path operations mutated containers: %#v", service.CurrentLab().Containers)
	}
	if len(service.CurrentLab().Switches) != 1 || service.CurrentLab().Switches[0].ID != "lan" {
		t.Fatalf("missing-path operations mutated switches: %#v", service.CurrentLab().Switches)
	}
	if len(service.CurrentLab().ExternalLinks) != 1 || service.CurrentLab().ExternalLinks[0].ID != "uplink" {
		t.Fatalf("missing-path operations mutated externals: %#v", service.CurrentLab().ExternalLinks)
	}
	if len(service.CurrentLab().NetworkLinks) != 1 {
		t.Fatalf("missing-path operations mutated network links: %#v", service.CurrentLab().NetworkLinks)
	}
	if len(service.CurrentLab().VMs[0].Networks) != 1 {
		t.Fatalf("missing-path operations mutated vm nics: %#v", service.CurrentLab().VMs[0].Networks)
	}
}

func TestNetworkNodeRenameRequiresSavePathBeforeMutation(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Service) string
		want string
	}{
		{
			name: "switch",
			run: func(s *Service) string {
				return s.UpdateSwitch("lan", SwitchUpdate{Name: SetField("renamed-lan")}).Message
			},
			want: "switch config failed: missing lab path",
		},
		{
			name: "uplink",
			run: func(s *Service) string {
				return s.UpdateExternal("uplink", ExternalUpdate{Name: SetField("renamed-uplink")}).Message
			},
			want: "uplink config failed: missing lab path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initial := &lab.Lab{
				ID:            "demo",
				Switches:      []lab.Switch{{ID: "lan", Mode: "bridge", ExternalLinks: []string{"uplink"}}},
				ExternalLinks: []lab.ExternalLink{{ID: "uplink", Interface: "eth0", Mode: lab.ExternalModeNAT}},
				VMs:           []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Networks: []lab.VMNetwork{{Switch: "lan"}}}},
				Layout: lab.Layout{
					Nodes: map[string]lab.Position{"lan": {}, "uplink": {}, "vm1": {}},
					Links: []lab.LayoutLink{{
						From: lab.LayoutEndpoint{Type: "switch", ID: "lan"},
						To:   lab.LayoutEndpoint{Type: "external", ID: "uplink"},
					}},
				},
			}
			service := NewService(lab.Clone(initial), "")
			if got := tt.run(service); got != tt.want {
				t.Fatalf("rename = %q, want %q", got, tt.want)
			}
			if !reflect.DeepEqual(service.CurrentLab(), initial) {
				t.Fatalf("missing-path rename mutated lab:\ngot  %#v\nwant %#v", service.CurrentLab(), initial)
			}
		})
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
			run: func(s *Service) string {
				return s.CreateVM(VMCreateRequest{Name: "vm2", Network: WorkloadNetworkInput{Switch: "missing"}}).Message
			},
			want: `create failed: switch not found: missing`,
		},
		{
			name: "vm set",
			run: func(s *Service) string {
				return s.UpdateVM("vm1", VMUpdate{Network: WorkloadNetworkInput{Uplink: "missing"}}).Message
			},
			want: `config failed: uplink not found: missing`,
		},
		{
			name: "container create",
			run: func(s *Service) string {
				request := ContainerCreateRequest{Name: "api", Image: "alpine", Network: WorkloadNetworkInput{Uplink: "missing"}}
				return s.CreateContainer(request).Message
			},
			want: `container create failed: uplink not found: missing`,
		},
		{
			name: "container set",
			run: func(s *Service) string {
				return s.UpdateContainer("web", ContainerUpdate{Network: WorkloadNetworkInput{Switch: "missing"}}).Message
			},
			want: `container config failed: switch not found: missing`,
		},
	}

	for _, tt := range tests {
		initial := lab.Clone(base)
		service := NewService(lab.Clone(initial), "")
		if got := tt.run(service); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
		if !reflect.DeepEqual(service.CurrentLab(), initial) {
			t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.CurrentLab(), initial)
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
			run:  func(s *Service) string { return s.AddVMNIC("vm1", NICAddRequest{MAC: "not-a-mac"}).Message },
			want: "invalid vm nic mac: not-a-mac",
		},
		{
			name: "vm nic connect",
			run: func(s *Service) string {
				request := NICConnectRequest{
					NIC:      0,
					Endpoint: NetworkEndpointRef{Type: NetworkEndpointAuto, ID: "lan"},
					MAC:      "not-a-mac",
				}
				return s.ConnectVMNIC("vm1", request).Message
			},
			want: "invalid vm nic mac: not-a-mac",
		},
		{
			name: "container create",
			run: func(s *Service) string {
				request := ContainerCreateRequest{Name: "api", Image: "alpine"}
				request.Network = WorkloadNetworkInput{Switch: "lan", MAC: "not-a-mac"}
				return s.CreateContainer(request).Message
			},
			want: "invalid container nic mac: not-a-mac",
		},
		{
			name: "container set",
			run: func(s *Service) string {
				update := ContainerUpdate{Network: WorkloadNetworkInput{Switch: "lan", MAC: "not-a-mac"}}
				return s.UpdateContainer("web", update).Message
			},
			want: "invalid container nic mac: not-a-mac",
		},
		{
			name: "container nic add",
			run: func(s *Service) string {
				return s.AddContainerNIC("web", NICAddRequest{MAC: "not-a-mac"}).Message
			},
			want: "invalid container nic mac: not-a-mac",
		},
		{
			name: "container nic connect",
			run: func(s *Service) string {
				request := NICConnectRequest{
					NIC:      0,
					Endpoint: NetworkEndpointRef{Type: NetworkEndpointAuto, ID: "lan"},
					MAC:      "not-a-mac",
				}
				return s.ConnectContainerNIC("web", request).Message
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
		if !reflect.DeepEqual(service.CurrentLab(), initial) {
			t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.CurrentLab(), initial)
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
			run:  func(s *Service) string { return s.CreateVM(VMCreateRequest{Name: "web"}).Message },
			want: "node id already exists as container: web",
		},
		{
			name: "container collides with vm",
			run: func(s *Service) string {
				return s.CreateContainer(ContainerCreateRequest{Name: "vm1", Image: "alpine"}).Message
			},
			want: "node id already exists as vm: vm1",
		},
		{
			name: "switch collides with external",
			run:  func(s *Service) string { return s.CreateSwitch(SwitchCreateRequest{Name: "uplink"}).Message },
			want: "node id already exists as uplink: uplink",
		},
		{
			name: "external collides with switch",
			run: func(s *Service) string {
				return s.CreateExternal(ExternalCreateRequest{Name: "lan", Interface: "eth0"}).Message
			},
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
			if !reflect.DeepEqual(service.CurrentLab(), initial) {
				t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.CurrentLab(), initial)
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
			want: "vm-1",
		},
		{
			name: "container skips vm id",
			lab:  &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "ct2", MemoryMB: 512, CPUs: 1}}},
			got:  func(s *Service) string { return s.NextContainerID() },
			want: "container-1",
		},
		{
			name: "switch skips external id",
			lab:  &lab.Lab{ID: "demo", ExternalLinks: []lab.ExternalLink{{ID: "sw2", Interface: "eth0", Mode: lab.ExternalModeNAT}}},
			got:  func(s *Service) string { return s.NextSwitchID() },
			want: "switch-1",
		},
		{
			name: "external skips switch id",
			lab:  &lab.Lab{ID: "demo", Switches: []lab.Switch{{ID: "uplink2", Mode: "bridge"}}},
			got:  func(s *Service) string { return s.NextExternalID() },
			want: "uplink-1",
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
			run:  func(s *Service) string { return s.UpdateVM("vm1", VMUpdate{}).Message },
			want: "configured vm:VM 1",
		},
		{
			name: "container set",
			run:  func(s *Service) string { return s.UpdateContainer("web", ContainerUpdate{}).Message },
			want: "configured container:Web",
		},
		{
			name: "switch set",
			run:  func(s *Service) string { return s.UpdateSwitch("lan", SwitchUpdate{}).Message },
			want: "configured switch:LAN",
		},
		{
			name: "external set",
			run:  func(s *Service) string { return s.UpdateExternal("uplink", ExternalUpdate{}).Message },
			want: "configured uplink:WAN",
		},
	}

	for _, tt := range tests {
		initial := lab.Clone(base)
		service := NewService(lab.Clone(initial), "")
		if got := tt.run(service); got != tt.want {
			t.Fatalf("%s = %q, want %q", tt.name, got, tt.want)
		}
		if !reflect.DeepEqual(service.CurrentLab(), initial) {
			t.Fatalf("%s mutated lab:\ngot  %#v\nwant %#v", tt.name, service.CurrentLab(), initial)
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

	endpoint := NetworkEndpointRef{Type: NetworkEndpointType("pod"), ID: "vm1", NIC: SetField(0)}
	if got := service.DisconnectNIC(endpoint).Message; got != "direct link target must be vm or container" {
		t.Fatalf("NICDisconnect invalid type = %q, want endpoint validation", got)
	}
	if len(service.CurrentLab().NetworkLinks) != 1 {
		t.Fatalf("invalid disconnect mutated network links: %#v", service.CurrentLab().NetworkLinks)
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
	got := service.CreateSwitch(SwitchCreateRequest{Name: "badmode", Mode: "bad"}).Message
	if !strings.Contains(got, "unsupported mode") {
		t.Fatalf("SwitchCreate = %q, want validation failure", got)
	}
	if len(service.CurrentLab().Switches) != 1 || service.CurrentLab().Switches[0].ID != "lan" || service.CurrentLab().Switches[0].Name != "" || service.CurrentLab().Switches[0].Mode != "bridge" {
		t.Fatalf("service did not reload persisted lab after failed save: %#v", service.CurrentLab().Switches)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("reload lab: %v", err)
	}
	if len(reloaded.Switches) != 1 || reloaded.Switches[0].ID != "lan" || reloaded.Switches[0].Name != "" || reloaded.Switches[0].Mode != "bridge" {
		t.Fatalf("persisted lab changed after failed save: %#v", reloaded.Switches)
	}
}

func TestNodeRenameRewritesAllMnemonicReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm-old",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm.qcow2",
			Networks: []lab.VMNetwork{{Switch: "sw-old"}, {ExternalLink: "up-old"}, {}},
		}},
		Containers: []lab.Container{{
			ID:       "ct-old",
			Image:    "alpine",
			Disk:     "disks/ct.qcow2",
			Networks: []lab.ContainerNetwork{{Switch: "sw-old"}, {ExternalLink: "up-old"}, {}},
		}},
		Switches: []lab.Switch{{
			ID:            "sw-old",
			Mode:          "bridge",
			ExternalLinks: []string{"up-old"},
		}},
		ExternalLinks: []lab.ExternalLink{{ID: "up-old", Interface: "eth0", Mode: lab.ExternalModeNAT}},
		NetworkLinks: []lab.NetworkLink{{
			From: lab.NetworkEndpoint{Type: "vm", ID: "vm-old", NIC: 2},
			To:   lab.NetworkEndpoint{Type: "container", ID: "ct-old", NIC: 2},
		}},
		Disks: []lab.Disk{
			{ID: "vm-root", Path: "disks/vm.qcow2", Format: "qcow2", Kind: "base", AttachedType: "vm", AttachedTo: "vm-old"},
			{ID: "ct-root", Path: "disks/ct.qcow2", Format: "qcow2", Kind: "base", AttachedType: "container", AttachedTo: "ct-old"},
		},
		Layout: lab.Layout{
			Nodes: map[string]lab.Position{
				"vm-old": {X: 1, Y: 2}, "ct-old": {X: 3, Y: 4}, "sw-old": {X: 5, Y: 6}, "up-old": {X: 7, Y: 8},
			},
			Links: []lab.LayoutLink{
				{From: lab.LayoutEndpoint{Type: "vm", ID: "vm-old"}, To: lab.LayoutEndpoint{Type: "switch", ID: "sw-old"}},
				{From: lab.LayoutEndpoint{Type: "container", ID: "ct-old"}, To: lab.LayoutEndpoint{Type: "external", ID: "up-old"}},
			},
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
	for label, got := range map[string]string{
		"vm":        service.UpdateVM("vm-old", VMUpdate{Name: SetField("vm-new")}).Message,
		"container": service.UpdateContainer("ct-old", ContainerUpdate{Name: SetField("ct-new")}).Message,
		"switch":    service.UpdateSwitch("sw-old", SwitchUpdate{Name: SetField("sw-new")}).Message,
		"external":  service.UpdateExternal("up-old", ExternalUpdate{Name: SetField("up-new")}).Message,
	} {
		if !strings.Contains(got, "runtime will be recreated") {
			t.Fatalf("%s rename message = %q", label, got)
		}
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatalf("reload renamed lab: %v", err)
	}
	if reloaded.VMs[0].ID != "vm-new" || reloaded.VMs[0].Name != "" || reloaded.Containers[0].ID != "ct-new" || reloaded.Containers[0].Name != "" {
		t.Fatalf("workload ids after rename: vm=%#v container=%#v", reloaded.VMs[0], reloaded.Containers[0])
	}
	if reloaded.Switches[0].ID != "sw-new" || reloaded.ExternalLinks[0].ID != "up-new" {
		t.Fatalf("network node ids after rename: switch=%#v external=%#v", reloaded.Switches[0], reloaded.ExternalLinks[0])
	}
	if reloaded.VMs[0].Networks[0].Switch != "sw-new" || reloaded.VMs[0].Networks[1].ExternalLink != "up-new" || reloaded.Containers[0].Networks[0].Switch != "sw-new" || reloaded.Containers[0].Networks[1].ExternalLink != "up-new" {
		t.Fatalf("network references after rename: vm=%#v container=%#v", reloaded.VMs[0].Networks, reloaded.Containers[0].Networks)
	}
	if got := lab.SwitchExternalLinks(reloaded.Switches[0]); len(got) != 1 || got[0] != "up-new" {
		t.Fatalf("switch external refs after rename: %#v", got)
	}
	if reloaded.NetworkLinks[0].From.ID != "vm-new" || reloaded.NetworkLinks[0].To.ID != "ct-new" {
		t.Fatalf("direct link after rename: %#v", reloaded.NetworkLinks[0])
	}
	if reloaded.Disks[0].AttachedTo != "vm-new" || reloaded.Disks[1].AttachedTo != "ct-new" {
		t.Fatalf("disk attachments after rename: %#v", reloaded.Disks)
	}
	for _, id := range []string{"vm-new", "ct-new", "sw-new", "up-new"} {
		if _, ok := reloaded.Layout.Nodes[id]; !ok {
			t.Fatalf("layout node %q missing after rename: %#v", id, reloaded.Layout.Nodes)
		}
	}
	for _, link := range reloaded.Layout.Links {
		for _, endpoint := range []lab.LayoutEndpoint{link.From, link.To} {
			if strings.HasSuffix(endpoint.ID, "-old") {
				t.Fatalf("stale layout endpoint after rename: %#v", endpoint)
			}
		}
	}
}

func TestNodeRenameCollisionDoesNotMutateLab(t *testing.T) {
	initial := &lab.Lab{
		ID:       "demo",
		VMs:      []lab.VM{{ID: "router", MemoryMB: 512, CPUs: 1}},
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	service := NewService(lab.Clone(initial), "")
	if err := service.renameNodeID("vm", "router", "LAN"); err == nil || err.Error() != "node id already exists as switch: LAN" {
		t.Fatalf("rename collision = %v", err)
	}
	if !reflect.DeepEqual(service.CurrentLab(), initial) {
		t.Fatalf("rename collision mutated lab:\ngot  %#v\nwant %#v", service.CurrentLab(), initial)
	}
}
