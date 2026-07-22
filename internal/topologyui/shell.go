package topologyui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"foxlab-cli/internal/workload"
	"golang.org/x/sys/unix"
)

var errConsoleExit = errors.New("console exit")

type shellCommand struct {
	Display           string
	WorkloadType      string
	WorkloadID        string
	Session           workload.TerminalSession
	OpenSession       func(context.Context) (workload.OpenedTerminalSession, error)
	NativeRun         func(*App) error
	BackgroundCommand func(*App) *exec.Cmd
}

func (a *App) startShell(node Node) {
	a.queueShell(node)
}

func (a *App) RunShell(typ, id string) error {
	a.ensureExternalCommandIO()
	a.queueShell(Node{Type: typ, ID: id})
	if a.PendingShell == nil {
		return stateMessageError(a.State.Message, "shell failed")
	}
	command := *a.PendingShell
	a.PendingShell = nil
	return a.runShell(command)
}

func (a *App) queueShell(node Node) {
	if a.tabs != nil {
		a.openShellTab(node)
		return
	}
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
	if a.currentLab() == nil {
		return fmt.Errorf("shell needs a loaded .lab file")
	}
	stateCtx, stateCancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer stateCancel()
	snapshot := a.runtimeClient().readLiveStatus(stateCtx, a.currentLab(), liveStatusOptions{})
	if snapshot.runtimeErr != nil {
		return snapshot.runtimeErr
	}
	if snapshot.statesErr != nil {
		return fmt.Errorf("runtime status unavailable: %w", snapshot.statesErr)
	}
	key := NodeKey(node.Type, node.ID)
	if normalizeRuntimeState(snapshot.states[key]) == "running" {
		a.ensureService()
		a.applyRuntimeSnapshot(a.currentLab(), snapshot, runtimeSnapshotApplyOptions{})
		return nil
	}
	state := firstNonEmpty(snapshot.states[key], "missing")
	return fmt.Errorf("%s %s is %s; run it first", node.Type, a.displayNodeName(node.Type, node.ID), state)
}

func (a *App) shellCommand(node Node) (shellCommand, bool) {
	if a.currentLab() == nil {
		a.State.Message = "shell needs a loaded .lab file"
		return shellCommand{}, false
	}
	switch node.Type {
	case NodeVM:
		ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
		defer cancel()
		opened, err := a.openTerminalSession(ctx, workload.Ref{Type: workload.TypeVM, ID: node.ID}, workload.TerminalSize{})
		if err != nil {
			a.State.Message = "vm console failed: " + err.Error()
			return shellCommand{}, false
		}
		display := "vm console " + firstNonEmpty(opened.Endpoint, node.ID)
		return shellCommand{Display: display, WorkloadType: workload.TypeVM, Session: opened.Session}, true
	case NodeContainer:
		ct, ok := a.labContainer(node.ID)
		if !ok {
			a.State.Message = "container not found: " + a.displayNodeName(node.Type, node.ID)
			return shellCommand{}, false
		}
		display := "container shell " + a.currentLab().ManagedContainerName(ct)
		return shellCommand{
			Display:      display,
			WorkloadType: workload.TypeContainer,
			OpenSession: func(ctx context.Context) (workload.OpenedTerminalSession, error) {
				return a.openTerminalSession(ctx, workload.Ref{Type: workload.TypeContainer, ID: node.ID}, workload.TerminalSize{})
			},
		}, true
	default:
		a.State.Message = "shell is available for vm and container nodes"
		return shellCommand{}, false
	}
}

func containerShellNeedsRestart(detail string) bool {
	detail = strings.ToLower(detail)
	return strings.Contains(detail, "input/output error") ||
		strings.Contains(detail, "cannot resize a stopped container") ||
		strings.Contains(detail, "container is stopped") ||
		strings.Contains(detail, "task not found")
}

func (a *App) runShell(command shellCommand) error {
	if command.NativeRun != nil {
		return command.NativeRun(a)
	}
	if command.Session != nil {
		return a.runTerminalSession(context.Background(), command.Session, command.Display, command.WorkloadType)
	}
	if command.OpenSession != nil {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer stop()
		opened, err := command.OpenSession(ctx)
		if err != nil {
			return err
		}
		return a.runTerminalSession(ctx, opened.Session, command.Display, command.WorkloadType)
	}
	return fmt.Errorf("shell command has no runner")
}

func (a *App) ensureExternalCommandIO() {
	if a.In == nil {
		a.In = os.Stdin
	}
	if a.Out == nil {
		a.Out = os.Stdout
	}
	a.ensureService()
}

func stateMessageError(message, fallback string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = fallback
	}
	return errors.New(message)
}

func (a *App) runTerminalSession(ctx context.Context, session workload.TerminalSession, display, workloadType string) error {
	defer session.Close()
	makeRaw := makeShellRaw
	waitForBackend := false
	if workloadType == workload.TypeContainer {
		makeRaw = makeShellBlockingRaw
		waitForBackend = true
	}
	restoreRaw, err := makeRaw(int(a.In.Fd()))
	if err != nil {
		return err
	}
	defer restoreRaw()

	_, _ = io.WriteString(a.Out, consoleConnectMessage(display))
	done := make(chan struct{})
	defer close(done)
	go func() {
		_ = copyConsoleOutput(a.Out, session, done)
	}()
	var wait <-chan error
	if waitForBackend {
		waitC := make(chan error, 1)
		go func() { waitC <- session.Wait(ctx) }()
		wait = waitC
	}

	buf := make([]byte, 4096)
	for {
		select {
		case waitErr := <-wait:
			if waitErr != nil && containerShellNeedsRestart(waitErr.Error()) {
				return fmt.Errorf("%w; stop and run the container to rebuild/restart its rootfs", waitErr)
			}
			return waitErr
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		ready, pollErr := terminalInputReady(int(a.In.Fd()), 100*time.Millisecond)
		if pollErr != nil {
			return pollErr
		}
		if !ready {
			continue
		}
		n, err := a.In.Read(buf)
		if n > 0 {
			if writeErr := writeConsoleInput(session, buf[:n]); writeErr != nil {
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

func terminalInputReady(fd int, timeout time.Duration) (bool, error) {
	pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	ready, err := unix.Poll(pollFDs, int(timeout.Milliseconds()))
	if err != nil {
		if errors.Is(err, unix.EINTR) {
			return false, nil
		}
		return false, err
	}
	return ready > 0 && pollFDs[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0, nil
}

func consoleConnectMessage(display string) string {
	message := "\r\nconnected to " + display + "; Ctrl-] exits\r\n"
	if strings.HasPrefix(display, "vm console ") {
		message += "VM console is a serial port; use VNC unless the guest has ttyS0/getty enabled.\r\n"
	}
	return message
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
