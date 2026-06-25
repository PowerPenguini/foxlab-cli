package containerd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	distref "github.com/distribution/reference"
	specs "github.com/opencontainers/runtime-spec/specs-go"

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
	imageRef, err := containerImageRef(ct.Image)
	if err != nil {
		return err
	}
	image, err := containerImage(ctx, client, imageRef)
	if err != nil {
		return err
	}
	diskMount := containerDiskMount{}
	if ct.Disk != "" {
		var err error
		diskMount, err = prepareContainerDiskMount(ctx, l, ct)
		if err != nil {
			return err
		}
	}
	container, err := client.LoadContainer(ctx, name)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
		container, err = createContainer(ctx, client, name, image, ct, diskMount)
	} else if changed, err := containerConfigChanged(ctx, container, ct, diskMount); err != nil {
		return err
	} else if changed {
		if err := deleteContainer(ctx, container); err != nil {
			return err
		}
		container, err = createContainer(ctx, client, name, image, ct, diskMount)
	}
	if err != nil {
		return err
	}
	if nilInterface(container) {
		return fmt.Errorf("containerd returned nil container handle: %s", name)
	}
	task, err := container.Task(ctx, nil)
	if err == nil {
		if nilInterface(task) {
			return fmt.Errorf("containerd returned nil task handle: %s", name)
		}
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
	if nilInterface(task) {
		return fmt.Errorf("containerd returned nil task handle after create: %s", name)
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
	if nilInterface(container) {
		return fmt.Errorf("containerd returned nil container handle: %s", l.ManagedContainerName(ct))
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return cleanupContainerDiskMount(ctx, l, ct)
		}
		return err
	}
	if nilInterface(task) {
		return cleanupContainerDiskMount(ctx, l, ct)
	}
	_ = task.Kill(ctx, syscall.SIGTERM)
	if err := deleteTask(ctx, task); err != nil {
		return err
	}
	return cleanupContainerDiskMount(ctx, l, ct)
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

func containerSpecOpts(image containerd.Image, ct lab.Container, diskMount containerDiskMount) []oci.SpecOpts {
	opts := []oci.SpecOpts{oci.WithImageConfig(image)}
	opts = append(opts, oci.WithProcessArgs(containerProcessArgs(ct)...))
	env := []string{}
	for key, value := range ct.Env {
		env = append(env, key+"="+value)
	}
	if len(env) > 0 {
		opts = append(opts, oci.WithEnv(env))
	}
	if diskMount.Source != "" {
		opts = append(opts, oci.WithMounts([]specs.Mount{{
			Type:        "bind",
			Source:      diskMount.Source,
			Destination: diskMount.Destination,
			Options:     []string{"rbind", "rw"},
		}}))
	}
	return opts
}

func containerImageRef(image string) (string, error) {
	image = strings.TrimSpace(image)
	if image == "" || image == "?" || image == "empty" {
		return "", fmt.Errorf("container image is empty")
	}
	named, err := distref.ParseDockerRef(image)
	if err != nil {
		return "", fmt.Errorf("invalid container image %q: %w", image, err)
	}
	return named.String(), nil
}

func containerImage(ctx context.Context, client *containerd.Client, imageRef string) (containerd.Image, error) {
	image, err := client.GetImage(ctx, imageRef)
	if err == nil {
		if unpacked, unpackErr := image.IsUnpacked(ctx, containerd.DefaultSnapshotter); unpackErr != nil || !unpacked {
			if err := image.Unpack(ctx, containerd.DefaultSnapshotter); err != nil {
				return nil, fmt.Errorf("unpack local image %q: %w", imageRef, err)
			}
		}
		return image, nil
	}
	if !errdefs.IsNotFound(err) {
		return nil, fmt.Errorf("load local image %q: %w", imageRef, err)
	}
	image, err = client.Pull(ctx, imageRef, containerd.WithPullUnpack, containerd.WithPullSnapshotter(containerd.DefaultSnapshotter))
	if err != nil {
		return nil, fmt.Errorf("pull image %q: %w", imageRef, err)
	}
	return image, nil
}

func createContainer(ctx context.Context, client *containerd.Client, name string, image containerd.Image, ct lab.Container, diskMount containerDiskMount) (containerd.Container, error) {
	opts := []containerd.NewContainerOpts{
		containerd.WithImage(image),
		containerd.WithNewSnapshot(name+"-rootfs", image),
		containerd.WithNewSpec(containerSpecOpts(image, ct, diskMount)...),
		containerd.WithContainerLabels(map[string]string{configLabel: containerConfigHash(ct, diskMount)}),
	}
	return client.NewContainer(
		ctx,
		name,
		opts...,
	)
}

func containerConfigChanged(ctx context.Context, container containerd.Container, ct lab.Container, diskMount containerDiskMount) (bool, error) {
	labels, err := container.Labels(ctx)
	if err != nil {
		return false, err
	}
	return labels[configLabel] != containerConfigHash(ct, diskMount), nil
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

func containerConfigHash(ct lab.Container, diskMount containerDiskMount) string {
	var parts []string
	parts = append(parts, "image="+ct.Image)
	parts = append(parts, "shell="+ct.Shell)
	parts = append(parts, "disk="+ct.Disk)
	parts = append(parts, "diskSource="+diskMount.Source)
	parts = append(parts, "diskDestination="+diskMount.Destination)
	parts = append(parts, "command="+strings.Join(containerProcessArgs(ct), "\x00"))
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

func containerDiskDestination(l *lab.Lab, ct lab.Container) string {
	disk, ok := attachedContainerDisk(l, ct)
	if !ok {
		return "/data"
	}
	value := strings.TrimSpace(disk.MountPath)
	if value == "" {
		return "/data"
	}
	clean := path.Clean("/" + strings.TrimLeft(value, "/"))
	if clean == "/" {
		return "/data"
	}
	return clean
}

func attachedContainerDisk(l *lab.Lab, ct lab.Container) (lab.Disk, bool) {
	if l == nil || ct.Disk == "" {
		return lab.Disk{}, false
	}
	resolvedContainerDisk := l.ResolvePath(ct.Disk)
	for _, disk := range l.Disks {
		if disk.AttachedType == "container" && disk.AttachedTo == ct.ID {
			return disk, true
		}
	}
	for _, disk := range l.Disks {
		if l.ResolvePath(disk.Path) == resolvedContainerDisk {
			return disk, true
		}
	}
	return lab.Disk{}, false
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
	env := append([]string(nil), base...)
	shell := firstContainerShell(ct)
	if !envHasKey(env, "TERM") {
		env = append(env, "TERM=xterm-256color")
	}
	if !envHasKey(env, "SHELL") {
		env = append(env, "SHELL="+shell)
	}
	return env
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
