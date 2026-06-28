package topologyui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"foxlab-cli/internal/foxruntime"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/virt"
)

var errConsoleExit = errors.New("console exit")

type shellCommand struct {
	Display   string
	Console   io.ReadWriteCloser
	NativeRun func(*App) error
}

func (a *App) startShell(node Node) {
	a.queueShell(node)
}

func (a *App) queueShell(node Node) {
	if err := a.ensureShellWorkloadRunning(node); err != nil {
		a.State.Message = "shell failed: " + err.Error()
		return
	}
	command, ok := a.shellCommand(node)
	if !ok {
		return
	}
	a.PendingShell = &command
	a.State.Message = "opening shell: " + command.Display
}

func (a *App) ensureShellWorkloadRunning(node Node) error {
	if node.Type != NodeVM && node.Type != NodeContainer {
		return fmt.Errorf("shell is available for vm and container nodes")
	}
	if a.Lab == nil {
		return fmt.Errorf("shell needs a loaded .lab file")
	}
	runtime, closeRuntime, err := a.runtime()
	if err != nil {
		return err
	}
	defer closeRuntime()
	stateCtx, stateCancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer stateCancel()
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if states, err := runtime.States(stateCtx, a.Lab); err == nil {
		key := NodeKey(node.Type, node.ID)
		if normalizeRuntimeState(states[key]) == "running" {
			a.WorkloadStates = cloneRuntimeStateMap(states)
			a.ensureService().States = a.WorkloadStates
			a.applyWorkloadStates()
			return nil
		}
		state := firstNonEmpty(states[key], "missing")
		return fmt.Errorf("%s %s is %s; run it first", node.Type, node.ID, state)
	} else {
		return fmt.Errorf("runtime status unavailable: %w", err)
	}
}

func (a *App) shellCommand(node Node) (shellCommand, bool) {
	if a.Lab == nil {
		a.State.Message = "shell needs a loaded .lab file"
		return shellCommand{}, false
	}
	switch node.Type {
	case NodeVM:
		ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
		defer cancel()
		console, display, err := a.vmConsole(ctx, node.ID)
		if err != nil {
			a.State.Message = "vm console failed: " + err.Error()
			return shellCommand{}, false
		}
		return shellCommand{Display: display, Console: console}, true
	case NodeContainer:
		ct, ok := a.labContainer(node.ID)
		if !ok {
			a.State.Message = "container not found: " + node.ID
			return shellCommand{}, false
		}
		display := "container shell " + a.Lab.ManagedContainerName(ct)
		cmd := a.containerShellExecCommand(ct)
		return shellCommand{
			Display: display,
			NativeRun: func(a *App) error {
				return a.runContainerShellExec(cmd)
			},
		}, true
	default:
		a.State.Message = "shell is available for vm and container nodes"
		return shellCommand{}, false
	}
}

func (a *App) runContainerShellExec(cmd *exec.Cmd) error {
	var stderr bytes.Buffer
	cmd.Stdin = a.In
	cmd.Stdout = a.Out
	cmd.Stderr = io.MultiWriter(a.Out, &stderr)
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			if containerShellNeedsRestart(detail) {
				detail += "; stop and run the container to rebuild/restart its rootfs"
			}
			return fmt.Errorf("%w: %s", err, detail)
		}
		return err
	}
	return nil
}

func containerShellNeedsRestart(detail string) bool {
	detail = strings.ToLower(detail)
	return strings.Contains(detail, "input/output error") ||
		strings.Contains(detail, "cannot resize a stopped container") ||
		strings.Contains(detail, "container is stopped") ||
		strings.Contains(detail, "task not found")
}

func (a *App) containerShellExecCommand(ct lab.Container) *exec.Cmd {
	args := []string{}
	if address := firstNonEmpty(a.ContainerdAddress, foxruntime.ContainerdAddressFromLab(a.Lab)); address != "" {
		args = append(args, "--address", address)
	}
	args = append(args,
		"--namespace", "foxlab",
		"tasks", "exec",
		"--tty",
		"--exec-id", containerShellExecID(ct.ID),
		a.Lab.ManagedContainerName(ct),
	)
	args = append(args, containerShellArgs(ct)...)
	return exec.Command("ctr", args...)
}

