package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLabPathAcceptsSinglePositionalLab(t *testing.T) {
	got, err := resolveLabPath("", []string{"demo.lab"})
	if err != nil {
		t.Fatalf("resolveLabPath returned error: %v", err)
	}
	if got != "demo.lab" {
		t.Fatalf("lab path = %q, want demo.lab", got)
	}
}

func TestResolveLabPathRejectsAmbiguousInputs(t *testing.T) {
	tests := []struct {
		name     string
		flagPath string
		args     []string
		want     string
	}{
		{name: "extra positional", args: []string{"one.lab", "two.lab"}, want: "unexpected argument"},
		{name: "flag and positional", flagPath: "one.lab", args: []string{"two.lab"}, want: "--lab is already set"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveLabPath(tt.flagPath, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("resolveLabPath error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoadModelLoadsRealLabFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "real.lab")
	if err := os.WriteFile(path, []byte("name: real\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	model, err := loadModel(path)
	if err != nil {
		t.Fatalf("loadModel returned error: %v", err)
	}
	if model.ID != "real" {
		t.Fatalf("ID = %q, want real", model.ID)
	}
}

func TestLoadModelRequiresPath(t *testing.T) {
	if _, err := loadModel(""); err == nil {
		t.Fatal("expected missing path error")
	}
}
