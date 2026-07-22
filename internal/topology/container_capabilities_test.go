package topology

import (
	"path/filepath"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestContainerCapabilitySetPersistsAddAndDefaultDrop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo", Containers: []lab.Container{{ID: "kali", Image: "kali"}}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	result := service.ContainerCapabilitySet("kali", "cap_net_admin", true)
	if !result.OK() || !result.Changed || !lab.ContainerCapabilityEnabled(service.CurrentLab().Containers[0], "NET_ADMIN") {
		t.Fatalf("enable NET_ADMIN = %#v, container=%#v", result, service.CurrentLab().Containers[0])
	}
	capabilities := service.CurrentLab().Containers[0].Capabilities
	if capabilities == nil || len(capabilities.Add) != 1 || capabilities.Add[0] != "NET_ADMIN" {
		t.Fatalf("capability additions = %#v", capabilities)
	}

	result = service.ContainerCapabilitySet("kali", "NET_RAW", false)
	if !result.OK() || !result.Changed || lab.ContainerCapabilityEnabled(service.CurrentLab().Containers[0], "NET_RAW") {
		t.Fatalf("disable NET_RAW = %#v, container=%#v", result, service.CurrentLab().Containers[0])
	}
	capabilities = service.CurrentLab().Containers[0].Capabilities
	if capabilities == nil || len(capabilities.Drop) != 1 || capabilities.Drop[0] != "NET_RAW" {
		t.Fatalf("capability drops = %#v", capabilities)
	}

	result = service.ContainerCapabilitySet("kali", "NET_RAW", true)
	if !result.OK() || !result.Changed || !lab.ContainerCapabilityEnabled(service.CurrentLab().Containers[0], "NET_RAW") {
		t.Fatalf("re-enable NET_RAW = %#v, container=%#v", result, service.CurrentLab().Containers[0])
	}
	if got := service.CurrentLab().Containers[0].Capabilities; got == nil || len(got.Drop) != 0 || len(got.Add) != 1 {
		t.Fatalf("capabilities after re-enable = %#v", got)
	}
}

func TestContainerCapabilitySetRejectsUnsupportedCapability(t *testing.T) {
	service := NewService(&lab.Lab{ID: "demo", Containers: []lab.Container{{ID: "kali", Image: "kali"}}}, "")
	result := service.ContainerCapabilitySet("kali", "ROOT", true)
	if result.OK() || result.Message != "unsupported container capability: ROOT" {
		t.Fatalf("result = %#v", result)
	}
}
