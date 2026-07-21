package topologyui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestInspectorEditsContainerConfigurationAndCapabilities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{{
			ID:    "kali",
			Image: "docker.io/kalilinux/kali-rolling:latest",
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
		ViewWidth:  120,
		ViewHeight: 30,
	}

	app.handleKey("tab")
	if app.State.Focus != FocusInspector {
		t.Fatalf("focus after tab = %d, want inspector", app.State.Focus)
	}

	app.State.InspectorSelected = inspectorFieldIndex(t, app.selectedInspectorFields(), "shell", "")
	app.handleKey("enter")
	if !app.State.InspectorEditing {
		t.Fatal("enter did not start inline shell editing")
	}
	for _, key := range []string{"char:/", "char:b", "char:i", "char:n", "char:/", "char:b", "char:a", "char:s", "char:h"} {
		app.handleKey(key)
	}
	app.handleKey("enter")

	app.State.InspectorSelected = inspectorFieldIndex(t, app.selectedInspectorFields(), "capabilities", "")
	app.handleKey("enter")
	if !app.State.InspectorCapOpen {
		t.Fatal("capabilities field did not open the picker")
	}
	for _, key := range []string{"char:N", "char:E", "char:T", "char:_", "char:A", "char:D", "char:M", "char:I", "char:N"} {
		app.handleKey(key)
	}
	app.handleKey("space")
	if !app.State.InspectorCapOpen {
		t.Fatal("capability picker closed after toggling an option")
	}
	app.handleKey("escape")
	if app.State.InspectorCapOpen {
		t.Fatal("escape did not close capability picker")
	}

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ct := reloaded.Containers[0]
	if ct.Shell != "/bin/bash" {
		t.Fatalf("saved shell = %q, want /bin/bash", ct.Shell)
	}
	if ct.Capabilities == nil || !containsString(ct.Capabilities.Add, "NET_ADMIN") {
		t.Fatalf("saved capabilities = %#v, want NET_ADMIN added", ct.Capabilities)
	}
}

func TestInspectorShellButtonOpensContainerShell(t *testing.T) {
	loaded := &lab.Lab{ID: "demo", Containers: []lab.Container{{ID: "kali", Image: "kali"}}}
	key := NodeKey(NodeContainer, "kali")
	runtime := &fakeVMRuntime{states: map[string]string{key: "running"}}
	app := App{
		Model:         ModelFromLab(loaded),
		Lab:           loaded,
		runtimeAccess: testRuntimeAccess(runtime),
		State:         ViewState{Focus: FocusInspector},
		ViewWidth:     120,
		ViewHeight:    30,
	}
	app.Model.Nodes[0].State = "running"
	fields := app.selectedInspectorFields()
	shellIndex := inspectorFieldIndex(t, fields, "shellAction", "")
	app.State.InspectorSelected = shellIndex

	app.handleKey("enter")
	if app.PendingShell == nil || app.PendingShell.OpenSession == nil {
		t.Fatalf("keyboard Shell action did not queue container shell: %#v", app.PendingShell)
	}

	app.PendingShell = nil
	panel := inspectorBounds(app.ViewWidth, app.contentHeight())
	button := inspectorShellButtonRect(panel)
	app.handleKey("mouse:" + strconv.Itoa(button.X+button.W/2) + ":" + strconv.Itoa(button.Y) + ":0")
	if app.PendingShell == nil || app.PendingShell.OpenSession == nil {
		t.Fatalf("mouse Shell action did not queue container shell: %#v", app.PendingShell)
	}
	if app.State.Focus != FocusInspector || app.State.InspectorSelected != shellIndex {
		t.Fatalf("shell button selection = focus %d index %d, want inspector index %d", app.State.Focus, app.State.InspectorSelected, shellIndex)
	}
	g := renderGrid(app.Model, app.State, app.ViewWidth, app.contentHeight())
	if got := g.Cells[(button.Y+1)*g.Width+button.X].Style; got != inspectorButtonCyanActiveStyle {
		t.Fatalf("clicked Shell button style = %q, want persistent active style %q", got, inspectorButtonCyanActiveStyle)
	}
	fullButton := inspectorShellButtonRectForFields(panel, fields)
	if fullButton.W != panel.W-6 {
		t.Fatalf("container Shell button width = %d, want full content width %d", fullButton.W, panel.W-6)
	}
}

