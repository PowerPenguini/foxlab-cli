package lab

import "testing"

func TestValidatePreservesProblemOrderAcrossSections(t *testing.T) {
	endpoint := NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 0}
	l := &Lab{
		ID:  "demo",
		VMs: []VM{{ID: "vm1", MemoryMB: 1, CPUs: 1}},
		Disks: []Disk{{
			ID: "disk1",
		}},
		NetworkLinks: []NetworkLink{{From: endpoint, To: endpoint}},
		Layout: Layout{Nodes: map[string]Position{
			"ghost": {},
		}},
	}
	want := "disk \"disk1\" path is required; network link endpoints must be different; layout references missing node \"ghost\""
	if err := l.Validate(); err == nil || err.Error() != want {
		t.Fatalf("Validate error = %v, want %q", err, want)
	}
}
