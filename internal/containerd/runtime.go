package containerd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
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
	labLabel         = "foxlab.lab"
	containerIDLabel = "foxlab.container.id"
	containerDNSMode = "host-resolvconf"
	taskExitTimeout  = 3 * time.Second
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
				if ct.Disk != "" {
					mounted, mountErr := containerDiskMountActive(l, ct)
					if mountErr != nil {
						return states, fmt.Errorf("check container disk mount %s: %w", l.ManagedContainerName(ct), mountErr)
					}
					if mounted {
						states[key] = "created-mounted"
					}
				}
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

func (r *Runtime) Start(ctx context.Context, l *lab.Lab, ref workload.Ref) (err error) {
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
	desiredDiskMount := containerDiskMount{}
	if ct.Disk != "" {
		var err error
		desiredDiskMount, err = desiredContainerDiskMount(l, ct)
		if err != nil {
			return err
		}
	}
	container, loadErr := client.LoadContainer(ctx, name)
	if loadErr == nil {
		if nilInterface(container) {
			return fmt.Errorf("containerd returned nil container handle: %s", name)
		}
		changed, err := containerConfigChanged(ctx, container, l.ID, ct, desiredDiskMount)
		if err != nil {
			return err
		}
		if !changed {
			task, err := container.Task(ctx, nil)
			if err == nil {
				if nilInterface(task) {
					return fmt.Errorf("containerd returned nil task handle: %s", name)
				}
				recreateForUnhealthyDisk := false
				status, statusErr := task.Status(ctx)
				if statusErr == nil && status.Status == containerd.Running {
					diskHealthy := true
					if ct.Disk != "" {
						var healthErr error
						diskHealthy, healthErr = mountedContainerDiskHealthy(l, ct)
						if healthErr != nil {
							return fmt.Errorf("check mounted container disk for %s: %w", name, healthErr)
						}
					}
					if diskHealthy {
						if r.Bridge != nil {
							if err := r.Bridge.AttachContainer(ctx, l, ct, int(task.Pid())); err != nil {
								return err
							}
						}
						return nil
					}
					if err := deleteContainer(ctx, container); err != nil {
						return fmt.Errorf("delete container with unhealthy disk mount %s: %w", name, err)
					}
					if err := cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace()); err != nil {
						return fmt.Errorf("cleanup unhealthy container disk mount %s: %w", name, err)
					}
					recreateForUnhealthyDisk = true
				}
				if !recreateForUnhealthyDisk {
					if err := deleteTask(ctx, task); err != nil {
						return fmt.Errorf("delete stale task for %s: %w", name, err)
					}
				}
			} else if !errdefs.IsNotFound(err) {
				return err
			}
		}
		if changed {
			if err := deleteContainer(ctx, container); err != nil {
				return err
			}
			if err := cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace()); err != nil {
				return err
			}
		}
	} else if !errdefs.IsNotFound(loadErr) {
		return loadErr
	}
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
		diskMount, err = prepareContainerDiskMount(ctx, l, ct, imageRef, r.containerdAddress(), r.containerdNamespace())
		if err != nil {
			return err
		}
	}
	startComplete := false
	defer func() {
		if !startComplete {
			if cleanupErr := cleanupPreparedContainerDiskMount(l, ct, diskMount, r.containerdAddress(), r.containerdNamespace()); cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()
	container, err = client.LoadContainer(ctx, name)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
		container, err = createContainer(ctx, client, name, image, l.ID, ct, diskMount)
	} else {
		changed, changeErr := containerConfigChanged(ctx, container, l.ID, ct, diskMount)
		if changeErr != nil {
			return changeErr
		}
		if changed {
			if err := deleteContainer(ctx, container); err != nil {
				return err
			}
			container, err = createContainer(ctx, client, name, image, l.ID, ct, diskMount)
		}
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
				if err := r.Bridge.AttachContainer(ctx, l, ct, int(task.Pid())); err != nil {
					return err
				}
			}
			startComplete = true
			return nil
		}
		if err := deleteTask(ctx, task); err != nil {
			return fmt.Errorf("delete stale task for %s: %w", name, err)
		}
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
		if cleanupErr := deleteTask(ctx, task); cleanupErr != nil {
			return errors.Join(err, fmt.Errorf("delete failed task for %s: %w", name, cleanupErr))
		}
		return err
	}
	if r.Bridge != nil {
		if err := r.Bridge.AttachContainer(ctx, l, ct, int(task.Pid())); err != nil {
			if cleanupErr := deleteTask(ctx, task); cleanupErr != nil {
				return errors.Join(err, fmt.Errorf("delete failed task for %s: %w", name, cleanupErr))
			}
			return err
		}
	}
	startComplete = true
	return nil
}

