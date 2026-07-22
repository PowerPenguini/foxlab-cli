package lab

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionPathAndRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	if err := SaveFile(path, &Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	session := NewSession(loaded, "")
	if got := session.Path(); got != loaded.Path() {
		t.Fatalf("Path() = %q, want loaded lab path %q", got, loaded.Path())
	}
	if got := session.Revision(); got != 0 {
		t.Fatalf("Revision() = %d, want 0", got)
	}

	explicit := filepath.Join(t.TempDir(), "explicit.lab")
	session.SetPath(explicit)
	if got := session.Path(); got != explicit {
		t.Fatalf("Path() = %q, want explicit path %q", got, explicit)
	}
	if got := session.Revision(); got != 1 {
		t.Fatalf("Revision() = %d after path change, want 1", got)
	}
	session.SetPath(explicit)
	if got := session.Revision(); got != 1 {
		t.Fatalf("Revision() = %d after unchanged path, want 1", got)
	}
	session.Replace(&Lab{ID: "replacement"})
	if got := session.Revision(); got != 2 {
		t.Fatalf("Revision() = %d after replace, want 2", got)
	}
}

func TestSessionSaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	session := NewSession(&Lab{ID: "demo", VMs: []VM{{ID: "vm1", MemoryMB: 512, CPUs: 1}}}, path)
	before := session.Current()
	if err := session.SaveAndReload(); err != nil {
		t.Fatal(err)
	}
	if session.Current() == before {
		t.Fatal("SaveAndReload did not replace the current lab")
	}
	if got := session.Revision(); got != 1 {
		t.Fatalf("Revision() = %d, want 1", got)
	}
	if got := session.Current().Path(); got != path {
		t.Fatalf("reloaded path = %q, want %q", got, path)
	}
}

func TestSessionSaveFailureReloadsPersistedState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	if err := SaveFile(path, &Lab{ID: "demo", Switches: []Switch{{ID: "lan", Mode: "bridge"}}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	session := NewSession(loaded, path)
	session.Current().Switches = append(session.Current().Switches, Switch{ID: "bad", Mode: "unsupported"})
	if err := session.SaveAndReload(); err == nil || !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("SaveAndReload error = %v, want unsupported mode", err)
	}
	if got := session.Current().Switches; len(got) != 1 || got[0].ID != "lan" {
		t.Fatalf("current switches after failed save = %#v, want persisted state", got)
	}
	if got := session.Revision(); got != 1 {
		t.Fatalf("Revision() = %d after recovery reload, want 1", got)
	}
}

func TestSessionSaveErrorsWithoutLabOrPath(t *testing.T) {
	if err := NewSession(nil, "demo.lab").SaveAndReload(); err == nil || err.Error() != "missing loaded lab" {
		t.Fatalf("missing lab error = %v", err)
	}
	if err := NewSession(&Lab{ID: "demo"}, "").SaveAndReload(); err == nil || err.Error() != "missing lab path" {
		t.Fatalf("missing path error = %v", err)
	}
}
