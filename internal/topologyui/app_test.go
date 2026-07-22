package topologyui

import (
	"bytes"
	"context"
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

func (f *fakeConsole) Resize(_, _ int) {}

func (f *fakeConsole) Wait(context.Context) error { return nil }

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
	mu           sync.Mutex
	states       map[string]string
	vncPorts     map[string]int
	statesErr    error
	started      string
	stopped      string
	starts       int
	stops        int
	openTerminal func(context.Context, *lab.Lab, workload.Ref, workload.TerminalSize) (workload.OpenedTerminalSession, error)
}

func testRuntimeFactory(runtime workload.Runtime) RuntimeFactory {
	return func(*lab.Lab) (workload.Runtime, func(), error) {
		return runtime, func() {}, nil
	}
}

func testRuntimeAccess(runtime workload.Runtime) *runtimeAccess {
	return newRuntimeAccess(testRuntimeFactory(runtime), "", nil)
}

func testRuntimeAccessWithStatus(runtime workload.Runtime, query func(context.Context, string) (daemonstatus.Snapshot, error)) *runtimeAccess {
	return newRuntimeAccess(testRuntimeFactory(runtime), "", query)
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

func (f *fakeVMRuntime) OpenTerminalSession(ctx context.Context, l *lab.Lab, ref workload.Ref, size workload.TerminalSize) (workload.OpenedTerminalSession, error) {
	if f.openTerminal != nil {
		return f.openTerminal(ctx, l, ref, size)
	}
	endpoint := ref.ID
	if ref.Type == workload.TypeContainer && l != nil {
		for _, ct := range l.Containers {
			if ct.ID == ref.ID {
				endpoint = l.ManagedContainerName(ct)
				break
			}
		}
	}
	return workload.OpenedTerminalSession{Session: &fakeConsole{}, Endpoint: endpoint}, nil
}

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

func TestSpaceDoesNotOpenLegacyContextMenu(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{Focus: FocusGraph, Selected: 1},
	}

	app.handleKey("space")
	if app.State.ContextMenu || app.State.ContextGroup != "" || app.State.ContextInSubmenu {
		t.Fatalf("Space opened legacy context menu: %#v", app.State)
	}
}

