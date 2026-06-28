package daemonstatus

import (
	"io"
	"net"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestServeConnReturnsYAMLSnapshot(t *testing.T) {
	store := NewStore()
	store.Set(Snapshot{
		LabPath:   "/tmp/demo.lab",
		LabName:   "demo",
		UpdatedAt: time.Unix(100, 0).UTC(),
		States:    map[string]string{"container:web": "running"},
		VNCPorts:  map[string]int{"vm:router": 5903},
		Actions:   []string{"started container:web"},
	})
	server, client := net.Pipe()
	defer client.Close()
	go serveConn(server, store)

	if _, err := io.WriteString(client, CommandStatus+"\n"); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.LabPath != "/tmp/demo.lab" || got.LabName != "demo" {
		t.Fatalf("snapshot identity = %#v", got)
	}
	if got.States["container:web"] != "running" {
		t.Fatalf("state = %q, want running", got.States["container:web"])
	}
	if got.VNCPorts["vm:router"] != 5903 {
		t.Fatalf("vnc port = %d, want 5903", got.VNCPorts["vm:router"])
	}
	if len(got.Actions) != 1 || got.Actions[0] != "started container:web" {
		t.Fatalf("actions = %#v", got.Actions)
	}
}

func decodeSnapshot(data []byte) (Snapshot, error) {
	var snapshot Snapshot
	err := yaml.Unmarshal(data, &snapshot)
	return snapshot, err
}

func TestStoreCopiesSnapshots(t *testing.T) {
	store := NewStore()
	states := map[string]string{"vm:one": "running"}
	store.Set(Snapshot{States: states})
	states["vm:one"] = "shutoff"

	got := store.Get()
	got.States["vm:one"] = "missing"
	again := store.Get()
	if again.States["vm:one"] != "running" {
		t.Fatalf("stored state = %q, want immutable running", again.States["vm:one"])
	}
}