func TestInspectorVNCButtonOpensVMViewer(t *testing.T) {
	loaded := &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "router", CPUs: 2, MemoryMB: 2048, VNC: true}}}
	key := NodeKey(NodeVM, "router")
	app := App{
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		VNCPorts:   map[string]int{key: 5905},
		VNCViewer:  "/bin/true",
		State:      ViewState{Focus: FocusInspector},
		ViewWidth:  120,
		ViewHeight: 30,
	}
	app.Model.Nodes[0].State = "running"
	fields := app.selectedInspectorFields()
	vncIndex := inspectorFieldIndex(t, fields, "vncAction", "")
	app.State.InspectorSelected = vncIndex

	app.handleKey("enter")
	if app.PendingVNC == nil || app.PendingVNC.Display != "vnc 127.0.0.1::5905" {
		t.Fatalf("keyboard VNC action did not queue viewer: %#v", app.PendingVNC)
	}

	app.PendingVNC = nil
	panel := inspectorBounds(app.ViewWidth, app.contentHeight())
	button := inspectorVNCButtonRect(panel)
	app.handleKey("mouse:" + strconv.Itoa(button.X+button.W/2) + ":" + strconv.Itoa(button.Y) + ":0")
	if app.PendingVNC == nil || app.PendingVNC.Display != "vnc 127.0.0.1::5905" {
		t.Fatalf("mouse VNC action did not queue viewer: %#v", app.PendingVNC)
	}
	if app.State.Focus != FocusInspector || app.State.InspectorSelected != vncIndex {
		t.Fatalf("VNC button selection = focus %d index %d, want inspector index %d", app.State.Focus, app.State.InspectorSelected, vncIndex)
	}
	g := renderGrid(app.Model, app.State, app.ViewWidth, app.contentHeight())
	if got := g.Cells[(button.Y+1)*g.Width+button.X].Style; got != inspectorButtonCyanActiveStyle {
		t.Fatalf("clicked VNC button style = %q, want persistent active style %q", got, inspectorButtonCyanActiveStyle)
	}
	app.State.VNCViewerActive = map[string]bool{key: true}
	stopGrid := renderGrid(app.Model, app.State, app.ViewWidth, app.contentHeight())
	if !strings.Contains(stopGrid.String(false), "Stop VNC") {
		t.Fatalf("active VNC viewer did not change button label:\n%s", stopGrid.String(false))
	}
	if got := stopGrid.Cells[(button.Y+1)*stopGrid.Width+button.X].Style; got != inspectorButtonRedActiveStyle {
		t.Fatalf("active Stop VNC button style = %q, want %q", got, inspectorButtonRedActiveStyle)
	}
}

func TestInspectorDeleteButtonDeletesWithKeyboardAndMouse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID: "demo",
		Containers: []lab.Container{
			{ID: "first", Image: "first"},
			{ID: "second", Image: "second"},
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
		State:      ViewState{Focus: FocusInspector},
		ViewWidth:  120,
		ViewHeight: 30,
	}
	fields := app.selectedInspectorFields()
	app.State.InspectorSelected = inspectorFieldIndex(t, fields, "deleteAction", "")
	app.handleKey("enter")
	if len(app.Lab.Containers) != 1 || app.Lab.Containers[0].ID != "second" {
		t.Fatalf("containers after keyboard delete = %#v, want second only", app.Lab.Containers)
	}

	panel := inspectorBounds(app.ViewWidth, app.contentHeight())
	fields = app.selectedInspectorFields()
	button, ok := inspectorDeleteButtonRect(panel, app.State, fields)
	if !ok {
		t.Fatal("Delete button is not visible after the Inspector sections")
	}
	app.handleKey("mouse:" + strconv.Itoa(button.X+button.W/2) + ":" + strconv.Itoa(button.Y) + ":0")
	if len(app.Lab.Containers) != 0 {
		t.Fatalf("containers after mouse delete = %#v, want none", app.Lab.Containers)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Containers) != 0 {
		t.Fatalf("saved containers after Inspector deletes = %#v, want none", reloaded.Containers)
	}
}

