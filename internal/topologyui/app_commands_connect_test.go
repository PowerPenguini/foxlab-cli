package topologyui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

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
		runtimeAccess:    testRuntimeAccess(runtime),
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
	runtime := &fakeVMRuntime{states: map[string]string{NodeKey(NodeVM, "vm1"): " Running "}}
	runtime.openTerminal = func(ctx context.Context, _ *lab.Lab, ref workload.Ref, _ workload.TerminalSize) (workload.OpenedTerminalSession, error) {
		consoleCtx = ctx
		if ref.ID != "vm1" {
			t.Fatalf("console id = %q", ref.ID)
		}
		return workload.OpenedTerminalSession{Session: &fakeConsole{}, Endpoint: "/dev/pts/7"}, nil
	}
	app := App{
		Model:         MockModel(),
		Lab:           &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2}}},
		runtimeAccess: testRuntimeAccess(runtime),
		State:         ViewState{Focus: FocusGraph},
	}

	app.executeCommand("shell vm vm1")

	if app.PendingShell == nil {
		t.Fatal("vm shell did not set pending command")
	}
	if got := runtime.starts; got != 0 {
		t.Fatalf("vm shell started workload %d times", got)
	}
	if got := app.WorkloadStates[NodeKey(NodeVM, "vm1")]; got != "running" {
		t.Fatalf("workload state = %q, want normalized running", got)
	}
	if app.PendingShell.Session == nil || app.PendingShell.NativeRun != nil {
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
	runtime := &fakeVMRuntime{states: map[string]string{NodeKey(NodeVM, "vm1"): "running"}}
	runtime.openTerminal = func(context.Context, *lab.Lab, workload.Ref, workload.TerminalSize) (workload.OpenedTerminalSession, error) {
		consoleCalled = true
		return workload.OpenedTerminalSession{Session: &fakeConsole{}, Endpoint: "/dev/pts/7"}, nil
	}
	app := App{
		Model:         MockModel(),
		Lab:           &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 2048, CPUs: 2}}},
		runtimeAccess: testRuntimeAccess(runtime),
		State:         ViewState{Focus: FocusGraph},
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
		Model:         ModelFromLab(loaded),
		Lab:           loaded,
		LabPath:       path,
		runtimeAccess: testRuntimeAccess(runtime),
		State:         ViewState{Focus: FocusGraph},
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

func TestUplinkRenamePersistsNewIDAndDisplaysIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:            "demo",
		Switches:      []lab.Switch{{ID: "lan", Mode: "bridge", ExternalLinks: []string{"old-uplink"}}},
		ExternalLinks: []lab.ExternalLink{{ID: "old-uplink", Interface: "eth0", Mode: lab.ExternalModeNAT}},
		Layout: lab.Layout{Nodes: map[string]lab.Position{
			"old-uplink": {X: 20, Y: 4},
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Lab: loaded, LabPath: path, State: ViewState{Focus: FocusGraph}}

	app.externalSet("old-uplink", map[string]string{"name": "new-uplink"})

	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ExternalLinks[0].ID; got != "new-uplink" {
		t.Fatalf("persisted uplink id = %q, want new-uplink", got)
	}
	if got := lab.SwitchExternalLinks(reloaded.Switches[0]); !reflect.DeepEqual(got, []string{"new-uplink"}) {
		t.Fatalf("persisted switch uplinks = %#v, want new-uplink", got)
	}
	if _, ok := reloaded.Layout.Nodes["old-uplink"]; ok {
		t.Fatalf("old layout id still present: %#v", reloaded.Layout.Nodes)
	}
	node, ok := nodeByKey(app.Model, NodeKey(NodeExternal, "new-uplink"))
	if !ok {
		t.Fatalf("renamed uplink missing from model: %#v", app.Model.Nodes)
	}
	if node.Label != "new-uplink" {
		t.Fatalf("renamed uplink label = %q, want new-uplink", node.Label)
	}
	if app.State.Message != "configured uplink:new-uplink; runtime will be recreated" {
		t.Fatalf("rename message = %q", app.State.Message)
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
	runtime := &fakeVMRuntime{states: map[string]string{
		NodeKey(NodeVM, loaded.VMs[0].ID):               "running",
		NodeKey(NodeContainer, loaded.Containers[0].ID): "running",
	}}
	runtime.openTerminal = func(_ context.Context, _ *lab.Lab, ref workload.Ref, _ workload.TerminalSize) (workload.OpenedTerminalSession, error) {
		if ref.Type == workload.TypeVM && ref.ID != loaded.VMs[0].ID {
			t.Fatalf("console id = %q", ref.ID)
		}
		endpoint := ref.ID
		if ref.Type == workload.TypeVM {
			endpoint = "/dev/pts/7"
		}
		return workload.OpenedTerminalSession{Session: &fakeConsole{}, Endpoint: endpoint}, nil
	}
	app := App{
		Model:         MockModel(),
		Lab:           loaded,
		runtimeAccess: testRuntimeAccess(runtime),
		LabPath:       path,
		State:         ViewState{Focus: FocusGraph},
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
	if got := runtime.starts; got != 0 {
		t.Fatalf("vm shell started workload %d times", got)
	}
	if app.PendingShell.Session == nil || app.PendingShell.Display != "vm console /dev/pts/7" {
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
	if got := runtime.starts; got != 0 {
		t.Fatalf("container shell started workload %d times", got)
	}
	if app.PendingShell.OpenSession == nil || app.PendingShell.Session != nil || !strings.Contains(app.PendingShell.Display, "foxlab-demo-"+loaded.Containers[0].ID) {
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
		Model:         ModelFromLab(loaded),
		Lab:           loaded,
		runtimeAccess: testRuntimeAccess(runtime),
		VNCViewer:     "/bin/true",
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
		Model:         ModelFromLab(loaded),
		Lab:           loaded,
		runtimeAccess: testRuntimeAccess(runtime),
		VNCViewer:     "/bin/true",
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
		Model:         ModelFromLab(loaded),
		Lab:           loaded,
		runtimeAccess: testRuntimeAccess(runtime),
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
		runtimeAccess: testRuntimeAccess(&fakeVMRuntime{
			states: map[string]string{NodeKey(NodeVM, "vm1"): "running"},
		}),
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
	if app.State.Message != "uplink already connected: uplink1" {
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
