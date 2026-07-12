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
	"sync"
	"testing"
	"time"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

type fakeConsole struct {
	bytes.Buffer
	closed bool
}

func (f *fakeConsole) Close() error {
	f.closed = true
	return nil
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) {
	return 0, io.EOF
}

func fakeQemuImg(t *testing.T) func() {
	t.Helper()
	return fakeQemuImgScript(t, "#!/bin/sh\nexit 0\n")
}

func fakeQemuImgScript(t *testing.T, script string) func() {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "qemu-img")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	return func() {}
}

func fakeHostInterfaces(t *testing.T, names ...string) {
	t.Helper()
	previous := hostInterfaceNames
	hostInterfaceNames = func() []string {
		return append([]string{}, names...)
	}
	t.Cleanup(func() {
		hostInterfaceNames = previous
	})
}

type closedPipeReader struct{}

func (closedPipeReader) Read([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

type fakeVMRuntime struct {
	mu        sync.Mutex
	states    map[string]string
	vncPorts  map[string]int
	statesErr error
	started   string
	stopped   string
	starts    int
	stops     int
}

type fakeDaemonController struct {
	status     DaemonStatus
	statusErr  error
	applyErr   error
	applyCalls int
	lastApply  DaemonApplyRequest
}

func (f *fakeDaemonController) Status(context.Context) (DaemonStatus, error) {
	return f.status, f.statusErr
}

func (f *fakeDaemonController) Apply(_ context.Context, req DaemonApplyRequest) error {
	f.applyCalls++
	f.lastApply = req
	return f.applyErr
}

func (f *fakeVMRuntime) States(context.Context, *lab.Lab) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statesErr != nil {
		return nil, f.statesErr
	}
	return copyStringMap(f.states), nil
}

func (f *fakeVMRuntime) VNCPorts(context.Context, *lab.Lab) (map[string]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return copyIntMap(f.vncPorts), nil
}

func (f *fakeVMRuntime) Start(_ context.Context, _ *lab.Lab, ref workload.Ref) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	f.started = workload.Key(ref)
	if f.states == nil {
		f.states = map[string]string{}
	}
	f.states[workload.Key(ref)] = "running"
	return nil
}

func (f *fakeVMRuntime) Stop(_ context.Context, _ *lab.Lab, ref workload.Ref) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.stopped = workload.Key(ref)
	if f.states == nil {
		f.states = map[string]string{}
	}
	f.states[workload.Key(ref)] = "shutoff"
	return nil
}

func (f *fakeVMRuntime) Close() error { return nil }

func (f *fakeVMRuntime) setState(key, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.states == nil {
		f.states = map[string]string{}
	}
	f.states[key] = state
}

type blockingStatusRuntime struct {
	entered         chan struct{}
	release         chan struct{}
	seenLab         *lab.Lab
	seenContainerID string
}

func (r *blockingStatusRuntime) States(ctx context.Context, l *lab.Lab) (map[string]string, error) {
	r.seenLab = l
	close(r.entered)
	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if len(l.Containers) > 0 {
		r.seenContainerID = l.Containers[0].ID
	}
	return map[string]string{}, nil
}

func (r *blockingStatusRuntime) Start(context.Context, *lab.Lab, workload.Ref) error {
	return nil
}

func (r *blockingStatusRuntime) Stop(context.Context, *lab.Lab, workload.Ref) error {
	return nil
}

func (r *blockingStatusRuntime) Close() error {
	return nil
}

type serialRuntime struct {
	mu        sync.Mutex
	states    int
	entered   chan struct{}
	release   chan struct{}
	enterOnce sync.Once
}

func (r *serialRuntime) States(ctx context.Context, _ *lab.Lab) (map[string]string, error) {
	r.mu.Lock()
	r.states++
	first := r.states == 1
	r.mu.Unlock()
	if first {
		r.enterOnce.Do(func() { close(r.entered) })
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return map[string]string{NodeKey(NodeVM, "vm1"): "running"}, nil
}

func (r *serialRuntime) Start(context.Context, *lab.Lab, workload.Ref) error {
	return nil
}

func (r *serialRuntime) Stop(context.Context, *lab.Lab, workload.Ref) error {
	return nil
}

func (r *serialRuntime) Close() error {
	return nil
}

type directMapRuntime struct {
	states   map[string]string
	vncPorts map[string]int
}

func (r *directMapRuntime) States(context.Context, *lab.Lab) (map[string]string, error) {
	return r.states, nil
}

func (r *directMapRuntime) VNCPorts(context.Context, *lab.Lab) (map[string]int, error) {
	return r.vncPorts, nil
}

func (r *directMapRuntime) Start(context.Context, *lab.Lab, workload.Ref) error {
	return nil
}

func (r *directMapRuntime) Stop(context.Context, *lab.Lab, workload.Ref) error {
	return nil
}

func (r *directMapRuntime) Close() error {
	return nil
}

type deadlineRuntime struct {
	statesCtx context.Context
	vncCtx    context.Context
}

func (r *deadlineRuntime) States(ctx context.Context, _ *lab.Lab) (map[string]string, error) {
	r.statesCtx = ctx
	return map[string]string{NodeKey(NodeVM, "vm1"): "running"}, nil
}

func (r *deadlineRuntime) VNCPorts(ctx context.Context, _ *lab.Lab) (map[string]int, error) {
	r.vncCtx = ctx
	return map[string]int{NodeKey(NodeVM, "vm1"): 5908}, nil
}

func (r *deadlineRuntime) Start(context.Context, *lab.Lab, workload.Ref) error {
	return nil
}

func (r *deadlineRuntime) Stop(context.Context, *lab.Lab, workload.Ref) error {
	return nil
}

func (r *deadlineRuntime) Close() error {
	return nil
}

func copyStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func copyIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func TestHandleKeyContextMenuFlow(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{Focus: FocusGraph, Selected: 1},
	}

	app.handleKey("space")
	if !app.State.ContextMenu {
		t.Fatal("space did not open context menu")
	}
	app.handleKey("enter")
	if app.State.ContextGroup != "config-menu" {
		t.Fatalf("context group = %q, want config-menu", app.State.ContextGroup)
	}
	if !app.State.ContextInSubmenu {
		t.Fatalf("expected submenu column to be focused")
	}

	node, _ := selectedNode(app.Model, app.State.Selected)
	items := contextMenuItems(node, app.State.ContextGroup)
	for i, item := range items {
		if contextItemKey(item) == "name" {
			app.State.ContextSubSelected = i
			break
		}
	}
	app.handleKey("enter")
	if !app.State.ContextEdit {
		t.Fatal("enter on config value did not start inline edit")
	}
	if app.State.ContextEditValue != "client01" {
		t.Fatalf("inline edit value = %q, want client01", app.State.ContextEditValue)
	}
}

func TestContextMenuInlineEditQuestionFallbackStartsEmpty(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{Focus: FocusGraph, Selected: 2},
	}

	app.handleKey("space")
	app.handleKey("enter")
	node, _ := selectedNode(app.Model, app.State.Selected)
	items := contextMenuItems(node, app.State.ContextGroup)
	for i, item := range items {
		if contextItemKey(item) == "command" {
			app.State.ContextSubSelected = i
			break
		}
	}
	app.handleKey("enter")

	if !app.State.ContextEdit {
		t.Fatal("enter on command value did not start inline edit")
	}
	if app.State.ContextEditValue != "" {
		t.Fatalf("inline edit value = %q, want empty", app.State.ContextEditValue)
	}
}

func TestExternalInterfaceFieldOpensChoiceMenu(t *testing.T) {
	fakeHostInterfaces(t, "br0", "eth0")
	loaded := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0"}},
	}
	app := App{
		Model: ModelFromLab(loaded),
		Lab:   loaded,
		State: ViewState{
			Focus:            FocusGraph,
			Selected:         0,
			ContextMenu:      true,
			ContextGroup:     "config-menu",
			ContextInSubmenu: true,
		},
	}
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok {
		t.Fatal("selected node not found")
	}
	app.State.ContextSubSelected = externalInterfaceFieldIndex(node)

	app.handleKey("enter")

	if app.State.ContextGroup != "config-menu" || app.State.ContextSelectGroup != "interface-menu" || !app.State.ContextInSubmenu {
		t.Fatalf("context group = %q select=%q submenu=%t, want config-menu/interface-menu submenu", app.State.ContextGroup, app.State.ContextSelectGroup, app.State.ContextInSubmenu)
	}
	if app.State.ContextEdit {
		t.Fatal("interface choice opened inline edit")
	}
	items := app.contextMenuSelectItems(node, true)
	if !reflect.DeepEqual(items, []string{"br0", "eth0"}) {
		t.Fatalf("interface items = %#v", items)
	}
}

func TestExternalInterfaceChoiceAppliesSelectedInterface(t *testing.T) {
	fakeHostInterfaces(t, "br0", "eth0")
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0"}},
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
		State: ViewState{
			Focus:                 FocusGraph,
			Selected:              0,
			ContextMenu:           true,
			ContextGroup:          "config-menu",
			ContextInSubmenu:      true,
			ContextSubSelected:    externalInterfaceFieldIndex(ModelFromLab(loaded).Nodes[0]),
			ContextSelectGroup:    "interface-menu",
			ContextSelectSelected: 0,
		},
	}

	app.handleKey("enter")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ExternalLinks) != 1 || reloaded.ExternalLinks[0].Interface != "br0" {
		t.Fatalf("external links = %#v, want interface br0", reloaded.ExternalLinks)
	}
	if app.State.ContextMenu {
		t.Fatal("interface choice did not close context menu")
	}
}

func TestExternalModeFieldOpensThirdMenuAndAppliesChoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0", Mode: lab.ExternalModeNAT}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	model := ModelFromLab(loaded)
	app := App{
		Model:   model,
		Lab:     loaded,
		LabPath: path,
		State: ViewState{
			Focus:              FocusGraph,
			Selected:           0,
			ContextMenu:        true,
			ContextGroup:       "config-menu",
			ContextInSubmenu:   true,
			ContextSubSelected: externalModeFieldIndex(model.Nodes[0]),
		},
	}

	app.handleKey("enter")

	if app.State.ContextGroup != "config-menu" || app.State.ContextSelectGroup != "mode-menu" {
		t.Fatalf("context group = %q select=%q, want config-menu/mode-menu", app.State.ContextGroup, app.State.ContextSelectGroup)
	}

	app.handleKey("down")
	app.handleKey("enter")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ExternalLinks) != 1 || reloaded.ExternalLinks[0].Mode != lab.ExternalModeDirect {
		t.Fatalf("external links = %#v, want direct mode", reloaded.ExternalLinks)
	}
	if app.State.ContextMenu {
		t.Fatal("mode choice did not close context menu")
	}
}

func TestSwitchModeFieldOpensThirdMenuAndAppliesChoice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	model := ModelFromLab(loaded)
	app := App{
		Model:   model,
		Lab:     loaded,
		LabPath: path,
		State: ViewState{
			Focus:              FocusGraph,
			Selected:           0,
			ContextMenu:        true,
			ContextGroup:       "config-menu",
			ContextInSubmenu:   true,
			ContextSubSelected: switchModeFieldIndex(model.Nodes[0]),
		},
	}

	app.handleKey("enter")

	if app.State.ContextGroup != "config-menu" || app.State.ContextSelectGroup != "mode-menu" {
		t.Fatalf("context group = %q select=%q, want config-menu/mode-menu", app.State.ContextGroup, app.State.ContextSelectGroup)
	}

	app.handleKey("down")
	app.handleKey("enter")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Switches) != 1 || reloaded.Switches[0].Mode != "nat" {
		t.Fatalf("switches = %#v, want nat mode", reloaded.Switches)
	}
	if app.State.ContextMenu {
		t.Fatal("mode choice did not close context menu")
	}
}

func TestContextMenuGroupRequiresRootConfirmation(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{Focus: FocusGraph, Selected: 1},
	}

	app.handleKey("space")
	app.handleKey("down")
	app.handleKey("down")
	if app.State.ContextGroup != "" || app.State.ContextInSubmenu {
		t.Fatalf("context group after hovering disk = %q submenu=%t, want closed", app.State.ContextGroup, app.State.ContextInSubmenu)
	}

	app.handleKey("down")
	if app.State.ContextGroup != "" {
		t.Fatalf("context group after moving to action = %q, want empty", app.State.ContextGroup)
	}

	app.handleKey("right")
	if app.State.ContextInSubmenu {
		t.Fatal("right focused submenu for root action")
	}
	if app.State.ContextGroup != "" {
		t.Fatalf("context group after right = %q, want empty", app.State.ContextGroup)
	}
}

func TestContextMenuRowChangeClearsInlineActionState(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{
			Focus:               FocusGraph,
			Selected:            1,
			ContextMenu:         true,
			ContextGroup:        "disk-menu",
			ContextInSubmenu:    true,
			ContextSubSelected:  1,
			ContextDeleteNIC:    true,
			ContextDeleteUplink: true,
			ContextAddDiskLayer: true,
			ContextMergeDisk:    true,
			ContextDetachDisk:   true,
			ContextDeleteDisk:   true,
			DiskMenuItems:       []string{"Add Disk", "base 10G", diskMenuLayerTreePrefix + "base-layer"},
			DiskMenuActions:     []string{diskMenuActionCreate, diskMenuActionAttach, diskMenuActionNone},
			DiskMenuKinds:       []string{"", "base", "layer"},
		},
	}

	app.handleKey("down")

	if app.State.ContextDeleteNIC || app.State.ContextDeleteUplink || app.State.ContextAddDiskLayer || app.State.ContextMergeDisk || app.State.ContextDetachDisk || app.State.ContextDeleteDisk {
		t.Fatalf("row action flags were not cleared: %#v", app.State)
	}
}