func cleanupPreparedContainerDiskMount(l *lab.Lab, ct lab.Container, diskMount containerDiskMount, address, namespace string) error {
	if !diskMount.CleanupDiskOnFailure && !diskMount.CleanupOverlayOnFailure {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if diskMount.CleanupOverlayOnFailure {
		mountPath, err := containerDiskMountPath(l, ct)
		if err != nil {
			return err
		}
		if err := cleanupContainerOverlayMount(ctx, l, ct, mountPath, address, namespace); err != nil {
			return err
		}
	}
	if diskMount.CleanupDiskOnFailure {
		return cleanupContainerDiskMount(ctx, l, ct, address, namespace)
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
			return cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
		}
		return err
	}
	if nilInterface(container) {
		return fmt.Errorf("containerd returned nil container handle: %s", l.ManagedContainerName(ct))
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
		}
		return err
	}
	if nilInterface(task) {
		return cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
	}
	_ = task.Kill(ctx, syscall.SIGTERM)
	if err := deleteTask(ctx, task); err != nil {
		return err
	}
	return cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
}

func (r *Runtime) Destroy(ctx context.Context, l *lab.Lab, ref workload.Ref) error {
	if ref.Type != workload.TypeContainer {
		return fmt.Errorf("containerd cannot destroy workload type %q", ref.Type)
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
			return cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
		}
		return err
	}
	if nilInterface(container) {
		return fmt.Errorf("containerd returned nil container handle: %s", l.ManagedContainerName(ct))
	}
	if err := deleteContainer(ctx, container); err != nil {
		return err
	}
	return cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
}

func (r *Runtime) CleanupOrphans(ctx context.Context, l *lab.Lab) ([]string, error) {
	if l == nil {
		return nil, nil
	}
	client, ctx, closeClient, err := r.client(ctx)
	if err != nil {
		return nil, err
	}
	defer closeClient()
	containers, err := client.Containers(ctx)
	if err != nil {
		return nil, err
	}
	prefix := managedContainerPrefix(l)
	desired := desiredContainerIDs(l)
	actions := []string{}
	var errs []error
	failedOrphanIDs := map[string]bool{}
	for _, container := range containers {
		name := container.ID()
		labels, labelErr := container.Labels(ctx)
		if labelErr != nil {
			errs = append(errs, fmt.Errorf("read container labels %s: %w", name, labelErr))
			continue
		}
		if desiredID, isDesired := desired[name]; isDesired {
			if owner := labels[labLabel]; owner != "" && owner != l.ID {
				errs = append(errs, fmt.Errorf("container %s belongs to lab %q, not %q", name, owner, l.ID))
				continue
			}
			if resourceID := labels[containerIDLabel]; resourceID != "" && resourceID != desiredID {
				errs = append(errs, fmt.Errorf("container %s has workload id %q, not %q", name, resourceID, desiredID))
				continue
			}
			missingLabels := map[string]string{}
			if labels[labLabel] == "" {
				missingLabels[labLabel] = l.ID
			}
			if labels[containerIDLabel] == "" {
				missingLabels[containerIDLabel] = desiredID
			}
			if len(missingLabels) > 0 {
				if _, err := container.SetLabels(ctx, missingLabels); err != nil {
					errs = append(errs, fmt.Errorf("label managed container %s: %w", name, err))
				}
			}
			continue
		}
		if !managedContainerOwnedByLab(labels, l) {
			continue
		}
		id := labels[containerIDLabel]
		if id == "" {
			if !strings.HasPrefix(name, prefix) {
				continue
			}
			id = strings.TrimPrefix(name, prefix)
		}
		ct := lab.Container{ID: id}
		if r.Bridge != nil {
			r.Bridge.DetachContainer(ctx, l, ct)
		}
		if err := deleteContainer(ctx, container); err != nil {
			errs = append(errs, fmt.Errorf("delete orphan container %s: %w", name, err))
			failedOrphanIDs[id] = true
			continue
		}
		if err := cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace()); err != nil {
			errs = append(errs, fmt.Errorf("cleanup orphan container disk %s: %w", name, err))
			continue
		}
		actions = append(actions, "deleted orphan container:"+id)
	}
	diskActions, diskErr := cleanupOrphanContainerDiskMounts(ctx, l, failedOrphanIDs, r.containerdAddress(), r.containerdNamespace())
	actions = append(actions, diskActions...)
	if diskErr != nil {
		errs = append(errs, diskErr)
	}
	return actions, errors.Join(errs...)
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

func (r *Runtime) containerdNamespace() string {
	if strings.TrimSpace(r.Namespace) == "" {
		return DefaultNamespace
	}
	return r.Namespace
}

func (r *Runtime) containerdAddress() string {
	if strings.TrimSpace(r.Address) == "" {
		return DefaultAddress
	}
	return r.Address
}

