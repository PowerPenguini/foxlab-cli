package topologyui

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

type fakeVMRuntime struct {
	states  map[string]string
	started string
	stopped string
}

func (f *fakeVMRuntime) States(context.Context, *lab.Lab) (map[string]string, error) {
	return f.states, nil
}

func (f *fakeVMRuntime) Start(_ context.Context, _ *lab.Lab, ref workload.Ref) error {
	f.started = workload.Key(ref)
	if f.states == nil {
		f.states = map[string]string{}
	}
	f.states[workload.Key(ref)] = "running"
	return nil
}

func (f *fakeVMRuntime) Stop(_ context.Context, _ *lab.Lab, ref workload.Ref) error {
	f.stopped = workload.Key(ref)
	if f.states == nil {
		f.states = map[string]string{}
	}
	f.states[workload.Key(ref)] = "shutoff"
	return nil
}

func (f *fakeVMRuntime) Close() error { return nil }

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

func TestContextMenuGroupFollowsRootSelection(t *testing.T) {
	app := App{
		Model: MockModel(),
		State: ViewState{Focus: FocusGraph, Selected: 1},
	}

	app.handleKey("space")
	app.handleKey("down")
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
	if reloaded.VMs[0].Name != "web" {
		t.Fatalf("vm name = %q, want web", reloaded.VMs[0].Name)
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
	app.handleKey("down")
	app.handleKey("down")
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

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Layout.Nodes["vm1"]
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

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Layout.Nodes["vm1"]
	if got.X != 96 || got.Y != 96 {
		t.Fatalf("saved layout = %#v, want X=96 Y=96", got)
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
}

func TestRunStopActionsUseVMRuntime(t *testing.T) {
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", Name: "vm1", MemoryMB: 2048, CPUs: 2, Disk: "labs/demo/disks/vm1.img"}},
	}
	loaded.Normalize()
	runtime := &fakeVMRuntime{states: map[string]string{NodeKey(NodeVM, "vm1"): "shutoff"}}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		Runtime: runtime,
		State:   ViewState{Focus: FocusGraph},
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "run")
	if runtime.started != NodeKey(NodeVM, "vm1") {
		t.Fatalf("started vm = %q, want vm:vm1", runtime.started)
	}
	if app.Model.Nodes[0].State != "running" {
		t.Fatalf("model state after run = %q, want running", app.Model.Nodes[0].State)
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "stop")
	if runtime.stopped != NodeKey(NodeVM, "vm1") {
		t.Fatalf("stopped vm = %q, want vm:vm1", runtime.stopped)
	}
	if app.Model.Nodes[0].State != "shutoff" {
		t.Fatalf("model state after stop = %q, want shutoff", app.Model.Nodes[0].State)
	}
}