func TestContextMenuClickOutsideClearsMenuState(t *testing.T) {
	app := App{
		Model:      MockModel(),
		ViewWidth:  100,
		ViewHeight: 30,
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

	app.handleContextMenuMouse(mouseEvent{x: 0, y: 29, button: 0})

	if app.State.ContextMenu || app.State.ContextInSubmenu || app.State.ContextGroup != "" || app.State.ContextEdit {
		t.Fatalf("context state was not closed: %#v", app.State)
	}
	if app.State.DiskMenuItems != nil || app.State.DiskMenuActions != nil || app.State.DiskMenuKinds != nil {
		t.Fatalf("disk menu cache was not cleared: %#v", app.State)
	}
}

func TestContextMenuMouseSwitchesInlineEditFieldsWithoutCopyingValue(t *testing.T) {
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			Name:     "vm1",
			MemoryMB: 2048,
			CPUs:     2,
		}},
	}
	model := ModelFromLab(loaded)
	node := model.Nodes[0]
	items := contextMenuItems(node, "config-menu")
	nameIndex := -1
	cpuIndex := -1
	for i, item := range items {
		switch contextItemKey(item) {
		case "name":
			nameIndex = i
		case "cpu":
			cpuIndex = i
		}
	}
	if nameIndex < 0 || cpuIndex < 0 {
		t.Fatalf("config menu missing name/cpu items: %#v", items)
	}
	app := App{
		Model:      model,
		Lab:        loaded,
		ViewWidth:  100,
		ViewHeight: 30,
		State: ViewState{
			Focus:              FocusGraph,
			Selected:           0,
			ContextMenu:        true,
			ContextGroup:       "config-menu",
			ContextInSubmenu:   true,
			ContextSelected:    0,
			ContextSubSelected: nameIndex,
			ContextEdit:        true,
			ContextEditValue:   "copied-name",
			ContextEditCursor:  len("copied-name"),
		},
	}
	layout, _, _, ok := app.currentContextMenuLayout()
	if !ok || !layout.hasSub {
		t.Fatal("missing context submenu layout")
	}

	app.handleContextMenuMouse(mouseEvent{
		x:      layout.sub.rect.X + 2,
		y:      layout.sub.rect.Y + cpuIndex,
		button: 0,
	})

	if !app.State.ContextEdit {
		t.Fatal("clicking CPU field did not start inline edit")
	}
	if app.State.ContextSubSelected != cpuIndex {
		t.Fatalf("selected row = %d, want cpu row %d", app.State.ContextSubSelected, cpuIndex)
	}
	if app.State.ContextEditValue != "2" {
		t.Fatalf("edit value = %q, want CPU value 2", app.State.ContextEditValue)
	}
	if app.Lab.VMs[0].Name != "vm1" || app.Lab.VMs[0].CPUs != 2 {
		t.Fatalf("old edit value was applied to lab: %#v", app.Lab.VMs[0])
	}
}

func TestContextMenuIncludesConsoleForVMsAndShellForContainers(t *testing.T) {
	foundVM := false
	foundVNC := false
	for _, item := range contextMenuItems(Node{ID: "vm1", Type: NodeVM}, "") {
		if item == "Console" {
			foundVM = true
		}
		if item == "VNC" {
			foundVNC = true
		}
	}
	if !foundVM {
		t.Fatalf("vm context menu missing Console: %#v", contextMenuItems(Node{ID: "vm1", Type: NodeVM}, ""))
	}
	if !foundVNC {
		t.Fatalf("vm context menu missing VNC: %#v", contextMenuItems(Node{ID: "vm1", Type: NodeVM}, ""))
	}
	if action := contextMenuAction("Console"); action != "shell" {
		t.Fatalf("Console action = %q, want shell", action)
	}
	found := false
	foundContainerVNC := false
	for _, item := range contextMenuItems(Node{ID: "web", Type: NodeContainer}, "") {
		if item == "Shell" {
			found = true
		}
		if item == "VNC" {
			foundContainerVNC = true
		}
	}
	if !found {
		t.Fatalf("container context menu missing Shell: %#v", contextMenuItems(Node{ID: "web", Type: NodeContainer}, ""))
	}
	if foundContainerVNC {
		t.Fatalf("container context menu unexpectedly contains VNC: %#v", contextMenuItems(Node{ID: "web", Type: NodeContainer}, ""))
	}
}

func TestContextMenuVNCInfoIsNoOp(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{
			Focus:            FocusGraph,
			Selected:         0,
			ContextMenu:      true,
			ContextGroup:     "config-menu",
			ContextInSubmenu: true,
		},
	}
	node, _ := selectedNode(app.Model, app.State.Selected)
	items := contextMenuItems(node, app.State.ContextGroup)
	for i, item := range items {
		if isContextInfoItem(item) {
			app.State.ContextSubSelected = i
			break
		}
	}
	if !isContextInfoItem(items[app.State.ContextSubSelected]) {
		t.Fatalf("enabled VNC info item missing: %#v", items)
	}

	app.handleKey("enter")
	if !app.State.ContextMenu || !app.State.ContextInSubmenu {
		t.Fatalf("VNC info closed menu: %#v", app.State)
	}
	if app.State.ContextEdit {
		t.Fatal("VNC info opened inline edit")
	}
}

func TestRefreshWorkloadStatesAddsRuntimeVNCPort(t *testing.T) {
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, VNC: true}},
	}
	app := App{
		Model: ModelFromLab(loaded),
		Lab:   loaded,
		Runtime: &fakeVMRuntime{
			states:   map[string]string{NodeKey(NodeVM, "vm1"): "running"},
			vncPorts: map[string]int{NodeKey(NodeVM, "vm1"): 5903},
		},
	}

	app.refreshWorkloadStates()
	if got := strings.Join(app.Model.Nodes[0].Details, "\n"); !strings.Contains(got, "vnc-port=5903") {
		t.Fatalf("model details missing runtime VNC port: %#v", app.Model.Nodes[0].Details)
	}
	items := contextMenuItems(app.Model.Nodes[0], "config-menu")
	if got := strings.Join(items, "\n"); !strings.Contains(got, "VNC: 127.0.0.1:5903") {
		t.Fatalf("config menu missing runtime VNC port: %#v", items)
	}
}

func TestRefreshWorkloadStatesCopiesRuntimeMaps(t *testing.T) {
	stateKey := NodeKey(NodeVM, "vm1")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, VNC: true}},
	}
	runtime := &directMapRuntime{
		states:   map[string]string{stateKey: "running"},
		vncPorts: map[string]int{stateKey: 5903},
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		Runtime: runtime,
	}

	app.refreshWorkloadStates()
	runtime.states[stateKey] = "shutoff"
	runtime.vncPorts[stateKey] = 5999

	if app.WorkloadStates[stateKey] != "running" {
		t.Fatalf("app workload state = %q, want copied running state", app.WorkloadStates[stateKey])
	}
	if app.Service.States[stateKey] != "running" {
		t.Fatalf("service workload state = %q, want copied running state", app.Service.States[stateKey])
	}
	if app.VNCPorts[stateKey] != 5903 {
		t.Fatalf("app VNC port = %d, want copied 5903", app.VNCPorts[stateKey])
	}
}

func TestRefreshWorkloadStatesShowsActualStateWithoutAppliedLab(t *testing.T) {
	loaded := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "kali", Image: "docker.io/kalilinux/kali-rolling:latest", DesiredState: lab.DesiredStateRunning}},
	}
	app := App{
		Model: ModelFromLab(loaded),
		Lab:   loaded,
		Runtime: &fakeVMRuntime{
			states: map[string]string{NodeKey(NodeContainer, "kali"): "missing"},
		},
	}

	app.refreshWorkloadStates()
	node, ok := nodeByKey(app.Model, NodeKey(NodeContainer, "kali"))
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "missing" {
		t.Fatalf("container state = %q, want missing", node.State)
	}
}

func TestRefreshWorkloadStatesShowsMissingForAppliedDesiredRunningMissingContainer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "kali", Image: "docker.io/kalilinux/kali-rolling:latest", DesiredState: lab.DesiredStateRunning}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:            ModelFromLab(loaded),
		Lab:              loaded,
		LabPath:          path,
		State:            ViewState{ApplyLabDisabled: true},
		DaemonController: &fakeDaemonController{status: DaemonStatus{Active: true, LabPath: path}},
		Runtime: &fakeVMRuntime{
			states: map[string]string{NodeKey(NodeContainer, "kali"): "missing"},
		},
	}

	app.refreshWorkloadStates()
	node, ok := nodeByKey(app.Model, NodeKey(NodeContainer, "kali"))
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "missing" {
		t.Fatalf("container state = %q, want missing", node.State)
	}
}

func TestRefreshWorkloadStatesUsesFoxlabdSnapshot(t *testing.T) {
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
	snapshot := daemonstatus.Snapshot{
		LabPath: path,
		LabName: "demo",
		States:  map[string]string{NodeKey(NodeContainer, "web"): "running"},
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Message: "foxlabd status: runtime unavailable"},
		StatusQuery: func(context.Context, string) (daemonstatus.Snapshot, error) {
			return snapshot, nil
		},
		Runtime: &fakeVMRuntime{
			states: map[string]string{NodeKey(NodeContainer, "web"): "missing"},
		},
	}

	app.refreshWorkloadStates()

	node, ok := nodeByKey(app.Model, NodeKey(NodeContainer, "web"))
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "running" {
		t.Fatalf("container state = %q, want daemon running", node.State)
	}
	if app.State.Message != "" {
		t.Fatalf("message = %q, want recovered daemon status cleared", app.State.Message)
	}
}

func TestRefreshWorkloadStatesShowsStoppedMissingVMAsDefined(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:           "vm1",
			Name:         "victim",
			DesiredState: lab.DesiredStateStopped,
			MemoryMB:     512,
			CPUs:         1,
			Disk:         "disks/vm1.qcow2",
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
		StatusQuery: func(context.Context, string) (daemonstatus.Snapshot, error) {
			return daemonstatus.Snapshot{
				LabPath: path,
				LabName: "demo",
				States:  map[string]string{NodeKey(NodeVM, loaded.VMs[0].ID): "missing"},
			}, nil
		},
	}

	app.refreshWorkloadStates()

	node, ok := nodeByKey(app.Model, NodeKey(NodeVM, loaded.VMs[0].ID))
	if !ok {
		t.Fatal("vm node not found")
	}
	if node.State != "defined" {
		t.Fatalf("vm state = %q, want defined for stopped missing VM", node.State)
	}
}

func TestRefreshWorkloadStatesQueriesAppliedDaemonSocketPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")
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
	var queriedSocket string
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		StatusQuery: func(_ context.Context, socket string) (daemonstatus.Snapshot, error) {
			queriedSocket = socket
			return daemonstatus.Snapshot{
				LabPath: path,
				LabName: "demo",
				States:  map[string]string{NodeKey(NodeContainer, "web"): "running"},
			}, nil
		},
		Runtime: &fakeVMRuntime{
			states: map[string]string{NodeKey(NodeContainer, "web"): "missing"},
		},
	}

	app.refreshWorkloadStates()

	want := filepath.Join(home, ".foxlab", "run", "foxlabd.sock")
	if queriedSocket != want {
		t.Fatalf("queried status socket = %q, want %q", queriedSocket, want)
	}
	node, ok := nodeByKey(app.Model, NodeKey(NodeContainer, "web"))
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "running" {
		t.Fatalf("container state = %q, want daemon running", node.State)
	}
}

func TestDaemonRestartActionIsShownWithDisplayName(t *testing.T) {
	l := &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm-id", Name: "router", MemoryMB: 512, CPUs: 1}}}
	app := App{Lab: l}
	update := app.statusUpdateFromDaemonSnapshot(l, daemonstatus.Snapshot{
		States:  map[string]string{NodeKey(NodeVM, "vm-id"): "running"},
		Actions: []string{"restarted vm:vm-id for configuration change"},
	})
	if update.message != "foxlabd: restarted vm:router for configuration change" {
		t.Fatalf("message = %q", update.message)
	}
	if !update.statesConfirmed {
		t.Fatal("successful daemon snapshot did not confirm states")
	}
}

func TestRefreshWorkloadStatesFallsBackWhenFoxlabdSnapshotIsForAnotherLab(t *testing.T) {
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
	snapshot := daemonstatus.Snapshot{
		LabPath: filepath.Join(t.TempDir(), "other.lab"),
		LabName: "other",
		States:  map[string]string{NodeKey(NodeContainer, "web"): "running"},
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		StatusQuery: func(context.Context, string) (daemonstatus.Snapshot, error) {
			return snapshot, nil
		},
		Runtime: &fakeVMRuntime{
			states: map[string]string{NodeKey(NodeContainer, "web"): "missing"},
		},
	}

	app.refreshWorkloadStates()

	node, ok := nodeByKey(app.Model, NodeKey(NodeContainer, "web"))
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "missing" {
		t.Fatalf("container state = %q, want fallback missing", node.State)
	}
}

func TestRefreshWorkloadStatesNormalizesRuntimeStates(t *testing.T) {
	key := NodeKey(NodeContainer, "kali")
	loaded := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "kali", Image: "docker.io/kalilinux/kali-rolling:latest", DesiredState: lab.DesiredStateRunning}},
	}
	app := App{
		Model: ModelFromLab(loaded),
		Lab:   loaded,
		Runtime: &fakeVMRuntime{
			states: map[string]string{key: " Missing "},
		},
	}

	app.refreshWorkloadStates()

	if app.WorkloadStates[key] != "missing" {
		t.Fatalf("workload state = %q, want normalized missing", app.WorkloadStates[key])
	}
	node, ok := nodeByKey(app.Model, key)
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "missing" {
		t.Fatalf("container state = %q, want normalized missing", node.State)
	}
}

func TestNormalModeQDoesNotQuit(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	for _, key := range []string{"char:q", "char:Q"} {
		if app.handleKey(key) {
			t.Fatalf("handleKey(%q) quit in normal mode", key)
		}
	}
}

func TestEnterDoesNotShowSelectedNodeMessage(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	if app.handleKey("enter") {
		t.Fatal("enter quit unexpectedly")
	}
	if strings.Contains(app.State.Message, "selected ") {
		t.Fatalf("enter set selected-node message %q", app.State.Message)
	}
}

func TestHJKLMoveGraphFocusLikeArrows(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph, Selected: 0}}

	app.handleKey("char:j")
	if app.State.Selected != 1 {
		t.Fatalf("j selected = %d, want client01", app.State.Selected)
	}
	app.handleKey("char:l")
	if app.State.Selected != 4 {
		t.Fatalf("l selected = %d, want lan", app.State.Selected)
	}
	app.handleKey("char:h")
	if app.State.Selected != 1 {
		t.Fatalf("h selected = %d, want client01", app.State.Selected)
	}
	app.handleKey("char:k")
	if app.State.Selected != 0 {
		t.Fatalf("k selected = %d, want router", app.State.Selected)
	}
}

