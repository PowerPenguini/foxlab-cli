package topologyui

import (
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestDisplayDaemonMessageUsesNodeNames(t *testing.T) {
	const id = "victim-a-id"
	l := &lab.Lab{
		VMs: []lab.VM{{
			ID:   id,
			Name: "Victim-a",
		}},
	}

	got := displayDaemonMessage(l, `start vm:`+id+`: start domain "`+id+`": virError`)

	if strings.Contains(got, id) {
		t.Fatalf("message still contains internal id: %q", got)
	}
	if !strings.Contains(got, "start vm:Victim-a") {
		t.Fatalf("message = %q, want named workload", got)
	}
	if !strings.Contains(got, `start domain "Victim-a"`) {
		t.Fatalf("message = %q, want named domain text", got)
	}
}