func TestInspectorInterfacePickerSearchesAndSelectsHostInterface(t *testing.T) {
	fakeHostInterfaces(t, "wlp0s20f3", "eth0", "br0")
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Name: "Internet", Interface: "wlp0s20f3", Mode: lab.ExternalModeNAT}},
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
		State:      ViewState{Focus: FocusInspector},
		ViewWidth:  120,
		ViewHeight: 30,
	}
	fields := app.selectedInspectorFields()
	interfaceIndex := inspectorFieldIndex(t, fields, "interface", "")
	app.State.InspectorSelected = interfaceIndex

	app.handleKey("enter")
	if !app.State.InspectorCapOpen {
		t.Fatal("Interface did not open the searchable picker")
	}
	for _, key := range []string{"char:e", "char:t", "char:h"} {
		app.handleKey(key)
	}
	if got := inspectorInterfaceOptions(app.State.InspectorCapQuery); len(got) != 1 || got[0] != "eth0" {
		t.Fatalf("filtered interfaces = %#v, want eth0", got)
	}
	app.handleKey("enter")
	if app.State.InspectorCapOpen {
		t.Fatal("Interface picker stayed open after keyboard selection")
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ExternalLinks[0].Interface != "eth0" {
		t.Fatalf("keyboard-selected interface = %q, want eth0", reloaded.ExternalLinks[0].Interface)
	}

	fields = app.selectedInspectorFields()
	app.State.InspectorSelected = inspectorFieldIndex(t, fields, "interface", "")
	app.handleKey("enter")
	app.handleKey("char:b")
	app.handleKey("char:r")
	panel := inspectorBounds(app.ViewWidth, app.contentHeight())
	options := inspectorInterfaceOptions(app.State.InspectorCapQuery)
	layout, ok := inspectorInterfaceLayout(panel, app.State, fields, len(options))
	if !ok || len(options) != 1 || options[0] != "br0" {
		t.Fatalf("mouse picker options = %#v, layout ok=%t", options, ok)
	}
	app.handleKey("mouse:" + strconv.Itoa(layout.rect.X+1) + ":" + strconv.Itoa(layout.optionsY) + ":0")
	if app.State.InspectorCapOpen {
		t.Fatal("Interface picker stayed open after mouse selection")
	}
	reloaded, err = lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ExternalLinks[0].Interface != "br0" {
		t.Fatalf("mouse-selected interface = %q, want br0", reloaded.ExternalLinks[0].Interface)
	}
}

func TestRenderInspectorInterfaceAsSearchablePicker(t *testing.T) {
	fakeHostInterfaces(t, "br0", "eth0", "wlp0s20f3")
	m := ModelFromLab(&lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "wlp0s20f3", Mode: lab.ExternalModeNAT}},
	})
	fields := inspectorFields(m.Nodes[0])
	interfaceIndex := inspectorFieldIndex(t, fields, "interface", "")
	closed := RenderString(m, ViewState{Focus: FocusInspector, InspectorSelected: interfaceIndex}, 120, 30, false)
	if !strings.Contains(closed, "Interface") || !strings.Contains(closed, "[wlp0s20f3 ▾]") {
		t.Fatalf("closed Interface picker missing combobox value:\n%s", closed)
	}
	open := RenderString(m, ViewState{Focus: FocusInspector, InspectorSelected: interfaceIndex, InspectorCapOpen: true, InspectorCapQuery: "eth"}, 120, 30, false)
	for _, want := range []string{"⌕ eth|", "[ ] eth0"} {
		if !strings.Contains(open, want) {
			t.Fatalf("open Interface picker missing %q:\n%s", want, open)
		}
	}
	if strings.Contains(open, "[X] wlp0s20f3") {
		t.Fatalf("Interface search kept a non-matching option:\n%s", open)
	}
}

