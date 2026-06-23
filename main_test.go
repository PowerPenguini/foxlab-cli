package main

import (
	"os"
	"path/filepath"
	"testing"

	"foxlab-cli/internal/lab"
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

func TestDefaultLabPathSearchesFoxlabHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	foxlabDir := filepath.Join(home, ".foxlab")
	if err := os.MkdirAll(foxlabDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(foxlabDir, "default.lab")
	if err := os.WriteFile(want, []byte("id: topology-tui\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok, err := defaultLabPath()
	if err != nil {
		t.Fatalf("defaultLabPath returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected default lab path")
	}
	if got != want {
		t.Fatalf("default lab path = %q, want %q", got, want)
	}
}

func TestDefaultLabPathCreatesFoxlabHomeLab(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, ok, err := defaultLabPath()
	if err != nil {
		t.Fatalf("defaultLabPath returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected default lab path")
	}
	want := filepath.Join(home, ".foxlab", "default.lab")
	if got != want {
		t.Fatalf("default lab path = %q, want %q", got, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected created default lab: %v", err)
	}
	loaded, err := lab.LoadFile(want)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if loaded.ID != "default" {
		t.Fatalf("created lab ID = %q, want default", loaded.ID)
	}
}