func TestContextMenuInlineEditVMName(t *testing.T) {
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
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}

	app.handleKey("space")
	app.handleKey("enter")
	app.State.ContextSubSelected = 1
	app.handleKey("enter")
	if !app.State.ContextEdit {
		t.Fatal("enter on config value did not start inline edit")
	}
	for range "vm1" {
		app.handleKey("backspace")
	}
	for _, ch := range "web" {
		app.handleKey("char:" + string(ch))
	}
	app.handleKey("enter")
	if app.State.CommandMode {
		t.Fatal("inline edit opened command bar")
	}
	if app.State.ContextEdit {
		t.Fatal("inline edit did not finish")
	}

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].ID != "web" || reloaded.VMs[0].Name != "" {
		t.Fatalf("vm identity = id:%q name:%q, want mnemonic id web", reloaded.VMs[0].ID, reloaded.VMs[0].Name)
	}
}

func TestContextMenuMoveSavesLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.img"}},
		Layout: lab.Layout{Nodes: map[string]lab.Position{
			"vm1": {X: 80, Y: 72},
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

	app.handleKey("space")
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok {
		t.Fatal("no selected node")
	}
	moveIndex := -1
	for i, item := range app.contextMenuRootItems(node, ok) {
		if contextMenuAction(item) == "move" {
			moveIndex = i
			break
		}
	}
	if moveIndex < 0 {
		t.Fatal("Move menu item not found")
	}
	app.State.ContextSelected = moveIndex
	app.handleKey("enter")
	if !app.State.MoveMode {
		t.Fatal("Move menu action did not enter move mode")
	}
	app.handleKey("right")
	app.handleKey("down")
	app.handleKey("enter")
	if app.State.MoveMode {
		t.Fatal("move mode did not finish after enter")
	}
	if app.State.Message != "" {
		t.Fatalf("move message = %q, want no move notification", app.State.Message)
	}

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Layout.Nodes[reloaded.VMs[0].ID]
	if got.X != 96 || got.Y != 96 {
		t.Fatalf("saved layout = %#v, want X=96 Y=96", got)
	}
}

func TestNormalModeMStartsMoveAndSavesLayout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.img"}},
		Layout: lab.Layout{Nodes: map[string]lab.Position{
			"vm1": {X: 80, Y: 72},
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

	app.handleKey("char:m")
	if !app.State.MoveMode {
		t.Fatal("m did not enter move mode")
	}
	app.handleKey("right")
	app.handleKey("down")
	app.handleKey("enter")
	if app.State.MoveMode {
		t.Fatal("move mode did not finish after enter")
	}
	if app.State.Message != "" {
		t.Fatalf("move message = %q, want no move notification", app.State.Message)
	}

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Layout.Nodes[reloaded.VMs[0].ID]
	if got.X != 96 || got.Y != 96 {
		t.Fatalf("saved layout = %#v, want X=96 Y=96", got)
	}
}

func TestMouseDragNodeSavesLayoutWithoutMoveAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.img"}},
		Layout: lab.Layout{Nodes: map[string]lab.Position{
			"vm1": {X: 80, Y: 72},
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
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  100,
		ViewHeight: 30,
	}
	rects := layoutNodeRects(app.Model, app.graphBounds())
	nodeRect := rects[NodeKey(NodeVM, loaded.VMs[0].ID)]
	startX := nodeRect.X + 1
	startY := nodeRect.Y + 1

	app.handleKey("mouse:" + strconv.Itoa(startX) + ":" + strconv.Itoa(startY) + ":0")
	app.handleKey("mouse-drag:" + strconv.Itoa(startX+3) + ":" + strconv.Itoa(startY+2) + ":0")
	if app.State.ContextMenu {
		t.Fatal("drag kept context menu open")
	}
	if !app.State.MoveMode {
		t.Fatal("mouse drag did not enter transient move mode")
	}
	if app.Model.Nodes[0].X != 8 || app.Model.Nodes[0].Y != 5 {
		t.Fatalf("dragged model position = (%d,%d), want (8,5)", app.Model.Nodes[0].X, app.Model.Nodes[0].Y)
	}
	app.handleKey("mouse-release:" + strconv.Itoa(startX+3) + ":" + strconv.Itoa(startY+2) + ":0")
	if app.State.MoveMode {
		t.Fatal("mouse release did not finish drag move")
	}

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Layout.Nodes[reloaded.VMs[0].ID]
	if got.X != 128 || got.Y != 120 {
		t.Fatalf("saved layout = %#v, want X=128 Y=120", got)
	}
}

func TestMouseDragEmptyGraphPansViewportWithoutMovingNode(t *testing.T) {
	app := App{
		Model:      Model{Nodes: []Node{{ID: "right", Type: NodeSwitch, X: 80, Y: 2}}},
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  56,
		ViewHeight: 20,
	}

	app.handleKey("mouse:30:10:0")
	app.handleKey("mouse-drag:20:10:0")

	if app.State.PanX != -10 || app.State.PanY != 0 {
		t.Fatalf("pan = (%d,%d), want (-10,0)", app.State.PanX, app.State.PanY)
	}
	if app.Model.Nodes[0].X != 80 || app.Model.Nodes[0].Y != 2 {
		t.Fatalf("node moved during pan: (%d,%d)", app.Model.Nodes[0].X, app.Model.Nodes[0].Y)
	}
	if app.State.MoveMode {
		t.Fatal("empty graph pan entered move mode")
	}

	app.handleKey("mouse-release:20:10:0")
	if app.State.PanX != -10 || app.State.PanY != 0 {
		t.Fatalf("pan after release = (%d,%d), want (-10,0)", app.State.PanX, app.State.PanY)
	}
}

func TestShiftArrowPansViewportWithoutChangingSelection(t *testing.T) {
	app := App{
		Model:      Model{Nodes: []Node{{ID: "left", Type: NodeVM, X: 0, Y: 2}, {ID: "right", Type: NodeSwitch, X: 80, Y: 2}}},
		State:      ViewState{Focus: FocusGraph, Selected: 1},
		ViewWidth:  56,
		ViewHeight: 20,
	}

	app.handleKey("shift-right")

	if app.State.PanX != -8 || app.State.PanY != 0 {
		t.Fatalf("pan = (%d,%d), want (-8,0)", app.State.PanX, app.State.PanY)
	}
	if app.State.Selected != 1 {
		t.Fatalf("selected = %d, want unchanged 1", app.State.Selected)
	}
}

func TestPanClampsToContentBounds(t *testing.T) {
	app := App{
		Model:      Model{Nodes: []Node{{ID: "right", Type: NodeSwitch, X: 80, Y: 2}}},
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  56,
		ViewHeight: 20,
	}

	app.panGraph(-999, 0)
	if app.State.PanX != -96 {
		t.Fatalf("left clamp panX = %d, want -96", app.State.PanX)
	}
	app.panGraph(999, 0)
	if app.State.PanX != 0 {
		t.Fatalf("right clamp panX = %d, want 0", app.State.PanX)
	}

	app.Model = Model{Nodes: []Node{{ID: "fit", Type: NodeVM, X: 4, Y: 2}}}
	app.State.PanX = 0
	app.State.PanY = 0
	app.panGraph(-8, -4)
	if app.State.PanX != -8 || app.State.PanY != -4 {
		t.Fatalf("fit content pan = (%d,%d), want (-8,-4)", app.State.PanX, app.State.PanY)
	}
}

func TestMoveSaveFailureRestoresLabLayout(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.img"}},
		Layout: lab.Layout{Nodes: map[string]lab.Position{
			"vm1": {X: 80, Y: 72},
		}},
	}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: filepath.Join(blocker, "demo.lab"),
		State:   ViewState{Focus: FocusGraph},
	}

	app.handleKey("char:m")
	app.handleKey("right")
	app.handleKey("down")
	app.handleKey("enter")

	if !app.State.MoveMode {
		t.Fatal("move mode ended after failed save")
	}
	if !strings.HasPrefix(app.State.Message, "move failed:") {
		t.Fatalf("message = %q, want move failed", app.State.Message)
	}
	if got := app.Lab.Layout.Nodes["vm1"]; got != (lab.Position{X: 80, Y: 72}) {
		t.Fatalf("lab layout after failed save = %#v, want original", got)
	}
	if app.Model.Nodes[0].X != 6 || app.Model.Nodes[0].Y != 4 {
		t.Fatalf("move preview = (%d,%d), want (6,4)", app.Model.Nodes[0].X, app.Model.Nodes[0].Y)
	}
}

func TestContextMenuMoveEscapeCancels(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	node, _ := selectedNode(app.Model, 0)
	app.startMove(node)
	app.handleKey("right")
	app.handleKey("escape")

	if app.State.MoveMode {
		t.Fatal("move mode stayed active after escape")
	}
	if app.Model.Nodes[0].X != node.X || app.Model.Nodes[0].Y != node.Y {
		t.Fatalf("node position after cancel = (%d,%d), want (%d,%d)", app.Model.Nodes[0].X, app.Model.Nodes[0].Y, node.X, node.Y)
	}
}

func TestMoveModeClampsToTerminalCanvas(t *testing.T) {
	app := App{
		Model:      Model{Nodes: []Node{{ID: "bottom", Type: NodeVM, X: 4, Y: 25}}},
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  80,
		ViewHeight: 30,
	}
	app.startMove(app.Model.Nodes[0])

	app.handleKey("down")
	if app.Model.Nodes[0].Y != 25 {
		t.Fatalf("down moved past terminal canvas: y=%d", app.Model.Nodes[0].Y)
	}
	app.handleKey("up")
	if app.Model.Nodes[0].Y != 24 {
		t.Fatalf("up did not move bottom node: y=%d", app.Model.Nodes[0].Y)
	}
}

func TestRunInteractiveUpdatesMoveBoundsFromTerminalSize(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}, Out: tempOutputFile(t)}
	start := func(*App) (func(), error) { return func() {}, nil }
	read := func(*App) (string, error) { return "quit", nil }
	size := func(*App) (int, int) { return 120, 40 }

	if err := app.runInteractive(start, read, size); err != nil {
		t.Fatal(err)
	}
	if app.ViewWidth != 120 || app.ViewHeight != 40 {
		t.Fatalf("view size = %dx%d, want 120x40", app.ViewWidth, app.ViewHeight)
	}
	maxX, maxY := app.moveBounds()
	if maxX != 104 || maxY != 35 {
		t.Fatalf("move bounds = %d,%d, want 104,35", maxX, maxY)
	}
}

func TestRunInteractiveSkipsRenderOnEmptyKeyTimeout(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}, Out: tempOutputFile(t)}
	start := func(*App) (func(), error) { return func() {}, nil }
	keys := []string{"", "", "quit"}
	read := func(*App) (string, error) {
		key := keys[0]
		keys = keys[1:]
		return key, nil
	}
	size := func(*App) (int, int) { return 80, 20 }

	if err := app.runInteractive(start, read, size); err != nil {
		t.Fatal(err)
	}

	got := outputFileString(t, app.Out)
	if count := strings.Count(got, ansiMoveHome); count != 1 {
		t.Fatalf("render count = %d, want 1; output=%q", count, got)
	}
}

func TestRunInteractiveRefreshesRuntimeStatusWithoutReconciling(t *testing.T) {
	key := NodeKey(NodeContainer, "web")
	loaded := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{{
			ID:           "web",
			Image:        "docker.io/library/nginx:latest",
			DesiredState: lab.DesiredStateRunning,
		}},
	}
	runtime := &fakeVMRuntime{states: map[string]string{key: "missing"}}
	app := App{
		Model:                 ModelFromLab(loaded),
		Lab:                   loaded,
		Runtime:               runtime,
		StatusRefreshInterval: time.Millisecond,
		State:                 ViewState{Focus: FocusGraph},
		Out:                   tempOutputFile(t),
	}
	start := func(*App) (func(), error) { return func() {}, nil }
	reads := 0
	read := func(*App) (string, error) {
		reads++
		if reads == 1 {
			runtime.setState(key, "running")
			time.Sleep(2 * time.Millisecond)
			return "", nil
		}
		if reads < 6 {
			time.Sleep(time.Millisecond)
			return "", nil
		}
		return "quit", nil
	}
	size := func(*App) (int, int) { return 80, 20 }

	if err := app.runInteractive(start, read, size); err != nil {
		t.Fatal(err)
	}

	node, ok := nodeByKey(app.Model, key)
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "running" {
		t.Fatalf("container state = %q, want refreshed running", node.State)
	}
	if runtime.starts != 0 || runtime.stops != 0 {
		t.Fatalf("runtime starts/stops = %d/%d, want status-only refresh", runtime.starts, runtime.stops)
	}
}

func TestDrainStatusUpdatesIgnoresStaleLabUpdate(t *testing.T) {
	oldLab := &lab.Lab{
		ID:         "old",
		Containers: []lab.Container{{ID: "web", Image: "nginx", DesiredState: lab.DesiredStateRunning}},
	}
	currentLab := &lab.Lab{
		ID:         "current",
		Containers: []lab.Container{{ID: "web", Image: "nginx", DesiredState: lab.DesiredStateRunning}},
	}
	app := App{
		Model: ModelFromLab(currentLab),
		Lab:   currentLab,
		State: ViewState{Focus: FocusGraph},
	}
	updates := make(chan statusUpdate, 1)
	updates <- statusUpdate{
		lab:    oldLab,
		states: map[string]string{NodeKey(NodeContainer, "web"): "running"},
	}
	active := true

	if changed := app.drainStatusUpdates(updates, &active); changed {
		t.Fatal("stale update changed app state")
	}
	if active {
		t.Fatal("stale update did not clear active refresh flag")
	}
	node, ok := nodeByKey(app.Model, NodeKey(NodeContainer, "web"))
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "missing" {
		t.Fatalf("container state = %q, want unchanged missing", node.State)
	}
}

func TestDrainStatusUpdatesCopiesUpdateMaps(t *testing.T) {
	stateKey := NodeVM + ":vm1"
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2"}},
	}
	states := map[string]string{stateKey: "running"}
	ports := map[string]int{stateKey: 5903}
	app := App{
		Model: ModelFromLab(loaded),
		Lab:   loaded,
		State: ViewState{Focus: FocusGraph},
	}
	updates := make(chan statusUpdate, 1)
	updates <- statusUpdate{lab: loaded, states: states, vncPorts: ports}
	active := true

	if changed := app.drainStatusUpdates(updates, &active); !changed {
		t.Fatal("status update did not change app")
	}
	states[stateKey] = "shutoff"
	ports[stateKey] = 5999

	if app.WorkloadStates[stateKey] != "running" {
		t.Fatalf("app workload state = %q, want copied running state", app.WorkloadStates[stateKey])
	}
	if app.VNCPorts[stateKey] != 5903 {
		t.Fatalf("app VNC port = %d, want copied 5903", app.VNCPorts[stateKey])
	}
}

