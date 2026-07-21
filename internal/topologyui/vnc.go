package topologyui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
	"time"
)

type managedVNCViewer struct {
	id      string
	command *exec.Cmd
	done    chan error
}

func (a *App) startVNC(node Node) {
	if a.vncViewerRunning(node.ID) {
		if err := a.stopVNCViewer(node.ID); err != nil {
			a.State.Message = "vnc stop failed: " + err.Error()
		} else {
			a.State.Message = "vnc stopped"
		}
		return
	}
	a.queueVNC(node)
}

func (a *App) RunVNC(id string) error {
	a.ensureExternalCommandIO()
	a.queueVNC(Node{Type: NodeVM, ID: id})
	if a.PendingVNC == nil {
		return stateMessageError(a.State.Message, "vnc failed")
	}
	command := *a.PendingVNC
	a.PendingVNC = nil
	return a.runShell(command)
}

func (a *App) queueVNC(node Node) {
	command, err := a.vncCommand(node)
	if err != nil {
		a.State.Message = "vnc start failed: " + err.Error()
		return
	}
	a.PendingVNC = &command
	a.State.Message = "opening vnc: " + command.Display
}

func (a *App) vncCommand(node Node) (shellCommand, error) {
	if node.Type != NodeVM {
		return shellCommand{}, fmt.Errorf("vnc is available for vm nodes")
	}
	if a.Lab == nil {
		return shellCommand{}, fmt.Errorf("vnc needs a loaded .lab file")
	}
	vm, ok := a.labVM(node.ID)
	if !ok {
		return shellCommand{}, fmt.Errorf("vm not found: %s", a.displayNodeName(node.Type, node.ID))
	}
	if !vm.VNC {
		return shellCommand{}, fmt.Errorf("vnc is disabled; enable it in Configuration")
	}
	viewerName := firstNonEmpty(a.VNCViewer, "vncviewer")
	viewer, err := exec.LookPath(viewerName)
	if err != nil {
		return shellCommand{}, fmt.Errorf("%s not found in PATH", viewerName)
	}
	port := a.vncPortForVM(node.ID)
	if port == 0 {
		if err := a.refreshVNCWorkloadStatus(node); err != nil {
			return shellCommand{}, err
		}
		port = a.vncPortForVM(node.ID)
	}
	if port == 0 {
		if refreshed, ok := a.modelNode(NodeVM, node.ID); ok && vncNeedsRestart(refreshed.State) {
			return shellCommand{}, fmt.Errorf("vnc needs restart: stop and run the VM to apply VNC")
		}
		return shellCommand{}, fmt.Errorf("vnc port is not available yet")
	}
	target := "127.0.0.1::" + strconv.Itoa(port)
	return shellCommand{
		Display:      "vnc " + target,
		WorkloadType: NodeVM,
		WorkloadID:   node.ID,
		NativeRun: func(a *App) error {
			cmd := vncViewerCommand(viewer, target)
			cmd.Stdin = a.In
			cmd.Stdout = a.Out
			cmd.Stderr = a.Out
			return cmd.Run()
		},
		BackgroundCommand: func(_ *App) *exec.Cmd {
			return vncViewerCommand(viewer, target)
		},
	}, nil
}

func (a *App) startVNCViewer(command shellCommand) error {
	if command.WorkloadID == "" || command.BackgroundCommand == nil {
		return fmt.Errorf("vnc command has no background runner")
	}
	if a.vncViewerRunning(command.WorkloadID) {
		return nil
	}
	cmd := command.BackgroundCommand(a)
	if cmd == nil {
		return fmt.Errorf("vnc command is empty")
	}
	cmd.Stdin = nil
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	viewer := &managedVNCViewer{id: command.WorkloadID, command: cmd, done: make(chan error, 1)}
	if a.vncViewers == nil {
		a.vncViewers = map[string]*managedVNCViewer{}
	}
	key := NodeKey(NodeVM, command.WorkloadID)
	a.vncViewers[key] = viewer
	a.setVNCViewerActive(command.WorkloadID, true)
	go func() {
		viewer.done <- cmd.Wait()
		if a.tabs != nil {
			a.tabs.notify()
		}
	}()
	return nil
}

func (a *App) vncViewerRunning(id string) bool {
	key := NodeKey(NodeVM, id)
	viewer, ok := a.vncViewers[key]
	if !ok || viewer == nil {
		a.setVNCViewerActive(id, false)
		return false
	}
	select {
	case <-viewer.done:
		delete(a.vncViewers, key)
		a.setVNCViewerActive(id, false)
		return false
	default:
		a.setVNCViewerActive(id, true)
		return true
	}
}

