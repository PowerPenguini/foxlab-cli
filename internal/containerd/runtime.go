package containerd

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"foxlab-cli/internal/hostnet"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

const (
	DefaultAddress   = "/run/containerd/containerd.sock"
	DefaultNamespace = "foxlab"
)

type CommandRunner interface {
	Output(context.Context, string, ...string) (string, error)
	Run(context.Context, string, ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %v: %w: %s", name, args, err, string(output))
	}
	return string(output), nil
}

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	_, err := r.Output(ctx, name, args...)
	return err
}

type Runtime struct {
	Address   string
	Namespace string
	Runner    CommandRunner
	Bridge    *hostnet.Bridge
}

func NewRuntime(address string) *Runtime {
	if address == "" {
		address = DefaultAddress
	}
	return &Runtime{
		Address:   address,
		Namespace: DefaultNamespace,
		Runner:    ExecRunner{},
		Bridge:    hostnet.NewBridge(),
	}
}

func (r *Runtime) Close() error { return nil }

func (r *Runtime) States(ctx context.Context, l *lab.Lab) (map[string]string, error) {
	states := map[string]string{}
	for _, ct := range l.Containers {
		states[workload.Key(workload.Ref{Type: workload.TypeContainer, ID: ct.ID})] = "missing"
	}
	tasks, err := r.ctrOutput(ctx, "tasks", "ls")
	if err != nil {
		return states, err
	}
	for _, line := range strings.Split(tasks, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "TASK" {
			continue
		}
		if ct, ok := findContainerByManagedName(l, fields[0]); ok {
			states[workload.Key(workload.Ref{Type: workload.TypeContainer, ID: ct.ID})] = strings.ToLower(fields[1])
		}
	}
	return states, nil
}

func (r *Runtime) Start(ctx context.Context, l *lab.Lab, ref workload.Ref) error {
	if ref.Type != workload.TypeContainer {
		return fmt.Errorf("containerd cannot start workload type %q", ref.Type)
	}
	ct, ok := findContainer(l, ref.ID)
	if !ok {
		return fmt.Errorf("container not found: %s", ref.ID)
	}
	name := l.ManagedContainerName(ct)
	_ = r.ctrRun(ctx, "images", "pull", ct.Image)
	if running, _ := r.taskRunning(ctx, name); running {
		return nil
	}
	if exists, _ := r.containerExists(ctx, name); !exists {
		args := []string{"run", "--detach"}
		for key, value := range ct.Env {
			args = append(args, "--env", key+"="+value)
		}
		args = append(args, ct.Image, name)
		args = append(args, ct.Command...)
		if err := r.ctrRun(ctx, args...); err != nil {
			return err
		}
	} else if err := r.ctrRun(ctx, "tasks", "start", name); err != nil {
		return err
	}
	pid, err := r.taskPID(ctx, name)
	if err != nil {
		return err
	}
	if r.Bridge != nil {
		if err := r.Bridge.AttachContainer(ctx, l, ct, pid); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) Stop(ctx context.Context, l *lab.Lab, ref workload.Ref) error {
	if ref.Type != workload.TypeContainer {
		return fmt.Errorf("containerd cannot stop workload type %q", ref.Type)
	}
	ct, ok := findContainer(l, ref.ID)
	if !ok {
		return fmt.Errorf("container not found: %s", ref.ID)
	}
	if r.Bridge != nil {
		r.Bridge.DetachContainer(ctx, l, ct)
	}
	name := l.ManagedContainerName(ct)
	_ = r.ctrRun(ctx, "tasks", "kill", name)
	_ = r.ctrRun(ctx, "tasks", "delete", "--force", name)
	return nil
}

func (r *Runtime) containerExists(ctx context.Context, name string) (bool, error) {
	if _, err := r.ctrOutput(ctx, "containers", "info", name); err != nil {
		return false, nil
	}
	return true, nil
}

func (r *Runtime) taskRunning(ctx context.Context, name string) (bool, error) {
	output, err := r.ctrOutput(ctx, "tasks", "ls")
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == name {
			return strings.EqualFold(fields[1], "running"), nil
		}
	}
	return false, nil
}

func (r *Runtime) taskPID(ctx context.Context, name string) (int, error) {
	output, err := r.ctrOutput(ctx, "tasks", "ls")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == name {
			pid, err := strconv.Atoi(fields[2])
			if err != nil {
				return 0, err
			}
			return pid, nil
		}
	}
	return 0, fmt.Errorf("container task pid not found: %s", name)
}

func (r *Runtime) ctrOutput(ctx context.Context, args ...string) (string, error) {
	if r.Runner == nil {
		r.Runner = ExecRunner{}
	}
	return r.Runner.Output(ctx, "ctr", r.ctrArgs(args...)...)
}

func (r *Runtime) ctrRun(ctx context.Context, args ...string) error {
	if r.Runner == nil {
		r.Runner = ExecRunner{}
	}
	return r.Runner.Run(ctx, "ctr", r.ctrArgs(args...)...)
}

func (r *Runtime) ctrArgs(args ...string) []string {
	address := r.Address
	if address == "" {
		address = DefaultAddress
	}
	namespace := r.Namespace
	if namespace == "" {
		namespace = DefaultNamespace
	}
	out := []string{"--address", address, "--namespace", namespace}
	return append(out, args...)
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

func findContainerByManagedName(l *lab.Lab, name string) (lab.Container, bool) {
	for _, ct := range l.Containers {
		if l.ManagedContainerName(ct) == name {
			return ct, true
		}
	}
	return lab.Container{}, false
}
