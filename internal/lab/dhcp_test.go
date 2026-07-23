package lab

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDHCPContainerDefaultsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &Lab{
		ID:         "demo",
		Switches:   []Switch{{ID: "lan", Mode: "nat"}},
		Containers: []Container{{ID: "dhcp", Service: " DHCP ", Networks: []ContainerNetwork{{Switch: "lan"}}}},
	}
	if err := SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ct := reloaded.Containers[0]
	if ct.Service != ContainerServiceDHCP || ct.Image != DefaultDHCPImage || ct.DesiredState != DesiredStateRunning {
		t.Fatalf("DHCP defaults = %#v", ct)
	}
	if !IsDHCPContainer(ct) {
		t.Fatal("reloaded DHCP container was not recognized")
	}
}

func TestValidateRejectsExplicitDHCPImage(t *testing.T) {
	const explicitImage = "docker.io/4km3/dnsmasq:2.90-r3-alpine-3.22.2"
	l := &Lab{
		ID:       "demo",
		Switches: []Switch{{ID: "lan", Mode: "nat"}},
		Containers: []Container{{
			ID:       "dhcp",
			Service:  ContainerServiceDHCP,
			Image:    explicitImage,
			Networks: []ContainerNetwork{{Switch: "lan"}},
		}},
	}

	l.Normalize()

	if err := l.Validate(); err == nil || !strings.Contains(err.Error(), "image is managed by FoxLab") {
		t.Fatalf("Validate() error = %v, want managed image error", err)
	}
}

func TestValidateDHCPContainerConstraints(t *testing.T) {
	tests := []struct {
		name string
		lab  *Lab
		want string
	}{
		{
			name: "requires NAT switch",
			lab: &Lab{
				ID:         "demo",
				Switches:   []Switch{{ID: "lan", Mode: "bridge"}},
				Containers: []Container{{ID: "dhcp", Service: ContainerServiceDHCP, Image: DefaultDHCPImage, Networks: []ContainerNetwork{{Switch: "lan"}}}},
			},
			want: `requires NAT switch "lan"`,
		},
		{
			name: "rejects duplicate server",
			lab: &Lab{
				ID:       "demo",
				Switches: []Switch{{ID: "lan", Mode: "nat"}},
				Containers: []Container{
					{ID: "dhcp-a", Service: ContainerServiceDHCP, Image: DefaultDHCPImage, Networks: []ContainerNetwork{{Switch: "lan"}}},
					{ID: "dhcp-b", Service: ContainerServiceDHCP, Image: DefaultDHCPImage, Networks: []ContainerNetwork{{Switch: "lan"}}},
				},
			},
			want: `has more than one DHCP container`,
		},
		{
			name: "rejects managed command override",
			lab: &Lab{
				ID:         "demo",
				Switches:   []Switch{{ID: "lan", Mode: "nat"}},
				Containers: []Container{{ID: "dhcp", Service: ContainerServiceDHCP, Image: DefaultDHCPImage, Command: []string{"sleep"}, Networks: []ContainerNetwork{{Switch: "lan"}}}},
			},
			want: `command is managed by FoxLab`,
		},
		{
			name: "rejects shell override",
			lab: &Lab{
				ID:         "demo",
				Switches:   []Switch{{ID: "lan", Mode: "nat"}},
				Containers: []Container{{ID: "dhcp", Service: ContainerServiceDHCP, Image: DefaultDHCPImage, Shell: "/bin/sh", Networks: []ContainerNetwork{{Switch: "lan"}}}},
			},
			want: `does not expose a configurable shell`,
		},
		{
			name: "rejects capabilities override",
			lab: &Lab{
				ID:         "demo",
				Switches:   []Switch{{ID: "lan", Mode: "nat"}},
				Containers: []Container{{ID: "dhcp", Service: ContainerServiceDHCP, Image: DefaultDHCPImage, Capabilities: &ContainerCapabilities{Add: []string{"NET_ADMIN"}}, Networks: []ContainerNetwork{{Switch: "lan"}}}},
			},
			want: `capabilities are managed by FoxLab`,
		},
		{
			name: "rejects disk",
			lab: &Lab{
				ID:         "demo",
				Switches:   []Switch{{ID: "lan", Mode: "nat"}},
				Containers: []Container{{ID: "dhcp", Service: ContainerServiceDHCP, Image: DefaultDHCPImage, Disk: "data", Networks: []ContainerNetwork{{Switch: "lan"}}}},
			},
			want: `does not support disks`,
		},
		{
			name: "rejects explicit MAC",
			lab: &Lab{
				ID:         "demo",
				Switches:   []Switch{{ID: "lan", Mode: "nat"}},
				Containers: []Container{{ID: "dhcp", Service: ContainerServiceDHCP, Image: DefaultDHCPImage, Networks: []ContainerNetwork{{Switch: "lan", MAC: "02:00:00:00:00:01"}}}},
			},
			want: `network MAC is managed by FoxLab`,
		},
		{
			name: "rejects unknown service",
			lab: &Lab{
				ID:         "demo",
				Containers: []Container{{ID: "infra", Service: "other", Image: "alpine"}},
			},
			want: `uses unsupported service "other"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.lab.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
