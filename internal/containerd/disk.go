package containerd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"foxlab-cli/internal/lab"
)

func runHostCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, text)
	}
	return nil
}

type containerDiskMount struct {
	Source                  string
	Destination             string
	CleanupDiskOnFailure    bool
	CleanupOverlayOnFailure bool
}

const containerDiskSourceMarker = ".foxlab-disk-source"
const containerImageSourceMarker = ".foxlab-image-source"

type diskHostOps struct {
	runCommand     func(context.Context, string, ...string) error
	lookPath       func(string) (string, error)
	requireTools   func() error
	mountSource    func(string) (bool, string, error)
	filesystemType func(string) (string, error)
	rootWritable   func(string) bool
	sourceHealthy  func(string) bool
	freeNBD        func() (string, error)
}

type diskManager struct {
	ops diskHostOps
}

func newDiskManager() *diskManager {
	ops := diskHostOps{
		runCommand:     runHostCommand,
		lookPath:       exec.LookPath,
		mountSource:    mountSource,
		filesystemType: mountedFilesystemType,
		rootWritable:   containerRootFSWritable,
		sourceHealthy:  containerDiskSourceHealthy,
		freeNBD:        freeNBDDevice,
	}
	ops.requireTools = func() error { return requireContainerDiskTools(ops.lookPath) }
	return &diskManager{ops: ops}
}

func (m *diskManager) prepareContainerDiskMount(ctx context.Context, l *lab.Lab, ct lab.Container, imageRef, address, namespace string) (containerDiskMount, error) {
	if err := m.ops.requireTools(); err != nil {
		return containerDiskMount{}, err
	}
	diskPath := l.ResolvePath(ct.Disk)
	if _, err := os.Stat(diskPath); err != nil {
		return containerDiskMount{}, fmt.Errorf("container disk %q: %w", diskPath, err)
	}
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		return containerDiskMount{}, err
	}
	cleanupDiskOnFailure := false
	if mounted, source, err := m.ops.mountSource(mountPath); err != nil {
		return containerDiskMount{}, err
	} else if mounted {
		if !m.mountedContainerDiskUsable(mountPath, source) {
			if err := m.cleanupMountedContainerDiskMount(ctx, mountPath, source); err != nil {
				return containerDiskMount{}, fmt.Errorf("reset unusable container disk mount: %w", err)
			}
		} else {
			if mountedDiskPath, ok := mountedContainerDiskSource(mountPath); ok {
				if mountedDiskPath != filepath.Clean(diskPath) {
					if err := m.cleanupMountedContainerDiskMount(ctx, mountPath, source); err != nil {
						return containerDiskMount{}, fmt.Errorf("reset stale container disk mount: %w", err)
					}
				} else {
					if err := m.growMountedContainerDiskFilesystem(ctx, source, mountPath); err != nil {
						return containerDiskMount{}, err
					}
					return m.prepareContainerOverlayMount(ctx, l, ct, imageRef, address, namespace, mountPath)
				}
			} else {
				if err := m.cleanupMountedContainerDiskMount(ctx, mountPath, source); err != nil {
					return containerDiskMount{}, fmt.Errorf("reset untracked container disk mount: %w", err)
				}
			}
		}
	}
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		return containerDiskMount{}, err
	}
	_ = m.ops.runCommand(ctx, "modprobe", "nbd", "max_part=16")
	device, err := m.ops.freeNBD()
	if err != nil {
		return containerDiskMount{}, err
	}
	if err := m.connectContainerDisk(ctx, device, diskPath); err != nil {
		return containerDiskMount{}, fmt.Errorf("connect container disk: %w", err)
	}
	if err := m.waitContainerDiskReady(ctx, device); err != nil {
		_ = m.ops.runCommand(ctx, "qemu-nbd", "--disconnect", device)
		return containerDiskMount{}, fmt.Errorf("wait for container disk: %w", err)
	}
	if err := m.mountContainerDisk(ctx, device, mountPath); err != nil {
		_ = m.ops.runCommand(ctx, "qemu-nbd", "--disconnect", device)
		return containerDiskMount{}, fmt.Errorf("mount container disk: %w", err)
	}
	cleanupDiskOnFailure = true
	if err := m.growMountedContainerDiskFilesystem(ctx, device, mountPath); err != nil {
		_ = m.cleanupMountedContainerDiskMount(ctx, mountPath, device)
		return containerDiskMount{}, err
	}
	if err := writeContainerDiskSourceMarker(mountPath, diskPath); err != nil {
		_ = m.cleanupMountedContainerDiskMount(ctx, mountPath, device)
		return containerDiskMount{}, fmt.Errorf("record container disk source: %w", err)
	}
	mount, err := m.prepareContainerOverlayMount(ctx, l, ct, imageRef, address, namespace, mountPath)
	if err != nil {
		_ = m.cleanupContainerOverlayMount(ctx, l, ct, mountPath, address, namespace)
		_ = m.cleanupMountedContainerDiskMount(ctx, mountPath, device)
		return containerDiskMount{}, err
	}
	mount.CleanupDiskOnFailure = cleanupDiskOnFailure
	return mount, nil
}

