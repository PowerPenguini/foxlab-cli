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
	if len(vm.Networks) != 0 {
		t.Fatalf("vm1 still references deleted switch: %+v", vm.Networks)
	}
	if service.HasLabSwitch("sw1") {
		t.Fatalf("deleted switch still present in lab")
	}
}
