package containerd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	specs "github.com/opencontainers/runtime-spec/specs-go"

	"foxlab-cli/internal/lab"
)

func (r *Runtime) ExecShell(ctx context.Context, l *lab.Lab, id string, in io.Reader, out io.Writer) error {
	ct, ok := findContainer(l, id)
	if !ok {
		return fmt.Errorf("container not found: %s", id)
	}
	address := r.Address
	if address == "" {
		address = DefaultAddress
	}
	namespace := r.Namespace
	if namespace == "" {
		namespace = DefaultNamespace
	}
	client, err := containerd.New(address)
	if err != nil {
		return err
	}
	defer client.Close()

	ctx = namespaces.WithNamespace(ctx, namespace)
	container, err := client.LoadContainer(ctx, l.ManagedContainerName(ct))
	if err != nil {
		return err
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		return err
	}
	execID := "foxlab-shell-" + strings.ToLower(id) + "-" + time.Now().Format("20060102150405.000000000")
	process, err := task.Exec(ctx, execID, containerShellProcess(ct), cio.NewCreator(cio.WithStreams(in, out, nil), cio.WithTerminal))
	if err != nil {
		return err
	}
	defer process.Delete(ctx)

	statusC, err := process.Wait(ctx)
	if err != nil {
		return err
	}
	if err := process.Start(ctx); err != nil {
		return err
	}
	status := <-statusC
	code, _, err := status.Result()
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("container shell exited with status %d", code)
	}
	return nil
}

func containerShellProcess(ct lab.Container) *specs.Process {
	return &specs.Process{
		Terminal: true,
		Cwd:      "/",
		Args:     containerShellArgs(ct),
		Env:      containerEnv(ct),
	}
}

func containerEnv(ct lab.Container) []string {
	env := []string{"TERM=xterm-256color", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"}
	for key, value := range ct.Env {
		env = append(env, key+"="+value)
	}
	return env
}
