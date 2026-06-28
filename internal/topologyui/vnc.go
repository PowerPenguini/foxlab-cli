package topologyui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
)

func (a *App) startVNC(node Node) {
	a.queueVNC(node)
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
		return shellCommand{}, fmt.Errorf("vm not found: %s", node.ID)
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
		Display: "vnc " + target,
		NativeRun: func(a *App) error {
			cmd := vncViewerCommand(viewer, target)
			cmd.Stdin = a.In
			cmd.Stdout = a.Out
			cmd.Stderr = a.Out
			return cmd.Run()
		},
	}, nil
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

func (a *App) refreshVNCWorkloadStatus(node Node) error {
	runtime, closeRuntime, err := a.runtime()
	if err != nil {
		return err
	}
	defer closeRuntime()
	ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer cancel()
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	var statesErr error
	if states, err := runtime.States(ctx, a.Lab); err == nil {
		a.WorkloadStates = cloneRuntimeStateMap(states)
		if a.Service != nil {
			a.Service.States = a.WorkloadStates
		}
		a.applyWorkloadStates()
	} else {
		statesErr = err
	}
	if err := a.refreshVNCPortsWithRuntime(ctx, runtime); err != nil {
		if statesErr != nil {
			return fmt.Errorf("runtime status unavailable: %w", statesErr)
		}
		return err
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