func TestContextMenuInlineEditQuestionFallbackStartsEmpty(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{Focus: FocusGraph, Selected: 2, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true},
	}

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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, ""), State: ViewState{
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			path), State: ViewState{
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
		Model: model, Session: lab.NewSession(loaded,
			path), State: ViewState{
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
		Model: model, Session: lab.NewSession(loaded,
			path), State: ViewState{
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
		State: ViewState{Focus: FocusGraph, Selected: 1, ContextMenu: true},
	}

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
		Model: model, Session: lab.NewSession(loaded, ""), ViewWidth: 100,
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
	if app.currentLab().VMs[0].Name != "vm1" || app.currentLab().VMs[0].CPUs != 2 {
		t.Fatalf("old edit value was applied to lab: %#v", app.currentLab().VMs[0])
	}
}

func TestContextMenuKeepsShellInInspectorOnly(t *testing.T) {
	foundVMConsole := false
	foundVNC := false
	for _, item := range contextMenuItems(Node{ID: "vm1", Type: NodeVM}, "") {
		if item == "Console" || item == "Shell" {
			foundVMConsole = true
		}
		if item == "VNC" {
			foundVNC = true
		}
	}
	if foundVMConsole {
		t.Fatalf("vm context menu still contains shell action: %#v", contextMenuItems(Node{ID: "vm1", Type: NodeVM}, ""))
	}
	if !foundVNC {
		t.Fatalf("vm context menu missing VNC: %#v", contextMenuItems(Node{ID: "vm1", Type: NodeVM}, ""))
	}
	foundContainerShell := false
	foundContainerVNC := false
	foundPermissions := false
	for _, item := range contextMenuItems(Node{ID: "web", Type: NodeContainer}, "") {
		if item == "Shell" {
			foundContainerShell = true
		}
		if item == "VNC" {
			foundContainerVNC = true
		}
		if item == "Permissions >" {
			foundPermissions = true
		}
	}
	if foundContainerShell {
		t.Fatalf("container context menu still contains Shell: %#v", contextMenuItems(Node{ID: "web", Type: NodeContainer}, ""))
	}
	if foundContainerVNC {
		t.Fatalf("container context menu unexpectedly contains VNC: %#v", contextMenuItems(Node{ID: "web", Type: NodeContainer}, ""))
	}
	if !foundPermissions {
		t.Fatalf("container context menu missing Permissions: %#v", contextMenuItems(Node{ID: "web", Type: NodeContainer}, ""))
	}
	for _, nodeType := range []string{NodeVM, NodeContainer, NodeSwitch, NodeExternal} {
		if containsString(contextMenuItems(Node{ID: "node", Type: nodeType}, ""), "Delete") {
			t.Fatalf("%s context menu still contains Delete: %#v", nodeType, contextMenuItems(Node{ID: "node", Type: nodeType}, ""))
		}
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, ""), runtimeAccess: testRuntimeAccess(&fakeVMRuntime{
			states:   map[string]string{NodeKey(NodeVM, "vm1"): "running"},
			vncPorts: map[string]int{NodeKey(NodeVM, "vm1"): 5903},
		}),
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, ""), runtimeAccess: testRuntimeAccess(runtime),
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, ""), runtimeAccess: testRuntimeAccess(&fakeVMRuntime{
			states: map[string]string{NodeKey(NodeContainer, "kali"): "missing"},
		}),
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			path), State: ViewState{ApplyLabDisabled: true},
		DaemonController: &fakeDaemonController{status: DaemonStatus{Active: true, LabPath: path}},
		runtimeAccess: testRuntimeAccess(&fakeVMRuntime{
			states: map[string]string{NodeKey(NodeContainer, "kali"): "missing"},
		}),
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			path), State: ViewState{Message: "foxlabd status: runtime unavailable"},
		runtimeAccess: testRuntimeAccessWithStatus(
			&fakeVMRuntime{states: map[string]string{NodeKey(NodeContainer, "web"): "missing"}},
			func(context.Context, string) (daemonstatus.Snapshot, error) { return snapshot, nil },
		),
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			path), runtimeAccess: newRuntimeAccess(nil, "", func(context.Context, string) (daemonstatus.Snapshot, error) {
			return daemonstatus.Snapshot{
				LabPath: path,
				LabName: "demo",
				States:  map[string]string{NodeKey(NodeVM, loaded.VMs[0].ID): "missing"},
			}, nil
		}),
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			path), runtimeAccess: testRuntimeAccessWithStatus(
			&fakeVMRuntime{states: map[string]string{NodeKey(NodeContainer, "web"): "missing"}},
			func(_ context.Context, socket string) (daemonstatus.Snapshot, error) {
				queriedSocket = socket
				return daemonstatus.Snapshot{
					LabPath: path,
					LabName: "demo",
					States:  map[string]string{NodeKey(NodeContainer, "web"): "running"},
				}, nil
			},
		),
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
	snapshot := runtimeSnapshot{
		source:          runtimeSnapshotDaemon,
		states:          map[string]string{NodeKey(NodeVM, "vm-id"): "running"},
		statesReceived:  true,
		statesConfirmed: true,
		actions:         []string{"restarted vm:vm-id for configuration change"},
	}
	message, _ := runtimeSnapshotMessage(l, snapshot)
	if message != "foxlabd: restarted vm:router for configuration change" {
		t.Fatalf("message = %q", message)
	}
	if !snapshot.statesConfirmed {
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			path), runtimeAccess: testRuntimeAccessWithStatus(
			&fakeVMRuntime{states: map[string]string{NodeKey(NodeContainer, "web"): "missing"}},
			func(context.Context, string) (daemonstatus.Snapshot, error) { return snapshot, nil },
		),
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, ""), runtimeAccess: testRuntimeAccess(&fakeVMRuntime{
			states: map[string]string{key: " Missing "},
		}),
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			path), State: ViewState{Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true},
	}

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

func TestInspectorMoveSavesLayout(t *testing.T) {
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, path), State: ViewState{Focus: FocusInspector}, ViewWidth: 120, ViewHeight: 30,
	}

	fields := app.selectedInspectorFields()
	moveIndex := inspectorFieldIndex(t, fields, "moveAction", "")
	deleteIndex := inspectorFieldIndex(t, fields, "deleteAction", "")
	if moveIndex+1 != deleteIndex {
		t.Fatalf("Move index = %d Delete index = %d, want Move directly before Delete", moveIndex, deleteIndex)
	}
	app.State.InspectorSelected = moveIndex
	app.handleKey("enter")
	if !app.State.MoveMode {
		t.Fatal("Move inspector action did not enter move mode")
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			path), State: ViewState{Focus: FocusGraph},
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			path), State: ViewState{Focus: FocusGraph},
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded,
			filepath.Join(blocker, "demo.lab")), State: ViewState{Focus: FocusGraph},
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
	if got := app.currentLab().Layout.Nodes["vm1"]; got != (lab.Position{X: 80, Y: 72}) {
		t.Fatalf("lab layout after failed save = %#v, want original", got)
	}
	if app.Model.Nodes[0].X != 5 || app.Model.Nodes[0].Y != 3 {
		t.Fatalf("move rollback = (%d,%d), want (5,3)", app.Model.Nodes[0].X, app.Model.Nodes[0].Y)
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, ""), runtimeAccess: testRuntimeAccess(runtime),
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