type containerOverlayPathSet struct {
	lower       string
	imageMarker string
	upper       string
	work        string
	merged      string
}

func containerOverlayPaths(l *lab.Lab, ct lab.Container, diskRoot string) (containerOverlayPathSet, error) {
	root, err := l.StorageRoot()
	if err != nil {
		return containerOverlayPathSet{}, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return containerOverlayPathSet{}, err
	}
	return containerOverlayPathSet{
		lower:       filepath.Join(root, "container-image-rootfs", ct.ID),
		imageMarker: filepath.Join(root, "container-image-rootfs", ct.ID+containerImageSourceMarker),
		upper:       filepath.Join(diskRoot, "upper"),
		work:        filepath.Join(diskRoot, "work"),
		merged:      filepath.Join(diskRoot, "merged"),
	}, nil
}

func (m *diskManager) prepareContainerOverlayMount(ctx context.Context, l *lab.Lab, ct lab.Container, imageRef, address, namespace, diskRoot string) (containerDiskMount, error) {
	paths, err := containerOverlayPaths(l, ct, diskRoot)
	if err != nil {
		return containerDiskMount{}, err
	}
	if err := os.MkdirAll(paths.lower, 0o755); err != nil {
		return containerDiskMount{}, err
	}
	for _, path := range []string{paths.upper, paths.work, paths.merged} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return containerDiskMount{}, err
		}
	}
	if err := m.ensureContainerImageMount(ctx, paths, imageRef, address, namespace); err != nil {
		return containerDiskMount{}, err
	}
	if err := m.ensureContainerOverlayRoot(ctx, paths); err != nil {
		return containerDiskMount{}, err
	}
	return containerDiskMount{
		Source:                  paths.merged,
		Destination:             "/",
		CleanupOverlayOnFailure: true,
	}, nil
}

func (m *diskManager) ensureContainerImageMount(ctx context.Context, paths containerOverlayPathSet, imageRef, address, namespace string) error {
	if mounted, _, err := m.ops.mountSource(paths.lower); err != nil {
		return err
	} else if mounted {
		if mountedImageRef, ok := mountedContainerImageSource(paths.imageMarker); ok && mountedImageRef == imageRef {
			return nil
		}
		if err := m.cleanupContainerImageMount(ctx, paths.lower, address, namespace); err != nil {
			return err
		}
	}
	args := ctrGlobalArgs(address, namespace)
	args = append(args, "images", "mount", "--snapshotter", "overlayfs", imageRef, paths.lower)
	if err := m.ops.runCommand(ctx, "ctr", args...); err != nil {
		return fmt.Errorf("mount container image rootfs: %w", err)
	}
	if err := writeContainerImageSourceMarker(paths.imageMarker, imageRef); err != nil {
		_ = m.cleanupContainerImageMount(ctx, paths.lower, address, namespace)
		return err
	}
	return nil
}