func TestDrainStatusUpdatesClearsRecoveredStatusMessage(t *testing.T) {
	loaded := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "nginx", DesiredState: lab.DesiredStateRunning}},
	}
	app := App{
		Model: ModelFromLab(loaded),
		Lab:   loaded,
		State: ViewState{Focus: FocusGraph, Message: "foxlabd status: runtime unavailable"},
	}
	updates := make(chan statusUpdate, 1)
	updates <- statusUpdate{
		lab:                loaded,
		states:             map[string]string{NodeKey(NodeContainer, "web"): "running"},
		clearStatusMessage: true,
	}
	active := true

	if changed := app.drainStatusUpdates(updates, &active); !changed {
		t.Fatal("status recovery update did not change app")
	}
	if app.State.Message != "" {
		t.Fatalf("message = %q, want cleared recovered status error", app.State.Message)
	}
}

func TestDrainStatusUpdatesKeepsCommandMessageOnHealthyStatus(t *testing.T) {
	loaded := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "nginx", DesiredState: lab.DesiredStateRunning}},
	}
	app := App{
		Model: ModelFromLab(loaded),
		Lab:   loaded,
		State: ViewState{Focus: FocusGraph, Message: "renamed disk:data to system"},
	}
	updates := make(chan statusUpdate, 1)
	updates <- statusUpdate{
		lab:                loaded,
		states:             map[string]string{NodeKey(NodeContainer, "web"): "running"},
		clearStatusMessage: true,
	}
	active := true

	if changed := app.drainStatusUpdates(updates, &active); !changed {
		t.Fatal("status update did not change app")
	}
	if app.State.Message != "renamed disk:data to system" {
		t.Fatalf("message = %q, want command feedback preserved", app.State.Message)
	}
}

func TestStartStatusRefreshUsesLabSnapshot(t *testing.T) {
	loaded := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{{
			ID:           "web",
			Image:        "docker.io/library/nginx:latest",
			DesiredState: lab.DesiredStateRunning,
		}},
	}
	runtime := &blockingStatusRuntime{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	app := App{Lab: loaded, Runtime: runtime}
	updates := make(chan statusUpdate, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.startStatusRefresh(ctx, updates)
	select {
	case <-runtime.entered:
	case <-time.After(time.Second):
		t.Fatal("runtime States was not called")
	}
	loaded.Containers[0].ID = "changed"
	close(runtime.release)

	var update statusUpdate
	select {
	case update = <-updates:
	case <-time.After(time.Second):
		t.Fatal("status update was not sent")
	}
	if update.lab != loaded {
		t.Fatal("status update did not keep original lab marker")
	}
	if runtime.seenLab == loaded {
		t.Fatal("runtime received mutable app lab instead of snapshot")
	}
	if runtime.seenContainerID != "web" {
		t.Fatalf("runtime saw container ID %q, want snapshot value web", runtime.seenContainerID)
	}
}

func TestRuntimeCallsAreSerializedDuringStatusRefresh(t *testing.T) {
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:           "vm1",
			MemoryMB:     512,
			CPUs:         1,
			Disk:         "vm1.qcow2",
			VNC:          true,
			DesiredState: lab.DesiredStateRunning,
		}},
	}
	runtime := &serialRuntime{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	app := App{Lab: loaded, Runtime: runtime}
	updates := make(chan statusUpdate, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.startStatusRefresh(ctx, updates)
	select {
	case <-runtime.entered:
	case <-time.After(time.Second):
		t.Fatal("runtime States was not called")
	}

	done := make(chan error, 1)
	go func() {
		done <- app.refreshVNCWorkloadStatus(Node{Type: NodeVM, ID: "vm1"})
	}()
	select {
	case err := <-done:
		t.Fatalf("foreground VNC refresh completed while status refresh held runtime lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(runtime.release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("refreshVNCWorkloadStatus returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("refreshVNCWorkloadStatus did not return after status refresh released runtime lock")
	}
}

func TestRunInteractiveFlashesAndClearsMouseClickFeedback(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}, Out: tempOutputFile(t)}
	rects := layoutNodeRects(app.Model, graphBounds(80, 20))
	router := rects[NodeKey(NodeVM, "router")]
	start := func(*App) (func(), error) { return func() {}, nil }
	keys := []string{"mouse:" + strconv.Itoa(router.X+1) + ":" + strconv.Itoa(router.Y+1) + ":0", "escape", "quit"}
	read := func(*App) (string, error) {
		if len(keys) == 0 {
			return "quit", nil
		}
		key := keys[0]
		keys = keys[1:]
		return key, nil
	}
	size := func(*App) (int, int) { return 80, 20 }

	if err := app.runInteractive(start, read, size); err != nil {
		t.Fatal(err)
	}

	if app.State.MouseClickActive {
		t.Fatal("mouse click feedback stayed active after interactive flash")
	}
	got := outputFileString(t, app.Out)
	if count := strings.Count(got, ansiMoveHome); count < 3 {
		t.Fatalf("render count = %d, want at least initial, flash, and cleared frames; output=%q", count, got)
	}
	if !strings.Contains(got, ansiInverse) {
		t.Fatalf("interactive output missing click flash style:\n%q", got)
	}
}

func TestNotificationMessageExpiresAfterTTL(t *testing.T) {
	app := App{State: ViewState{Message: "created disk:disk"}}
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	if changed := app.updateMessageLifetime(now); changed {
		t.Fatal("initial message lifetime tracking marked state dirty")
	}
	if app.State.Message == "" {
		t.Fatal("initial message was cleared")
	}
	if changed := app.updateMessageLifetime(now.Add(notificationMessageTTL - time.Second)); changed {
		t.Fatal("message expired before TTL")
	}
	if app.State.Message == "" {
		t.Fatal("message cleared before TTL")
	}
	if changed := app.updateMessageLifetime(now.Add(notificationMessageTTL)); !changed {
		t.Fatal("message did not expire at TTL")
	}
	if app.State.Message != "" {
		t.Fatalf("message after TTL = %q, want empty", app.State.Message)
	}
}

func TestAppRenderReusesRouteCacheAcrossViewStateChanges(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	var out bytes.Buffer
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if len(app.RouteCacheRoutes) == 0 {
		t.Fatal("route cache was not populated")
	}
	key := app.RouteCacheKey
	routes := app.RouteCacheRoutes

	out.Reset()
	app.State.Selected = 2
	app.State.ContextMenu = true
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.RouteCacheKey != key {
		t.Fatalf("route cache key changed after view-only state update: %q -> %q", key, app.RouteCacheKey)
	}
	if &app.RouteCacheRoutes[0] != &routes[0] {
		t.Fatal("route cache was recomputed for view-only state update")
	}

	out.Reset()
	app.State.PanX = -1
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.RouteCacheKey == key {
		t.Fatal("route cache key did not change after viewport pan")
	}
	key = app.RouteCacheKey

	out.Reset()
	app.Model.Nodes[0].X++
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.RouteCacheKey == key {
		t.Fatal("route cache key did not change after model layout update")
	}
}

func TestAppRenderTranslatesRouteCacheWhileMousePanning(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	var out bytes.Buffer
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if len(app.RouteCacheRoutes) == 0 || len(app.RouteCacheRoutes[0].route.cells) == 0 {
		t.Fatal("route cache was not populated")
	}
	key := app.RouteCacheKey
	routes := app.RouteCacheRoutes
	first := app.RouteCacheRoutes[0].route.cells[0]

	out.Reset()
	app.mousePanActive = true
	app.State.PanX = -2
	app.State.PanY = 1
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.RouteCacheKey != key {
		t.Fatalf("route cache key changed while panning: %q -> %q", key, app.RouteCacheKey)
	}
	if &app.RouteCacheRoutes[0] != &routes[0] {
		t.Fatal("route cache was recomputed while panning")
	}
	if got := app.RouteCacheRoutes[0].route.cells[0]; got != first {
		t.Fatalf("cached route was mutated while panning: %+v -> %+v", first, got)
	}

	out.Reset()
	app.mousePanActive = false
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.RouteCacheKey == key {
		t.Fatal("route cache did not refresh after mouse panning ended")
	}
}

func TestAppRenderReusesRouteCacheWhileMovingNode(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	var out bytes.Buffer
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	key := app.RouteCacheKey
	routes := app.RouteCacheRoutes

	app.startMove(app.Model.Nodes[0])
	app.moveActiveNode(4, 0)
	out.Reset()
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.RouteCacheKey != key {
		t.Fatalf("route cache key changed while moving node: %q -> %q", key, app.RouteCacheKey)
	}
	if &app.RouteCacheRoutes[0] != &routes[0] {
		t.Fatal("route cache was recomputed while moving node")
	}
	rects := layoutNodeRects(app.Model, rect{X: 0, Y: 0, W: 100, H: 30})
	moved := rects[app.Model.Nodes[0].Key()]
	g := renderGridWithRoutes(app.Model, app.State, 100, 30, app.RouteCacheRoutes)
	followed := false
	for y := moved.Y + 1; y < moved.Y+moved.H-1; y++ {
		if g.Cells[y*g.Width+moved.X+moved.W].Ch != ' ' {
			followed = true
			break
		}
	}
	if !followed {
		t.Fatalf("moving node live route did not follow new node position:\n%s", g.String(false))
	}

	app.clearMoveMode()
	out.Reset()
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.RouteCacheKey == key {
		t.Fatal("route cache did not refresh after move mode ended")
	}
}

