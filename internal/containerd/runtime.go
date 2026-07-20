package containerd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"time"

	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/errdefs"

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
	containerDNSMode = "managed-host-public-v1"
	taskExitTimeout  = 3 * time.Second
)

type Runtime struct {
	Address   string
	Namespace string
	Bridge    *hostnet.Bridge
	disks     *diskManager
}

func NewRuntime(address string) *Runtime {
	if address == "" {
		address = DefaultAddress
	}
	return &Runtime{
		Address:   address,
		Namespace: DefaultNamespace,
		Bridge:    hostnet.NewBridge(),
		disks:     newDiskManager(),
	}
}

func (r *Runtime) diskManager() *diskManager {
	if r.disks == nil {
		r.disks = newDiskManager()
	}
	return r.disks
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
					mounted, mountErr := r.diskManager().containerDiskMountActive(l, ct)
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
	resolvconfPath, err := syncContainerResolvconf(l, ct)
	if err != nil {
		return err
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
	alreadyRunning, err := r.inspectExistingContainer(ctx, client, l, ct, name, desiredDiskMount)
	if err != nil {
		return err
	}
	if alreadyRunning {
		return nil
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
		diskMount, err = r.diskManager().prepareContainerDiskMount(ctx, l, ct, imageRef, r.containerdAddress(), r.containerdNamespace())
		if err != nil {
			return err
		}
	}
	startComplete := false
	defer func() {
		if !startComplete {
			if cleanupErr := r.cleanupPreparedContainerDiskMount(l, ct, diskMount, r.containerdAddress(), r.containerdNamespace()); cleanupErr != nil {
				err = errors.Join(err, cleanupErr)
			}
		}
	}()
	container, created, err := ensureContainerForStart(ctx, client, name, image, l.ID, ct, diskMount, resolvconfPath)
	if err != nil {
		return err
	}
	if err := startContainerTaskWithRollback(ctx, container, created, name, func() error {
		return r.ensureContainerTaskRunning(ctx, l, ct, name, container)
	}); err != nil {
		return err
	}
	startComplete = true
	return nil
}

func (r *Runtime) inspectExistingContainer(ctx context.Context, client *containerd.Client, l *lab.Lab, ct lab.Container, name string, desiredDiskMount containerDiskMount) (bool, error) {
	container, err := client.LoadContainer(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if nilInterface(container) {
		return false, fmt.Errorf("containerd returned nil container handle: %s", name)
	}
	changed, err := containerConfigChanged(ctx, container, l.ID, ct, desiredDiskMount)
	if err != nil {
		return false, err
	}
	if changed {
		if err := deleteContainer(ctx, container); err != nil {
			return false, err
		}
		if err := r.diskManager().cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace()); err != nil {
			return false, err
		}
		return false, nil
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if nilInterface(task) {
		return false, fmt.Errorf("containerd returned nil task handle: %s", name)
	}
	status, statusErr := task.Status(ctx)
	if statusErr == nil && status.Status == containerd.Running {
		diskHealthy := true
		if ct.Disk != "" {
			diskHealthy, err = r.diskManager().mountedContainerDiskHealthy(l, ct)
			if err != nil {
				return false, fmt.Errorf("check mounted container disk for %s: %w", name, err)
			}
		}
		if diskHealthy {
			if r.Bridge != nil {
				if err := r.Bridge.AttachContainer(ctx, l, ct, int(task.Pid())); err != nil {
					return false, err
				}
			}
			return true, nil
		}
		if err := deleteContainer(ctx, container); err != nil {
			return false, fmt.Errorf("delete container with unhealthy disk mount %s: %w", name, err)
		}
		if err := r.diskManager().cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace()); err != nil {
			return false, fmt.Errorf("cleanup unhealthy container disk mount %s: %w", name, err)
		}
		return false, nil
	}
	if err := deleteTask(ctx, task); err != nil {
		return false, fmt.Errorf("delete stale task for %s: %w", name, err)
	}
	return false, nil
}

func ensureContainerForStart(ctx context.Context, client *containerd.Client, name string, image containerd.Image, labID string, ct lab.Container, diskMount containerDiskMount, resolvconfPath string) (containerd.Container, bool, error) {
	created := false
	container, err := client.LoadContainer(ctx, name)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return nil, false, err
		}
		container, err = createContainer(ctx, client, name, image, labID, ct, diskMount, resolvconfPath)
		created = err == nil
	} else {
		changed, changeErr := containerConfigChanged(ctx, container, labID, ct, diskMount)
		if changeErr != nil {
			return nil, false, changeErr
		}
		if changed {
			if err := deleteContainer(ctx, container); err != nil {
				return nil, false, err
			}
			container, err = createContainer(ctx, client, name, image, labID, ct, diskMount, resolvconfPath)
			created = err == nil
		}
	}
	if err != nil {
		return nil, false, err
	}
	if nilInterface(container) {
		return nil, false, fmt.Errorf("containerd returned nil container handle: %s", name)
	}
	return container, created, nil
}

func startContainerTaskWithRollback(ctx context.Context, container containerd.Container, created bool, name string, startTask func() error) error {
	if startTask == nil {
		return fmt.Errorf("missing container task start for %s", name)
	}
	if err := startTask(); err != nil {
		if !created || nilInterface(container) {
			return err
		}
		if cleanupErr := deleteContainer(ctx, container); cleanupErr != nil {
			return errors.Join(err, fmt.Errorf("delete container after failed start %s: %w", name, cleanupErr))
		}
		return err
	}
	return nil
}

func (r *Runtime) ensureContainerTaskRunning(ctx context.Context, l *lab.Lab, ct lab.Container, name string, container containerd.Container) error {
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
	return nil
}

func (r *Runtime) cleanupPreparedContainerDiskMount(l *lab.Lab, ct lab.Container, diskMount containerDiskMount, address, namespace string) error {
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
		if err := r.diskManager().cleanupContainerOverlayMount(ctx, l, ct, mountPath, address, namespace); err != nil {
			return err
		}
	}
	if diskMount.CleanupDiskOnFailure {
		return r.diskManager().cleanupContainerDiskMount(ctx, l, ct, address, namespace)
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
			return r.diskManager().cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
		}
		return err
	}
	if nilInterface(container) {
		return fmt.Errorf("containerd returned nil container handle: %s", l.ManagedContainerName(ct))
	}
	task, err := container.Task(ctx, nil)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return r.diskManager().cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
		}
		return err
	}
	if nilInterface(task) {
		return r.diskManager().cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
	}
	_ = task.Kill(ctx, syscall.SIGTERM)
	if err := deleteTask(ctx, task); err != nil {
		return err
	}
	return r.diskManager().cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
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
			return r.diskManager().cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
		}
		return err
	}
	if nilInterface(container) {
		return fmt.Errorf("containerd returned nil container handle: %s", l.ManagedContainerName(ct))
	}
	if err := deleteContainer(ctx, container); err != nil {
		return err
	}
	return r.diskManager().cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace())
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
		if err := r.diskManager().cleanupContainerDiskMount(ctx, l, ct, r.containerdAddress(), r.containerdNamespace()); err != nil {
			errs = append(errs, fmt.Errorf("cleanup orphan container disk %s: %w", name, err))
			continue
		}
		actions = append(actions, "deleted orphan container:"+id)
	}
	diskActions, diskErr := r.diskManager().cleanupOrphanContainerDiskMounts(ctx, l, failedOrphanIDs, r.containerdAddress(), r.containerdNamespace())
	actions = append(actions, diskActions...)
	if diskErr != nil {
		errs = append(errs, diskErr)
	}
	return actions, errors.Join(errs...)
}