func (m *diskManager) ensureContainerOverlayRoot(ctx context.Context, paths containerOverlayPathSet) error {
	if mounted, _, err := m.ops.mountSource(paths.merged); err != nil {
		return err
	} else if mounted {
		if err := m.ops.runCommand(ctx, "umount", paths.merged); err != nil {
			return fmt.Errorf("reset container overlay rootfs: %w", err)
		}
	}
	options := "lowerdir=" + paths.lower + ",upperdir=" + paths.upper + ",workdir=" + paths.work
	if err := m.ops.runCommand(ctx, "mount", "-t", "overlay", "overlay", "-o", options, paths.merged); err != nil {
		return fmt.Errorf("mount container overlay rootfs: %w", err)
	}
	return nil
}

func mountedContainerImageSource(marker string) (string, bool) {
	data, err := os.ReadFile(marker)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

func writeContainerImageSourceMarker(marker, imageRef string) error {
	return os.WriteFile(marker, []byte(strings.TrimSpace(imageRef)+"\n"), 0o644)
}

func (m *diskManager) mountContainerDisk(ctx context.Context, device, mountPath string) error {
	err := m.ops.runCommand(ctx, "mount", device, mountPath)
	if err == nil {
		return nil
	}
	if !isUnformattedDiskMountError(err) {
		return err
	}
	if m.containerDiskHasFilesystem(ctx, device) {
		return err
	}
	if mkfsErr := m.formatContainerDisk(ctx, device); mkfsErr != nil {
		return mkfsErr
	}
	return m.ops.runCommand(ctx, "mount", device, mountPath)
}

func (m *diskManager) formatContainerDisk(ctx context.Context, device string) error {
	if err := m.ops.runCommand(ctx, "mkfs.ext4", "-F", device); err != nil {
		return fmt.Errorf("format empty container disk after mount failed: %w", err)
	}
	return nil
}

func (m *diskManager) growMountedContainerDiskFilesystem(ctx context.Context, device, mountPath string) error {
	fsType, err := m.ops.filesystemType(mountPath)
	if err != nil {
		return fmt.Errorf("detect container disk filesystem: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(fsType)) {
	case "ext2", "ext3", "ext4":
		if err := m.ops.runCommand(ctx, "resize2fs", device); err != nil {
			return fmt.Errorf("grow container disk filesystem: %w", err)
		}
	}
	return nil
}

func containerRootFSWritable(path string) bool {
	dir, err := os.MkdirTemp(path, ".foxlab-rootfs-write-check-")
	if err != nil {
		return false
	}
	_ = os.RemoveAll(dir)
	return true
}

func (m *diskManager) mountedContainerDiskUsable(mountPath, source string) bool {
	return m.ops.rootWritable(mountPath) && m.ops.sourceHealthy(source)
}

func containerDiskSourceHealthy(source string) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	f, err := os.Open(source)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, err := f.ReadAt(buf, 0)
	return err == nil || (err == io.EOF && n > 0)
}

func (m *diskManager) mountedContainerDiskHealthy(l *lab.Lab, ct lab.Container) (bool, error) {
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		return false, err
	}
	mounted, source, err := m.ops.mountSource(mountPath)
	if err != nil || !mounted {
		return false, err
	}
	mountedDiskPath, ok := mountedContainerDiskSource(mountPath)
	if !ok {
		return false, nil
	}
	if mountedDiskPath != filepath.Clean(l.ResolvePath(ct.Disk)) {
		return false, nil
	}
	return m.mountedContainerDiskUsable(mountPath, source), nil
}

func (m *diskManager) containerDiskMountActive(l *lab.Lab, ct lab.Container) (bool, error) {
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		return false, err
	}
	mounted, _, err := m.ops.mountSource(mountPath)
	return mounted, err
}

func mountedContainerDiskSource(mountPath string) (string, bool) {
	data, err := os.ReadFile(containerDiskSourceMarkerPath(mountPath))
	if err != nil {
		return "", false
	}
	return filepath.Clean(strings.TrimSpace(string(data))), true
}

