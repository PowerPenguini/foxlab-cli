package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModelLoadsRealLabFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "real.lab")
	if err := os.WriteFile(path, []byte("id: real\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	model, err := loadModel(path, false)
	if err != nil {
		t.Fatalf("loadModel returned error: %v", err)
	}
	if model.ID != "real" {
		t.Fatalf("ID = %q, want real", model.ID)
	}
}

func TestLoadModelRequiresPathUnlessMock(t *testing.T) {
	if _, err := loadModel("", false); err == nil {
		t.Fatal("expected missing path error")
	}
}

func TestLoadModelMockIsExplicit(t *testing.T) {
	model, err := loadModel("", true)
	if err != nil {
		t.Fatalf("loadModel returned error: %v", err)
	}
	if model.ID != "mock" {
		t.Fatalf("ID = %q, want mock", model.ID)
	}
}

func TestDefaultLabPathSearchesParents(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "apps", "topology-tui", "cmd", "topology-tui")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "topology-tui.lab")
	if err := os.WriteFile(want, []byte("id: topology-tui\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)

	got, ok := defaultLabPath()
	if !ok {
		t.Fatal("expected default lab path")
	}
	if got != want {
		t.Fatalf("default lab path = %q, want %q", got, want)
	}
}
