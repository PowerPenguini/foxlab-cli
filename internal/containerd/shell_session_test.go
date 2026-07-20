package containerd

import (
	"context"
	"testing"
	"time"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

func TestOpenTerminalSessionReportsManagedContainerEndpoint(t *testing.T) {
	l := &lab.Lab{ID: "demo", Containers: []lab.Container{{ID: "web", Image: "nginx"}}}
	runtime := NewRuntime("/tmp/foxlab-containerd-session-test.sock")
	opened, err := runtime.OpenTerminalSession(context.Background(), l, workload.Ref{Type: workload.TypeContainer, ID: "web"}, workload.TerminalSize{Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if opened.Endpoint != l.ManagedContainerName(l.Containers[0]) {
		t.Fatalf("terminal endpoint = %q", opened.Endpoint)
	}
	if err := opened.Session.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = opened.Session.Wait(ctx)
}
