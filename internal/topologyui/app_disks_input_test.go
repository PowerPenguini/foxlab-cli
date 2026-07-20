package topologyui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/lab"
)

func TestContextMenuInlineEditInsertsAtCursor(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{
			Focus:              FocusGraph,
			Selected:           1,
			ContextMenu:        true,
			ContextGroup:       "config-menu",
			ContextInSubmenu:   true,
			ContextSubSelected: 1,
			ContextEdit:        true,
			ContextEditValue:   "ac",
			ContextEditCursor:  1,
		},
	}

	app.handleKey("char:b")
	if app.State.ContextEditValue != "abc" {
		t.Fatalf("edit value = %q, want abc", app.State.ContextEditValue)
	}
	if app.State.ContextEditCursor != 2 {
		t.Fatalf("edit cursor = %d, want 2", app.State.ContextEditCursor)
	}

	app.handleKey("backspace")
	if app.State.ContextEditValue != "ac" {
		t.Fatalf("edit value after cursor backspace = %q, want ac", app.State.ContextEditValue)
	}
}

func TestContextMenuCheckboxTogglesBool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.img", VNC: false}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		runtimeAccess: testRuntimeAccess(&fakeVMRuntime{
			states:   map[string]string{NodeKey(NodeVM, "vm1"): "running"},
			vncPorts: map[string]int{NodeKey(NodeVM, "vm1"): 5904},
		}),
		State: ViewState{
			Focus:              FocusGraph,
			ContextMenu:        true,
			ContextGroup:       "config-menu",
			ContextInSubmenu:   true,
			ContextSubSelected: 4,
		},
	}

	app.handleKey("enter")
	if app.State.ContextEdit {
		t.Fatal("checkbox opened text editor")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.VMs[0].VNC {
		t.Fatalf("vnc = %t, want true", reloaded.VMs[0].VNC)
	}
	if details := strings.Join(app.Model.Nodes[0].Details, "\n"); !strings.Contains(details, "vnc-port=5904") {
		t.Fatalf("model details missing refreshed VNC port: %#v", app.Model.Nodes[0].Details)
	}
}

func TestContainerPermissionsMenuTogglesCapabilitiesAndPersistsThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{ID: "demo", Containers: []lab.Container{{ID: "kali", Image: "kali"}}}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State: ViewState{
			Focus:            FocusGraph,
			ContextMenu:      true,
			ContextGroup:     "permissions-menu",
			ContextInSubmenu: true,
		},
	}
	items := app.contextMenuSubmenuItems(app.Model.Nodes[0], true)
	if len(items) < 2 || items[0] != "[ ] NET_ADMIN" || items[1] != "[X] NET_RAW" {
		t.Fatalf("permission menu starts with %#v", items[:min(2, len(items))])
	}

	app.handleKey("enter")
	if !app.State.ContextMenu || !app.State.ContextInSubmenu {
		t.Fatalf("capability toggle closed menu: %#v", app.State)
	}
	if !lab.ContainerCapabilityEnabled(app.Lab.Containers[0], "NET_ADMIN") {
		t.Fatalf("NET_ADMIN was not enabled: %#v", app.Lab.Containers[0].Capabilities)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !lab.ContainerCapabilityEnabled(reloaded.Containers[0], "NET_ADMIN") {
		t.Fatalf("persisted capabilities = %#v", reloaded.Containers[0].Capabilities)
	}

	app.State.ContextSubSelected = 1
	app.handleKey("enter")
	if lab.ContainerCapabilityEnabled(app.Lab.Containers[0], "NET_RAW") {
		t.Fatalf("NET_RAW was not disabled: %#v", app.Lab.Containers[0].Capabilities)
	}
	if got := app.Lab.Containers[0].Capabilities; got == nil || len(got.Drop) != 1 || got.Drop[0] != "NET_RAW" {
		t.Fatalf("NET_RAW drop = %#v", got)
	}
}

func TestContextMenuDiskRootOpensDiskSubmenu(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2}},
		Disks: []lab.Disk{{
			ID:     "data",
			Path:   "disks/data.qcow2",
			SizeGB: 4,
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}

	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok {
		t.Fatal("no selected node")
	}
	items := app.contextMenuRootItems(node, true)
	diskIndex := -1
	for i, item := range items {
		if contextMenuAction(item) == "disk-menu" {
			diskIndex = i
			break
		}
	}
	if diskIndex < 0 {
		t.Fatal("Disk root menu item not found")
	}
	app.State.ContextMenu = true
	app.State.ContextSelected = diskIndex
	app.handleKey("enter")

	if !app.State.ContextInSubmenu || app.State.ContextGroup != "disk-menu" {
		t.Fatalf("disk root did not open submenu: %#v", app.State)
	}
	if app.State.ContextEdit {
		t.Fatal("disk submenu opened text editor")
	}
	if got := strings.Join(app.State.DiskMenuItems, "\n"); !strings.Contains(got, "Add Disk") || !strings.Contains(got, "data 4G") {
		t.Fatalf("disk menu items = %#v, want data disk", app.State.DiskMenuItems)
	}
	out := RenderString(app.Model, app.State, 100, 30, false)
	if !strings.Contains(out, "data 4G") {
		t.Fatalf("rendered disk menu missing disk:\n%s", out)
	}
}

func TestDiskMenuEnterAttachesContainerBaseThroughLayer(t *testing.T) {
	restore := fakeQemuImg(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Name: "web", Image: "docker.io/library/alpine:latest"}},
		Disks: []lab.Disk{{
			ID:     "data",
			Path:   "disks/data.qcow2",
			SizeGB: 4,
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}
	node := Node{ID: loaded.Containers[0].ID, Type: NodeContainer}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	app.State.ContextSubSelected = 1
	app.handleKey("enter")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("disk count after data attach = %d, want base only", len(reloaded.Disks))
	}
	if reloaded.Disks[0].Kind != "base" || reloaded.Disks[0].AttachedType != "container" || reloaded.Disks[0].AttachedTo != reloaded.Containers[0].ID {
		t.Fatalf("base disk not attached as container root layer: %#v", reloaded.Disks[0])
	}
	if reloaded.Containers[0].Disk == "" || !strings.Contains(reloaded.Containers[0].Disk, "/disks/data.qcow2") {
		t.Fatalf("container disk after data attach = %q", reloaded.Containers[0].Disk)
	}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	if len(app.State.DiskMenuItems) < 2 || app.State.DiskMenuItems[1] != "data 4G" {
		t.Fatalf("disk menu items after base attach = %#v", app.State.DiskMenuItems)
	}
	if len(app.State.DiskMenuActions) < 2 || app.State.DiskMenuActions[1] != diskMenuActionNone {
		t.Fatalf("disk menu actions after base attach = %#v", app.State.DiskMenuActions)
	}
	app.State.ContextSubSelected = 1
	app.handleKey("right")
	app.handleKey("enter")

	reloaded, err = lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Containers[0].Disk != "" {
		t.Fatalf("container disk after data detach = %q", reloaded.Containers[0].Disk)
	}
	if reloaded.Disks[0].AttachedTo != "" {
		t.Fatalf("base disk still attached after detach: %#v", reloaded.Disks[0])
	}
}

