package containerd

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"

	"foxlab-cli/internal/lab"
)

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
	status, statusErr := task.Status(ctx)
	if statusErr != nil && !errdefs.IsNotFound(statusErr) {
		return statusErr
	}
	if statusErr == nil && status.Status == containerd.Running {
		statusC, waitErr := task.Wait(ctx)
		if waitErr == nil {
			select {
			case <-statusC:
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(taskExitTimeout):
				if err := task.Kill(ctx, syscall.SIGKILL); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsFailedPrecondition(err) {
					return fmt.Errorf("kill container task: %w", err)
				}
				if err := waitTaskExit(ctx, statusC, taskExitTimeout); err != nil {
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
	if err := task.Kill(ctx, syscall.SIGKILL); err != nil && !errdefs.IsNotFound(err) && !errdefs.IsFailedPrecondition(err) {
		return fmt.Errorf("kill container task: %w", err)
	}
	statusC, waitErr := task.Wait(ctx)
	if waitErr == nil {
		if err := waitTaskExit(ctx, statusC, taskExitTimeout); err != nil {
			return err
		}
	}
	_, err = task.Delete(ctx)
	if err == nil || errdefs.IsNotFound(err) {
		return nil
	}
	return err
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
		return fmt.Errorf("timed out waiting for container task to exit")
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