func writeContainerDiskSourceMarker(mountPath, diskPath string) error {
	return os.WriteFile(containerDiskSourceMarkerPath(mountPath), []byte(filepath.Clean(diskPath)+"\n"), 0o644)
}

func containerDiskSourceMarkerPath(mountPath string) string {
	return filepath.Join(mountPath, containerDiskSourceMarker)
}

func (m *diskManager) containerDiskHasFilesystem(ctx context.Context, device string) bool {
	return m.ops.runCommand(ctx, "blkid", "-o", "value", "-s", "TYPE", device) == nil
}

func isUnformattedDiskMountError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "wrong fs type") || strings.Contains(text, "bad superblock")
}

func (m *diskManager) connectContainerDisk(ctx context.Context, device, diskPath string) error {
	if _, err := m.ops.lookPath("systemd-run"); err == nil {
		args := []string{
			"--scope",
			"--collect",
			"--quiet",
			"--unit", containerDiskScopeUnit(device),
			"qemu-nbd",
			"--fork",
			"--connect=" + device,
			diskPath,
		}
		if err := m.ops.runCommand(ctx, "systemd-run", args...); err == nil {
			return nil
		}
	}
	return m.ops.runCommand(ctx, "qemu-nbd", "--fork", "--connect="+device, diskPath)
}

func (m *diskManager) waitContainerDiskReady(ctx context.Context, device string) error {
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if m.ops.sourceHealthy(device) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("%s did not become readable", device)
		case <-ticker.C:
		}
	}
}

func containerDiskScopeUnit(device string) string {
	base := filepath.Base(strings.TrimSpace(device))
	clean := strings.Builder{}
	for _, ch := range base {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			clean.WriteRune(ch)
		}
	}
	if clean.Len() == 0 {
		return "foxlab-nbd-device"
	}
	return "foxlab-nbd-" + clean.String()
}

func (m *diskManager) cleanupMountedContainerDiskMount(ctx context.Context, mountPath, source string) error {
	if err := m.ops.runCommand(ctx, "umount", mountPath); err != nil {
		return fmt.Errorf("unmount container disk: %w", err)
	}
	if strings.HasPrefix(source, "/dev/nbd") {
		if err := m.ops.runCommand(ctx, "qemu-nbd", "--disconnect", nbdBaseDevice(source)); err != nil {
			return fmt.Errorf("disconnect container disk: %w", err)
		}
	}
	return nil
}

func (m *diskManager) cleanupContainerDiskMount(ctx context.Context, l *lab.Lab, ct lab.Container, address, namespace string) error {
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		return err
	}
	if err := m.cleanupContainerOverlayMount(ctx, l, ct, mountPath, address, namespace); err != nil {
		return err
	}
	mounted, source, err := m.ops.mountSource(mountPath)
	if err != nil || !mounted {
		return err
	}
	return m.cleanupMountedContainerDiskMount(ctx, mountPath, source)
}

func (m *diskManager) cleanupOrphanContainerDiskMounts(ctx context.Context, l *lab.Lab, skip map[string]bool, address, namespace string) ([]string, error) {
	root, err := l.StorageRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(root, "container-data"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	desired := map[string]bool{}
	for _, ct := range l.Containers {
		desired[ct.ID] = true
	}
	var actions []string
	var errs []error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		id := entry.Name()
		if !entry.IsDir() || desired[id] || skippedContainerDiskID(skip, id) {
			continue
		}
		ct := lab.Container{ID: id}
		mountPath, pathErr := containerDiskMountPath(l, ct)
		if pathErr != nil {
			errs = append(errs, pathErr)
			continue
		}
		if _, managed := mountedContainerDiskSource(mountPath); !managed {
			continue
		}
		mounted, mountErr := m.containerDiskMountActive(l, ct)
		if mountErr != nil {
			errs = append(errs, fmt.Errorf("check orphan container disk %s: %w", id, mountErr))
			continue
		}
		if !mounted {
			continue
		}
		if cleanupErr := m.cleanupContainerDiskMount(ctx, l, ct, address, namespace); cleanupErr != nil {
			errs = append(errs, fmt.Errorf("cleanup orphan container disk %s: %w", id, cleanupErr))
			continue
		}
		actions = append(actions, "cleaned orphan container disk:"+id)
	}
	return actions, errors.Join(errs...)
}

