package containerd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"

	"foxlab-cli/internal/hostnet"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

const (
	DefaultAddress   = "/run/containerd/containerd.sock"
	DefaultNamespace = "foxlab"
	configLabel      = "foxlab.config.sha256"
)

type Runtime struct {
	Address   string
	Namespace string
	Bridge    *hostnet.Bridge
}

func NewRuntime(address string) *Runtime {
	if address == "" {
		address = DefaultAddress
	}
	return &Runtime{
		Address:   address,
		Namespace: DefaultNamespace,
		Bridge:    hostnet.NewBridge(),
	}
}

func (r *Runtime) Close() error { return nil }

func (r *Runtime) States(ctx context.Context, l *lab.Lab) (map[string]string, error) {
	client, ctx, closeClient, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	defer closeClient()
	states := map[string]string{}
	for _, ct := range l.Containers {
		key := workload.Key(workload.Ref{Type: workload.TypeContainer, ID: ct.ID})
		states[key] = "missing"
		container, err := client.LoadContainer(ctx, l.ManagedContainerName(ct))
		if err != nil {
			if errdefs.IsNotFound(err) {
				continue
			}
			return states, err
		}
		task, err := container.Task(ctx, nil)
		if err != nil {
			if errdefs.IsNotFound(err) {
				states[key] = "created"
				continue
			}
			return states, err
		}
		status, err := task.Status(ctx)
		if err != nil {
			states[key] = "unknown"
			continue
		}
		states[key] = string(status.Status)
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
	client, ctx, closeClient, err := r.client(ctx)
	if err != nil {
		return err
	}
	defer closeClient()
	name := l.ManagedContainerName(ct)
	image, err := client.Pull(ctx, ct.Image, containerd.WithPullUnpack)
	if err != nil {
		return err
	}
	container, err := client.LoadContainer(ctx, name)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
		container, err = createContainer(ctx, client, name, image, ct)
	} else if changed, err := containerConfigChanged(ctx, container, ct); err != nil {
		return err
	} else if changed {
		if err := deleteContainer(ctx, container); err != nil {
			return err
		}
		container, err = createContainer(ctx, client, name, image, ct)
	}
	if err != nil {
		return err
	}
	task, err := container.Task(ctx, nil)
	if err == nil {
		status, statusErr := task.Status(ctx)
		if statusErr == nil && status.Status == containerd.Running {
			if r.Bridge != nil {
				return r.Bridge.AttachContainer(ctx, l, ct, int(task.Pid()))
			}
			return nil
		}
		_ = deleteTask(ctx, task)
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	task, err = container.NewTask(ctx, cio.NullIO)
	if err != nil {
		return err
	}
	if err := task.Start(ctx); err != nil {
		_ = deleteTask(ctx, task)
		return err
	}
	if r.Bridge != nil {
		if err := r.Bridge.AttachContainer(ctx, l, ct, int(task.Pid())); err != nil {
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
	client, ctx, closeClient, err := r.client(ctx)
	if err != nil {
		return err
	}
	defer closeClient()
	container, err := client.LoadContainer(ctx, l.ManagedContainerName(ct))
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	_ = task.Kill(ctx, syscall.SIGTERM)
	return deleteTask(ctx, task)
}

func (r *Runtime) client(ctx context.Context) (*containerd.Client, context.Context, func(), error) {
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
		return nil, nil, nil, err
	}
	return client, namespaces.WithNamespace(ctx, namespace), func() { _ = client.Close() }, nil
}

func containerSpecOpts(image containerd.Image, ct lab.Container) []oci.SpecOpts {
	opts := []oci.SpecOpts{
		oci.WithImageConfig(image),
	}
	if len(ct.Command) > 0 {
		opts = append(opts, oci.WithProcessArgs(ct.Command...))
	}
	env := []string{}
	for key, value := range ct.Env {
		env = append(env, key+"="+value)
	}
	if len(env) > 0 {
		opts = append(opts, oci.WithEnv(env))
	}
	return opts
}

func createContainer(ctx context.Context, client *containerd.Client, name string, image containerd.Image, ct lab.Container) (containerd.Container, error) {
	return client.NewContainer(
		ctx,
		name,
		containerd.WithImage(image),
		containerd.WithNewSnapshot(name+"-rootfs", image),
		containerd.WithNewSpec(containerSpecOpts(image, ct)...),
		containerd.WithContainerLabels(map[string]string{configLabel: containerConfigHash(ct)}),
	)
}

func containerConfigChanged(ctx context.Context, container containerd.Container, ct lab.Container) (bool, error) {
	labels, err := container.Labels(ctx)
	if err != nil {
		return false, err
	}
	return labels[configLabel] != containerConfigHash(ct), nil
}

func deleteContainer(ctx context.Context, container containerd.Container) error {
	task, err := container.Task(ctx, nil)
	if err == nil {
		if err := deleteTask(ctx, task); err != nil {
			return err
		}
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	if err := container.Delete(ctx); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

func containerConfigHash(ct lab.Container) string {
	var parts []string
	parts = append(parts, "image="+ct.Image)
	parts = append(parts, "shell="+ct.Shell)
	parts = append(parts, "command="+strings.Join(ct.Command, "\x00"))
	keys := make([]string, 0, len(ct.Env))
	for key := range ct.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, "env:"+key+"="+ct.Env[key])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
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
			case <-time.After(3 * time.Second):
				_ = task.Kill(ctx, syscall.SIGKILL)
				select {
				case <-statusC:
				case <-ctx.Done():
					return ctx.Err()
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
	_ = task.Kill(ctx, syscall.SIGKILL)
	statusC, waitErr := task.Wait(ctx)
	if waitErr == nil {
		select {
		case <-statusC:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	_, err = task.Delete(ctx)
	if err == nil || errdefs.IsNotFound(err) {
		return nil
	}
	return err
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

func containerShellArgs(ct lab.Container) []string {
	if ct.Shell != "" {
		return []string{ct.Shell}
	}
	return []string{"/bin/sh"}
}
