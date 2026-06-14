package topology

import (
	"path/filepath"
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
