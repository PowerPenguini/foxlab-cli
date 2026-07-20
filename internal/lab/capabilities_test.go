package lab

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestContainerCapabilitiesRoundTripAndNormalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	data := `name: demo
containers:
  - id: kali
    image: docker.io/kalilinux/kali-rolling:latest
    capabilities:
      add: [cap_net_admin, SYS_PTRACE, NET_ADMIN]
      drop: [cap_net_raw]
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := loaded.Containers[0].Capabilities
	if capabilities == nil {
		t.Fatal("capabilities were discarded")
	}
	if !reflect.DeepEqual(capabilities.Add, []string{"NET_ADMIN", "SYS_PTRACE"}) {
		t.Fatalf("add = %#v", capabilities.Add)
	}
	if !reflect.DeepEqual(capabilities.Drop, []string{"NET_RAW"}) {
		t.Fatalf("drop = %#v", capabilities.Drop)
	}
	if err := SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"capabilities:", "add:", "- NET_ADMIN", "drop:", "- NET_RAW"} {
		if !strings.Contains(string(saved), want) {
			t.Fatalf("saved lab missing %q:\n%s", want, saved)
		}
	}
}

func TestContainerCapabilitiesRejectUnsupportedAndConflictingEntries(t *testing.T) {
	tests := []struct {
		name         string
		capabilities *ContainerCapabilities
		want         string
	}{
		{name: "unsupported", capabilities: &ContainerCapabilities{Add: []string{"ROOT"}}, want: `adds unsupported capability "ROOT"`},
		{name: "conflict", capabilities: &ContainerCapabilities{Add: []string{"NET_ADMIN"}, Drop: []string{"NET_ADMIN"}}, want: `cannot both add and drop capability "NET_ADMIN"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loaded := &Lab{ID: "demo", Containers: []Container{{ID: "kali", Image: "kali", Capabilities: tt.capabilities}}}
			loaded.Normalize()
			err := loaded.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestEffectiveContainerCapabilitiesApplyAddAndDrop(t *testing.T) {
	ct := Container{Capabilities: &ContainerCapabilities{Add: []string{"NET_ADMIN"}, Drop: []string{"NET_RAW"}}}
	if !ContainerCapabilityEnabled(ct, "CAP_NET_ADMIN") {
		t.Fatal("NET_ADMIN addition was not effective")
	}
	if ContainerCapabilityEnabled(ct, "NET_RAW") {
		t.Fatal("NET_RAW drop was not effective")
	}
	if !ContainerCapabilityEnabled(ct, "CHOWN") {
		t.Fatal("unchanged default capability disappeared")
	}
}