func TestDiskMenuAttachAndDetach(t *testing.T) {
	restore := fakeQemuImg(t)
	defer restore()

	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2}},
		Disks: []lab.Disk{{
			ID:     "data",
			Path:   "disks/data.qcow2",
			SizeGB: 4,
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}
	node := Node{ID: loaded.VMs[0].ID, Type: NodeVM}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	if len(app.State.DiskMenuItems) < 2 || app.State.DiskMenuItems[0] != "Add Disk" || !strings.Contains(app.State.DiskMenuItems[1], "data") {
		t.Fatalf("disk menu items before attach = %#v", app.State.DiskMenuItems)
	}
	app.handleKey("down")
	app.handleKey("enter")
	if app.State.ContextMenu {
		t.Fatal("context menu stayed open after attach")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Disk == "" || !strings.Contains(reloaded.VMs[0].Disk, "/disks/data.qcow2") {
		t.Fatalf("vm disk after attach = %q", reloaded.VMs[0].Disk)
	}
	if len(reloaded.Disks) != 1 || reloaded.Disks[0].ID != "data" || reloaded.Disks[0].AttachedType != "vm" || reloaded.Disks[0].AttachedTo != reloaded.VMs[0].ID {
		t.Fatalf("disk after attach = %#v", reloaded.Disks)
	}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", Node{ID: reloaded.VMs[0].ID, Type: NodeVM}, true)
	app.State.ContextInSubmenu = true
	app.State.ContextSubSelected = 1
	app.handleKey("right")
	app.handleKey("enter")
	reloaded, err = lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Disk != "" {
		t.Fatalf("vm disk after detach = %q", reloaded.VMs[0].Disk)
	}
	if len(reloaded.Disks) != 1 || reloaded.Disks[0].AttachedTo != "" {
		t.Fatalf("disk after detach = %#v, want detached base", reloaded.Disks)
	}
}

func TestDiskMenuDeleteActiveLayerWithX(t *testing.T) {
	restore := fakeQemuImg(t)
	defer restore()

	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2}},
		Disks: []lab.Disk{{
			ID:     "data",
			Path:   "disks/data.qcow2",
			SizeGB: 4,
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}
	node := Node{ID: loaded.VMs[0].ID, Type: NodeVM}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	app.State.ContextSubSelected = 1
	app.handleKey("right")
	app.handleKey("enter")
	app.handleKey("enter")

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	app.State.ContextSubSelected = 2
	app.handleKey("right")
	app.handleKey("right")
	app.handleKey("right")
	app.handleKey("enter")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Disk != "" {
		t.Fatalf("vm disk after delete = %q, want detached", reloaded.VMs[0].Disk)
	}
	if len(reloaded.Disks) != 1 || reloaded.Disks[0].ID != "data" {
		t.Fatalf("disks after delete = %#v, want only base", reloaded.Disks)
	}
}

func TestDiskMenuAddDiskCreatesBaseDisk(t *testing.T) {
	restore := fakeQemuImg(t)
	defer restore()

	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}
	node := Node{ID: "vm1", Type: NodeVM}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	if len(app.State.DiskMenuItems) < 1 || app.State.DiskMenuItems[0] != "Add Disk" {
		t.Fatalf("disk menu items = %#v", app.State.DiskMenuItems)
	}
	app.handleKey("enter")

	if !app.State.ContextEdit {
		t.Fatal("add disk did not open inline name edit")
	}
	if app.State.CommandMode || app.State.Command != "" {
		t.Fatalf("add disk opened command mode: mode=%t command=%q", app.State.CommandMode, app.State.Command)
	}
	if !app.State.ContextMenu || app.State.ContextGroup != "disk-menu" {
		t.Fatalf("add disk edit left context menu: %#v", app.State)
	}
	for range "disk" {
		app.handleKey("backspace")
	}
	for _, r := range "data" {
		app.handleKey("char:" + string(r))
	}
	app.handleKey("enter")
	if app.State.ContextEdit {
		t.Fatal("inline disk name edit stayed open after enter")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("disk count = %d, want base only", len(reloaded.Disks))
	}
	if reloaded.Disks[0].ID != "data" {
		t.Fatalf("base disk id = %q, want custom disk name", reloaded.Disks[0].ID)
	}
	if reloaded.VMs[0].Disk != "" {
		t.Fatalf("vm disk after add = %q, want no attached layer", reloaded.VMs[0].Disk)
	}
	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	if got := strings.Join(app.State.DiskMenuItems, "\n"); !strings.Contains(got, "data 10G") {
		t.Fatalf("disk menu after add = %#v, want attachable base", app.State.DiskMenuItems)
	}
}

func TestDiskMenuCreatesSwitchesAndDeletesLayerVariants(t *testing.T) {
	restore := fakeQemuImg(t)
	defer restore()

	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2}},
		Disks: []lab.Disk{{
			ID:     "data",
			Path:   "disks/data.qcow2",
			SizeGB: 4,
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}
	node := Node{ID: "vm1", Type: NodeVM}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	app.State.ContextSubSelected = 1
	app.handleKey("right")
	app.handleKey("enter")
	app.handleKey("enter")

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	baseIndex := indexOfContextItem(app.State.DiskMenuItems, "data")
	if baseIndex < 0 {
		t.Fatalf("base row missing after first attach: %#v", app.State.DiskMenuItems)
	}
	app.State.ContextSubSelected = baseIndex
	app.handleKey("right")
	app.handleKey("enter")
	app.handleKey("enter")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 3 {
		t.Fatalf("disk count after second layer = %d, want 3", len(reloaded.Disks))
	}
	if !strings.Contains(reloaded.VMs[0].Disk, "/layers/data-layer-2.qcow2") {
		t.Fatalf("active disk after second layer = %q", reloaded.VMs[0].Disk)
	}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	savedFirst := indexOfExactContextItem(app.State.DiskMenuItems, "data | data-layer")
	if savedFirst < 0 {
		t.Fatalf("saved first layer missing: %#v", app.State.DiskMenuItems)
	}
	app.State.ContextSubSelected = savedFirst
	app.handleKey("enter")

	reloaded, err = lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reloaded.VMs[0].Disk, "/layers/data-layer.qcow2") {
		t.Fatalf("active disk after switching layer = %q", reloaded.VMs[0].Disk)
	}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	savedSecond := indexOfExactContextItem(app.State.DiskMenuItems, "data | data-layer-2")
	if savedSecond < 0 {
		t.Fatalf("saved second layer missing: %#v", app.State.DiskMenuItems)
	}
	app.State.ContextSubSelected = savedSecond
	app.handleKey("right")
	app.handleKey("right")
	app.handleKey("enter")

	reloaded, err = lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, disk := range reloaded.Disks {
		if disk.ID == "data-layer-2" {
			t.Fatalf("deleted layer still present: %#v", reloaded.Disks)
		}
	}
}

func TestDiskMenuAddLayerPromptsForLayerName(t *testing.T) {
	restore := fakeQemuImg(t)
	defer restore()

	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2}},
		Disks: []lab.Disk{{
			ID:     "data",
			Path:   "disks/data.qcow2",
			SizeGB: 4,
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}
	node := Node{ID: "vm1", Type: NodeVM}

	app.State.ContextMenu = true
	app.setContextGroup("disk-menu", node, true)
	app.State.ContextInSubmenu = true
	app.State.ContextSubSelected = 1
	app.handleKey("right")
	app.handleKey("enter")
	if !app.State.ContextEdit {
		t.Fatal("add layer did not open inline name edit")
	}
	for range "data-layer" {
		app.handleKey("backspace")
	}
	for _, r := range "clean" {
		app.handleKey("char:" + string(r))
	}
	app.handleKey("enter")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 2 || reloaded.Disks[1].ID != "clean" {
		t.Fatalf("disks = %#v, want custom layer", reloaded.Disks)
	}
	if !strings.Contains(reloaded.VMs[0].Disk, "/layers/clean.qcow2") {
		t.Fatalf("vm disk = %q", reloaded.VMs[0].Disk)
	}
}

func indexOfContextItem(items []string, prefix string) int {
	for i, item := range items {
		if strings.HasPrefix(item, prefix) {
			return i
		}
	}
	return -1
}

func indexOfExactContextItem(items []string, want string) int {
	for i, item := range items {
		treeItem := strings.TrimPrefix(item, diskMenuLayerTreePrefix)
		_, wantLayer, hasWantBase := strings.Cut(want, "|")
		if item == want || treeItem == want || (hasWantBase && strings.TrimSpace(wantLayer) == treeItem) {
			return i
		}
	}
	return -1
}

