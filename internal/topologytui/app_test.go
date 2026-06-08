package topologytui

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

type fakeVMRuntime struct {
	states  map[string]string
	started string
	stopped string
}

func (f *fakeVMRuntime) VMStates(context.Context, *lab.Lab) (map[string]string, error) {
	return f.states, nil
}

func (f *fakeVMRuntime) StartVM(_ context.Context, _ *lab.Lab, id string) error {
	f.started = id
	if f.states == nil {
		f.states = map[string]string{}
	}
	f.states[id] = "running"
	return nil
}

func (f *fakeVMRuntime) StopVM(_ context.Context, _ *lab.Lab, id string) error {
	f.stopped = id
	if f.states == nil {
		f.states = map[string]string{}
	}
	f.states[id] = "shutoff"
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

	node, _ := app.Model.selected(app.State.Selected)
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

	node, _ := app.Model.selected(0)
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
	runtime := &fakeVMRuntime{states: map[string]string{"vm1": "shutoff"}}
	app := App{
		Model:   ModelFromLab(loaded),
		Lab:     loaded,
		Runtime: runtime,
		State:   ViewState{Focus: FocusGraph},
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "run")
	if runtime.started != "vm1" {
		t.Fatalf("started vm = %q, want vm1", runtime.started)
	}
	if app.Model.Nodes[0].State != "running" {
		t.Fatalf("model state after run = %q, want running", app.Model.Nodes[0].State)
	}

	app.runMenuAction(Node{ID: "vm1", Type: NodeVM}, "stop")
	if runtime.stopped != "vm1" {
		t.Fatalf("stopped vm = %q, want vm1", runtime.stopped)
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
	file, err := os.CreateTemp(t.TempDir(), "topologytui-output-*")
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
		{"help vm", "vm create:"},
		{"help switch", "switch create:"},
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

	app.executeCommand("vm create vm2 mem-=512")
	if !strings.Contains(app.State.Message, "unsupported increment syntax") {
		t.Fatalf("vm create invalid args message = %q", app.State.Message)
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

	app.executeCommand("vm create vm1 cpus=4 memory=4096 switch=lan")

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

	app.executeCommand("vm create vm1 disk=explicit/path/test.qcow2")

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
	app.executeCommand("switch create lan mode=bridge external=uplink1")
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
		Model: Model{LabID: "empty"},
		Lab:   &lab.Lab{ID: "empty"},
		State: ViewState{Focus: FocusGraph},
	}

	app.runGlobalMenuAction("create-vm")
	if app.State.Command != "vm create vm1 cpus=2 memory=2048" {
		t.Fatalf("global create-vm command = %q", app.State.Command)
	}

	app.runGlobalMenuAction("create-switch")
	if !strings.Contains(app.State.Command, "switch create sw1") {
		t.Fatalf("global create-switch command = %q", app.State.Command)
	}

	app.runGlobalMenuAction("create-external")
	if !strings.Contains(app.State.Command, "external create uplink1") {
		t.Fatalf("global create-external command = %q", app.State.Command)
	}
}

func TestContextMenuActionsOpenPrefilledCommands(t *testing.T) {
	app := App{
		Model: MockModel(),
		Lab: &lab.Lab{
			ID: "demo",
			VMs: []lab.VM{{
				ID:       "vm1",
				Name:     "web server",
				MemoryMB: 2048,
				CPUs:     2,
				Disk:     "labs/demo/disks/web server.qcow2",
				ISO:      "images/debian 12.iso",
			}},
			Switches:      []lab.Switch{{ID: "lan", Mode: "bridge"}},
			ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Name: "office uplink", Interface: "enp 1s0"}},
		},
		State: ViewState{Focus: FocusGraph},
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

	app.runMenuAction(Node{ID: "uplink1", Type: NodeExternal}, "create-switch")
	if !strings.HasPrefix(app.State.Command, "switch create ") || !strings.Contains(app.State.Command, " external=uplink1") {
		t.Fatalf("create-switch command = %q", app.State.Command)
	}

	app.runMenuAction(Node{ID: "lan", Type: NodeSwitch}, "create-vm")
	if !strings.Contains(app.State.Command, " switch=lan") {
		t.Fatalf("switch create-vm command = %q", app.State.Command)
	}
}