func TestDrainStatusUpdatesIgnoresStaleSessionRevision(t *testing.T) {
	oldLab := &lab.Lab{
		ID:         "old",
		Containers: []lab.Container{{ID: "web", Image: "nginx", DesiredState: lab.DesiredStateRunning}},
	}
	currentLab := &lab.Lab{
		ID:         "current",
		Containers: []lab.Container{{ID: "web", Image: "nginx", DesiredState: lab.DesiredStateRunning}},
	}
	session := lab.NewSession(oldLab, "")
	session.Replace(currentLab)
	app := App{Model: ModelFromLab(currentLab), Session: session, State: ViewState{Focus: FocusGraph}}
	updates := make(chan statusUpdate, 1)
	updates <- statusUpdate{
		revision: 0,
		snapshot: runtimeSnapshot{
			states:         map[string]string{NodeKey(NodeContainer, "web"): "running"},
			statesReceived: true,
		},
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, ""), State: ViewState{Focus: FocusGraph},
	}
	updates := make(chan statusUpdate, 1)
	updates <- statusUpdate{snapshot: runtimeSnapshot{
		states:         states,
		statesReceived: true,
		vncPorts:       ports,
		vncReceived:    true,
	}}
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, ""), State: ViewState{Focus: FocusGraph, Message: "foxlabd status: runtime unavailable"},
	}
	updates := make(chan statusUpdate, 1)
	updates <- statusUpdate{
		snapshot: runtimeSnapshot{
			states:          map[string]string{NodeKey(NodeContainer, "web"): "running"},
			statesReceived:  true,
			statesConfirmed: true,
		},
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
		Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, ""), State: ViewState{Focus: FocusGraph, Message: "renamed disk:data to system"},
	}
	updates := make(chan statusUpdate, 1)
	updates <- statusUpdate{
		snapshot: runtimeSnapshot{
			states:          map[string]string{NodeKey(NodeContainer, "web"): "running"},
			statesReceived:  true,
			statesConfirmed: true,
		},
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
	app := App{Session: lab.NewSession(loaded, ""), runtimeAccess: testRuntimeAccess(runtime)}
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
	if update.revision != app.Session.Revision() {
		t.Fatalf("status update revision = %d, want %d", update.revision, app.Session.Revision())
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
	app := App{Session: lab.NewSession(loaded, ""), runtimeAccess: testRuntimeAccess(runtime)}
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
	if changed := app.updateMessageLifetime(now.Add(9 * time.Second)); changed {
		t.Fatal("message expired before TTL")
	}
	if app.State.Message == "" {
		t.Fatal("message cleared before TTL")
	}
	if changed := app.updateMessageLifetime(now.Add(10 * time.Second)); !changed {
		t.Fatal("message did not expire at TTL")
	}
	if app.State.Message != "" {
		t.Fatalf("message after TTL = %q, want empty", app.State.Message)
	}
}

func TestRepeatedTypedNotificationRestartsLifetime(t *testing.T) {
	app := &App{}
	start := time.Now()
	app.setNotification(Notification{Text: "same message", Level: NotificationInfo})
	if app.updateMessageLifetime(start) {
		t.Fatal("first notification unexpectedly expired")
	}
	app.setNotification(Notification{Text: "same message", Level: NotificationInfo})
	if app.updateMessageLifetime(start.Add(notificationMessageTTL - time.Second)) {
		t.Fatal("repeated notification unexpectedly expired on its old lifetime")
	}
	if !app.updateMessageLifetime(start.Add(2*notificationMessageTTL - time.Second)) {
		t.Fatal("repeated notification did not expire on its new lifetime")
	}
}

func TestMouseClickOnNotificationDismissesWithoutActivatingGraph(t *testing.T) {
	app := App{
		Model:      Model{Nodes: []Node{{ID: "under-toast", Type: NodeVM, Label: "under-toast", X: 1, Y: 27}}},
		State:      ViewState{Message: "created disk:disk", Focus: FocusGraph},
		ViewWidth:  100,
		ViewHeight: 30,
		notificationState: appNotificationState{
			message: "created disk:disk",
			setAt:   time.Now(),
		},
	}

	app.handleKey("mouse:4:28:0")

	if app.State.Message != "" {
		t.Fatalf("message after notification click = %q, want empty", app.State.Message)
	}
	if app.State.ContextMenu {
		t.Fatal("notification click activated graph content underneath")
	}
	if app.notificationState.message != "" || !app.notificationState.setAt.IsZero() {
		t.Fatal("notification click did not reset lifetime tracking")
	}
}

func TestAppRenderReusesRouteCacheAcrossViewStateChanges(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	var out bytes.Buffer
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if len(app.routeCache.routes) == 0 {
		t.Fatal("route cache was not populated")
	}
	key := app.routeCache.key
	routes := app.routeCache.routes

	out.Reset()
	app.State.Selected = 2
	app.State.ContextMenu = true
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.routeCache.key != key {
		t.Fatalf("route cache key changed after view-only state update: %q -> %q", key, app.routeCache.key)
	}
	if &app.routeCache.routes[0] != &routes[0] {
		t.Fatal("route cache was recomputed for view-only state update")
	}

	out.Reset()
	app.State.PanX = -1
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.routeCache.key == key {
		t.Fatal("route cache key did not change after viewport pan")
	}
	key = app.routeCache.key

	out.Reset()
	app.Model.Nodes[0].X++
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.routeCache.key == key {
		t.Fatal("route cache key did not change after model layout update")
	}
}

func TestAppRenderTranslatesRouteCacheWhileMousePanning(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	var out bytes.Buffer
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if len(app.routeCache.routes) == 0 || len(app.routeCache.routes[0].route.cells) == 0 {
		t.Fatal("route cache was not populated")
	}
	key := app.routeCache.key
	routes := app.routeCache.routes
	first := app.routeCache.routes[0].route.cells[0]

	out.Reset()
	app.inputState.mouse.panActive = true
	app.State.PanX = -2
	app.State.PanY = 1
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.routeCache.key != key {
		t.Fatalf("route cache key changed while panning: %q -> %q", key, app.routeCache.key)
	}
	if &app.routeCache.routes[0] != &routes[0] {
		t.Fatal("route cache was recomputed while panning")
	}
	if got := app.routeCache.routes[0].route.cells[0]; got != first {
		t.Fatalf("cached route was mutated while panning: %+v -> %+v", first, got)
	}

	out.Reset()
	app.inputState.mouse.panActive = false
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.routeCache.key == key {
		t.Fatal("route cache did not refresh after mouse panning ended")
	}
}

func TestAppRenderReusesRouteCacheWhileMovingNode(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	var out bytes.Buffer
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	key := app.routeCache.key
	routes := app.routeCache.routes

	app.startMove(app.Model.Nodes[0])
	app.moveActiveNode(4, 0)
	out.Reset()
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.routeCache.key != key {
		t.Fatalf("route cache key changed while moving node: %q -> %q", key, app.routeCache.key)
	}
	if &app.routeCache.routes[0] != &routes[0] {
		t.Fatal("route cache was recomputed while moving node")
	}
	rects := layoutNodeRects(app.Model, rect{X: 0, Y: 0, W: 100, H: 30})
	moved := rects[app.Model.Nodes[0].Key()]
	g := renderGridWithRoutes(app.Model, app.State, 100, 30, app.routeCache.routes)
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
	if app.routeCache.key == key {
		t.Fatal("route cache did not refresh after move mode ended")
	}
}

func TestAppRenderRefreshesRouteCacheWhenViewportChangesDuringMoveMode(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	var out bytes.Buffer
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	key := app.routeCache.key
	app.startMove(app.Model.Nodes[0])
	app.State.PanX = -2
	out.Reset()
	if err := app.render(&out, 100, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.routeCache.key == key {
		t.Fatal("route cache was reused after viewport pan during move mode")
	}
	if app.routeCache.panX != -2 {
		t.Fatalf("route cache panX = %d, want -2", app.routeCache.panX)
	}

	key = app.routeCache.key
	out.Reset()
	if err := app.render(&out, 96, 30, true); err != nil {
		t.Fatal(err)
	}
	if app.routeCache.key == key {
		t.Fatal("route cache was reused after viewport resize during move mode")
	}
	if app.routeCache.width != 96 || app.routeCache.height != 30 {
		t.Fatalf("route cache size = %dx%d, want 96x30", app.routeCache.width, app.routeCache.height)
	}
}