func TestRunStopActionsSetVMDesiredState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.img"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeVMRuntime{states: map[string]string{NodeKey(NodeVM, "vm1"): "shutoff"}}
	daemon := &fakeDaemonController{}
	stateKey := NodeKey(NodeVM, "vm1")
	app := App{
		Model:            ModelFromLab(loaded),
		Lab:              loaded,
		LabPath:          path,
		runtimeAccess:    testRuntimeAccess(runtime),
		WorkloadStates:   map[string]string{stateKey: "running"},
		DaemonController: daemon,
		State:            ViewState{Focus: FocusGraph},
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "run")
	if runtime.starts != 0 {
		t.Fatalf("run called runtime Start %d times", runtime.starts)
	}
	if app.Lab.VMs[0].DesiredState != lab.DesiredStateRunning {
		t.Fatalf("desired state after run = %q, want running", app.Lab.VMs[0].DesiredState)
	}
	if daemon.applyCalls != 1 {
		t.Fatalf("run applied lab %d times, want 1", daemon.applyCalls)
	}
	if app.State.Message != "" {
		t.Fatalf("run message = %q, want no desired-state notification", app.State.Message)
	}
	if daemon.lastApply.LabPath != path {
		t.Fatalf("applied lab path = %q, want %q", daemon.lastApply.LabPath, path)
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "stop")
	if runtime.stops != 0 {
		t.Fatalf("stop called runtime Stop %d times", runtime.stops)
	}
	if app.Lab.VMs[0].DesiredState != lab.DesiredStateStopped {
		t.Fatalf("desired state after stop = %q, want stopped", app.Lab.VMs[0].DesiredState)
	}
	if daemon.applyCalls != 2 {
		t.Fatalf("run+stop applied lab %d times, want 2", daemon.applyCalls)
	}
	if app.State.Message != "" {
		t.Fatalf("stop message = %q, want no desired-state notification", app.State.Message)
	}
	node, ok := nodeByKey(app.Model, stateKey)
	if !ok {
		t.Fatal("vm node not found")
	}
	if node.State != "shutoff" {
		t.Fatalf("vm state after stop = %q, want actual shutoff after daemon apply", node.State)
	}
}

func TestStopActionShowsStoppingWhenAppliedLabIsReconciling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.img"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stateKey := NodeKey(NodeVM, "vm1")
	runtime := &fakeVMRuntime{states: map[string]string{stateKey: "running"}}
	daemon := &fakeDaemonController{status: DaemonStatus{Active: true, LabPath: path}}
	app := App{
		Model:            ModelFromLab(loaded),
		Lab:              loaded,
		LabPath:          path,
		runtimeAccess:    testRuntimeAccess(runtime),
		WorkloadStates:   map[string]string{stateKey: "running"},
		DaemonController: daemon,
		State:            ViewState{Focus: FocusGraph, ApplyLabDisabled: true},
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "stop")
	if daemon.applyCalls != 0 {
		t.Fatalf("stop applied already active lab %d times, want 0", daemon.applyCalls)
	}

	node, ok := nodeByKey(app.Model, stateKey)
	if !ok {
		t.Fatal("vm node not found")
	}
	if node.State != "stopping" {
		t.Fatalf("vm state after stop = %q, want stopping for applied lab", node.State)
	}
}

func TestRunStopActionsSetContainerDesiredState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:         "demo",
		Switches:   []lab.Switch{{ID: "lan", Mode: "bridge"}},
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", Networks: []lab.ContainerNetwork{{Switch: "lan"}}}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeVMRuntime{states: map[string]string{NodeKey(NodeContainer, "web"): "stopped"}}
	daemon := &fakeDaemonController{}
	app := App{
		Model:            ModelFromLab(loaded),
		Lab:              loaded,
		LabPath:          path,
		runtimeAccess:    testRuntimeAccess(runtime),
		DaemonController: daemon,
		State:            ViewState{Focus: FocusGraph},
	}

	app.runMenuAction(Node{ID: "web", Type: NodeContainer}, "run")
	if runtime.starts != 0 {
		t.Fatalf("run called runtime Start %d times", runtime.starts)
	}
	if app.Lab.Containers[0].DesiredState != lab.DesiredStateRunning {
		t.Fatalf("desired state after run = %q, want running", app.Lab.Containers[0].DesiredState)
	}
	if daemon.applyCalls != 1 {
		t.Fatalf("container run applied lab %d times, want 1", daemon.applyCalls)
	}
	if app.State.Message != "" {
		t.Fatalf("container run message = %q, want no desired-state notification", app.State.Message)
	}

	app.runMenuAction(Node{ID: "web", Type: NodeContainer}, "stop")
	if runtime.stops != 0 {
		t.Fatalf("stop called runtime Stop %d times", runtime.stops)
	}
	if app.Lab.Containers[0].DesiredState != lab.DesiredStateStopped {
		t.Fatalf("desired state after stop = %q, want stopped", app.Lab.Containers[0].DesiredState)
	}
	if daemon.applyCalls != 2 {
		t.Fatalf("container run+stop applied lab %d times, want 2", daemon.applyCalls)
	}
	if app.State.Message != "" {
		t.Fatalf("container stop message = %q, want no desired-state notification", app.State.Message)
	}
}

func TestRunActionShowsStartingForPendingMissingContainer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:         "demo",
		Switches:   []lab.Switch{{ID: "lan", Mode: "bridge"}},
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", Networks: []lab.ContainerNetwork{{Switch: "lan"}}}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stateKey := NodeKey(NodeContainer, loaded.Containers[0].ID)
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		runtimeAccess: testRuntimeAccessWithStatus(
			&fakeVMRuntime{states: map[string]string{stateKey: "missing"}},
			func(context.Context, string) (daemonstatus.Snapshot, error) {
				return daemonstatus.Snapshot{}, errors.New("no daemon snapshot")
			},
		),
		DaemonController: &fakeDaemonController{},
		State:            ViewState{Focus: FocusGraph},
	}

	app.runMenuAction(Node{ID: loaded.Containers[0].ID, Type: NodeContainer}, "run")

	node, ok := nodeByKey(app.Model, stateKey)
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "starting" {
		t.Fatalf("container state after run = %q, want starting", node.State)
	}
	if !app.PendingStarts[stateKey] {
		t.Fatalf("pending starts = %#v, want %s", app.PendingStarts, stateKey)
	}
	if app.State.Message != "" {
		t.Fatalf("run message = %q, want no desired-state notification", app.State.Message)
	}
}

func TestDaemonErrorClearsPendingMissingStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", DesiredState: lab.DesiredStateRunning}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stateKey := NodeKey(NodeContainer, "web")
	app := App{
		Model:         ModelFromLab(loaded),
		Lab:           loaded,
		LabPath:       path,
		PendingStarts: map[string]bool{stateKey: true},
		runtimeAccess: newRuntimeAccess(nil, "", func(context.Context, string) (daemonstatus.Snapshot, error) {
			return daemonstatus.Snapshot{
				LabPath: path,
				LabName: "demo",
				States:  map[string]string{stateKey: "missing"},
				Errors:  []string{"start container:web: image unavailable"},
			}, nil
		}),
	}

	app.refreshWorkloadStates()

	node, ok := nodeByKey(app.Model, stateKey)
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "missing" {
		t.Fatalf("container state after daemon error = %q, want missing", node.State)
	}
	if app.PendingStarts != nil {
		t.Fatalf("pending starts = %#v, want cleared", app.PendingStarts)
	}
	if !strings.Contains(app.State.Message, "image unavailable") {
		t.Fatalf("message = %q, want daemon error", app.State.Message)
	}
}

func TestInteractiveRunEntersAltScreenBeforeRendering(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{Focus: FocusGraph},
	}
	start := func(app *App) (func(), error) {
		_, _ = io.WriteString(app.Out, ansiEnterAltScreen+ansiHide+ansiClear)
		return func() {
			_, _ = io.WriteString(app.Out, ansiShow+ansiReset+ansiExitAltScreen)
		}, nil
	}
	read := func(*App) (string, error) { return "quit", nil }
	size := func(*App) (int, int) { return 80, 20 }
	app.Out = tempOutputFile(t)

	if err := app.runInteractive(start, read, size); err != nil {
		t.Fatal(err)
	}

	got := outputFileString(t, app.Out)
	frameStart := strings.Index(got, "[VM]")
	labelStart := strings.Index(got, "router")
	altStart := strings.Index(got, ansiEnterAltScreen)
	if altStart == -1 {
		t.Fatalf("interactive output missing enter alt-screen sequence: %q", got)
	}
	if frameStart == -1 || labelStart == -1 {
		t.Fatalf("interactive output missing rendered frame: %q", got)
	}
	if altStart > frameStart {
		t.Fatalf("enter alt-screen sequence appears after frame render: %q", got)
	}
}