func (a *App) setVNCViewerActive(id string, active bool) {
	if a.State.VNCViewerActive == nil {
		if !active {
			return
		}
		a.State.VNCViewerActive = map[string]bool{}
	}
	key := NodeKey(NodeVM, id)
	if active {
		a.State.VNCViewerActive[key] = true
		return
	}
	delete(a.State.VNCViewerActive, key)
}

func (a *App) stopVNCViewer(id string) error {
	key := NodeKey(NodeVM, id)
	viewer, ok := a.vncViewers[key]
	if !ok || viewer == nil {
		a.setVNCViewerActive(id, false)
		return nil
	}
	err := signalVNCViewer(viewer, syscall.SIGTERM)
	if err != nil && !errors.Is(err, os.ErrProcessDone) && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	delete(a.vncViewers, key)
	a.setVNCViewerActive(id, false)
	return nil
}

func signalVNCViewer(viewer *managedVNCViewer, signal syscall.Signal) error {
	if viewer == nil || viewer.command == nil || viewer.command.Process == nil {
		return nil
	}
	return syscall.Kill(-viewer.command.Process.Pid, signal)
}

func (a *App) refreshVNCViewerProcesses() {
	for _, viewer := range a.vncViewers {
		a.vncViewerRunning(viewer.id)
	}
}

func (a *App) stopAllVNCViewers() {
	viewers := make([]*managedVNCViewer, 0, len(a.vncViewers))
	for _, viewer := range a.vncViewers {
		viewers = append(viewers, viewer)
		_ = a.stopVNCViewer(viewer.id)
	}
	for _, viewer := range viewers {
		select {
		case <-viewer.done:
		case <-time.After(500 * time.Millisecond):
			_ = signalVNCViewer(viewer, syscall.SIGKILL)
			select {
			case <-viewer.done:
			case <-time.After(500 * time.Millisecond):
			}
		}
	}
}

func vncViewerCommand(viewer, target string) *exec.Cmd {
	if os.Geteuid() == 0 {
		if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
			if u, err := user.Lookup(sudoUser); err == nil {
				args := append([]string{"-u", sudoUser, "env"}, vncViewerUserEnv(u)...)
				args = append(args, viewer, target)
				return exec.Command("sudo", args...)
			}
		}
	}
	return exec.Command(viewer, target)
}

func vncViewerUserEnv(u *user.User) []string {
	runtimeDir := firstNonEmpty(os.Getenv("XDG_RUNTIME_DIR"), "/run/user/"+u.Uid)
	env := []string{
		"HOME=" + u.HomeDir,
		"USER=" + u.Username,
		"LOGNAME=" + u.Username,
		"XDG_RUNTIME_DIR=" + runtimeDir,
	}
	for _, key := range []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_SESSION_TYPE"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	if value := os.Getenv("DBUS_SESSION_BUS_ADDRESS"); value != "" {
		env = append(env, "DBUS_SESSION_BUS_ADDRESS="+value)
	} else {
		env = append(env, "DBUS_SESSION_BUS_ADDRESS=unix:path="+runtimeDir+"/bus")
	}
	if value := os.Getenv("XAUTHORITY"); value != "" {
		env = append(env, "XAUTHORITY="+value)
	}
	return env
}

func (a *App) refreshVNCWorkloadStatus(_ Node) error {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer cancel()
	snapshot := a.runtimeClient().readLiveStatus(ctx, a.Lab, liveStatusOptions{includeVNC: true})
	if snapshot.runtimeErr != nil {
		return snapshot.runtimeErr
	}
	a.applyRuntimeSnapshot(a.Lab, snapshot, runtimeSnapshotApplyOptions{})
	if snapshot.vncErr != nil {
		if snapshot.statesErr != nil {
			return fmt.Errorf("runtime status unavailable: %w", snapshot.statesErr)
		}
		return snapshot.vncErr
	}
	return nil
}

func (a *App) vncPortForVM(id string) int {
	key := NodeKey(NodeVM, id)
	if port := a.VNCPorts[key]; port > 0 {
		return port
	}
	if node, ok := a.modelNode(NodeVM, id); ok {
		return vncPort(node)
	}
	return 0
}

func (a *App) modelNode(typ, id string) (Node, bool) {
	key := NodeKey(typ, id)
	for _, node := range a.Model.Nodes {
		if node.Key() == key {
			return node, true
		}
	}
	return Node{}, false
}
