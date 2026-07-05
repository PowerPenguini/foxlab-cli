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
	got, args, err := resolveDirectAction([]string{"demo.lab", "sh", "web"})
	if err != nil {
		t.Fatalf("resolveDirectAction returned error: %v", err)
	}
	if got.kind != "shell" || got.name != "web" {
		t.Fatalf("direct action = %#v, want shell web", got)
	}
	if len(args) != 1 || args[0] != "demo.lab" {
		t.Fatalf("lab args = %#v, want demo.lab", args)
	}
}

func TestResolveDirectActionVNC(t *testing.T) {
	got, args, err := resolveDirectAction([]string{"vnc", "vm1"})
	if err != nil {
		t.Fatalf("resolveDirectAction returned error: %v", err)
	}
	if got.kind != "vnc" || got.name != "vm1" {
		t.Fatalf("direct action = %#v, want vnc vm1", got)
	}
	if len(args) != 0 {
		t.Fatalf("lab args = %#v, want none", args)
	}
}

func TestResolveDirectActionCopy(t *testing.T) {
	got, args, err := resolveDirectAction([]string{"cp", "./file", "web:/tmp/file"})
	if err != nil {
		t.Fatalf("resolveDirectAction returned error: %v", err)
	}
	if got.kind != "cp" || got.src != "./file" || got.dst != "web:/tmp/file" {
		t.Fatalf("direct action = %#v, want cp", got)
	}
	if len(args) != 0 {
		t.Fatalf("lab args = %#v, want none", args)
	}
}

func TestResolveDirectActionCopyAfterPositionalLab(t *testing.T) {
	got, args, err := resolveDirectAction([]string{"demo.lab", "cp", "web:/tmp/file", "./file"})
	if err != nil {
		t.Fatalf("resolveDirectAction returned error: %v", err)
	}
	if got.kind != "cp" || got.src != "web:/tmp/file" || got.dst != "./file" {
		t.Fatalf("direct action = %#v, want cp", got)
	}
	if len(args) != 1 || args[0] != "demo.lab" {
		t.Fatalf("lab args = %#v, want demo.lab", args)
	}
}

func TestResolveDirectActionRejectsShellUsage(t *testing.T) {
	if _, _, err := resolveDirectAction([]string{"sh"}); err == nil || !strings.Contains(err.Error(), "usage: foxlab sh NAME") {
		t.Fatalf("shell usage error = %v, want usage", err)
	}
	if _, _, err := resolveDirectAction([]string{"sh", "web", "extra"}); err == nil || !strings.Contains(err.Error(), "usage: foxlab sh NAME") {
		t.Fatalf("shell extra args error = %v, want usage", err)
	}
}

func TestResolveDirectActionRejectsVNCUsage(t *testing.T) {
	if _, _, err := resolveDirectAction([]string{"vnc"}); err == nil || !strings.Contains(err.Error(), "usage: foxlab vnc NAME") {
		t.Fatalf("vnc usage error = %v, want usage", err)
	}
}

func TestResolveDirectActionRejectsCopyUsage(t *testing.T) {
	if _, _, err := resolveDirectAction([]string{"cp", "one"}); err == nil || !strings.Contains(err.Error(), "usage: foxlab cp SRC DST") {
		t.Fatalf("copy usage error = %v, want usage", err)
	}
}

func TestParseCopyEndpoint(t *testing.T) {
	remote := parseCopyEndpoint("web:/tmp/file")
	if !remote.Remote || remote.Workload != "web" || remote.Path != "/tmp/file" {
		t.Fatalf("remote endpoint = %#v", remote)
	}
	local := parseCopyEndpoint("./a:b")
	if local.Remote || local.Path != "./a:b" {
		t.Fatalf("local endpoint = %#v", local)
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
