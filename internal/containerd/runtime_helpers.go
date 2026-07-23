package containerd

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"

	"foxlab-cli/internal/lab"
)

var errTaskExitTimeout = errors.New("timed out waiting for container task to exit")

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func deleteTask(ctx context.Context, task containerd.Task) error {
	return deleteTaskWithTimeout(ctx, task, taskExitTimeout)
}

func deleteTaskWithTimeout(ctx context.Context, task containerd.Task, timeout time.Duration) error {
	status, statusErr := task.Status(ctx)
	if statusErr != nil && !errdefs.IsNotFound(statusErr) {
		return statusErr
	}
	if statusErr == nil && status.Status == containerd.Running {
		statusC, waitErr := task.Wait(ctx)
		if waitErr != nil && !errdefs.IsNotFound(waitErr) {
			return fmt.Errorf("wait for running container task: %w", waitErr)
		}
		if err := signalTask(ctx, task, syscall.SIGTERM); err != nil {
			return err
		}
		if waitErr == nil {
			if err := waitTaskExit(ctx, statusC, timeout); err != nil {
				if !errors.Is(err, errTaskExitTimeout) {
					return err
				}
				if err := signalTask(ctx, task, syscall.SIGKILL); err != nil {
					return err
				}
				if err := waitTaskExit(ctx, statusC, timeout); err != nil {
					return err
				}
			}
		}
	}
	_, err := task.Delete(ctx)
	if err == nil || errdefs.IsNotFound(err) {
		return nil
	}
	if !errdefs.IsFailedPrecondition(err) {
		return err
	}
	statusC, waitErr := task.Wait(ctx)
	if waitErr != nil && !errdefs.IsNotFound(waitErr) {
		return fmt.Errorf("wait before force-deleting container task: %w", waitErr)
	}
	if err := signalTask(ctx, task, syscall.SIGKILL); err != nil {
		return err
	}
	if waitErr == nil {
		if err := waitTaskExit(ctx, statusC, timeout); err != nil {
			return err
		}
	}
	_, err = task.Delete(ctx)
	if err == nil || errdefs.IsNotFound(err) {
		return nil
	}
	return err
}

func signalTask(ctx context.Context, task containerd.Task, signal syscall.Signal) error {
	err := task.Kill(ctx, signal)
	if err == nil || errdefs.IsNotFound(err) || errdefs.IsFailedPrecondition(err) {
		return nil
	}
	name := taskSignalName(signal)
	if errdefs.IsPermissionDenied(err) || strings.Contains(strings.ToLower(err.Error()), "permission denied") {
		return fmt.Errorf("send %s to container task: runtime cannot signal container init: permission denied; host AppArmor may be blocking runc (check the kernel audit log for apparmor DENIED): %w", name, err)
	}
	return fmt.Errorf("send %s to container task: %w", name, err)
}

func taskSignalName(signal syscall.Signal) string {
	switch signal {
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGKILL:
		return "SIGKILL"
	default:
		return fmt.Sprintf("signal %d", signal)
	}
}

func waitTaskExit(ctx context.Context, statusC <-chan containerd.ExitStatus, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = taskExitTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-statusC:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errTaskExitTimeout
	}
}

func findContainer(l *lab.Lab, id string) (lab.Container, bool) {
	if l == nil {
		return lab.Container{}, false
	}
	for _, ct := range l.Containers {
		if ct.ID == id {
			return ct, true
		}
	}
	return lab.Container{}, false
}

func containerProcessArgs(ct lab.Container) []string {
	if len(ct.Command) > 0 {
		return ct.Command
	}
	return []string{"/bin/sh", "-lc", "sleep infinity"}
}

func containerShellArgs(ct lab.Container) []string {
	shell := firstContainerShell(ct)
	return []string{shell, "-i"}
}

func containerShellEnv(ct lab.Container, base []string) []string {
	env := setContainerEnv(base, "TERM", "xterm-256color")
	shell := firstContainerShell(ct)
	if !envHasKey(env, "SHELL") {
		env = append(env, "SHELL="+shell)
	}
	return env
}

func setContainerEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && name == key {
			if !replaced {
				out = append(out, key+"="+value)
				replaced = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, key+"="+value)
	}
	return out
}

func envHasKey(env []string, key string) bool {
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && name == key {
			return true
		}
	}
	return false
}

func firstContainerShell(ct lab.Container) string {
	if strings.TrimSpace(ct.Shell) != "" {
		return strings.TrimSpace(ct.Shell)
	}
	return "/bin/sh"
}