func TestAppRenderRefreshesRouteCacheWhenViewportChangesDuringMoveMode(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	var out bytes.Buffer
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	key := app.RouteCacheKey
	app.startMove(app.Model.Nodes[0])
	app.State.PanX = -2
	out.Reset()
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.RouteCacheKey == key {
		t.Fatal("route cache was reused after viewport pan during move mode")
	}
	if app.RouteCachePanX != -2 {
		t.Fatalf("route cache panX = %d, want -2", app.RouteCachePanX)
	}

	key = app.RouteCacheKey
	out.Reset()
	if err := app.render(&out, 96, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.RouteCacheKey == key {
		t.Fatal("route cache was reused after viewport resize during move mode")
	}
	if app.RouteCacheWidth != 96 || app.RouteCacheHeight != 30 {
		t.Fatalf("route cache size = %dx%d, want 96x30", app.RouteCacheWidth, app.RouteCacheHeight)
	}
}

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
		Runtime: &fakeVMRuntime{
			states:   map[string]string{NodeKey(NodeVM, "vm1"): "running"},
			vncPorts: map[string]int{NodeKey(NodeVM, "vm1"): 5904},
		},
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
		Runtime:          runtime,
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
		Runtime:          runtime,
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
		Runtime:          runtime,
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
		Model:            ModelFromLab(loaded),
		Lab:              loaded,
		LabPath:          path,
		Runtime:          &fakeVMRuntime{states: map[string]string{stateKey: "missing"}},
		DaemonController: &fakeDaemonController{},
		StatusQuery: func(context.Context, string) (daemonstatus.Snapshot, error) {
			return daemonstatus.Snapshot{}, errors.New("no daemon snapshot")
		},
		State: ViewState{Focus: FocusGraph},
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
		StatusQuery: func(context.Context, string) (daemonstatus.Snapshot, error) {
			return daemonstatus.Snapshot{
				LabPath: path,
				LabName: "demo",
				States:  map[string]string{stateKey: "missing"},
				Errors:  []string{"start container:web: image unavailable"},
			}, nil
		},
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
		{"help vm", "Configuration edits"},
		{"help switch", "switch: Configuration"},
		{"help external", "uplink: Configuration"},
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

func TestCommandQQuits(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	if !app.executeCommand("q") {
		t.Fatal(":q command did not quit")
	}
	if !app.executeCommand("quit") {
		t.Fatal(":quit command did not quit")
	}
}

func TestCommandQRejectsExtraArgs(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	if app.executeCommand("quit now") {
		t.Fatal(":quit with extra args quit unexpectedly")
	}
	if app.State.Message != "usage: quit" {
		t.Fatalf("message = %q, want usage: quit", app.State.Message)
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

func TestPaletteExitQuits(t *testing.T) {
	app := App{
		Model:      Model{ID: "empty"},
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  80,
		ViewHeight: 20,
	}

	app.handleKey("char::")
	for _, key := range []string{"char:q"} {
		app.handleKey(key)
	}
	if !app.handleKey("enter") {
		t.Fatal("palette q did not quit")
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
		Runtime: &fakeVMRuntime{states: map[string]string{
			NodeKey(NodeVM, loaded.VMs[0].ID): "shutoff",
		}},
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
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		Runtime: &fakeVMRuntime{statesErr: errors.New("libvirt unavailable")},
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

func TestDiskExplorerTopRowClickClosesExplorerWithoutAction(t *testing.T) {
	app := App{
		Model:      Model{ID: "demo"},
		Lab:        &lab.Lab{ID: "demo"},
		State:      ViewState{DiskExplorerOpen: true},
		ViewWidth:  100,
		ViewHeight: 30,
	}

	if app.handleKey("mouse:10:0:0") {
		t.Fatal("top row click quit while disk explorer was open")
	}
	if app.State.DiskExplorerOpen {
		t.Fatal("top row click did not close disk explorer")
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
	if !app.State.ContextMenu {
		t.Fatal("node click did not open context menu")
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
	for _, key := range []string{"char:q"} {
		app.handleKey(key)
	}
	if !app.handleKey("enter") {
		t.Fatal("keyboard palette q did not quit")
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
	if err != nil {
		t.Fatalf("readAppKey after queue err=%v", err)
	}
	if got != "" {
		t.Fatalf("readAppKey after queue = %q, want empty", got)
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

func TestInspectCommandsAreRemoved(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	app.executeCommand("list")
	if app.State.Message != "unknown command: list" {
		t.Fatalf("list message = %q", app.State.Message)
	}

	app.executeCommand("status router")
	if app.State.Message != "unknown command: status" {
		t.Fatalf("status message = %q", app.State.Message)
	}

	app.State.Console = []string{"old"}
	app.executeCommand("clear")
	if app.State.Message != "unknown command: clear" {
		t.Fatalf("clear message = %q", app.State.Message)
	}
	if len(app.State.Console) == 0 {
		t.Fatal("clear still cleared console")
	}
}

func TestCommandVMCreateSavesLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
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
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}

	app.executeCommand("add vm vm1 cpus=4 memory=4096 switch=lan")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.VMs) != 1 {
		t.Fatalf("vm count = %d, want 1", len(reloaded.VMs))
	}
	if reloaded.VMs[0].ID != "vm1" || reloaded.VMs[0].Name != "" || reloaded.VMs[0].CPUs != 4 || reloaded.VMs[0].MemoryMB != 4096 {
		t.Fatalf("saved vm = %#v", reloaded.VMs[0])
	}
	if len(reloaded.VMs[0].Networks) != 1 || reloaded.VMs[0].Networks[0].Switch != reloaded.Switches[0].ID {
		t.Fatalf("vm networks = %#v, want switch %q", reloaded.VMs[0].Networks, reloaded.Switches[0].ID)
	}
	if len(app.Model.Nodes) == 0 || app.Model.Nodes[0].ID != reloaded.VMs[0].ID || app.Model.Nodes[0].Label != "vm1" {
		t.Fatalf("model not refreshed: %#v", app.Model.Nodes)
	}
}

func TestCommandContainerCreateSavesLabAndGraph(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
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
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}

	app.executeCommand(`add cont web image=docker.io/library/nginx:latest command="nginx -g daemon" switch=lan`)

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Containers) != 1 {
		t.Fatalf("container count = %d, want 1", len(reloaded.Containers))
	}
	ct := reloaded.Containers[0]
	if ct.ID != "web" || ct.Name != "" || ct.Image != "docker.io/library/nginx:latest" || len(ct.Networks) != 1 || ct.Networks[0].Switch != reloaded.Switches[0].ID {
		t.Fatalf("saved container = %#v", ct)
	}
	if len(app.Model.Nodes) == 0 || app.Model.Nodes[0].ID != ct.ID || app.Model.Nodes[0].Label != "web" || app.Model.Nodes[0].Type != NodeContainer || app.Model.Nodes[0].Badge != "CT" {
		t.Fatalf("container model not refreshed: %#v", app.Model.Nodes)
	}
	if len(app.Model.Edges) != 1 || app.Model.Edges[0].From != NodeKey(NodeContainer, ct.ID) || app.Model.Edges[0].To != NodeKey(NodeSwitch, reloaded.Switches[0].ID) {
		t.Fatalf("container edges = %#v", app.Model.Edges)
	}
}

func TestCommandContainerSetClearsCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{{
			ID:      "web",
			Image:   "docker.io/library/nginx:latest",
			Command: []string{"nginx", "-g", "daemon off;"},
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

	app.executeCommand("container set web command=")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Containers[0].Command; len(got) != 0 {
		t.Fatalf("container command = %#v, want empty", got)
	}
	if len(app.Model.Nodes) != 1 {
		t.Fatalf("model nodes = %#v, want one container", app.Model.Nodes)
	}
	for _, detail := range app.Model.Nodes[0].Details {
		if strings.HasPrefix(detail, "command=") {
			t.Fatalf("model kept command detail after clear: %#v", app.Model.Nodes[0].Details)
		}
	}
}

func TestCommandStartStopSetsDesiredState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 512,
			CPUs:     1,
			Disk:     "disks/vm1.qcow2",
		}},
		Containers: []lab.Container{{ID: "web", Image: "nginx"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeVMRuntime{}
	daemon := &fakeDaemonController{}
	app := App{
		Model:            ModelFromLab(loaded),
		Lab:              loaded,
		LabPath:          path,
		Runtime:          runtime,
		DaemonController: daemon,
		State:            ViewState{Focus: FocusGraph},
	}

	app.executeCommand("vm start vm1")
	if runtime.starts != 0 {
		t.Fatalf("vm start called runtime Start %d times", runtime.starts)
	}
	if app.Lab.VMs[0].DesiredState != lab.DesiredStateRunning {
		t.Fatalf("vm desired after start = %q", app.Lab.VMs[0].DesiredState)
	}
	app.executeCommand("container start web")
	if runtime.starts != 0 {
		t.Fatalf("container start called runtime Start %d times", runtime.starts)
	}
	if app.Lab.Containers[0].DesiredState != lab.DesiredStateRunning {
		t.Fatalf("container desired after start = %q", app.Lab.Containers[0].DesiredState)
	}
	app.executeCommand("vm stop vm1")
	app.executeCommand("container stop web")
	if runtime.stops != 0 {
		t.Fatalf("stop called runtime Stop %d times", runtime.stops)
	}
	if app.Lab.VMs[0].DesiredState != lab.DesiredStateStopped {
		t.Fatalf("vm desired after stop = %q", app.Lab.VMs[0].DesiredState)
	}
	if app.Lab.Containers[0].DesiredState != lab.DesiredStateStopped {
		t.Fatalf("container desired after stop = %q", app.Lab.Containers[0].DesiredState)
	}
	if daemon.applyCalls != 4 {
		t.Fatalf("start/stop commands applied lab %d times, want 4", daemon.applyCalls)
	}
	if app.State.Message != "" {
		t.Fatalf("start/stop message = %q, want no desired-state notification", app.State.Message)
	}
}

func TestShellVMUsesDirectConsole(t *testing.T) {
	var consoleCtx context.Context
	app := App{
		Model:   MockModel(),
		Lab:     &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2}}},
		Runtime: &fakeVMRuntime{states: map[string]string{NodeKey(NodeVM, "vm1"): " Running "}},
		State:   ViewState{Focus: FocusGraph},
		VMConsole: func(ctx context.Context, _ *lab.Lab, id string) (io.ReadWriteCloser, string, error) {
			consoleCtx = ctx
			if id != "vm1" {
				t.Fatalf("console id = %q", id)
			}
			return &fakeConsole{}, "vm console /dev/pts/7", nil
		},
	}

	app.executeCommand("shell vm vm1")

	if app.PendingShell == nil {
		t.Fatal("vm shell did not set pending command")
	}
	if got := app.Runtime.(*fakeVMRuntime).starts; got != 0 {
		t.Fatalf("vm shell started workload %d times", got)
	}
	if got := app.WorkloadStates[NodeKey(NodeVM, "vm1")]; got != "running" {
		t.Fatalf("workload state = %q, want normalized running", got)
	}
	if app.PendingShell.Console == nil || app.PendingShell.NativeRun != nil {
		t.Fatalf("vm shell command = %#v", app.PendingShell)
	}
	if app.PendingShell.Display != "vm console /dev/pts/7" {
		t.Fatalf("vm shell display = %q", app.PendingShell.Display)
	}
	if _, ok := consoleCtx.Deadline(); !ok {
		t.Fatal("VM console context had no deadline")
	}
}

func TestCommandShellRejectsExtraArgs(t *testing.T) {
	consoleCalled := false
	app := App{
		Model:   MockModel(),
		Lab:     &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2}}},
		Runtime: &fakeVMRuntime{states: map[string]string{NodeKey(NodeVM, "vm1"): "running"}},
		State:   ViewState{Focus: FocusGraph},
		VMConsole: func(context.Context, *lab.Lab, string) (io.ReadWriteCloser, string, error) {
			consoleCalled = true
			return &fakeConsole{}, "vm console /dev/pts/7", nil
		},
	}

	app.executeCommand("shell vm vm1 extra")

	if app.State.Message != "usage: shell <vm|container> <id>" {
		t.Fatalf("message = %q, want shell usage", app.State.Message)
	}
	if app.PendingShell != nil {
		t.Fatalf("extra-arg shell queued pending shell: %#v", app.PendingShell)
	}
	if consoleCalled {
		t.Fatal("extra-arg shell opened VM console")
	}
}

func TestContainerShellNeedsRestartForRootFSError(t *testing.T) {
	for _, detail := range []string{
		"exec /bin/sh: input/output error",
		`ERRO[0000] resize pty error="cannot resize a stopped container"`,
		"task not found",
	} {
		if !containerShellNeedsRestart(detail) {
			t.Fatalf("containerShellNeedsRestart(%q) = false", detail)
		}
	}
}

func TestConsoleConnectMessageExplainsVMSerialConsole(t *testing.T) {
	got := consoleConnectMessage("vm console /dev/pts/7")
	if !strings.Contains(got, "serial port") || !strings.Contains(got, "VNC") || !strings.Contains(got, "ttyS0") {
		t.Fatalf("VM console message missing serial hint: %q", got)
	}
	if got := consoleConnectMessage("container shell foxlab-demo-web"); strings.Contains(got, "serial port") {
		t.Fatalf("container shell message contains VM serial hint: %q", got)
	}
}

func TestCopyConsoleOutputKeepsSessionOpenOnEOF(t *testing.T) {
	done := make(chan struct{})
	errc := make(chan error, 1)
	go func() {
		errc <- copyConsoleOutput(io.Discard, eofReader{}, done)
	}()

	select {
	case err := <-errc:
		t.Fatalf("console copy exited on EOF: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(done)
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("console copy after done = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("console copy did not exit after done")
	}
}

func TestCopyConsoleOutputKeepsSessionOpenOnClosedPipe(t *testing.T) {
	var out bytes.Buffer
	done := make(chan struct{})
	errc := make(chan error, 1)
	go func() {
		errc <- copyConsoleOutput(&out, closedPipeReader{}, done)
	}()

	select {
	case err := <-errc:
		t.Fatalf("console copy exited on closed pipe: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	if strings.Contains(out.String(), "closed pipe") {
		t.Fatalf("console copy printed closed pipe: %q", out.String())
	}

	close(done)
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("console copy after done = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("console copy did not exit after done")
	}
}

func TestCommandAddCreatesGraphNodesWithMinimalData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
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

	app.executeCommand("add vm vm1")
	app.executeCommand("add sw sw1")
	app.executeCommand("add cont web")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.VMs) != 1 || reloaded.VMs[0].ID != "vm1" || reloaded.VMs[0].Name != "" {
		t.Fatalf("minimal vm was not saved: %#v", reloaded.VMs)
	}
	if len(reloaded.Switches) != 1 || reloaded.Switches[0].ID != "sw1" || reloaded.Switches[0].Name != "" {
		t.Fatalf("minimal switch was not saved: %#v", reloaded.Switches)
	}
	if len(reloaded.Containers) != 1 || reloaded.Containers[0].ID != "web" || reloaded.Containers[0].Name != "" || reloaded.Containers[0].Image == "" {
		t.Fatalf("minimal container was not saved with placeholder image: %#v", reloaded.Containers)
	}
	if len(app.Model.Nodes) != 3 {
		t.Fatalf("minimal add did not refresh graph nodes: %#v", app.Model.Nodes)
	}
}

func TestCommandVMCreateUsesDiskPathOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
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

	app.executeCommand("add vm vm1 disk=explicit/path/test.qcow2")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Disk != "explicit/path/test.qcow2" {
		t.Fatalf("vm disk = %q, want explicit path", reloaded.VMs[0].Disk)
	}
}

func TestCommandVMSetUpdatesDiskPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("vm set vm1 disk=labs/demo/disks/vm1.img iso=images/debian.iso")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Disk != "labs/demo/disks/vm1.img" || reloaded.VMs[0].ISO != "images/debian.iso" {
		t.Fatalf("vm after set = %#v", reloaded.VMs[0])
	}
}

func TestCommandVMSetClearsDiskAndISO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2", ISO: "images/debian.iso"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("vm set vm1 disk= iso=")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Disk != "" || reloaded.VMs[0].ISO != "" {
		t.Fatalf("vm after clear = %#v, want empty disk and iso", reloaded.VMs[0])
	}
}

func TestCommandVMSetAcceptsQuotedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand(`vm set vm1 name="web-server" iso="images/debian 12.iso"`)

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].ID != "web-server" || reloaded.VMs[0].Name != "" || reloaded.VMs[0].ISO != "images/debian 12.iso" {
		t.Fatalf("vm quoted fields = id:%q name:%q iso:%q", reloaded.VMs[0].ID, reloaded.VMs[0].Name, reloaded.VMs[0].ISO)
	}
}

func TestCommandVMNICAddAndConnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		VMs:           []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2", Networks: []lab.VMNetwork{{Switch: "lan"}}}},
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}, {ID: "wan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("vm nic add vm1 mac=02:00:00:00:00:22")
	app.executeCommand("vm nic add vm1")
	app.executeCommand("vm nic connect vm1 1 to=wan")
	app.executeCommand("vm nic connect vm1 2 to=uplink1")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	networks := reloaded.VMs[0].Networks
	if len(networks) != 3 {
		t.Fatalf("vm networks count = %d, want 3: %#v", len(networks), networks)
	}
	if networks[0].Switch != reloaded.Switches[0].ID || networks[1].Switch != reloaded.Switches[1].ID || networks[1].MAC != "02:00:00:00:00:22" || networks[2].ExternalLink != reloaded.ExternalLinks[0].ID {
		t.Fatalf("vm networks = %#v", networks)
	}
	if len(app.Model.Edges) != 3 {
		t.Fatalf("model edges = %#v, want 3", app.Model.Edges)
	}
}

func TestCommandContainerNICAddAndConnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		Containers:    []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", Networks: []lab.ContainerNetwork{{Switch: "lan"}}}},
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}, {ID: "wan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "br0"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("container nic add web mac=02:00:00:00:00:33")
	app.executeCommand("container nic connect web 1 to=wan")
	app.executeCommand("container nic add web")
	app.executeCommand("container nic connect web 2 to=uplink1")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	networks := reloaded.Containers[0].Networks
	if len(networks) != 3 {
		t.Fatalf("container networks count = %d, want 3: %#v", len(networks), networks)
	}
	if networks[0].Switch != reloaded.Switches[0].ID || networks[1].Switch != reloaded.Switches[1].ID || networks[1].MAC != "02:00:00:00:00:33" || networks[2].ExternalLink != reloaded.ExternalLinks[0].ID {
		t.Fatalf("container networks = %#v", networks)
	}
	if len(app.Model.Edges) != 3 {
		t.Fatalf("model edges = %#v, want 3", app.Model.Edges)
	}
}

func TestContainerNICConnectDoesNotReconcileRunningContainer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{{
			ID:           "web",
			DesiredState: lab.DesiredStateRunning,
			Image:        "docker.io/library/nginx:latest",
			Networks:     []lab.ContainerNetwork{{}},
		}},
		Switches: []lab.Switch{{ID: "wan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeVMRuntime{states: map[string]string{NodeKey(NodeContainer, "web"): "running"}}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		Runtime: runtime,
		State:   ViewState{Focus: FocusGraph},
	}

	app.containerNICConnect("web", "0", map[string]string{"to": "wan"})

	if runtime.started != "" {
		t.Fatalf("started = %q, want no direct TUI reconcile", runtime.started)
	}
	if !strings.HasPrefix(app.State.Message, "connected nic to container:") {
		t.Fatalf("message = %q", app.State.Message)
	}
}