func containerShellExecID(id string) string {
	return "foxlab-shell-" + strings.ToLower(id) + "-" + time.Now().Format("20060102150405.000000000")
}

func containerShellArgs(ct lab.Container) []string {
	shell := strings.TrimSpace(ct.Shell)
	if shell == "" {
		shell = "/bin/sh"
	}
	return []string{shell, "-i"}
}

func (a *App) runShell(command shellCommand) error {
	if command.NativeRun != nil {
		return command.NativeRun(a)
	}
	if command.Console != nil {
		return a.runConsole(command.Console, command.Display)
	}
	return fmt.Errorf("shell command has no runner")
}

func (a *App) vmConsole(ctx context.Context, id string) (io.ReadWriteCloser, string, error) {
	if a.VMConsole != nil {
		return a.VMConsole(ctx, a.Lab, id)
	}
	runtime, err := virt.NewLibvirtRuntime(a.LibvirtURI)
	if err != nil {
		return nil, "", err
	}
	console, err := runtime.OpenConsole(ctx, a.Lab, id)
	if err != nil {
		_ = runtime.Close()
		return nil, "", err
	}
	display := "vm console " + id
	if console.Path() != "" {
		display = "vm console " + console.Path()
	}
	return &runtimeConsole{console: console, closeRuntime: runtime.Close}, display, nil
}

type runtimeConsole struct {
	console      io.ReadWriteCloser
	closeRuntime func() error
}

func (c *runtimeConsole) Read(p []byte) (int, error) {
	return c.console.Read(p)
}

func (c *runtimeConsole) Write(p []byte) (int, error) {
	return c.console.Write(p)
}

func (c *runtimeConsole) Close() error {
	consoleErr := c.console.Close()
	runtimeErr := c.closeRuntime()
	if consoleErr != nil {
		return consoleErr
	}
	return runtimeErr
}

func (a *App) runConsole(console io.ReadWriteCloser, display string) error {
	defer console.Close()
	restoreRaw, err := makeShellRaw(int(a.In.Fd()))
	if err != nil {
		return err
	}
	defer restoreRaw()

	_, _ = io.WriteString(a.Out, "\r\nconnected to "+display+"; Ctrl-] exits\r\n")
	done := make(chan struct{})
	defer close(done)
	go func() {
		_ = copyConsoleOutput(a.Out, console, done)
	}()

	buf := make([]byte, 4096)
	for {
		n, err := a.In.Read(buf)
		if n > 0 {
			if writeErr := writeConsoleInput(console, buf[:n]); writeErr != nil {
				if errors.Is(writeErr, errConsoleExit) {
					return nil
				}
				return writeErr
			}
		}
		if err != nil {
			if err == io.EOF {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			return err
		}
		if n == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func writeConsoleInput(w io.Writer, input []byte) error {
	start := 0
	for i, b := range input {
		if b != 0x1d {
			continue
		}
		if i > start {
			if err := writeFull(w, input[start:i]); err != nil {
				return err
			}
		}
		return errConsoleExit
	}
	if len(input) == 0 {
		return nil
	}
	return writeFull(w, input)
}

func copyConsoleOutput(dst io.Writer, src io.Reader, done <-chan struct{}) error {
	buf := make([]byte, 4096)
	var lastErr string
	for {
		select {
		case <-done:
			return nil
		default:
		}
		n, err := src.Read(buf)
		if n > 0 {
			if writeErr := writeFull(dst, buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			message := err.Error()
			if message != lastErr {
				_ = writeFull(dst, []byte("\r\nconsole read error: "+message+"\r\n"))
				lastErr = message
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if n == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func writeFull(w io.Writer, input []byte) error {
	for len(input) > 0 {
		n, err := w.Write(input)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		input = input[n:]
	}
	return nil
}

func (a *App) executeShellCommand(fields []string) {
	if !a.requireExactCommandArgs(fields, 3, "usage: shell <vm|container> <id>") {
		return
	}
	typ := fields[1]
	switch typ {
	case "ct", "cont":
		typ = NodeContainer
	case "vm":
	}
	a.queueShell(Node{Type: typ, ID: fields[2]})
}