func TestInteractiveRunCleanupExitsAltScreen(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	start := func(app *App) (func(), error) {
		_, _ = io.WriteString(app.Out, ansiEnterAltScreen+ansiHide+ansiClear)
		return func() {
			_, _ = io.WriteString(app.Out, ansiShow+ansiReset+ansiExitAltScreen)
		}, nil
	}
	read := func(*App) (string, error) { return "quit", nil }
	size := func(*App) (int, int) { return 80, 20 }
	app.Out = tempOutputFile(t)

	if err := app.runInteractive(start, read, size); err != nil {
		t.Fatal(err)
	}

	got := outputFileString(t, app.Out)
	cleanup := ansiShow + ansiReset + ansiExitAltScreen
	if !strings.HasSuffix(got, cleanup) {
		t.Fatalf("interactive output does not end with cleanup %q:\n%q", cleanup, got)
	}
}

func TestInteractiveRunTreatsTerminalEOFAsCleanExit(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}, Out: tempOutputFile(t)}
	cleaned := false
	start := func(*App) (func(), error) {
		return func() { cleaned = true }, nil
	}
	read := func(*App) (string, error) { return "", io.EOF }
	if err := app.runInteractive(start, read, func(*App) (int, int) { return 80, 20 }); err != nil {
		t.Fatalf("terminal EOF = %v", err)
	}
	if !cleaned {
		t.Fatal("terminal EOF skipped cleanup")
	}
}

func TestOneFrameDoesNotUseAltScreen(t *testing.T) {
	var out bytes.Buffer
	if err := OneFrame(&out, MockModel(), 80, 20); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, notWant := range []string{ansiEnterAltScreen, ansiExitAltScreen, ansiHide, ansiShow} {
		if strings.Contains(got, notWant) {
			t.Fatalf("one-frame output contains terminal session sequence %q:\n%q", notWant, got)
		}
	}
}

func tempOutputFile(t *testing.T) *os.File {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "topologyui-output-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = file.Close()
	})
	return file
}

func outputFileString(t *testing.T, file *os.File) string {
	t.Helper()
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestCommandHelpTopics(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"help add", "global add menu"},
		{"help vm", "right inspector"},
		{"help switch", "switch: edit name"},
		{"help external", "uplink: edit name"},
		{"help wat", "unknown help topic: wat"},
	}

	for _, tt := range tests {
		app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
		app.executeCommand(tt.command)
		got := strings.Join(app.State.Console, "\n")
		if !strings.Contains(got, tt.want) {
			t.Fatalf("%q output missing %q:\n%s", tt.command, tt.want, got)
		}
		if len(app.State.Console) > 5 {
			t.Fatalf("%q help lines = %d, want <= 5", tt.command, len(app.State.Console))
		}
	}
}

func TestCommandHelpRejectsExtraArgs(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{
			Focus:   FocusGraph,
			Console: []string{"existing help"},
		},
	}

	app.executeCommand("help vm extra")
	if app.State.Message != "usage: help [topic]" {
		t.Fatalf("message = %q, want usage: help [topic]", app.State.Message)
	}
	if got := strings.Join(app.State.Console, "\n"); got != "existing help" {
		t.Fatalf("console = %q, want existing help", got)
	}
}

func TestCommandBarIsRemoved(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	app.openCommand("")
	if app.State.CommandMode || app.State.Command != "" {
		t.Fatalf("command bar state = mode:%t command:%q, want disabled", app.State.CommandMode, app.State.Command)
	}
	if app.State.Message != "command bar removed; use the menu" {
		t.Fatalf("message = %q", app.State.Message)
	}
}

func TestCommandQClosesOnlyActiveCard(t *testing.T) {
	app := App{Model: MockModel(), Lab: &lab.Lab{ID: "demo"}, State: ViewState{Focus: FocusGraph}}
	app.openDiskExplorer()

	if app.executeCommand("q") {
		t.Fatal(":q quit the application")
	}
	if len(app.tabs.tabs) != 1 || app.tabs.active != 0 {
		t.Fatalf(":q left tabs=%d active=%d", len(app.tabs.tabs), app.tabs.active)
	}
	if !app.executeCommand("q") {
		t.Fatal(":q on the final Lab card did not quit the application")
	}

	other := App{Model: MockModel(), Lab: &lab.Lab{ID: "demo"}, State: ViewState{Focus: FocusGraph}}
	other.openDiskExplorer()
	if other.executeCommand("quit") {
		t.Fatal(":quit quit the application")
	}
	if len(other.tabs.tabs) != 1 {
		t.Fatalf(":quit left %d tabs", len(other.tabs.tabs))
	}
}

func TestCommandQAIsSilentQuitAllAlias(t *testing.T) {
	app := App{Model: MockModel(), Lab: &lab.Lab{ID: "demo"}, State: ViewState{Focus: FocusGraph, Message: "unchanged"}}
	app.openDiskExplorer()

	if !app.executeCommand("quit all") {
		t.Fatal(":quit all did not quit")
	}
	if !app.executeCommand("qa") {
		t.Fatal(":qa did not alias :quit all")
	}
	if app.State.Message != "unchanged" {
		t.Fatalf(":qa changed message to %q", app.State.Message)
	}
}

func TestPaletteTabCompletesQAToQuitAll(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph, PaletteOpen: true, PaletteQuery: "qa"}}

	app.handleKey("tab")

	if app.State.PaletteQuery != "quit all" {
		t.Fatalf("qa completed to %q, want quit all", app.State.PaletteQuery)
	}
}

func TestCommandQRejectsExtraArgs(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	if app.executeCommand("quit now") {
		t.Fatal(":quit with extra args quit unexpectedly")
	}
	if app.State.Message != "usage: quit [all]" {
		t.Fatalf("message = %q, want usage: quit [all]", app.State.Message)
	}
}

func TestCommandInputAcceptsSpaces(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	app.openCommand("")
	for _, key := range []string{"char:v", "char:m", "char: ", "char:s", "char:e", "char:t"} {
		app.handleKey(key)
	}

	if app.State.Command != "" || app.State.CommandMode {
		t.Fatalf("command input state = mode:%t command:%q, want disabled", app.State.CommandMode, app.State.Command)
	}
}

func TestGlobalCreateContextMenuIsRemoved(t *testing.T) {
	app := App{
		Model:      Model{ID: "empty"},
		Lab:        &lab.Lab{ID: "empty"},
		State:      ViewState{Focus: FocusGraph, ContextMenu: true},
		ViewWidth:  80,
		ViewHeight: 20,
	}

	if _, _, _, ok := app.currentContextMenuLayout(); ok {
		t.Fatal("global add context menu layout still exists")
	}
	app.handleKey("space")
	if app.State.ContextMenu {
		t.Fatalf("space opened global context menu: %#v", app.State)
	}
	if len(app.Lab.VMs) != 0 {
		t.Fatalf("global context path created vms: %#v", app.Lab.VMs)
	}
}

func topRootButtonForAction(t *testing.T, width int, action string) rect {
	t.Helper()
	items := topRibbonRootItems()
	rects := topMenuButtonRects(items, width)
	for i, item := range items {
		if i < len(rects) && contextMenuAction(item) == action {
			return rects[i]
		}
	}
	t.Fatalf("top root action %q not found in %#v", action, items)
	return rect{}
}

func topAddDropdownRowForAction(t *testing.T, app *App, action string) rect {
	t.Helper()
	menu, ok := app.topMenuDropdownLayout()
	if !ok {
		t.Fatal("top add dropdown layout unavailable")
	}
	actions := topRibbonAddActions()
	for i, candidate := range actions {
		if candidate == action {
			return rect{X: menu.rect.X, Y: menu.rect.Y + i, W: menu.rect.W, H: 1}
		}
	}
	t.Fatalf("top add action %q not found in %#v", action, actions)
	return rect{}
}

func TestMouseClickPaletteCreatesVM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{ID: "demo"}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  80,
		ViewHeight: 20,
	}

	app.handleKey("char::")
	app.State.PaletteQuery = "add"
	layout, ok := paletteLayout(app.ViewWidth, app.ViewHeight)
	if !ok {
		t.Fatal("palette layout unavailable")
	}
	app.handleKey("mouse:" + strconv.Itoa(layout.X+2) + ":" + strconv.Itoa(paletteRowsY(layout)) + ":0")
	if len(app.Lab.VMs) != 1 {
		t.Fatalf("vms after palette add = %#v", app.Lab.VMs)
	}
	if app.State.PaletteOpen {
		t.Fatal("palette stayed open after create")
	}
}