func skippedContainerDiskID(skip map[string]bool, id string) bool {
	for candidate, skipped := range skip {
		if skipped && strings.EqualFold(candidate, id) {
			return true
		}
	}
	return false
}

func (m *diskManager) cleanupContainerOverlayMount(ctx context.Context, l *lab.Lab, ct lab.Container, diskRoot, address, namespace string) error {
	paths, err := containerOverlayPaths(l, ct, diskRoot)
	if err != nil {
		return err
	}
	if mounted, _, err := m.ops.mountSource(paths.merged); err != nil {
		return err
	} else if mounted {
		if err := m.ops.runCommand(ctx, "umount", paths.merged); err != nil {
			return fmt.Errorf("unmount container overlay rootfs: %w", err)
		}
	}
	if mounted, _, err := m.ops.mountSource(paths.lower); err != nil {
		return err
	} else if mounted {
		if err := m.cleanupContainerImageMount(ctx, paths.lower, address, namespace); err != nil {
			return err
		}
	}
	return nil
}

func (m *diskManager) cleanupContainerImageMount(ctx context.Context, lower, address, namespace string) error {
	args := ctrGlobalArgs(address, namespace)
	args = append(args, "images", "unmount", "--rm", lower)
	if err := m.ops.runCommand(ctx, "ctr", args...); err != nil {
		return fmt.Errorf("unmount container image rootfs: %w", err)
	}
	_ = os.Remove(lower + containerImageSourceMarker)
	return nil
}

func ctrGlobalArgs(address, namespace string) []string {
	args := []string{}
	if address = strings.TrimSpace(address); address != "" && address != DefaultAddress {
		args = append(args, "--address", address)
	}
	if namespace = strings.TrimSpace(namespace); namespace != "" {
		args = append(args, "-n", namespace)
	}
	return args
}

func requireContainerDiskTools(lookPath func(string) (string, error)) error {
	for _, name := range []string{"qemu-nbd", "modprobe", "mount", "umount", "mkfs.ext4", "resize2fs", "blkid", "ctr"} {
		if _, err := lookPath(name); err != nil {
			return fmt.Errorf("container disk mount requires %s", name)
		}
	}
	return nil
}

func containerDiskMountPath(l *lab.Lab, ct lab.Container) (string, error) {
	root, err := l.StorageRoot()
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "container-data", ct.ID), nil
}

func freeNBDDevice() (string, error) {
	for i := 0; i < 16; i++ {
		device := fmt.Sprintf("/dev/nbd%d", i)
		if _, err := os.Stat(device); err != nil {
			continue
		}
		if busy, err := nbdDeviceBusy(device); err == nil && !busy {
			return device, nil
		}
	}
	return "", fmt.Errorf("no free /dev/nbd device")
}

func nbdDeviceBusy(device string) (bool, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 0 && nbdBaseDevice(fields[0]) == device {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func mountSource(target string) (bool, string, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return false, "", err
	}
	defer f.Close()
	target = filepath.Clean(target)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && filepath.Clean(fields[1]) == target {
			return true, fields[0], nil
		}
	}
	return false, "", scanner.Err()
}

func mountedFilesystemType(target string) (string, error) {
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return "", err
	}
	defer f.Close()
	target = filepath.Clean(target)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 && filepath.Clean(fields[1]) == target {
			return fields[2], nil
		}
	}
	return "", scanner.Err()
}

func nbdBaseDevice(source string) string {
	source = strings.TrimSpace(source)
	if !strings.HasPrefix(source, "/dev/nbd") {
		return source
	}
	rest := strings.TrimPrefix(source, "/dev/nbd")
	digits := strings.Builder{}
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		digits.WriteRune(r)
	}
	if digits.Len() == 0 {
		return source
	}
	return "/dev/nbd" + digits.String()
}