func TestInspectorMouseTogglesCapability(t *testing.T) {
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
		Model:      ModelFromLab(loaded),
		Lab:        loaded,
		LabPath:    path,
		State:      ViewState{Focus: FocusInspector},
		ViewWidth:  120,
		ViewHeight: 30,
	}
	fields := app.selectedInspectorFields()
	index := inspectorFieldIndex(t, fields, "capabilities", "")
	app.State.InspectorSelected = index
	panel := inspectorBounds(app.ViewWidth, app.contentHeight())
	y, ok := inspectorFieldY(panel, app.State, fields, index)
	if !ok {
		t.Fatalf("capabilities field is not visible in inspector")
	}

	app.handleKey("mouse:" + strconv.Itoa(panel.X+2) + ":" + strconv.Itoa(y) + ":0")
	if !app.State.InspectorCapOpen {
		t.Fatal("mouse did not open capability picker")
	}
	for _, key := range []string{"char:N", "char:E", "char:T", "char:_", "char:R", "char:A", "char:W"} {
		app.handleKey(key)
	}
	node, _ := selectedNode(app.Model, app.State.Selected)
	fields = app.selectedInspectorFields()
	options := inspectorCapabilityOptions(node, app.State.InspectorCapQuery)
	layout, ok := inspectorCapabilityLayout(panel, app.State, fields, len(options))
	if !ok || len(options) != 1 || options[0] != "NET_RAW" {
		t.Fatalf("filtered picker options = %#v, layout ok=%t", options, ok)
	}
	app.handleKey("mouse:" + strconv.Itoa(layout.rect.X+1) + ":" + strconv.Itoa(layout.optionsY) + ":0")
	if !app.State.InspectorCapOpen {
		t.Fatal("mouse selection closed capability picker")
	}
	app.handleKey("mouse:" + strconv.Itoa(panel.X+2) + ":" + strconv.Itoa(y) + ":0")
	if app.State.InspectorCapOpen {
		t.Fatal("clicking open capabilities field did not close picker")
	}

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ct := reloaded.Containers[0]
	if ct.Capabilities == nil || !containsString(ct.Capabilities.Drop, "NET_RAW") {
		t.Fatalf("saved capabilities = %#v, want NET_RAW dropped", ct.Capabilities)
	}
}

func TestContextMenuMovesConfigurationToVisibleInspector(t *testing.T) {
	node := Node{ID: "kali", Type: NodeContainer}
	wide := App{ViewWidth: 120, ViewHeight: 30}
	wideItems := wide.contextMenuRootItems(node, true)
	if containsString(wideItems, "Configuration >") || containsString(wideItems, "Permissions >") || containsString(wideItems, "NIC >") || containsString(wideItems, "Disk >") {
		t.Fatalf("wide operations menu duplicated inspector fields: %#v", wideItems)
	}
	narrow := App{ViewWidth: 100, ViewHeight: 30}
	narrowItems := narrow.contextMenuRootItems(node, true)
	if !containsString(narrowItems, "Configuration >") || !containsString(narrowItems, "Permissions >") || !containsString(narrowItems, "NIC >") || !containsString(narrowItems, "Disk >") {
		t.Fatalf("narrow operations menu lost configuration fallback: %#v", narrowItems)
	}
}