func TestPaletteQClosesOnlyActiveCard(t *testing.T) {
	app := App{
		Model:      Model{ID: "empty"},
		Lab:        &lab.Lab{ID: "empty"},
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  80,
		ViewHeight: 20,
	}
	app.openDiskExplorer()

	app.handleKey("char::")
	for _, key := range []string{"char:q"} {
		app.handleKey(key)
	}
	if app.handleKey("enter") {
		t.Fatal("palette q quit the application")
	}
	if len(app.tabs.tabs) != 1 || app.tabs.active != 0 {
		t.Fatalf("palette q left tabs=%d active=%d", len(app.tabs.tabs), app.tabs.active)
	}
}

func TestPaletteDisksOpensDiskExplorer(t *testing.T) {
	app := App{
		Model:      Model{ID: "demo"},
		Lab:        &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", Name: "router", MemoryMB: 512, CPUs: 1}}, Disks: []lab.Disk{{ID: "data", Path: "disks/data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base", AttachedType: "vm", AttachedTo: "vm1"}}},
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  100,
		ViewHeight: 30,
	}
	app.handleKey("char::")
	for _, key := range []string{"char:d", "char:i", "char:s", "char:k", "char:s"} {
		app.handleKey(key)
	}
	app.handleKey("enter")

	if !app.State.DiskExplorerOpen {
		t.Fatalf("disk explorer did not open: %#v", app.State)
	}
	out := RenderString(app.Model, app.diskExplorerRenderState(), 100, 30, false)
	for _, want := range []string{"DISK", "TYPE", "ATTACHED/BASE", "data", "base", "10G", "vm:router", "N create"} {
		if !strings.Contains(out, want) {
			t.Fatalf("disk explorer render missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "I info") {
		t.Fatalf("disk explorer still renders info action:\n%s", out)
	}
	if strings.Contains(out, "Disks: demo") || strings.Contains(out, "1 disk") {
		t.Fatalf("disk explorer still renders title/count header:\n%s", out)
	}
	message := app.State.Message
	console := append([]string(nil), app.State.Console...)
	app.handleKey("char:I")
	app.handleKey("enter")
	if app.State.Message != message || !reflect.DeepEqual(app.State.Console, console) {
		t.Fatalf("disk explorer info keys changed state: %#v", app.State)
	}
}

func TestDiskExplorerRenderUsesStructuredColumns(t *testing.T) {
	app := App{
		Model: Model{ID: "demo"},
		Lab: &lab.Lab{
			ID:         "demo",
			Containers: []lab.Container{{ID: "ct1", Name: "Kali", Image: "kali"}},
			Disks: []lab.Disk{{
				ID:           "kali",
				Path:         "/home/powerpenguini/.foxlab/labs/default/disks/kali.qcow2",
				SizeGB:       30,
				Format:       "qcow2",
				Kind:         "base",
				AttachedType: "container",
				AttachedTo:   "ct1",
			}},
		},
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}
	out := RenderString(app.Model, app.diskExplorerRenderState(), 100, 30, false)
	for _, want := range []string{"DISK", "TYPE", "SIZE", "FMT", "ATTACHED/BASE", "PATH", "kali", "base", "30G", "qcow2", "container:Kali"} {
		if !strings.Contains(out, want) {
			t.Fatalf("disk explorer table missing %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{"ID / kind / size / format", "kali  base  30G"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("disk explorer still uses flat row %q:\n%s", notWant, out)
		}
	}
}

func TestDiskExplorerRenderMarksSelectedRow(t *testing.T) {
	app := App{
		Model: Model{ID: "demo"},
		Lab: &lab.Lab{
			ID:    "demo",
			Disks: []lab.Disk{{ID: "data", Path: "disks/data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"}},
		},
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}
	out := RenderString(app.Model, app.diskExplorerRenderState(), 100, 30, false)
	if !strings.Contains(out, "> data") {
		t.Fatalf("disk explorer render missing content:\n%s", out)
	}
}

func TestDiskExplorerKeyboardResize(t *testing.T) {
	fakeQemuImg(t)
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "router", MemoryMB: 512, CPUs: 1, Disk: "disks/data.qcow2"}},
		Disks: []lab.Disk{{
			ID:           "data",
			Path:         "disks/data.qcow2",
			SizeGB:       10,
			Format:       "qcow2",
			Kind:         "base",
			AttachedType: "vm",
			AttachedTo:   "vm1",
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		runtimeAccess: testRuntimeAccess(&fakeVMRuntime{states: map[string]string{
			NodeKey(NodeVM, loaded.VMs[0].ID): "shutoff",
		}}),
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	app.handleKey("char:r")
	for _, key := range []string{"backspace", "backspace", "char:1", "char:2", "enter"} {
		app.handleKey(key)
	}

	if app.State.Message != "resized disk:data" {
		t.Fatalf("resize message = %q", app.State.Message)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Disks[0].SizeGB != 12 {
		t.Fatalf("sizeGB = %d, want 12", reloaded.Disks[0].SizeGB)
	}
}

func TestAttachedDiskResizePreservesConcreteRuntimeStatusError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", DesiredState: lab.DesiredStateStopped, MemoryMB: 512, CPUs: 1, Disk: "data.qcow2"}},
		Disks: []lab.Disk{{
			ID: "data", Path: "data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base", AttachedType: "vm", AttachedTo: "vm1",
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:         ModelFromLab(loaded),
		Lab:           loaded,
		LabPath:       path,
		runtimeAccess: testRuntimeAccess(&fakeVMRuntime{statesErr: errors.New("libvirt unavailable")}),
	}
	app.diskResize("data", map[string]string{"size": "12"})
	if app.State.Message != "runtime status failed: libvirt unavailable" {
		t.Fatalf("message = %q", app.State.Message)
	}
}

func TestDiskInfoKeepsQemuFailureOutOfMetadata(t *testing.T) {
	fakeQemuImgScript(t, "#!/bin/sh\necho qemu unavailable >&2\nexit 1\n")
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:    "demo",
		Disks: []lab.Disk{{ID: "data", Path: "disks/data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	app.diskInfo("data")

	if !strings.HasPrefix(app.State.Message, "disk info failed:") {
		t.Fatalf("info message = %q", app.State.Message)
	}
	got := strings.Join(app.State.Console, "\n")
	if !strings.Contains(got, "disk data") || !strings.Contains(got, "path:") {
		t.Fatalf("info console = %#v", app.State.Console)
	}
	if strings.Contains(got, "disk info failed:") || strings.Contains(got, "qemu unavailable") {
		t.Fatalf("disk metadata contains runtime failure: %#v", app.State.Console)
	}
}

func TestDiskExplorerCreatesLayer(t *testing.T) {
	fakeQemuImg(t)
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:    "demo",
		Disks: []lab.Disk{{ID: "data", Path: "disks/data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	app.handleKey("char:l")

	if app.State.Message != "created disk layer:data-layer" {
		t.Fatalf("message = %q", app.State.Message)
	}
	if app.State.DiskExplorerSelected != 1 {
		t.Fatalf("selected row = %d, want new layer row", app.State.DiskExplorerSelected)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 2 || reloaded.Disks[1].ID != "data-layer" || reloaded.Disks[1].Base != "data" || reloaded.Disks[1].AttachedTo != "" {
		t.Fatalf("disks after layer create = %#v", reloaded.Disks)
	}
	out := RenderString(app.Model, app.diskExplorerRenderState(), 100, 30, false)
	if !strings.Contains(out, "L layer") || !strings.Contains(out, "data-layer") {
		t.Fatalf("explorer render missing layer action/row:\n%s", out)
	}
}

func TestDiskExplorerRenamesBaseDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		Disks: []lab.Disk{
			{ID: "data", Path: "disks/data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"},
			{ID: "data-layer", Path: "layers/data-layer.qcow2", Format: "qcow2", Kind: "layer", Base: "data"},
		},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	app.handleKey("char:e")
	for range "data" {
		app.handleKey("backspace")
	}
	for _, key := range []string{"char:s", "char:y", "char:s", "char:t", "char:e", "char:m", "enter"} {
		app.handleKey(key)
	}

	if app.State.Message != "renamed disk:data to system" {
		t.Fatalf("message = %q", app.State.Message)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Disks[0].ID != "system" || reloaded.Disks[0].Path != "disks/data.qcow2" {
		t.Fatalf("renamed disk = %#v", reloaded.Disks[0])
	}
	if reloaded.Disks[1].Base != "system" {
		t.Fatalf("layer base = %q, want system", reloaded.Disks[1].Base)
	}
	out := RenderString(app.Model, app.diskExplorerRenderState(), 100, 30, false)
	if !strings.Contains(out, "system") || !strings.Contains(out, "base:system") {
		t.Fatalf("explorer render missing renamed disk/base reference:\n%s", out)
	}
}

func TestDiskExplorerCreateCancelsActiveRename(t *testing.T) {
	fakeQemuImg(t)
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:    "demo",
		Disks: []lab.Disk{{ID: "data", Path: "disks/data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	app.handleKey("char:E")
	if app.State.DiskExplorerEdit != diskExplorerActionRename {
		t.Fatalf("rename edit did not start: %#v", app.State)
	}
	app.runDiskExplorerAction(diskExplorerActionCreate)

	if app.State.DiskExplorerEdit != "" || app.State.DiskExplorerEditValue != "" {
		t.Fatalf("rename edit survived create: %#v", app.State)
	}
	if app.State.Message != "created disk:disk" {
		t.Fatalf("message = %q", app.State.Message)
	}
	out := RenderString(app.Model, app.diskExplorerRenderState(), 100, 30, false)
	if strings.Contains(out, "rename=") {
		t.Fatalf("explorer render still shows rename after create:\n%s", out)
	}
}

func TestDiskExplorerDeleteCancelsActiveRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lab")
	for _, name := range []string{"data.qcow2", "scratch.qcow2"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loaded := &lab.Lab{
		ID: "demo",
		Disks: []lab.Disk{
			{ID: "data", Path: "data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"},
			{ID: "scratch", Path: "scratch.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"},
		},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	app.handleKey("char:E")
	if app.State.DiskExplorerEdit != diskExplorerActionRename {
		t.Fatalf("rename edit did not start: %#v", app.State)
	}
	app.runDiskExplorerAction(diskExplorerActionDelete)

	if app.State.DiskExplorerEdit != "" || app.State.DiskExplorerEditValue != "" {
		t.Fatalf("rename edit survived delete: %#v", app.State)
	}
	if app.State.Message != "deleted disk:data" {
		t.Fatalf("message = %q", app.State.Message)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 || reloaded.Disks[0].ID != "scratch" {
		t.Fatalf("disks after delete = %#v", reloaded.Disks)
	}
	out := RenderString(app.Model, app.diskExplorerRenderState(), 100, 30, false)
	if strings.Contains(out, "rename=") {
		t.Fatalf("explorer render still shows rename after delete:\n%s", out)
	}
}

func TestDiskExplorerMouseLayerActionCreatesLayer(t *testing.T) {
	fakeQemuImg(t)
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:    "demo",
		Disks: []lab.Disk{{ID: "data", Path: "disks/data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}
	layout, ok := diskExplorerLayout(app.ViewWidth, app.ViewHeight)
	if !ok {
		t.Fatal("disk explorer layout unavailable")
	}
	layerX := layout.X + 1 + runeLen(" N create ") + 1
	layerY := layout.Y + layout.H - 1

	app.handleKey("mouse:" + strconv.Itoa(layerX) + ":" + strconv.Itoa(layerY) + ":0")

	if app.State.Message != "created disk layer:data-layer" {
		t.Fatalf("message = %q", app.State.Message)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 2 || reloaded.Disks[1].ID != "data-layer" {
		t.Fatalf("disks after mouse layer create = %#v", reloaded.Disks)
	}
}

func TestDiskExplorerMouseActionFeedbackOnlyCoversButton(t *testing.T) {
	app := App{
		Model:      Model{ID: "demo"},
		Lab:        &lab.Lab{ID: "demo", Disks: []lab.Disk{{ID: "data", Path: "disks/data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"}}},
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}
	layout, ok := diskExplorerLayout(app.ViewWidth, app.ViewHeight)
	if !ok {
		t.Fatal("disk explorer layout unavailable")
	}
	layerX := layout.X + 1 + runeLen(" N create ") + 1
	layerY := layout.Y + layout.H - 1

	r, ok := app.diskExplorerFeedbackRect(mouseEvent{x: layerX, y: layerY, button: 0})
	if !ok {
		t.Fatal("layer action feedback rect unavailable")
	}
	if r.W != runeLen(" L layer ") {
		t.Fatalf("feedback width = %d, want button width %d", r.W, runeLen(" L layer "))
	}
	if r.W >= layout.W-2 {
		t.Fatalf("feedback covers whole action bar: rect=%#v layout=%#v", r, layout)
	}
}

func TestDiskExplorerClickOutsidePanelKeepsCardOpen(t *testing.T) {
	app := App{
		Model:      Model{ID: "demo"},
		Lab:        &lab.Lab{ID: "demo"},
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	if app.handleKey("mouse:10:0:0") {
		t.Fatal("click outside panel quit while disk explorer was open")
	}
	if !app.State.DiskExplorerOpen {
		t.Fatal("click outside panel closed the disk card")
	}
}

func TestMouseClickDisabledApplyLabDoesNothing(t *testing.T) {
	app := App{
		Model:      Model{ID: "empty"},
		State:      ViewState{Focus: FocusGraph, ApplyLabDisabled: true},
		ViewWidth:  80,
		ViewHeight: 20,
	}
	app.handleKey("char::")
	for _, key := range []string{"char:a", "char:p", "char:p", "char:l", "char:y"} {
		app.handleKey(key)
	}
	app.handleKey("enter")

	if app.State.Message != "lab already applied" {
		t.Fatalf("disabled apply lab changed message to %q", app.State.Message)
	}
	if !app.State.PaletteOpen {
		t.Fatal("disabled apply lab closed palette")
	}
}

func TestApplyOpenLabDoesNotReloadSameActiveLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	absPath, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	controller := &fakeDaemonController{
		status: DaemonStatus{Active: true, LabPath: absPath},
	}
	app := App{
		LabPath:          path,
		DaemonController: controller,
	}

	app.applyOpenLab()

	if controller.applyCalls != 0 {
		t.Fatalf("Apply calls = %d, want 0", controller.applyCalls)
	}
	if !app.State.ApplyLabDisabled {
		t.Fatal("Apply Lab was not disabled for already active lab")
	}
	if !strings.Contains(app.State.Message, "already applied") {
		t.Fatalf("message = %q, want already applied", app.State.Message)
	}
}

func TestTabKeepsGraphFocusWithoutTopRibbon(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph, Selected: 0}}
	startSelected := app.State.Selected

	app.handleKey("tab")
	if app.State.Focus != FocusGraph {
		t.Fatalf("focus after tab = %d, want graph", app.State.Focus)
	}
	app.handleKey("right")
	if app.State.Selected == startSelected {
		t.Fatalf("graph focus right did not move graph selection from %d", startSelected)
	}
}

func TestTabClosesContextMenuAndTogglesFocus(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{
			Focus:              FocusGraph,
			Selected:           1,
			ContextMenu:        true,
			ContextGroup:       "disk-menu",
			ContextInSubmenu:   true,
			ContextSubSelected: 1,
			ContextEdit:        true,
			ContextEditValue:   "data",
			ContextEditCursor:  4,
			ContextDeleteDisk:  true,
			DiskMenuItems:      []string{"Add Disk", "base 10G"},
			DiskMenuActions:    []string{diskMenuActionCreate, diskMenuActionAttach},
			DiskMenuKinds:      []string{"", "base"},
		},
	}

	app.handleKey("tab")

	if app.State.Focus != FocusGraph {
		t.Fatalf("focus after tab = %d, want graph", app.State.Focus)
	}
	if app.State.ContextMenu || app.State.ContextInSubmenu || app.State.ContextGroup != "" || app.State.ContextEdit {
		t.Fatalf("context menu state after tab = %#v, want closed", app.State)
	}
	if app.State.ContextDeleteDisk || app.State.DiskMenuItems != nil || app.State.DiskMenuActions != nil || app.State.DiskMenuKinds != nil {
		t.Fatalf("context menu flags/cache after tab = %#v, want cleared", app.State)
	}
}

func TestMouseClickNodeMovesFocusToGraph(t *testing.T) {
	app := App{
		Model:      MockModel(),
		State:      ViewState{Focus: FocusTop, Selected: 0},
		ViewWidth:  120,
		ViewHeight: 30,
	}
	rects := layoutNodeRects(app.Model, app.graphBounds())
	nodeRect := rects[NodeKey(NodeVM, "client01")]

	app.handleKey("mouse:" + strconv.Itoa(nodeRect.X+1) + ":" + strconv.Itoa(nodeRect.Y+1) + ":0")

	if app.State.Focus != FocusGraph {
		t.Fatalf("focus after node click = %d, want graph", app.State.Focus)
	}
	if app.State.Selected != 1 {
		t.Fatalf("selected after node click = %d, want client01 index 1", app.State.Selected)
	}
	if app.State.ContextMenu {
		t.Fatal("node click opened the operations menu instead of only selecting the node")
	}
}

func TestMouseClickWorkspaceMovesFocusToGraph(t *testing.T) {
	app := App{
		Model:      MockModel(),
		State:      ViewState{Focus: FocusTop, TopMenuOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	app.handleKey("mouse:1:2:0")

	if app.State.Focus != FocusGraph {
		t.Fatalf("focus after workspace click = %d, want graph", app.State.Focus)
	}
	if app.State.TopMenuOpen {
		t.Fatal("workspace click did not close top menu")
	}
}

func TestMouseClickTopRowDoesNotMoveFocusToRibbon(t *testing.T) {
	app := App{
		Model:      MockModel(),
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	app.handleKey("mouse:70:0:0")

	if app.State.Focus != FocusGraph {
		t.Fatalf("focus after top row click = %d, want graph", app.State.Focus)
	}
}

func TestPaletteKeyboardAddAndExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{ID: "demo"}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}

	app.handleKey("char::")
	for _, key := range []string{"char:a", "char:d", "char:d", "char: ", "char:v", "char:m"} {
		app.handleKey(key)
	}
	app.handleKey("enter")
	if len(app.Lab.VMs) != 1 {
		t.Fatalf("vms after keyboard palette add = %#v", app.Lab.VMs)
	}

	app.handleKey("char::")
	for _, key := range []string{"char:q", "char:a"} {
		app.handleKey(key)
	}
	if !app.handleKey("enter") {
		t.Fatal("keyboard palette qa did not quit all")
	}
}

func TestPaletteEnterAcceptsFirstAddSuggestion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{ID: "demo"}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}

	app.handleKey("char::")
	for _, key := range []string{"char:a", "char:d", "char:d"} {
		app.handleKey(key)
	}
	app.handleKey("enter")

	if app.State.PaletteOpen {
		t.Fatal("accepted add suggestion kept palette open")
	}
	if len(app.Lab.VMs) != 1 {
		t.Fatalf("accepted first add suggestion mutated lab to %#v, want one VM", app.Lab)
	}
}

func TestPaletteTabCompletesSelectedSuggestion(t *testing.T) {
	app := App{
		Model: Model{ID: "demo"},
		Lab:   &lab.Lab{ID: "demo"},
		State: ViewState{Focus: FocusGraph},
	}

	app.handleKey("char::")
	for _, key := range []string{"char:a", "char:d"} {
		app.handleKey(key)
	}
	app.handleKey("tab")

	if !app.State.PaletteOpen {
		t.Fatal("tab completion closed palette")
	}
	if app.State.PaletteQuery != "add" {
		t.Fatalf("tab completed query to %q, want add", app.State.PaletteQuery)
	}
}

func TestPaletteEnterAcceptsFirstExecutableSuggestion(t *testing.T) {
	app := App{
		Model: Model{ID: "demo"},
		Lab:   &lab.Lab{ID: "demo"},
		State: ViewState{Focus: FocusGraph},
	}

	app.handleKey("char::")
	for _, key := range []string{"char:d", "char:i"} {
		app.handleKey(key)
	}
	app.handleKey("enter")

	if app.State.PaletteOpen {
		t.Fatal("accepted disk suggestion kept palette open")
	}
	if !app.State.DiskExplorerOpen {
		t.Fatal("accepted first executable suggestion did not open disk explorer")
	}
}

func TestColonOpensCommandPalette(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	app.handleKey("char::")

	if !app.State.PaletteOpen {
		t.Fatal("colon did not open command palette")
	}
}

func TestCtrlPDoesNotOpenCommandPalette(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	app.handleKey("ctrl+p")

	if app.State.PaletteOpen {
		t.Fatal("ctrl+p opened command palette")
	}
}

func TestPaletteExcludesSelectedNodeActions(t *testing.T) {
	app := App{
		Model:      MockModel(),
		State:      ViewState{Focus: FocusGraph, Selected: 0},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	app.handleKey("char::")
	for _, key := range []string{"char:c", "char:o", "char:n", "char:f"} {
		app.handleKey(key)
	}
	app.handleKey("enter")

	if !app.State.PaletteOpen {
		t.Fatal("unknown node-specific command closed palette")
	}
	if app.State.PaletteQuery != "conf" {
		t.Fatalf("palette query = %q, want conf", app.State.PaletteQuery)
	}
	if app.State.ContextMenu || app.State.ContextInSubmenu || app.State.ContextGroup != "" {
		t.Fatalf("node-specific context action opened from palette: %#v", app.State)
	}
	if actions := filteredPaletteActions(app.Model, app.State); len(actions) != 0 {
		t.Fatalf("palette actions for node-specific query = %#v, want none", actions)
	}
}

func TestTopFocusDownDoesNotActivateActions(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusTop}}

	if app.handleKey("down") {
		t.Fatal("down on top focus quit unexpectedly")
	}
	if app.State.Focus != FocusGraph {
		t.Fatalf("focus after down on top focus = %d, want graph", app.State.Focus)
	}
}

func TestPaletteAddUplinkCreatesExternalLink(t *testing.T) {
	fakeHostInterfaces(t, "br0", "eth0")
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
		VMs:      []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 1024, CPUs: 1, Networks: []lab.VMNetwork{{}}}},
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{Focus: FocusGraph, Selected: 0},
		ViewWidth:  80,
		ViewHeight: 20,
	}

	app.handleKey("char::")
	for _, key := range []string{"char:a", "char:d", "char:d", "char: ", "char:u", "char:p", "char:l", "char:i", "char:n", "char:k"} {
		app.handleKey(key)
	}
	app.handleKey("enter")
	if app.State.ConnectMode {
		t.Fatalf("link started connect mode: %#v", app.State)
	}
	if len(app.Lab.ExternalLinks) != 1 {
		t.Fatalf("external links after top link add = %#v", app.Lab.ExternalLinks)
	}
	if app.Lab.ExternalLinks[0].Interface != "eth0" {
		t.Fatalf("external interface = %q, want eth0", app.Lab.ExternalLinks[0].Interface)
	}
	if app.Lab.ExternalLinks[0].Mode != lab.ExternalModeNAT {
		t.Fatalf("external mode = %q, want nat", app.Lab.ExternalLinks[0].Mode)
	}
	if len(app.Lab.VMs[0].Networks) != 1 {
		t.Fatalf("source nics changed: %#v", app.Lab.VMs[0].Networks)
	}
}

func TestTopAddLinkDoesNotCreateMissingSourceNIC(t *testing.T) {
	fakeHostInterfaces(t, "br0", "eth0")
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
		VMs:      []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 1024, CPUs: 1}},
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{Focus: FocusGraph, Selected: 0},
		ViewWidth:  80,
		ViewHeight: 20,
	}

	app.runGlobalMenuAction("link")
	if app.State.ConnectMode {
		t.Fatalf("link started connect mode: %#v", app.State)
	}
	if len(app.Lab.ExternalLinks) != 1 {
		t.Fatalf("external links after global link add = %#v", app.Lab.ExternalLinks)
	}
	if app.Lab.ExternalLinks[0].Interface != "eth0" {
		t.Fatalf("external interface = %q, want eth0", app.Lab.ExternalLinks[0].Interface)
	}
	if app.Lab.ExternalLinks[0].Mode != lab.ExternalModeNAT {
		t.Fatalf("external mode = %q, want nat", app.Lab.ExternalLinks[0].Mode)
	}
	if len(app.Lab.VMs[0].Networks) != 0 {
		t.Fatalf("source nics = %#v, want none", app.Lab.VMs[0].Networks)
	}
}

func TestCommandRejectsIncrementSuffixVMArgs(t *testing.T) {
	app := App{
		Model: MockModel(),
		Lab: &lab.Lab{
			ID: "demo",
			VMs: []lab.VM{{
				ID:       "vm1",
				MemoryMB: 1024,
				CPUs:     2,
			}},
		},
		State: ViewState{Focus: FocusGraph},
	}

	app.executeCommand("vm set vm1 cpus+=1")
	if !strings.Contains(app.State.Message, "unsupported increment syntax") {
		t.Fatalf("vm set invalid args message = %q", app.State.Message)
	}

	app.executeCommand("add vm vm2 mem-=512")
	if !strings.Contains(app.State.Message, "unsupported increment syntax") {
		t.Fatalf("add vm invalid args message = %q", app.State.Message)
	}
}

func TestCommandRejectsDuplicateArgsBeforeMutating(t *testing.T) {
	app := App{
		Model: MockModel(),
		Lab: &lab.Lab{
			ID: "demo",
			VMs: []lab.VM{{
				ID:       "vm1",
				Name:     "original",
				MemoryMB: 1024,
				CPUs:     2,
			}},
		},
		State: ViewState{Focus: FocusGraph},
	}

	app.executeCommand("vm set vm1 name=first name=second")
	if app.State.Message != "duplicate argument: name" {
		t.Fatalf("duplicate arg message = %q", app.State.Message)
	}
	if app.Lab.VMs[0].Name != "original" {
		t.Fatalf("duplicate arg mutated vm name to %q", app.Lab.VMs[0].Name)
	}
}

func TestReadKeyMapsHjklByMode(t *testing.T) {
	tests := []struct {
		mode bool
		in   byte
		want string
	}{
		{true, ' ', "char: "},
		{true, 'h', "char:h"},
		{true, 'j', "char:j"},
		{true, 'k', "char:k"},
		{true, 'l', "char:l"},
		{false, ' ', "space"},
		{false, 'h', "left"},
		{false, 'j', "down"},
		{false, 'k', "up"},
		{false, 'l', "right"},
	}

	for _, tc := range tests {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte{tc.in}); err != nil {
			_ = w.Close()
			_ = r.Close()
			t.Fatal(err)
		}
		_ = w.Close()
		got, err := readKey(int(r.Fd()), tc.mode)
		_ = r.Close()
		if err != nil {
			t.Fatalf("readKey mode=%v in=%q err=%v", tc.mode, tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("readKey mode=%v in=%q = %q, want %q", tc.mode, tc.in, got, tc.want)
		}
	}
}

func TestReadKeyKeepsArrowsAsNavigationInTextMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"\x1b[D", "left"},
		{"\x1b[C", "right"},
		{"\x1b[H", "home"},
		{"\x1b[F", "end"},
		{"\x1b[3~", "delete"},
	}

	for _, tc := range tests {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(tc.in)); err != nil {
			_ = w.Close()
			_ = r.Close()
			t.Fatal(err)
		}
		_ = w.Close()
		got, err := readKey(int(r.Fd()), true)
		_ = r.Close()
		if err != nil {
			t.Fatalf("readKey in=%q err=%v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("readKey in=%q = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDecodeKeysCtrlP(t *testing.T) {
	got := decodeKeys("\x10", false)
	want := []string{"ctrl+p"}
	assertKeys(t, got, want)
}

func TestDecodeKeysExpandsBracketedPasteInTextMode(t *testing.T) {
	got := decodeKeys(bracketedPasteStart+"hj kl"+bracketedPasteEnd, true)
	want := []string{"char:h", "char:j", "char: ", "char:k", "char:l"}
	assertKeys(t, got, want)
}

func TestDecodeKeysMouseClick(t *testing.T) {
	got := decodeKeys("\x1b[<0;12;5M", false)
	want := []string{"mouse:11:4:0"}
	assertKeys(t, got, want)
}

func TestDecodeKeysMouseDragAndRelease(t *testing.T) {
	got := decodeKeys("\x1b[<32;15;8M\x1b[<0;15;8m", false)
	want := []string{"mouse-drag:14:7:0", "mouse-release:14:7:0"}
	assertKeys(t, got, want)
}

func TestDecodeKeysShiftArrows(t *testing.T) {
	got := decodeKeys("\x1b[1;2A\x1b[1;2B\x1b[1;2C\x1b[1;2D", false)
	want := []string{"shift-up", "shift-down", "shift-right", "shift-left"}
	assertKeys(t, got, want)
}

func TestReadAppKeyQueuesPastedText(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("vm set")); err != nil {
		_ = w.Close()
		_ = r.Close()
		t.Fatal(err)
	}
	_ = w.Close()
	defer r.Close()

	app := App{In: r, State: ViewState{ContextEdit: true}}
	want := []string{"char:v", "char:m", "char: ", "char:s", "char:e", "char:t"}
	for _, expected := range want {
		got, err := readAppKey(&app)
		if err != nil {
			t.Fatalf("readAppKey err=%v", err)
		}
		if got != expected {
			t.Fatalf("readAppKey = %q, want %q", got, expected)
		}
	}
	got, err := readAppKey(&app)
	if !errors.Is(err, io.EOF) || got != "" {
		t.Fatalf("readAppKey after queue = %q err=%v, want EOF", got, err)
	}
}

func TestWaitReadableRejectsClosedDescriptor(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	fd := int(r.Fd())
	_ = r.Close()
	_ = w.Close()
	if ok, err := waitReadable(fd, time.Millisecond); ok || !errors.Is(err, unix.EBADF) {
		t.Fatalf("closed descriptor readable=%v err=%v", ok, err)
	}
}

func TestReadAppKeyMapsQueuedTextUsingCurrentUIState(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(":hjkl ")); err != nil {
		_ = w.Close()
		_ = r.Close()
		t.Fatal(err)
	}
	_ = w.Close()
	defer r.Close()

	app := App{In: r}
	got, err := readAppKey(&app)
	if err != nil || got != "char::" {
		t.Fatalf("palette opener key=%q err=%v", got, err)
	}
	app.State.PaletteOpen = true
	for _, want := range []string{"char:h", "char:j", "char:k", "char:l", "char: "} {
		got, err = readAppKey(&app)
		if err != nil || got != want {
			t.Fatalf("queued palette key=%q err=%v, want %q", got, err, want)
		}
	}
}

func TestDecodeEscapeFollowedByTextKeepsBothEvents(t *testing.T) {
	events := decodeKeyEvents("\x1bh", true)
	if len(events) != 2 || events[0].key != "escape" || events[1].key != "char:h" {
		t.Fatalf("escape plus text events = %#v", events)
	}
	if got := append(append([]byte(nil), events[0].raw...), events[1].raw...); !bytes.Equal(got, []byte("\x1bh")) {
		t.Fatalf("escape plus text raw = %q", got)
	}
}

func TestReadAppKeyKeepsActionCharsInDiskExplorer(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("lI")); err != nil {
		_ = w.Close()
		_ = r.Close()
		t.Fatal(err)
	}
	_ = w.Close()
	defer r.Close()

	app := App{In: r, State: ViewState{DiskExplorerOpen: true}}
	for _, want := range []string{"char:l", "char:I"} {
		got, err := readAppKey(&app)
		if err != nil {
			t.Fatalf("readAppKey err=%v", err)
		}
		if got != want {
			t.Fatalf("readAppKey = %q, want %s", got, want)
		}
	}
}

func TestReadAppKeyTimesOutForAnimationTick(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	app := App{In: r}
	got, err := readAppKey(&app)
	if err != nil {
		t.Fatalf("readAppKey err=%v", err)
	}
	if got != "" {
		t.Fatalf("readAppKey = %q, want empty timeout key", got)
	}
}

func assertKeys(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("keys = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %#v, want %#v", got, want)
		}
	}
}