func TestCommandLinkAddAndDeleteExplicitNICs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 2048, CPUs: 2, Networks: []lab.VMNetwork{{}}},
			{ID: "vm2", MemoryMB: 2048, CPUs: 2, Networks: []lab.VMNetwork{{Switch: "lan"}, {}}},
		},
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("link add vm:vm1:nic0 to=vm:vm2:nic1")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.NetworkLinks) != 1 {
		t.Fatalf("network links = %#v, want one direct link", reloaded.NetworkLinks)
	}
	link := reloaded.NetworkLinks[0]
	if link.From.Type != "vm" || link.From.ID != reloaded.VMs[0].ID || link.From.NIC != 0 || link.To.Type != "vm" || link.To.ID != reloaded.VMs[1].ID || link.To.NIC != 1 {
		t.Fatalf("network link = %#v", link)
	}
	if !hasEdge(app.Model, NodeKey(NodeVM, reloaded.VMs[0].ID), NodeKey(NodeVM, reloaded.VMs[1].ID)) {
		t.Fatalf("model edges = %#v", app.Model.Edges)
	}

	app.executeCommand("link delete vm:vm1:0")
	reloaded, err = lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.NetworkLinks) != 0 {
		t.Fatalf("network links after delete = %#v, want none", reloaded.NetworkLinks)
	}
}

func TestCommandLinkAddUsesFirstAvailableTargetNIC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:         "demo",
		VMs:        []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Networks: []lab.VMNetwork{{}}}},
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", Networks: []lab.ContainerNetwork{{Switch: "lan"}, {}}}},
		Switches:   []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("link add vm:vm1:0 to=ct:web")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.NetworkLinks) != 1 {
		t.Fatalf("network links = %#v, want one direct link", reloaded.NetworkLinks)
	}
	link := reloaded.NetworkLinks[0]
	if link.To.Type != "container" || link.To.ID != reloaded.Containers[0].ID || link.To.NIC != 1 {
		t.Fatalf("network link target = %#v, want web nic1", link)
	}
}

func TestCommandVMNICDeleteRemovesDirectLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 2048, CPUs: 2, Networks: []lab.VMNetwork{{}, {}}},
			{ID: "vm2", MemoryMB: 2048, CPUs: 2, Networks: []lab.VMNetwork{{}}},
		},
		NetworkLinks: []lab.NetworkLink{{
			From: lab.NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 1},
			To:   lab.NetworkEndpoint{Type: "vm", ID: "vm2", NIC: 0},
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("vm nic delete vm1 1")

	if app.State.Message != "deleted nic from vm:vm1 nic1" {
		t.Fatalf("message = %q", app.State.Message)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.VMs[0].Networks) != 1 {
		t.Fatalf("vm networks = %#v, want one nic", reloaded.VMs[0].Networks)
	}
	if len(reloaded.NetworkLinks) != 0 {
		t.Fatalf("network links after nic delete = %#v, want none", reloaded.NetworkLinks)
	}
}

func TestCommandContainerNICDeleteRemovesDirectLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Networks: []lab.VMNetwork{{}}}},
		Containers: []lab.Container{{
			ID:       "web",
			Image:    "nginx",
			Networks: []lab.ContainerNetwork{{}, {}},
		}},
		NetworkLinks: []lab.NetworkLink{{
			From: lab.NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 0},
			To:   lab.NetworkEndpoint{Type: "container", ID: "web", NIC: 1},
		}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("container nic rm web 1")

	if app.State.Message != "deleted nic from container:web nic1" {
		t.Fatalf("message = %q", app.State.Message)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Containers[0].Networks) != 1 {
		t.Fatalf("container networks = %#v, want one nic", reloaded.Containers[0].Networks)
	}
	if len(reloaded.NetworkLinks) != 0 {
		t.Fatalf("network links after nic delete = %#v, want none", reloaded.NetworkLinks)
	}
}

func TestCommandLinkReportsUsageForInvalidEndpoint(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	app.executeCommand("link add vm1 to=vm:vm2")

	if app.State.Message != "usage: link add <vm|container>:<id>:<nic> to=<vm|container>:<id>[:nic]" {
		t.Fatalf("message = %q", app.State.Message)
	}
}

func TestCommandLinkUnsupportedArgumentErrorIsDeterministic(t *testing.T) {
	app := App{}
	for i := 0; i < 100; i++ {
		app.executeCommand("link add vm:vm1:0 to=vm:vm2:0 zzz=1 aaa=2")
		if got, want := app.State.Message, "unsupported link add argument: aaa"; got != want {
			t.Fatalf("unsupported argument error = %q, want %q", got, want)
		}
	}
}

func TestCommandReportsUnterminatedQuote(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	app.executeCommand(`vm set vm1 name="unterminated`)
	if !strings.Contains(app.State.Message, "unterminated quote") {
		t.Fatalf("message = %q, want unterminated quote", app.State.Message)
	}
}

func TestCommandMissingRequiredIDReportsUsage(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"vm start", "usage: vm start <id>"},
		{"vm run", "usage: vm start <id>"},
		{"vm stop", "usage: vm stop <id>"},
		{"vm delete", "usage: vm delete <id>"},
		{"vm rm", "usage: vm delete <id>"},
		{"vm nic delete vm1", "usage: vm nic delete <id> <index>"},
		{"container start", "usage: container start <id>"},
		{"ct run", "usage: container start <id>"},
		{"container stop", "usage: container stop <id>"},
		{"container delete", "usage: container delete <id>"},
		{"ct rm", "usage: container delete <id>"},
		{"container nic delete web", "usage: container nic delete <id> <index>"},
		{"switch delete", "usage: switch delete <id>"},
		{"sw rm", "usage: switch delete <id>"},
		{"external delete", "usage: uplink delete <id>"},
		{"ext rm", "usage: uplink delete <id>"},
		{"disk merge", "usage: disk merge <id>"},
		{"disk resize", "usage: disk resize <id> size=N [force=true]"},
		{"disk resize data", "usage: disk resize <id> size=N [force=true]"},
		{"disk info", "usage: disk info <id>"},
		{"disk rename", "usage: disk rename <id> <new-id>"},
		{"disk rename data", "usage: disk rename <id> <new-id>"},
		{"disk delete", "usage: disk delete <id>"},
		{"disk rm", "usage: disk delete <id>"},
	}

	for _, tt := range tests {
		app := App{Model: MockModel(), Lab: &lab.Lab{ID: "demo"}, State: ViewState{Focus: FocusGraph}}
		app.executeCommand(tt.command)
		if app.State.Message != tt.want {
			t.Fatalf("%q message = %q, want %q", tt.command, app.State.Message, tt.want)
		}
	}
}

func TestCommandExtraArgsForFixedArityCommandsDoNotMutate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:           "vm1",
			MemoryMB:     2048,
			CPUs:         2,
			DesiredState: lab.DesiredStateStopped,
			Networks:     []lab.VMNetwork{{}},
		}},
		Containers: []lab.Container{{
			ID:           "web",
			Image:        "nginx",
			DesiredState: lab.DesiredStateStopped,
			Networks:     []lab.ContainerNetwork{{}},
		}},
		Switches:      []lab.Switch{{ID: "sw1", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink", Interface: "eth0"}},
		NetworkLinks: []lab.NetworkLink{{
			From: lab.NetworkEndpoint{Type: "vm", ID: "vm1", NIC: 0},
			To:   lab.NetworkEndpoint{Type: "container", ID: "web", NIC: 0},
		}},
		Disks: []lab.Disk{
			{ID: "data", Path: "disks/data.qcow2", Format: "qcow2", Kind: "base"},
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
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}
	tests := []struct {
		command string
		want    string
	}{
		{"vm start vm1 extra", "usage: vm start <id>"},
		{"vm stop vm1 extra", "usage: vm stop <id>"},
		{"vm delete vm1 extra", "usage: vm delete <id>"},
		{"vm nic delete vm1 0 extra", "usage: vm nic delete <id> <index>"},
		{"container start web extra", "usage: container start <id>"},
		{"container stop web extra", "usage: container stop <id>"},
		{"container delete web extra", "usage: container delete <id>"},
		{"container nic delete web 0 extra", "usage: container nic delete <id> <index>"},
		{"switch delete sw1 extra", "usage: switch delete <id>"},
		{"external delete uplink extra", "usage: uplink delete <id>"},
		{"link delete vm:vm1:0 extra", "usage: link delete <vm|container>:<id>:<nic>"},
		{"disk merge data-layer extra", "usage: disk merge <id>"},
		{"disk info data extra", "usage: disk info <id>"},
		{"disk rename data new extra", "usage: disk rename <id> <new-id>"},
		{"disk delete data extra", "usage: disk delete <id>"},
		{"disk layer delete data-layer extra", "usage: disk layer create <base-id> <layer-id> | disk layer delete <id>"},
	}
	for _, tt := range tests {
		app.executeCommand(tt.command)
		if app.State.Message != tt.want {
			t.Fatalf("%q message = %q, want %q", tt.command, app.State.Message, tt.want)
		}
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.VMs) != 1 || reloaded.VMs[0].DesiredState != lab.DesiredStateStopped {
		t.Fatalf("vm mutated after rejected commands: %#v", reloaded.VMs)
	}
	if len(reloaded.Containers) != 1 || reloaded.Containers[0].DesiredState != lab.DesiredStateStopped {
		t.Fatalf("container mutated after rejected commands: %#v", reloaded.Containers)
	}
	if len(reloaded.Switches) != 1 || len(reloaded.ExternalLinks) != 1 || len(reloaded.NetworkLinks) != 1 || len(reloaded.Disks) != 2 {
		t.Fatalf("lab mutated after rejected commands: switches=%#v externals=%#v links=%#v disks=%#v", reloaded.Switches, reloaded.ExternalLinks, reloaded.NetworkLinks, reloaded.Disks)
	}
}

func TestCommandVMDeleteRemovesVM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("vm delete vm1")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.VMs) != 0 {
		t.Fatalf("vms = %#v", reloaded.VMs)
	}
}

func TestCommandSwitchAndExternalCreateSetDeleteSaveLab(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.executeCommand("external create uplink1 interface=br0 mode=macnat")
	app.executeCommand("add sw lan mode=bridge external=uplink1")
	app.executeCommand("switch set lan mode=nat external=uplink1")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ExternalLinks) != 1 || reloaded.ExternalLinks[0].ID != "uplink1" || reloaded.ExternalLinks[0].Name != "" {
		t.Fatalf("external links = %#v", reloaded.ExternalLinks)
	}
	if reloaded.ExternalLinks[0].Mode != lab.ExternalModeMacNAT {
		t.Fatalf("external mode = %q, want macnat", reloaded.ExternalLinks[0].Mode)
	}
	if len(reloaded.Switches) != 1 || reloaded.Switches[0].ID != "lan" || reloaded.Switches[0].Name != "" || reloaded.Switches[0].Mode != "nat" || !reflect.DeepEqual(lab.SwitchExternalLinks(reloaded.Switches[0]), []string{reloaded.ExternalLinks[0].ID}) {
		t.Fatalf("switches = %#v", reloaded.Switches)
	}

	app.executeCommand("switch delete lan")
	app.executeCommand("external delete uplink1")
	reloaded, err = lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Switches) != 0 || len(reloaded.ExternalLinks) != 0 {
		t.Fatalf("resources not deleted: switches=%#v external=%#v", reloaded.Switches, reloaded.ExternalLinks)
	}
}

func TestContextMenuGlobalCreateCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.lab")
	loaded := &lab.Lab{ID: "empty"}
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

	app.runGlobalMenuAction("add vm")
	if len(app.Lab.VMs) != 1 || app.Lab.VMs[0].ID != "vm-1" || app.Lab.VMs[0].Name != "" {
		t.Fatalf("vms after global add = %#v", app.Lab.VMs)
	}

	app.runGlobalMenuAction("add sw")
	if len(app.Lab.Switches) != 1 || app.Lab.Switches[0].ID == "" {
		t.Fatalf("switches after global add = %#v", app.Lab.Switches)
	}

	app.runGlobalMenuAction("add cont")
	if len(app.Lab.Containers) != 1 || app.Lab.Containers[0].ID == "" {
		t.Fatalf("containers after global add = %#v", app.Lab.Containers)
	}

	app.runGlobalMenuAction("create external")
	if len(app.Lab.ExternalLinks) != 1 || app.Lab.ExternalLinks[0].ID == "" {
		t.Fatalf("external links after global add = %#v", app.Lab.ExternalLinks)
	}
}

func TestContextMenuActionsOpenPrefilledCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			Name:     "web-server",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     "labs/demo/disks/web server.qcow2",
			ISO:      "images/debian 12.iso",
			Networks: []lab.VMNetwork{{}},
		}},
		Containers:    []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", Networks: []lab.ContainerNetwork{{}}}},
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Name: "office-uplink", Interface: "enp 1s0"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model: MockModel(),
		Lab:   loaded,
		Runtime: &fakeVMRuntime{states: map[string]string{
			NodeKey(NodeVM, loaded.VMs[0].ID):               "running",
			NodeKey(NodeContainer, loaded.Containers[0].ID): "running",
		}},
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
		VMConsole: func(_ context.Context, _ *lab.Lab, id string) (io.ReadWriteCloser, string, error) {
			if id != loaded.VMs[0].ID {
				t.Fatalf("console id = %q", id)
			}
			return &fakeConsole{}, "vm console /dev/pts/7", nil
		},
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "edit")
	if app.State.Message != "edit fields from Configuration" {
		t.Fatalf("edit message = %q", app.State.Message)
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "iso")
	if app.State.Message != "command bar removed; use the menu" && app.State.Message != "edit fields from Configuration" {
		t.Fatalf("iso message = %q", app.State.Message)
	}

	app.runMenuAction(Node{ID: "uplink1", Type: NodeExternal}, "interface")
	if app.State.Message != "choose interface from Configuration" {
		t.Fatalf("interface message = %q", app.State.Message)
	}

	app.runMenuAction(Node{ID: "uplink1", Type: NodeExternal}, "name")
	if app.State.Message != "edit name from Configuration" {
		t.Fatalf("name message = %q", app.State.Message)
	}

	uplinkMenu := contextMenuItems(Node{ID: "uplink1", Type: NodeExternal}, "")
	if !reflect.DeepEqual(uplinkMenu, []string{"Configuration >", "Connect", "Move", "Delete"}) {
		t.Fatalf("uplink context menu = %#v", uplinkMenu)
	}

	app.runMenuAction(Node{ID: loaded.Switches[0].ID, Type: NodeSwitch}, "add vm")
	foundSwitchVM := false
	for _, vm := range app.Lab.VMs {
		if vm.Name != "web-server" && len(vm.Networks) > 0 && vm.Networks[0].Switch == loaded.Switches[0].ID {
			foundSwitchVM = true
		}
	}
	if !foundSwitchVM {
		t.Fatalf("vms after switch add vm = %#v", app.Lab.VMs)
	}

	vmNICsBefore := len(app.Lab.VMs[0].Networks)
	app.runMenuAction(Node{ID: loaded.VMs[0].ID, Type: NodeVM}, "add-nic")
	if len(app.Lab.VMs[0].Networks) != vmNICsBefore+1 {
		t.Fatalf("vm nics after add-nic = %#v", app.Lab.VMs[0].Networks)
	}

	app.runMenuAction(Node{ID: loaded.VMs[0].ID, Type: NodeVM}, "connect-nic:0")
	if !app.State.ConnectMode || app.State.ConnectNodeID != loaded.VMs[0].ID || app.State.ConnectNICIndex != "0" {
		t.Fatalf("vm connect-nic state = %#v", app.State)
	}
	app.State.ConnectMode = false

	app.runMenuAction(Node{ID: loaded.VMs[0].ID, Type: NodeVM}, "shell")
	if app.PendingShell == nil {
		t.Fatal("vm shell did not set pending shell")
	}
	if got := app.Runtime.(*fakeVMRuntime).starts; got != 0 {
		t.Fatalf("vm shell started workload %d times", got)
	}
	if app.PendingShell.Console == nil || app.PendingShell.Display != "vm console /dev/pts/7" {
		t.Fatalf("vm shell command = %#v", app.PendingShell)
	}
	app.PendingShell = nil

	containerNICsBefore := len(app.Lab.Containers[0].Networks)
	app.runMenuAction(Node{ID: loaded.Containers[0].ID, Type: NodeContainer}, "add-nic")
	if len(app.Lab.Containers[0].Networks) != containerNICsBefore+1 {
		t.Fatalf("container nics after add-nic = %#v", app.Lab.Containers[0].Networks)
	}

	app.runMenuAction(Node{ID: loaded.Containers[0].ID, Type: NodeContainer}, "connect-nic:0")
	if !app.State.ConnectMode || app.State.ConnectNodeID != loaded.Containers[0].ID || app.State.ConnectNICIndex != "0" {
		t.Fatalf("container connect-nic state = %#v", app.State)
	}
	app.State.ConnectMode = false

	app.runMenuAction(Node{ID: loaded.Containers[0].ID, Type: NodeContainer}, "shell")
	if app.PendingShell == nil {
		t.Fatal("container shell did not set pending shell")
	}
	if got := app.Runtime.(*fakeVMRuntime).starts; got != 0 {
		t.Fatalf("container shell started workload %d times", got)
	}
	if app.PendingShell.NativeRun == nil || app.PendingShell.Console != nil || !strings.Contains(app.PendingShell.Display, "foxlab-demo-"+loaded.Containers[0].ID) {
		t.Fatalf("container shell command = %#v", app.PendingShell)
	}
}

func TestContextMenuVNCActionUsesExistingRuntimePort(t *testing.T) {
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, VNC: true}},
	}
	app := App{
		Model:     ModelFromLab(loaded),
		Lab:       loaded,
		VNCPorts:  map[string]int{NodeKey(NodeVM, "vm1"): 5905},
		VNCViewer: "/bin/true",
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "vnc")
	if app.PendingVNC == nil {
		t.Fatal("vnc action did not set pending vnc")
	}
	if app.PendingVNC.Display != "vnc 127.0.0.1::5905" {
		t.Fatalf("vnc display = %q", app.PendingVNC.Display)
	}
	if err := app.runShell(*app.PendingVNC); err != nil {
		t.Fatalf("vnc viewer command failed: %v", err)
	}
}

func TestRunVNCDirectUsesExistingRuntimePort(t *testing.T) {
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, VNC: true}},
	}
	app := App{
		Model:     ModelFromLab(loaded),
		Lab:       loaded,
		VNCPorts:  map[string]int{NodeKey(NodeVM, "vm1"): 5905},
		VNCViewer: "/bin/true",
	}

	if err := app.RunVNC("vm1"); err != nil {
		t.Fatalf("RunVNC returned error: %v", err)
	}
	if app.PendingVNC != nil {
		t.Fatalf("RunVNC left pending vnc: %#v", app.PendingVNC)
	}
}

func TestContextMenuVNCActionRefreshesPortWithoutStartingVM(t *testing.T) {
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, VNC: true}},
	}
	runtime := &fakeVMRuntime{
		states:   map[string]string{NodeKey(NodeVM, "vm1"): "shutoff"},
		vncPorts: map[string]int{NodeKey(NodeVM, "vm1"): 5906},
	}
	app := App{
		Model:     ModelFromLab(loaded),
		Lab:       loaded,
		Runtime:   runtime,
		VNCViewer: "/bin/true",
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "vnc")
	if runtime.started != "" {
		t.Fatalf("vnc action started %q, want no direct TUI reconcile", runtime.started)
	}
	if app.PendingVNC == nil || app.PendingVNC.Display != "vnc 127.0.0.1::5906" {
		t.Fatalf("vnc command = %#v", app.PendingVNC)
	}
}

func TestContextMenuVNCActionUsesPortWhenStateRefreshFails(t *testing.T) {
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, VNC: true}},
	}
	runtime := &fakeVMRuntime{
		statesErr: errors.New("containerd unavailable"),
		vncPorts:  map[string]int{NodeKey(NodeVM, "vm1"): 5907},
	}
	app := App{
		Model:     ModelFromLab(loaded),
		Lab:       loaded,
		Runtime:   runtime,
		VNCViewer: "/bin/true",
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "vnc")
	if runtime.started != "" {
		t.Fatalf("vnc action started %q, want no direct TUI reconcile", runtime.started)
	}
	if app.PendingVNC == nil || app.PendingVNC.Display != "vnc 127.0.0.1::5907" {
		t.Fatalf("vnc command = %#v message=%q", app.PendingVNC, app.State.Message)
	}
}

func TestRefreshVNCWorkloadStatusUsesTimeoutContext(t *testing.T) {
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, VNC: true}},
	}
	runtime := &deadlineRuntime{}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		Runtime: runtime,
	}

	if err := app.refreshVNCWorkloadStatus(Node{ID: "vm1", Type: NodeVM}); err != nil {
		t.Fatalf("refreshVNCWorkloadStatus returned error: %v", err)
	}
	if _, ok := runtime.statesCtx.Deadline(); !ok {
		t.Fatal("runtime States context had no deadline")
	}
	if _, ok := runtime.vncCtx.Deadline(); !ok {
		t.Fatal("runtime VNCPorts context had no deadline")
	}
	if got := app.VNCPorts[NodeKey(NodeVM, "vm1")]; got != 5908 {
		t.Fatalf("VNC port = %d, want 5908", got)
	}
}

func TestContextMenuVNCActionRejectsDisabledVNC(t *testing.T) {
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, VNC: false}},
	}
	app := App{
		Model:     ModelFromLab(loaded),
		Lab:       loaded,
		VNCViewer: "/bin/true",
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "vnc")
	if app.PendingVNC != nil {
		t.Fatalf("disabled VNC set pending command: %#v", app.PendingVNC)
	}
	if !strings.Contains(app.State.Message, "vnc is disabled") {
		t.Fatalf("disabled VNC message = %q", app.State.Message)
	}
}

func TestContextMenuVNCActionReportsRestartNeededWithoutPort(t *testing.T) {
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, VNC: true}},
	}
	app := App{
		Model: Model{Nodes: []Node{{
			ID:      "vm1",
			Type:    NodeVM,
			State:   "running",
			Details: []string{"vnc=true"},
		}}},
		Lab: loaded,
		Runtime: &fakeVMRuntime{
			states: map[string]string{NodeKey(NodeVM, "vm1"): "running"},
		},
		VNCViewer: "/bin/true",
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "vnc")
	if app.PendingVNC != nil {
		t.Fatalf("missing VNC port set pending command: %#v", app.PendingVNC)
	}
	if !strings.Contains(app.State.Message, "vnc needs restart") {
		t.Fatalf("missing VNC port message = %q", app.State.Message)
	}
}

func TestConnectNICModeSelectsEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
		VMs:      []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2", Networks: []lab.VMNetwork{{}}}},
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.runMenuAction(Node{ID: loaded.VMs[0].ID, Type: NodeVM}, "connect-nic:0")
	if !app.State.ConnectMode {
		t.Fatalf("connect mode not started: %#v", app.State)
	}
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok || node.ID != loaded.Switches[0].ID || node.Label != "lan" || node.Type != NodeSwitch {
		t.Fatalf("selected endpoint = %#v, ok=%t", node, ok)
	}

	app.handleKey("enter")
	if app.State.ConnectMode {
		t.Fatal("connect mode did not finish after selecting endpoint")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Networks[0].Switch != reloaded.Switches[0].ID {
		t.Fatalf("vm networks = %#v", reloaded.VMs[0].Networks)
	}
}

func TestConnectContainerNICModeSelectsExternalEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		Containers:    []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", Networks: []lab.ContainerNetwork{{}}}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "br0"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.runMenuAction(Node{ID: loaded.Containers[0].ID, Type: NodeContainer}, "connect-nic:0")
	if !app.State.ConnectMode {
		t.Fatalf("connect mode not started: %#v", app.State)
	}
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok || node.Type != NodeExternal {
		t.Fatalf("selected endpoint = %#v, ok=%t", node, ok)
	}

	app.handleKey("enter")
	if app.State.ConnectMode {
		t.Fatal("connect mode did not finish after selecting external endpoint")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Containers[0].Networks[0].ExternalLink != reloaded.ExternalLinks[0].ID {
		t.Fatalf("container networks = %#v", reloaded.Containers[0].Networks)
	}
	if !hasEdge(app.Model, NodeKey(NodeContainer, reloaded.Containers[0].ID), NodeKey(NodeExternal, reloaded.ExternalLinks[0].ID)) {
		t.Fatalf("model edges = %#v", app.Model.Edges)
	}
}

func TestConnectSwitchModeSelectsExternalEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "br0"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.runMenuAction(Node{ID: loaded.Switches[0].ID, Type: NodeSwitch}, "connect")
	if !app.State.ConnectMode {
		t.Fatalf("connect mode not started: %#v", app.State)
	}
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok || node.Type != NodeExternal {
		t.Fatalf("selected endpoint = %#v, ok=%t", node, ok)
	}

	app.handleKey("enter")
	if app.State.ConnectMode {
		t.Fatal("connect mode did not finish after selecting external endpoint")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lab.SwitchExternalLinks(reloaded.Switches[0]), []string{reloaded.ExternalLinks[0].ID}) {
		t.Fatalf("switches = %#v", reloaded.Switches)
	}
	if !hasEdge(app.Model, NodeKey(NodeSwitch, reloaded.Switches[0].ID), NodeKey(NodeExternal, reloaded.ExternalLinks[0].ID)) {
		t.Fatalf("model edges = %#v", app.Model.Edges)
	}
}

func TestSwitchUplinkSubmenuConnectsExistingExternal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "br0"}},
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
		State: ViewState{
			Focus:            FocusGraph,
			ContextMenu:      true,
			ContextSelected:  1,
			ContextGroup:     "uplink-menu",
			ContextInSubmenu: true,
		},
	}

	app.handleKey("enter")

	if app.State.ContextMenu {
		t.Fatal("context menu stayed open after choosing uplink")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lab.SwitchExternalLinks(reloaded.Switches[0]), []string{reloaded.ExternalLinks[0].ID}) {
		t.Fatalf("switches = %#v", reloaded.Switches)
	}
	if !hasEdge(app.Model, NodeKey(NodeSwitch, reloaded.Switches[0].ID), NodeKey(NodeExternal, reloaded.ExternalLinks[0].ID)) {
		t.Fatalf("model edges = %#v", app.Model.Edges)
	}
}

func TestSwitchUplinkSubmenuAppendsSecondExternal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge", ExternalLinks: []string{"uplink1"}}},
		ExternalLinks: []lab.ExternalLink{
			{ID: "uplink1", Interface: "br0"},
			{ID: "uplink2", Interface: "br1"},
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
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State: ViewState{
			Focus:            FocusGraph,
			ContextMenu:      true,
			ContextSelected:  1,
			ContextGroup:     "uplink-menu",
			ContextInSubmenu: true,
		},
	}

	app.handleKey("enter")

	if app.State.ContextMenu {
		t.Fatal("context menu stayed open after choosing uplink")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := lab.SwitchExternalLinks(reloaded.Switches[0]), []string{reloaded.ExternalLinks[0].ID, reloaded.ExternalLinks[1].ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("switch externalLinks = %#v, want %#v", got, want)
	}
	for _, externalID := range []string{reloaded.ExternalLinks[0].ID, reloaded.ExternalLinks[1].ID} {
		if !hasEdge(app.Model, NodeKey(NodeSwitch, reloaded.Switches[0].ID), NodeKey(NodeExternal, externalID)) {
			t.Fatalf("model edges missing %s edge: %#v", externalID, app.Model.Edges)
		}
	}
}

func TestSwitchUplinkSubmenuMultipleExternalsStartsConnectMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{
			{ID: "uplink1", Interface: "br0"},
			{ID: "uplink2", Interface: "br1"},
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
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State: ViewState{
			Focus:            FocusGraph,
			ContextMenu:      true,
			ContextSelected:  1,
			ContextGroup:     "uplink-menu",
			ContextInSubmenu: true,
		},
	}

	app.handleKey("enter")

	if app.State.ContextMenu {
		t.Fatal("context menu stayed open after starting uplink selection")
	}
	if !app.State.ConnectMode || app.State.ConnectNodeType != NodeSwitch || app.State.ConnectNodeID != loaded.Switches[0].ID {
		t.Fatalf("connect mode not started for switch: %#v", app.State)
	}
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok || node.Type != NodeExternal {
		t.Fatalf("selected endpoint = %#v, ok=%t", node, ok)
	}

	uplink2Index := -1
	for i, node := range app.Model.Nodes {
		if node.Type == NodeExternal && node.ID == loaded.ExternalLinks[1].ID {
			uplink2Index = i
			break
		}
	}
	if uplink2Index < 0 {
		t.Fatalf("uplink2 missing from model: %#v", app.Model.Nodes)
	}
	app.State.Selected = uplink2Index
	app.handleKey("enter")

	if app.State.ConnectMode {
		t.Fatal("connect mode did not finish after selecting external endpoint")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lab.SwitchExternalLinks(reloaded.Switches[0]), []string{reloaded.ExternalLinks[1].ID}) {
		t.Fatalf("switches = %#v", reloaded.Switches)
	}
	if !hasEdge(app.Model, NodeKey(NodeSwitch, reloaded.Switches[0].ID), NodeKey(NodeExternal, reloaded.ExternalLinks[1].ID)) {
		t.Fatalf("model edges = %#v", app.Model.Edges)
	}
}