func TestInspectorBuildsNICAndDiskSectionsFromTypedMetadata(t *testing.T) {
	node := Node{ID: "vm1", Type: NodeVM, Details: []string{"nic3 → lan"}}
	state := ViewState{
		InspectorDiskItems:   []string{"Add Disk", "base-a 10G", "  | layer-a"},
		InspectorDiskActions: []string{diskMenuActionCreate, diskMenuActionNone, diskMenuActionAttach},
		InspectorDiskKinds:   []string{"", "base", "layer"},
		InspectorDiskIDs:     []string{"", "base-a", "layer-a"},
	}
	fields := inspectorFieldsForState(node, state)
	var nic, base, layer inspectorField
	for _, field := range fields {
		switch field.id {
		case "nic3":
			nic = field
		case "disk:base-a":
			base = field
		case "disk:layer-a":
			layer = field
		}
	}
	if nic.kind != inspectorFieldNIC || nic.nicIndex != "3" || nic.value != "lan" {
		t.Fatalf("NIC inspector field = %#v", nic)
	}
	if base.diskKind != "base" || base.diskAction != diskMenuActionNone || inspectorFieldSection(base) != "DISK" {
		t.Fatalf("base disk inspector field = %#v", base)
	}
	if layer.diskKind != "layer" || layer.diskAction != diskMenuActionAttach || layer.diskID != "layer-a" {
		t.Fatalf("layer disk inspector field = %#v", layer)
	}
}

func TestInspectorAddDiskShowsInlineCursorAndWorksFromMouse(t *testing.T) {
	fakeQemuImg(t)
	t.Setenv("HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "demo.lab")
	loaded := &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", CPUs: 2, MemoryMB: 2048}}}
	if err := lab.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model: ModelFromLab(loaded), Lab: loaded, LabPath: path,
		State: ViewState{Focus: FocusInspector}, ViewWidth: 120, ViewHeight: 30,
	}
	fields := app.selectedInspectorFields()
	addDisk := inspectorFieldIndex(t, fields, "addDisk", "")
	app.State.InspectorSelected = addDisk
	app.handleKey("enter")
	if !app.State.InspectorEditing || app.State.InspectorEditAction != "add-disk" {
		t.Fatalf("Add Disk keyboard activation state = %#v", app.State)
	}
	rendered := RenderString(app.Model, app.inspectorRenderState(), app.ViewWidth, app.contentHeight(), false)
	if !strings.Contains(rendered, "disk|") || !strings.Contains(rendered, "Enter save · Esc cancel") {
		t.Fatalf("Add Disk editor did not render its cursor and edit hint:\n%s", rendered)
	}

	app.handleKey("escape")
	fields = app.selectedInspectorFields()
	addDisk = inspectorFieldIndex(t, fields, "addDisk", "")
	panel := inspectorBounds(app.ViewWidth, app.contentHeight())
	y, ok := inspectorFieldY(panel, app.State, fields, addDisk)
	if !ok {
		t.Fatal("Add Disk row is not visible")
	}
	app.handleKey("mouse:" + strconv.Itoa(panel.X+5) + ":" + strconv.Itoa(y) + ":0")
	if !app.State.InspectorEditing || app.State.InspectorEditAction != "add-disk" {
		t.Fatalf("Add Disk mouse activation state = %#v", app.State)
	}
	app.handleKey("enter")
	if len(app.Lab.Disks) != 1 || app.Lab.Disks[0].ID != "disk" {
		t.Fatalf("disks after Inspector Add Disk = %#v, message=%q notification=%#v", app.Lab.Disks, app.State.Message, app.State.Notification)
	}
}

