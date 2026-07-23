package topology

import (
	"path/filepath"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestCreateDHCPAddsRunningManagedContainerToNATSwitch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{ID: "demo", Switches: []lab.Switch{{ID: "lan", Mode: "nat"}}}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	result := service.CreateDHCP(DHCPCreateRequest{Name: "dhcp"})
	if !result.OK() || !result.Changed {
		t.Fatalf("CreateDHCP = %#v", result)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Containers) != 1 {
		t.Fatalf("containers = %#v", reloaded.Containers)
	}
	ct := reloaded.Containers[0]
	if ct.ID != "dhcp" || ct.Service != lab.ContainerServiceDHCP || ct.DesiredState != lab.DesiredStateRunning || ct.Image != lab.DefaultDHCPImage {
		t.Fatalf("DHCP container = %#v", ct)
	}
	if len(ct.Networks) != 1 || ct.Networks[0].Switch != "lan" || ct.Capabilities != nil {
		t.Fatalf("DHCP network/capabilities = %#v / %#v", ct.Networks, ct.Capabilities)
	}
}

func TestManagedDHCPOnlyAllowsRenameSwitchAndPowerConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "nat"}, {ID: "other", Mode: "nat"}},
		Containers: []lab.Container{{
			ID: "dhcp", Service: lab.ContainerServiceDHCP, Image: lab.DefaultDHCPImage,
			Networks: []lab.ContainerNetwork{{Switch: "lan"}},
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	for name, result := range map[string]Result{
		"image":      service.UpdateContainer("dhcp", ContainerUpdate{Image: SetField("alpine")}),
		"shell":      service.UpdateContainer("dhcp", ContainerUpdate{Shell: SetField("/bin/sh")}),
		"capability": service.ContainerCapabilitySet("dhcp", "NET_RAW", true),
		"add nic":    service.AddContainerNIC("dhcp", NICAddRequest{}),
		"delete nic": service.DeleteContainerNIC("dhcp", 0),
	} {
		if result.OK() {
			t.Fatalf("%s unexpectedly succeeded: %#v", name, result)
		}
	}
	result := service.UpdateContainer("dhcp", ContainerUpdate{
		Name:    SetField("server"),
		Network: WorkloadNetworkInput{Switch: "other"},
	})
	if !result.OK() || !result.Changed {
		t.Fatalf("allowed DHCP update = %#v", result)
	}
	ct, ok := service.LabContainer("server")
	if !ok || len(ct.Networks) != 1 || ct.Networks[0].Switch != "other" {
		t.Fatalf("updated DHCP container = %#v, found=%t", ct, ok)
	}
}

func TestCreateDHCPRejectsIncompatibleOrOccupiedSwitch(t *testing.T) {
	tests := []struct {
		name    string
		initial *lab.Lab
		want    string
	}{
		{
			name:    "bridge",
			initial: &lab.Lab{ID: "demo", Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}}},
			want:    "no NAT switch available",
		},
		{
			name: "occupied",
			initial: &lab.Lab{
				ID:       "demo",
				Switches: []lab.Switch{{ID: "lan", Mode: "nat"}},
				Containers: []lab.Container{{
					ID: "existing", Service: lab.ContainerServiceDHCP, Image: lab.DefaultDHCPImage, Networks: []lab.ContainerNetwork{{Switch: "lan"}},
				}},
			},
			want: "already has DHCP container",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "demo.lab")
			if err := lab.SaveFile(path, tt.initial); err != nil {
				t.Fatal(err)
			}
			loaded, err := lab.LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			result := NewService(loaded, path).CreateDHCP(DHCPCreateRequest{Name: "dhcp"})
			if result.OK() || !strings.Contains(result.Message, tt.want) {
				t.Fatalf("CreateDHCP = %#v, want containing %q", result, tt.want)
			}
		})
	}
}