func TestSwitchUplinkSubmenuDoesNotListDisconnectedExternal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "br0"}},
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
		State: ViewState{
			Focus:            FocusGraph,
			ContextMenu:      true,
			ContextSelected:  1,
			ContextGroup:     "uplink-menu",
			ContextInSubmenu: true,
		},
	}

	items := app.contextMenuSubmenuItems(app.Model.Nodes[0], true)
	if len(items) != 1 || items[0] != attachUplinkMenuItem {
		t.Fatalf("switch uplink menu items = %#v, want only Attach Uplink", items)
	}
}

func TestSwitchUplinkSubmenuAttachDisabledWithoutExternal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
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
		ViewWidth:  100,
		ViewHeight: 30,
		State: ViewState{
			Focus:            FocusGraph,
			ContextMenu:      true,
			ContextSelected:  1,
			ContextGroup:     "uplink-menu",
			ContextInSubmenu: true,
		},
	}

	layout, _, _, ok := app.currentContextMenuLayout()
	if !ok || !layout.hasSub || len(layout.sub.items) == 0 {
		t.Fatalf("uplink submenu layout missing: %#v", layout)
	}
	if layout.sub.items[0].Label != attachUplinkMenuItem || layout.sub.items[0].Enabled {
		t.Fatalf("attach item = %#v, want disabled Attach Uplink", layout.sub.items[0])
	}

	app.handleKey("enter")
	if !app.State.ContextMenu {
		t.Fatal("disabled attach closed the menu")
	}
	if app.State.Message != "no uplink available" {
		t.Fatalf("message = %q", app.State.Message)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lab.SwitchExternalLinks(reloaded.Switches[0])) != 0 {
		t.Fatalf("switches = %#v", reloaded.Switches)
	}
}

func TestSwitchUplinkSubmenuXDisconnectsExternal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge", ExternalLinks: []string{"uplink1", "uplink2"}}},
		ExternalLinks: []lab.ExternalLink{
			{ID: "uplink1", Name: "Internet", Interface: "br0"},
			{ID: "uplink2", Name: "Wireguard", Interface: "br1"},
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
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State: ViewState{
			Focus:               FocusGraph,
			ContextMenu:         true,
			ContextGroup:        "uplink-menu",
			ContextInSubmenu:    true,
			ContextSubSelected:  1,
			ContextDeleteUplink: true,
		},
	}

	app.handleKey("enter")

	if app.State.ContextMenu {
		t.Fatal("context menu stayed open after disconnecting uplink")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ExternalLinks) != 2 {
		t.Fatalf("external link was deleted instead of detached: %#v", reloaded.ExternalLinks)
	}
	if got, want := lab.SwitchExternalLinks(reloaded.Switches[0]), []string{reloaded.ExternalLinks[1].ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("switches = %#v", reloaded.Switches)
	}
	if hasEdge(app.Model, NodeKey(NodeSwitch, reloaded.Switches[0].ID), NodeKey(NodeExternal, reloaded.ExternalLinks[0].ID)) {
		t.Fatalf("model still has switch-uplink edge = %#v", app.Model.Edges)
	}
	if !hasEdge(app.Model, NodeKey(NodeSwitch, reloaded.Switches[0].ID), NodeKey(NodeExternal, reloaded.ExternalLinks[1].ID)) {
		t.Fatalf("model lost remaining switch-uplink edge = %#v", app.Model.Edges)
	}

	switchNode, ok := nodeByKey(app.Model, NodeKey(NodeSwitch, reloaded.Switches[0].ID))
	if !ok {
		t.Fatal("switch node missing after detach")
	}
	app.State.ContextMenu = true
	app.State.ContextSelected = 1
	app.State.ContextGroup = "uplink-menu"
	app.State.ContextInSubmenu = true
	app.State.ContextSubSelected = 1
	layout, _, _, ok := app.currentContextMenuLayout()
	if !ok || !layout.hasSub || len(layout.sub.items) != 2 {
		t.Fatalf("switch uplink menu layout after detach = %#v", layout)
	}
	if layout.sub.items[1].Label != "Wireguard" || layout.sub.items[1].Action != "uplink:Wireguard" {
		t.Fatalf("switch uplink menu item after detach = %#v", layout.sub.items[1])
	}
	if switchNode.ID != reloaded.Switches[0].ID || switchNode.Label != "lan" {
		t.Fatalf("switch node = %#v", switchNode)
	}
}

func TestConnectedExternalConnectMenuItemIsDisabled(t *testing.T) {
	app := App{
		Model:      MockModel(),
		ViewWidth:  100,
		ViewHeight: 30,
		State: ViewState{
			Focus:       FocusGraph,
			Selected:    5,
			ContextMenu: true,
		},
	}

	layout, node, ok, layoutOK := app.currentContextMenuLayout()
	if !layoutOK || !ok {
		t.Fatal("context menu layout missing")
	}
	if node.ID != "uplink0" || node.Type != NodeExternal {
		t.Fatalf("selected node = %#v, want uplink0 external", node)
	}
	foundConnect := false
	for _, item := range layout.root.items {
		if item.Action != "connect" {
			continue
		}
		foundConnect = true
		if item.Enabled {
			t.Fatalf("connected uplink Connect item should be disabled: %#v", layout.root.items)
		}
	}
	if !foundConnect {
		t.Fatalf("connected uplink menu has no Connect item: %#v", layout.root.items)
	}
}

func TestConnectedExternalConnectActionDoesNotStartConnectMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge", ExternalLink: "uplink1"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "br0"}},
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
		State: ViewState{
			Focus:           FocusGraph,
			Selected:        1,
			ContextMenu:     true,
			ContextSelected: 1,
		},
	}

	app.handleKey("enter")

	if app.State.ConnectMode {
		t.Fatal("connected uplink started connect mode")
	}
	if !app.State.ContextMenu {
		t.Fatal("disabled connect action closed the context menu")
	}
	if app.State.Message != "uplink already connected: br0" {
		t.Fatalf("message = %q", app.State.Message)
	}
}

func TestConnectExternalModeSelectsSwitchEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "br0"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.runMenuAction(Node{ID: loaded.ExternalLinks[0].ID, Type: NodeExternal}, "connect")
	if !app.State.ConnectMode {
		t.Fatalf("connect mode not started: %#v", app.State)
	}
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok || node.ID != loaded.Switches[0].ID || node.Label != "lan" || node.Type != NodeSwitch {
		t.Fatalf("selected endpoint = %#v, ok=%t", node, ok)
	}

	app.handleKey("enter")
	if app.State.ConnectMode {
		t.Fatal("connect mode did not finish after selecting switch endpoint")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lab.SwitchExternalLinks(reloaded.Switches[0]), []string{reloaded.ExternalLinks[0].ID}) {
		t.Fatalf("switches = %#v", reloaded.Switches)
	}
	if !hasEdge(app.Model, NodeKey(NodeSwitch, reloaded.Switches[0].ID), NodeKey(NodeExternal, reloaded.ExternalLinks[0].ID)) {
		t.Fatalf("model edges = %#v", app.Model.Edges)
	}
}

func TestNICSubmenuNICDetailStartsConnectMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
		VMs:      []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2", Networks: []lab.VMNetwork{{}}}},
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
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		LabPath: path,
		State: ViewState{
			Focus:              FocusGraph,
			ContextMenu:        true,
			ContextGroup:       "nic-menu",
			ContextInSubmenu:   true,
			ContextSubSelected: 1,
		},
	}

	app.handleKey("enter")
	if !app.State.ConnectMode || app.State.ConnectNodeID != loaded.VMs[0].ID || app.State.ConnectNICIndex != "0" {
		t.Fatalf("nic detail did not start connect mode for nic0: %#v", app.State)
	}
	if app.State.ContextMenu {
		t.Fatal("context menu stayed open after choosing nic detail")
	}
}

func TestNICSubmenuDeleteXRemovesNIC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     "labs/demo/disks/vm1.qcow2",
			Networks: []lab.VMNetwork{{}, {}},
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
		State: ViewState{
			Focus:              FocusGraph,
			ContextMenu:        true,
			ContextGroup:       "nic-menu",
			ContextInSubmenu:   true,
			ContextSubSelected: 1,
		},
	}

	app.handleKey("right")
	if !app.State.ContextDeleteNIC {
		t.Fatal("right on nic detail did not select delete X")
	}
	app.handleKey("enter")
	if app.State.ContextMenu {
		t.Fatal("context menu stayed open after deleting nic")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.VMs[0].Networks) != 1 {
		t.Fatalf("vm networks = %#v, want one nic after delete", reloaded.VMs[0].Networks)
	}
}

func TestConnectNICDetachThenEscapeLeavesNICEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:       "demo",
		VMs:      []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2", Networks: []lab.VMNetwork{{Switch: "lan"}}}},
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}, {ID: "wan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.runMenuAction(Node{ID: loaded.VMs[0].ID, Type: NodeVM}, "connect-nic:0")
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Networks[0].Switch != "" || reloaded.VMs[0].Networks[0].ExternalLink != "" {
		t.Fatalf("source nic was not detached before endpoint selection: %#v", reloaded.VMs[0].Networks[0])
	}

	app.handleKey("escape")
	reloaded, err = lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if app.State.ConnectMode {
		t.Fatal("connect mode stayed active after escape")
	}
	if reloaded.VMs[0].Networks[0].Switch != "" || reloaded.VMs[0].Networks[0].ExternalLink != "" {
		t.Fatalf("source nic was reconnected after cancel: %#v", reloaded.VMs[0].Networks[0])
	}
}

func TestConnectNICModeCreatesDirectWorkloadLink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2", Networks: []lab.VMNetwork{{}}},
			{ID: "vm2", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm2.qcow2", Networks: []lab.VMNetwork{{Switch: "lan"}, {}}},
		},
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "connect-nic:0")
	if !app.State.ConnectMode {
		t.Fatalf("connect mode not started: %#v", app.State)
	}
	app.State.Selected = nodeIndexByLabel(t, app.Model, NodeVM, "vm2")
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok || node.ID != loaded.VMs[1].ID || node.Label != "vm2" || node.Type != NodeVM {
		t.Fatalf("selected endpoint = %#v, ok=%t", node, ok)
	}

	app.handleKey("enter")
	if !app.State.ConnectMode || !app.State.ConnectTargetMenu || app.State.ConnectTargetID != loaded.VMs[1].ID {
		t.Fatalf("target nic menu did not open after selecting workload endpoint: %#v", app.State)
	}
	app.State.ConnectTargetIndex = 1
	app.handleKey("enter")
	if app.State.ConnectMode || app.State.ConnectTargetMenu {
		t.Fatal("connect mode did not finish after selecting target nic")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.NetworkLinks) != 1 {
		t.Fatalf("network links = %#v", reloaded.NetworkLinks)
	}
	link := reloaded.NetworkLinks[0]
	if link.From.Type != "vm" || link.From.ID != reloaded.VMs[0].ID || link.From.NIC != 0 || link.To.Type != "vm" || link.To.ID != reloaded.VMs[1].ID || link.To.NIC != 1 {
		t.Fatalf("network link = %#v", link)
	}
	if !hasEdge(app.Model, NodeKey(NodeVM, reloaded.VMs[0].ID), NodeKey(NodeVM, reloaded.VMs[1].ID)) {
		t.Fatalf("model edges = %#v", app.Model.Edges)
	}
}

func TestConnectNICModeCanCreateTargetNIC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.qcow2", Networks: []lab.VMNetwork{{}}},
			{ID: "vm2", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm2.qcow2"},
		},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.runMenuAction(Node{ID: loaded.VMs[0].ID, Type: NodeVM}, "connect-nic:0")
	app.State.Selected = nodeIndexByLabel(t, app.Model, NodeVM, "vm2")
	app.handleKey("enter")
	if !app.State.ConnectTargetMenu {
		t.Fatalf("target nic menu not open: %#v", app.State)
	}
	app.handleKey("enter")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.VMs[1].Networks) != 1 {
		t.Fatalf("target networks = %#v, want newly-created nic", reloaded.VMs[1].Networks)
	}
	if len(reloaded.NetworkLinks) != 1 {
		t.Fatalf("network links = %#v", reloaded.NetworkLinks)
	}
	link := reloaded.NetworkLinks[0]
	if link.To.Type != "vm" || link.To.ID != reloaded.VMs[1].ID || link.To.NIC != 0 {
		t.Fatalf("network link target = %#v", link)
	}
}

func TestConnectNICModePreservesTargetNICCreateFailure(t *testing.T) {
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{
			{ID: "vm1", MemoryMB: 2048, CPUs: 2, Networks: []lab.VMNetwork{{}}},
			{ID: "vm2", MemoryMB: 2048, CPUs: 2},
		},
	}
	app := App{
		Model: ModelFromLab(loaded),
		Lab:   loaded,
		State: ViewState{
			Focus:             FocusGraph,
			ConnectMode:       true,
			ConnectTargetMenu: true,
			ConnectNodeID:     "vm1",
			ConnectNodeType:   NodeVM,
			ConnectNICIndex:   "0",
			ConnectTargetID:   "vm2",
			ConnectTargetType: NodeVM,
		},
	}

	app.connectSelectedTargetNIC(Node{ID: "vm2", Type: NodeVM}, "New NIC")

	if app.State.Message != "nic add failed: missing lab path" {
		t.Fatalf("message = %q, want concrete nic add failure", app.State.Message)
	}
	if !app.State.ConnectMode || !app.State.ConnectTargetMenu {
		t.Fatalf("connect mode should stay active after target nic create failure: %#v", app.State)
	}
	if len(app.Lab.VMs[1].Networks) != 0 {
		t.Fatalf("failed target nic create mutated target networks: %#v", app.Lab.VMs[1].Networks)
	}
}

func hasEdge(m Model, from, to string) bool {
	for _, edge := range m.Edges {
		if edge.From == from && edge.To == to {
			return true
		}
	}
	return false
}

func nodeIndexByLabel(t *testing.T, m Model, typ, label string) int {
	t.Helper()
	for i, node := range m.Nodes {
		if node.Type == typ && node.Label == label {
			return i
		}
	}
	t.Fatalf("node %s:%s missing from model: %#v", typ, label, m.Nodes)
	return -1
}