func TestInspectorDoesNotDeleteDisksWithKeyboardOrMouse(t *testing.T) {
	dir := t.TempDir()
	labPath := filepath.Join(dir, "demo.lab")
	diskPath := filepath.Join(dir, "data.qcow2")
	if err := os.WriteFile(diskPath, []byte("disk"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "router", CPUs: 2, MemoryMB: 2048, Disk: diskPath}},
		Disks: []lab.Disk{{
			ID: "data", Path: diskPath, Format: "qcow2", Kind: "base", AttachedType: NodeVM, AttachedTo: "router",
		}},
	}
	if err := lab.SaveFile(labPath, loaded); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(labPath)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model: ModelFromLab(loaded), Lab: loaded, LabPath: labPath,
		State: ViewState{Focus: FocusInspector}, ViewWidth: 120, ViewHeight: 30,
	}
	fields := app.selectedInspectorFields()
	diskIndex := inspectorFieldIndex(t, fields, "disk:data", "")
	app.State.InspectorSelected = diskIndex
	app.handleKey("char:x")
	if len(app.Lab.Disks) != 1 {
		t.Fatalf("keyboard X deleted Inspector disk: %#v", app.Lab.Disks)
	}

	panel := inspectorBounds(app.ViewWidth, app.contentHeight())
	y, ok := inspectorFieldY(panel, app.State, fields, diskIndex)
	if !ok {
		t.Fatal("selected disk row is not visible")
	}
	g := renderGrid(app.Model, app.inspectorRenderState(), app.ViewWidth, app.contentHeight())
	if got := g.Cells[y*g.Width+panel.X+panel.W-4].Ch; got == '×' {
		t.Fatalf("Inspector disk row still renders delete control:\n%s", g.String(false))
	}
	app.handleKey("mouse:" + strconv.Itoa(panel.X+panel.W-4) + ":" + strconv.Itoa(y) + ":0")
	if len(app.Lab.Disks) != 1 {
		t.Fatalf("mouse click deleted Inspector disk: %#v", app.Lab.Disks)
	}
	if _, err := os.Stat(diskPath); err != nil {
		t.Fatalf("Inspector removed disk file: %v", err)
	}
	if hint := inspectorDiskFooterHint(fields[diskIndex]); strings.Contains(hint, "delete") || strings.Contains(hint, "X") {
		t.Fatalf("Inspector disk footer still advertises deletion: %q", hint)
	}
}

func TestRenderContainerInspectorShowsEditableCapabilities(t *testing.T) {
	m := ModelFromLab(&lab.Lab{ID: "demo", Containers: []lab.Container{{ID: "kali", Image: "kali"}}})
	closed := RenderString(m, ViewState{Focus: FocusInspector, InspectorSelected: 7}, 120, 30, false)
	for _, want := range []string{"▾ WORKLOAD", "▾ LINUX CAPABILITIES", "Capabilities", "[14 selected ▾]", "›", "▶  Start", ">_ Shell", "↔  Move"} {
		if !strings.Contains(closed, want) {
			t.Fatalf("closed container capability picker missing %q:\n%s", want, closed)
		}
	}
	if strings.Contains(closed, "NET_ADMIN") {
		t.Fatalf("closed picker rendered inline capability rows:\n%s", closed)
	}
	fields := inspectorFields(m.Nodes[0])
	deleteIndex := inspectorFieldIndex(t, fields, "deleteAction", "")
	actions := RenderString(m, ViewState{Focus: FocusInspector, InspectorSelected: deleteIndex}, 120, 30, false)
	if moveAt, deleteAt := strings.Index(actions, "↔  Move"), strings.Index(actions, "×  Delete"); moveAt < 0 || deleteAt <= moveAt {
		t.Fatalf("Move/Delete buttons are not ordered after all Inspector sections:\n%s", actions)
	}
	if strings.Count(closed, "▾ WORKLOAD") != 1 {
		t.Fatalf("Delete button introduced an extra WORKLOAD section:\n%s", closed)
	}
	open := RenderString(m, ViewState{Focus: FocusInspector, InspectorSelected: 7, InspectorCapOpen: true, InspectorCapQuery: "NET_"}, 120, 30, false)
	for _, want := range []string{"⌕ NET_", "NET_ADMIN", "NET_RAW", "NET_BIND_SERVICE", "[X]", "[ ]"} {
		if !strings.Contains(open, want) {
			t.Fatalf("open container capability picker missing %q:\n%s", want, open)
		}
	}
	if strings.Contains(open, "SYS_ADMIN") {
		t.Fatalf("capability search did not filter SYS_ADMIN:\n%s", open)
	}
	empty := RenderString(m, ViewState{Focus: FocusInspector, InspectorSelected: 7, InspectorCapOpen: true, InspectorCapQuery: "NOT_A_CAP"}, 120, 30, false)
	if !strings.Contains(empty, "no matches") {
		t.Fatalf("empty capability search did not render no-matches state:\n%s", empty)
	}
	if strings.Contains(open, "FOXLAB / PROPERTIES") || strings.Contains(open, "│") {
		t.Fatalf("container inspector kept removed chrome:\n%s", open)
	}
}