func TestRunStopActionsUseContainerRuntime(t *testing.T) {
	loaded := &lab.Lab{
		ID:         "demo",
		Switches:   []lab.Switch{{ID: "lan", Mode: "bridge"}},
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", Networks: []lab.ContainerNetwork{{Switch: "lan"}}}},
	}
	loaded.Normalize()
	runtime := &fakeVMRuntime{states: map[string]string{NodeKey(NodeContainer, "web"): "stopped"}}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		Runtime: runtime,
		State:   ViewState{Focus: FocusGraph},
	}

	app.runMenuAction(Node{ID: "web", Type: NodeContainer}, "run")
	if runtime.started != NodeKey(NodeContainer, "web") {
		t.Fatalf("started container = %q, want container:web", runtime.started)
	}
	if app.Model.Nodes[0].State != "running" {
		t.Fatalf("model state after run = %q, want running", app.Model.Nodes[0].State)
	}

	app.runMenuAction(Node{ID: "web", Type: NodeContainer}, "stop")
	if runtime.stopped != NodeKey(NodeContainer, "web") {
		t.Fatalf("stopped container = %q, want container:web", runtime.stopped)
	}
	if app.Model.Nodes[0].State != "shutoff" {
		t.Fatalf("model state after stop = %q, want shutoff", app.Model.Nodes[0].State)
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
	frameStart := strings.Index(got, "[VM] router")
	altStart := strings.Index(got, ansiEnterAltScreen)
	if altStart == -1 {
		t.Fatalf("interactive output missing enter alt-screen sequence: %q", got)
	}
	if frameStart == -1 {
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
		{"help add", "add vm:"},
		{"help vm", "add vm:"},
		{"help switch", "add sw:"},
		{"help external", "external create:"},
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

func TestCommandHistoryRecall(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	app.openCommand("")
	for _, ch := range "help" {
		app.handleKey("char:" + string(ch))
	}
	app.handleKey("enter")

	app.openCommand("")
	app.handleKey("up")
	if app.State.Command != "help" {
		t.Fatalf("recalled command = %q, want help", app.State.Command)
	}
	app.handleKey("down")
	if app.State.Command != "" {
		t.Fatalf("command after down = %q, want empty", app.State.Command)
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

func TestCommandInputAcceptsSpaces(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}

	app.openCommand("")
	for _, key := range []string{"char:v", "char:m", "char: ", "char:s", "char:e", "char:t"} {
		app.handleKey(key)
	}

	if app.State.Command != "vm set" {
		t.Fatalf("command input = %q, want %q", app.State.Command, "vm set")
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
	if reloaded.VMs[0].ID != "vm1" || reloaded.VMs[0].CPUs != 4 || reloaded.VMs[0].MemoryMB != 4096 {
		t.Fatalf("saved vm = %#v", reloaded.VMs[0])
	}
	if len(app.Model.Nodes) == 0 || app.Model.Nodes[0].ID != "vm1" {
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
	if ct.ID != "web" || ct.Image != "docker.io/library/nginx:latest" || len(ct.Networks) != 1 || ct.Networks[0].Switch != "lan" {
		t.Fatalf("saved container = %#v", ct)
	}
	if len(app.Model.Nodes) == 0 || app.Model.Nodes[0].Type != NodeContainer || app.Model.Nodes[0].Badge != "CT" {
		t.Fatalf("container model not refreshed: %#v", app.Model.Nodes)
	}
	if len(app.Model.Edges) != 1 || app.Model.Edges[0].From != NodeKey(NodeContainer, "web") || app.Model.Edges[0].To != NodeKey(NodeSwitch, "lan") {
		t.Fatalf("container edges = %#v", app.Model.Edges)
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
	if len(reloaded.VMs) != 1 || reloaded.VMs[0].ID != "vm1" {
		t.Fatalf("minimal vm was not saved: %#v", reloaded.VMs)
	}
	if len(reloaded.Switches) != 1 || reloaded.Switches[0].ID != "sw1" {
		t.Fatalf("minimal switch was not saved: %#v", reloaded.Switches)
	}
	if len(reloaded.Containers) != 1 || reloaded.Containers[0].ID != "web" || reloaded.Containers[0].Image == "" {
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

	app.executeCommand(`vm set vm1 name="web server" iso="images/debian 12.iso"`)

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Name != "web server" || reloaded.VMs[0].ISO != "images/debian 12.iso" {
		t.Fatalf("vm quoted fields = name:%q iso:%q", reloaded.VMs[0].Name, reloaded.VMs[0].ISO)
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
	if networks[0].Switch != "lan" || networks[1].Switch != "wan" || networks[1].MAC != "02:00:00:00:00:22" || networks[2].ExternalLink != "uplink1" {
		t.Fatalf("vm networks = %#v", networks)
	}
	if len(app.Model.Edges) != 3 {
		t.Fatalf("model edges = %#v, want 3", app.Model.Edges)
	}
}

func TestCommandContainerNICAddAndConnect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", Networks: []lab.ContainerNetwork{{Switch: "lan"}}}},
		Switches:   []lab.Switch{{ID: "lan", Mode: "bridge"}, {ID: "wan", Mode: "bridge"}},
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

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	networks := reloaded.Containers[0].Networks
	if len(networks) != 2 {
		t.Fatalf("container networks count = %d, want 2: %#v", len(networks), networks)
	}
	if networks[0].Switch != "lan" || networks[1].Switch != "wan" || networks[1].MAC != "02:00:00:00:00:33" {
		t.Fatalf("container networks = %#v", networks)
	}
	if len(app.Model.Edges) != 2 {
		t.Fatalf("model edges = %#v, want 2", app.Model.Edges)
	}
}

func TestCommandReportsUnterminatedQuote(t *testing.T) {
	app := App{Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	app.executeCommand(`vm set vm1 name="unterminated`)
	if !strings.Contains(app.State.Message, "unterminated quote") {
		t.Fatalf("message = %q, want unterminated quote", app.State.Message)
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

	app.executeCommand("external create uplink1 interface=br0")
	app.executeCommand("add sw lan mode=bridge external=uplink1")
	app.executeCommand("switch set lan mode=nat external=uplink1")

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ExternalLinks) != 1 || reloaded.ExternalLinks[0].ID != "uplink1" {
		t.Fatalf("external links = %#v", reloaded.ExternalLinks)
	}
	if len(reloaded.Switches) != 1 || reloaded.Switches[0].Mode != "nat" || reloaded.Switches[0].ExternalLink != "uplink1" {
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
	app := App{
		Model: Model{ID: "empty"},
		Lab:   &lab.Lab{ID: "empty"},
		State: ViewState{Focus: FocusGraph},
	}

	app.runGlobalMenuAction("add vm")
	if app.State.Command != "add vm vm1" {
		t.Fatalf("global add vm command = %q", app.State.Command)
	}

	app.runGlobalMenuAction("add sw")
	if app.State.Command != "add sw sw1" {
		t.Fatalf("global add sw command = %q", app.State.Command)
	}

	app.runGlobalMenuAction("add cont")
	if app.State.Command != "add cont ct1" {
		t.Fatalf("global add cont command = %q", app.State.Command)
	}

	app.runGlobalMenuAction("create external")
	if !strings.Contains(app.State.Command, "external create uplink1") {
		t.Fatalf("global create external command = %q", app.State.Command)
	}
}

func TestContextMenuActionsOpenPrefilledCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			Name:     "web server",
			MemoryMB: 2048,
			CPUs:     2,
			Disk:     "labs/demo/disks/web server.qcow2",
			ISO:      "images/debian 12.iso",
			Networks: []lab.VMNetwork{{}},
		}},
		Containers:    []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", Networks: []lab.ContainerNetwork{{}}}},
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Name: "office uplink", Interface: "enp 1s0"}},
	}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:   MockModel(),
		Lab:     loaded,
		LabPath: path,
		State:   ViewState{Focus: FocusGraph},
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "edit")
	if !strings.HasPrefix(app.State.Command, "vm set vm1 ") {
		t.Fatalf("edit command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "disk")
	if !strings.Contains(app.State.Command, `disk="labs/demo/disks/web server.qcow2"`) {
		t.Fatalf("disk command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "iso")
	if app.State.Command != `vm set vm1 iso="images/debian 12.iso"` {
		t.Fatalf("iso command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "uplink1", Type: NodeExternal}, "interface")
	if app.State.Command != `external set uplink1 interface="enp 1s0"` {
		t.Fatalf("interface command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "uplink1", Type: NodeExternal}, "name")
	if app.State.Command != `external set uplink1 name="office uplink"` {
		t.Fatalf("name command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "uplink1", Type: NodeExternal}, "add sw")
	if !strings.HasPrefix(app.State.Command, "add sw ") || !strings.Contains(app.State.Command, " external=uplink1") {
		t.Fatalf("add sw command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "lan", Type: NodeSwitch}, "add vm")
	if !strings.Contains(app.State.Command, " switch=lan") {
		t.Fatalf("switch add vm command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "add-nic")
	if app.State.Command != "vm nic add vm1" {
		t.Fatalf("vm add-nic command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "connect-nic:0")
	if !app.State.ConnectMode || app.State.ConnectNodeID != "vm1" || app.State.ConnectNICIndex != "0" {
		t.Fatalf("vm connect-nic state = %#v", app.State)
	}
	app.State.ConnectMode = false

	app.runMenuAction(Node{ID: "web", Type: NodeContainer}, "add-nic")
	if app.State.Command != "container nic add web" {
		t.Fatalf("container add-nic command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "web", Type: NodeContainer}, "connect-nic:0")
	if !app.State.ConnectMode || app.State.ConnectNodeID != "web" || app.State.ConnectNICIndex != "0" {
		t.Fatalf("container connect-nic state = %#v", app.State)
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

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "connect-nic:0")
	if !app.State.ConnectMode {
		t.Fatalf("connect mode not started: %#v", app.State)
	}
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok || node.ID != "lan" || node.Type != NodeSwitch {
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
	if reloaded.VMs[0].Networks[0].Switch != "lan" {
		t.Fatalf("vm networks = %#v", reloaded.VMs[0].Networks)
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
	if !app.State.ConnectMode || app.State.ConnectNodeID != "vm1" || app.State.ConnectNICIndex != "0" {
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

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "connect-nic:0")
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
	node, ok := selectedNode(app.Model, app.State.Selected)
	if !ok || node.ID != "vm2" || node.Type != NodeVM {
		t.Fatalf("selected endpoint = %#v, ok=%t", node, ok)
	}

	app.handleKey("enter")
	if !app.State.ConnectMode || !app.State.ConnectTargetMenu || app.State.ConnectTargetID != "vm2" {
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
	if link.From.Type != "vm" || link.From.ID != "vm1" || link.From.NIC != 0 || link.To.Type != "vm" || link.To.ID != "vm2" || link.To.NIC != 1 {
		t.Fatalf("network link = %#v", link)
	}
	if !hasEdge(app.Model, NodeKey(NodeVM, "vm1"), NodeKey(NodeVM, "vm2")) {
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

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "connect-nic:0")
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
	if link.To.Type != "vm" || link.To.ID != "vm2" || link.To.NIC != 0 {
		t.Fatalf("network link target = %#v", link)
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