func containerSpecOpts(image containerd.Image, ct lab.Container, diskMount containerDiskMount) []oci.SpecOpts {
	opts := []oci.SpecOpts{}
	if diskMount.Source != "" && diskMount.Destination == "/" {
		opts = append(opts, oci.WithRootFSPath(diskMount.Source))
	}
	opts = append(opts, oci.WithImageConfig(image))
	opts = append(opts, oci.WithProcessArgs(containerProcessArgs(ct)...))
	env := []string{}
	for key, value := range ct.Env {
		env = append(env, key+"="+value)
	}
	if len(env) > 0 {
		opts = append(opts, oci.WithEnv(env))
	}
	opts = append(opts, oci.WithHostResolvconf)
	if diskMount.Source != "" && diskMount.Destination != "/" {
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

func createContainer(ctx context.Context, client *containerd.Client, name string, image containerd.Image, labID string, ct lab.Container, diskMount containerDiskMount) (containerd.Container, error) {
	opts := []containerd.NewContainerOpts{
		containerd.WithImage(image),
		containerd.WithNewSpec(containerSpecOpts(image, ct, diskMount)...),
		containerd.WithContainerLabels(map[string]string{
			configLabel:      containerConfigHash(ct, diskMount),
			labLabel:         labID,
			containerIDLabel: ct.ID,
		}),
	}
	if diskMount.Source == "" || diskMount.Destination != "/" {
		opts = append(opts, containerd.WithNewSnapshot(name+"-rootfs", image))
	}
	return client.NewContainer(
		ctx,
		name,
		opts...,
	)
}

func containerConfigChanged(ctx context.Context, container containerd.Container, labID string, ct lab.Container, diskMount containerDiskMount) (bool, error) {
	labels, err := container.Labels(ctx)
	if err != nil {
		return false, err
	}
	if owner := labels[labLabel]; owner != "" && owner != labID {
		return false, fmt.Errorf("container %s belongs to lab %q, not %q", container.ID(), owner, labID)
	}
	if resourceID := labels[containerIDLabel]; resourceID != "" && resourceID != ct.ID {
		return false, fmt.Errorf("container %s has workload id %q, not %q", container.ID(), resourceID, ct.ID)
	}
	missingLabels := map[string]string{}
	if labels[labLabel] == "" {
		missingLabels[labLabel] = labID
	}
	if labels[containerIDLabel] == "" {
		missingLabels[containerIDLabel] = ct.ID
	}
	if len(missingLabels) > 0 {
		if _, err := container.SetLabels(ctx, missingLabels); err != nil {
			return false, fmt.Errorf("label managed container %s: %w", container.ID(), err)
		}
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
	parts = append(parts, "id="+ct.ID)
	parts = append(parts, "image="+ct.Image)
	parts = append(parts, "shell="+ct.Shell)
	parts = append(parts, "disk="+ct.Disk)
	parts = append(parts, "diskSource="+diskMount.Source)
	parts = append(parts, "diskDestination="+diskMount.Destination)
	parts = append(parts, "dns="+containerDNSMode)
	parts = append(parts, "command="+strings.Join(containerProcessArgs(ct), "\x00"))
	for i, nic := range ct.Networks {
		parts = append(parts, fmt.Sprintf("network:%d:switch=%s", i, nic.Switch))
		parts = append(parts, fmt.Sprintf("network:%d:external=%s", i, nic.ExternalLink))
		parts = append(parts, fmt.Sprintf("network:%d:mac=%s", i, nic.MAC))
	}
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

func desiredContainerDiskMount(l *lab.Lab, ct lab.Container) (containerDiskMount, error) {
	if ct.Disk == "" {
		return containerDiskMount{}, nil
	}
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		return containerDiskMount{}, err
	}
	return containerDiskMount{Source: filepath.Join(mountPath, "merged"), Destination: "/"}, nil
}

func desiredContainerNames(l *lab.Lab) map[string]bool {
	names := map[string]bool{}
	for name := range desiredContainerIDs(l) {
		names[name] = true
	}
	return names
}

func desiredContainerIDs(l *lab.Lab) map[string]string {
	ids := map[string]string{}
	if l == nil {
		return ids
	}
	for _, ct := range l.Containers {
		ids[l.ManagedContainerName(ct)] = ct.ID
	}
	return ids
}

func managedContainerPrefix(l *lab.Lab) string {
	if l == nil {
		return ""
	}
	return strings.ToLower(lab.ManagedPrefix + "-" + l.ID + "-")
}

func managedContainerOwnedByLab(labels map[string]string, l *lab.Lab) bool {
	return l != nil && labels[labLabel] != "" && labels[labLabel] == l.ID
}

func containerExecID(kind, resourceID string, now time.Time) string {
	sum := sha256.Sum256([]byte(resourceID))
	return fmt.Sprintf("foxlab-%s-%s-%s", kind, hex.EncodeToString(sum[:8]), now.UTC().Format("20060102T150405.000000000"))
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
