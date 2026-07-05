package containerd

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"syscall"
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
	setupCtx, cancelSetup := context.WithTimeout(ctx, 15*time.Second)
	defer cancelSetup()
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

	setupCtx = namespaces.WithNamespace(setupCtx, namespace)
	container, err := client.LoadContainer(setupCtx, l.ManagedContainerName(ct))
	if err != nil {
		return err
	}
	task, err := container.Task(setupCtx, nil)
	if err != nil {
		return err
	}
	taskStatus, err := task.Status(setupCtx)
	if err != nil {
		return err
	}
	if taskStatus.Status != containerd.Running {
		return fmt.Errorf("container task is %s, not running", taskStatus.Status)
	}
	spec, err := container.Spec(setupCtx)
	if err != nil {
		return err
	}
	execID := "foxlab-shell-" + strings.ToLower(id) + "-" + time.Now().Format("20060102150405.000000000")
	exitC := make(chan struct{})
	stdin := newShellExitReader(in, exitC)
	process, err := task.Exec(setupCtx, execID, containerShellProcess(ct, spec.Process), cio.NewCreator(cio.WithStreams(stdin, out, out), cio.WithTerminal))
	if err != nil {
		return err
	}
	if ioSet := process.IO(); ioSet != nil {
		defer ioSet.Cancel()
	}
	defer deleteShellProcess(namespace, process)

	runCtx := namespaces.WithNamespace(ctx, namespace)
	statusC, err := process.Wait(runCtx)
	if err != nil {
		return err
	}
	if err := process.Start(setupCtx); err != nil {
		return err
	}
	var status containerd.ExitStatus
	select {
	case <-exitC:
		killShellProcess(namespace, process, syscall.SIGTERM)
		select {
		case <-statusC:
		case <-runCtx.Done():
			return runCtx.Err()
		case <-time.After(2 * time.Second):
			killShellProcess(namespace, process, syscall.SIGKILL)
			select {
			case <-statusC:
			case <-runCtx.Done():
				return runCtx.Err()
			}
		}
		return nil
	case status = <-statusC:
	case <-runCtx.Done():
		deleteShellProcess(namespace, process)
		return runCtx.Err()
	}
	code, _, err := status.Result()
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("container shell exited with status %d", code)
	}
	return nil
}

func killShellProcess(namespace string, process containerd.Process, signal syscall.Signal) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = namespaces.WithNamespace(ctx, namespace)
	_ = process.Kill(ctx, signal)
}

func deleteShellProcess(namespace string, process containerd.Process) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = namespaces.WithNamespace(ctx, namespace)
	if _, err := process.Delete(ctx); err == nil {
		return
	}
	_, _ = process.Delete(ctx, containerd.WithProcessKill)
}

type shellExitReader struct {
	r    io.Reader
	exit chan<- struct{}
	once sync.Once
}

func newShellExitReader(r io.Reader, exit chan<- struct{}) io.Reader {
	return &shellExitReader{r: r, exit: exit}
}

func (r *shellExitReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	for i := 0; i < n; i++ {
		if p[i] != 0x1d {
			continue
		}
		r.once.Do(func() { close(r.exit) })
		if i > 0 {
			return i, nil
		}
		return 0, io.EOF
	}
	return n, err
}

func containerShellProcess(ct lab.Container, base *specs.Process) *specs.Process {
	process := specs.Process{}
	if base != nil {
		process = *base
	}
	process.Terminal = true
	if process.Cwd == "" {
		process.Cwd = "/"
	}
	process.Args = containerShellArgs(ct)
	process.Env = containerShellEnv(ct, process.Env)
	return &process
}