func TestCapabilityPickerIsCompactAndScrollable(t *testing.T) {
	m := ModelFromLab(&lab.Lab{ID: "demo", Containers: []lab.Container{{ID: "kali", Image: "kali"}}})
	app := App{
		Model:      m,
		State:      ViewState{Focus: FocusInspector, InspectorSelected: 7, InspectorCapOpen: true},
		ViewWidth:  120,
		ViewHeight: 30,
	}
	panel := inspectorBounds(app.ViewWidth, app.contentHeight())
	fields := app.selectedInspectorFields()
	options := inspectorCapabilityOptions(m.Nodes[0], "")
	layout, ok := inspectorCapabilityLayout(panel, app.State, fields, len(options))
	if !ok {
		t.Fatal("compact capability picker layout unavailable")
	}
	if layout.optionRows != inspectorCapabilityPickerMaxRows || layout.rect.H != inspectorCapabilityPickerMaxRows+1 {
		t.Fatalf("picker size = rows %d height %d, want %d and %d", layout.optionRows, layout.rect.H, inspectorCapabilityPickerMaxRows, inspectorCapabilityPickerMaxRows+1)
	}

	app.handleKey("pagedown")
	if app.State.InspectorCapSelected != inspectorCapabilityPickerMaxRows {
		t.Fatalf("selection after PageDown = %d, want %d", app.State.InspectorCapSelected, inspectorCapabilityPickerMaxRows)
	}
	layout, _ = inspectorCapabilityLayout(panel, app.State, fields, len(options))
	g := renderGrid(app.Model, app.State, app.ViewWidth, app.contentHeight())
	upArrow := g.Cells[layout.optionsY*g.Width+layout.rect.X+layout.rect.W-2]
	if upArrow.Ch != '↑' || upArrow.Style != themeMenuMuted {
		t.Fatalf("up arrow = %q style %q, want muted scroll indicator", upArrow.Ch, upArrow.Style)
	}
	app.handleKey("mouse:" + strconv.Itoa(layout.rect.X+1) + ":" + strconv.Itoa(layout.optionsY) + ":65")
	if app.State.InspectorCapSelected != inspectorCapabilityPickerMaxRows+1 {
		t.Fatalf("selection after wheel down = %d, want %d", app.State.InspectorCapSelected, inspectorCapabilityPickerMaxRows+1)
	}
	if got := normalizeAppKey(&app, "char:k"); got != "char:k" {
		t.Fatalf("search key normalization = %q, want char:k", got)
	}
	if got := decodeKeys("\x1b[5~\x1b[6~", false); len(got) != 2 || got[0] != "pageup" || got[1] != "pagedown" {
		t.Fatalf("decoded page keys = %#v", got)
	}
}

func inspectorFieldIndex(t *testing.T, fields []inspectorField, id, capability string) int {
	t.Helper()
	for i, field := range fields {
		if field.id == id && field.capability == capability {
			return i
		}
	}
	t.Fatalf("inspector field %q capability %q not found in %#v", id, capability, fields)
	return 0
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
