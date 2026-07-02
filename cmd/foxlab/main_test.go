package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topologyui"
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

func TestResolveDirectActionShellTargets(t *testing.T) {
	got, err := resolveDirectAction("web", "")
	if err != nil {
		t.Fatalf("resolveDirectAction returned error: %v", err)
	}
	if got.kind != "shell" || got.name != "web" {
		t.Fatalf("direct action = %#v, want shell web", got)
	}
}

func TestResolveDirectActionVNC(t *testing.T) {
	got, err := resolveDirectAction("", "vm1")
	if err != nil {
		t.Fatalf("resolveDirectAction returned error: %v", err)
	}
	if got.kind != "vnc" || got.name != "vm1" {
		t.Fatalf("direct action = %#v, want vnc vm1", got)
	}
}

func TestResolveDirectActionRejectsConflicts(t *testing.T) {
	if _, err := resolveDirectAction("web", "vm1"); err == nil || !strings.Contains(err.Error(), "choose only one") {
		t.Fatalf("conflict error = %v, want choose only one", err)
	}
}

func TestResolveShellWorkloadByIDOrName(t *testing.T) {
	loaded := &lab.Lab{
		VMs:        []lab.VM{{ID: "vm1", Name: "router"}},
		Containers: []lab.Container{{ID: "ct1", Name: "web"}},
	}
	tests := []struct {
		name     string
		target   string
		wantType string
		wantID   string
	}{
		{name: "vm id", target: "vm1", wantType: topologyui.NodeVM, wantID: "vm1"},
		{name: "vm name", target: "router", wantType: topologyui.NodeVM, wantID: "vm1"},
		{name: "container id", target: "ct1", wantType: topologyui.NodeContainer, wantID: "ct1"},
		{name: "container name", target: "web", wantType: topologyui.NodeContainer, wantID: "ct1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotID, err := resolveShellWorkload(loaded, tt.target)
			if err != nil {
				t.Fatalf("resolveShellWorkload returned error: %v", err)
			}
			if gotType != tt.wantType || gotID != tt.wantID {
				t.Fatalf("shell target = %s:%s, want %s:%s", gotType, gotID, tt.wantType, tt.wantID)
			}
		})
	}
}

func TestResolveShellWorkloadRejectsAmbiguousName(t *testing.T) {
	loaded := &lab.Lab{
		VMs:        []lab.VM{{ID: "vm1", Name: "web"}},
		Containers: []lab.Container{{ID: "ct1", Name: "web"}},
	}
	if _, _, err := resolveShellWorkload(loaded, "web"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous shell target error = %v, want ambiguous", err)
	}
}

func TestResolveVMNameByIDOrName(t *testing.T) {
	loaded := &lab.Lab{
		VMs: []lab.VM{{ID: "vm1", Name: "router"}},
	}
	for _, target := range []string{"vm1", "router"} {
		t.Run(target, func(t *testing.T) {
			got, err := resolveVMName(loaded, target)
			if err != nil {
				t.Fatalf("resolveVMName returned error: %v", err)
			}
			if got != "vm1" {
				t.Fatalf("vm id = %q, want vm1", got)
			}
		})
	}
}

func TestResolveVMNameRejectsContainerOnlyName(t *testing.T) {
	loaded := &lab.Lab{
		Containers: []lab.Container{{ID: "ct1", Name: "web"}},
	}
	if _, err := resolveVMName(loaded, "web"); err == nil || !strings.Contains(err.Error(), "vm not found") {
		t.Fatalf("container-only vnc target error = %v, want vm not found", err)
	}
}
