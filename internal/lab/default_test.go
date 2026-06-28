package lab

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestDefaultPathSearchesFoxlabHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")

	foxlabDir := filepath.Join(home, ".foxlab")
	if err := os.MkdirAll(foxlabDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(foxlabDir, "default.lab")
	if err := os.WriteFile(want, []byte("name: topology-tui\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected default lab path")
	}
	if got != want {
		t.Fatalf("default lab path = %q, want %q", got, want)
	}
}

func TestDefaultPathCreatesFoxlabHomeLab(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")

	got, ok, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath returned error: %v", err)
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
	data, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "name: default\n" {
		t.Fatalf("created default lab = %q, want name field", string(data))
	}
	loaded, err := LoadFile(want)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if loaded.ID != "default" {
		t.Fatalf("created lab ID = %q, want default", loaded.ID)
	}
}

func TestDefaultPathCreatesDefaultLabEvenWhenOtherLabExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")

	foxlabDir := filepath.Join(home, ".foxlab")
	if err := os.MkdirAll(foxlabDir, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(foxlabDir, "demo.lab")
	if err := os.WriteFile(other, []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected default lab path")
	}
	want := filepath.Join(foxlabDir, "default.lab")
	if got != want {
		t.Fatalf("default lab path = %q, want %q", got, want)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("expected existing lab to be left alone: %v", err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected created default lab: %v", err)
	}
}

func TestDefaultPathRejectsNonRegularDefaultLab(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fifo regression is Linux-specific")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")

	foxlabDir := filepath.Join(home, ".foxlab")
	if err := os.MkdirAll(foxlabDir, 0o755); err != nil {
		t.Fatal(err)
	}
	defaultPath := filepath.Join(foxlabDir, "default.lab")
	if err := syscall.Mkfifo(defaultPath, 0o600); err != nil {
		t.Fatal(err)
	}

	_, ok, err := DefaultPath()
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("DefaultPath error = %v, want non-regular file error", err)
	}
	if ok {
		t.Fatal("DefaultPath reported ok for fifo default.lab")
	}
}
