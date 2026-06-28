package containerd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"foxlab-cli/internal/lab"
)

var runHostCommand = func(ctx context.Context, name string, args ...string) error {
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

var lookPath = exec.LookPath

type containerDiskMount struct {
	Source                  string
	Destination             string
	CleanupDiskOnFailure    bool
	CleanupOverlayOnFailure bool
}

const containerDiskSourceMarker = ".foxlab-disk-source"
const containerImageSourceMarker = ".foxlab-image-source"

var containerDiskHooks = struct {
	requireTools func() error
	mountSource  func(string) (bool, string, error)
	rootWritable func(string) bool
	freeNBD      func() (string, error)
}{
	requireTools: requireContainerDiskTools,
	mountSource:  mountSource,
	rootWritable: containerRootFSWritable,
	freeNBD:      freeNBDDevice,
}

func prepareContainerDiskMount(ctx context.Context, l *lab.Lab, ct lab.Container, imageRef, address, namespace string) (containerDiskMount, error) {
	if err := containerDiskHooks.requireTools(); err != nil {
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
	if mounted, source, err := containerDiskHooks.mountSource(mountPath); err != nil {
		return containerDiskMount{}, err
	} else if mounted {
		if !containerDiskHooks.rootWritable(mountPath) {
			if err := cleanupMountedContainerDiskMount(ctx, mountPath, source); err != nil {
				return containerDiskMount{}, fmt.Errorf("reset unusable container disk mount: %w", err)
			}
		} else {
			if mountedDiskPath, ok := mountedContainerDiskSource(mountPath); ok {
				if mountedDiskPath != filepath.Clean(diskPath) {
					if err := cleanupMountedContainerDiskMount(ctx, mountPath, source); err != nil {
						return containerDiskMount{}, fmt.Errorf("reset stale container disk mount: %w", err)
					}
				} else {
					return prepareContainerOverlayMount(ctx, l, ct, imageRef, address, namespace, mountPath, cleanupDiskOnFailure)
				}
			} else {
				return prepareContainerOverlayMount(ctx, l, ct, imageRef, address, namespace, mountPath, cleanupDiskOnFailure)
			}
		}
	}
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		return containerDiskMount{}, err
	}
	_ = runHostCommand(ctx, "modprobe", "nbd", "max_part=16")
	device, err := containerDiskHooks.freeNBD()
	if err != nil {
		return containerDiskMount{}, err
	}
	if err := runHostCommand(ctx, "qemu-nbd", "--connect="+device, diskPath); err != nil {
		return containerDiskMount{}, fmt.Errorf("connect container disk: %w", err)
	}
	if err := mountContainerDisk(ctx, device, mountPath); err != nil {
		_ = runHostCommand(ctx, "qemu-nbd", "--disconnect", device)
		return containerDiskMount{}, fmt.Errorf("mount container disk: %w", err)
	}
	cleanupDiskOnFailure = true
	if err := writeContainerDiskSourceMarker(mountPath, diskPath); err != nil {
		_ = cleanupMountedContainerDiskMount(ctx, mountPath, device)
		return containerDiskMount{}, fmt.Errorf("record container disk source: %w", err)
	}
	mount, err := prepareContainerOverlayMount(ctx, l, ct, imageRef, address, namespace, mountPath, cleanupDiskOnFailure)
	if err != nil {
		_ = cleanupMountedContainerDiskMount(ctx, mountPath, device)
		return containerDiskMount{}, err
	}
	return mount, nil
}

func prepareContainerOverlayMount(ctx context.Context, l *lab.Lab, ct lab.Container, imageRef, address, namespace, diskRoot string, cleanupDiskOnFailure bool) (containerDiskMount, error) {
	paths, err := containerOverlayPaths(l, ct, diskRoot)
	if err != nil {
		return containerDiskMount{}, err
	}
	if err := os.MkdirAll(paths.lower, 0o755); err != nil {
		return containerDiskMount{}, err
	}
	if err := os.MkdirAll(paths.upper, 0o755); err != nil {
		return containerDiskMount{}, err
	}
	if err := os.MkdirAll(paths.work, 0o755); err != nil {
		return containerDiskMount{}, err
	}
	if err := os.MkdirAll(paths.merged, 0o755); err != nil {
		return containerDiskMount{}, err
	}
	if err := ensureContainerImageMount(ctx, paths.lower, paths.imageMarker, imageRef, address, namespace); err != nil {
		return containerDiskMount{}, err
	}
	if err := ensureContainerOverlayRoot(ctx, paths); err != nil {
		_ = cleanupContainerImageMount(ctx, paths.lower, address, namespace)
		return containerDiskMount{}, err
	}
	return containerDiskMount{
		Source:                  paths.merged,
		Destination:             "/",
		CleanupDiskOnFailure:    cleanupDiskOnFailure,
		CleanupOverlayOnFailure: true,
	}, nil
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
	lower := filepath.Join(root, "container-image-rootfs", ct.ID)
	return containerOverlayPathSet{
		lower:       lower,
		imageMarker: lower + containerImageSourceMarker,
		upper:       filepath.Join(diskRoot, "upper"),
		work:        filepath.Join(diskRoot, "work"),
		merged:      filepath.Join(diskRoot, "merged"),
	}, nil
}

func ensureContainerImageMount(ctx context.Context, lower, marker, imageRef, address, namespace string) error {
	if mounted, _, err := containerDiskHooks.mountSource(lower); err != nil {
		return err
	} else if mounted {
		if mountedImageRef, ok := mountedContainerImageSource(marker); ok && mountedImageRef == imageRef {
			return nil
		}
		if err := cleanupContainerImageMount(ctx, lower, address, namespace); err != nil {
			return fmt.Errorf("reset stale container image rootfs: %w", err)
		}
	}
	args := ctrGlobalArgs(address, namespace)
	args = append(args, "images", "mount", "--snapshotter", "overlayfs", imageRef, lower)
	if err := runHostCommand(ctx, "ctr", args...); err != nil {
		return fmt.Errorf("mount container image rootfs: %w", err)
	}
	if err := writeContainerImageSourceMarker(marker, imageRef); err != nil {
		_ = cleanupContainerImageMount(ctx, lower, address, namespace)
		return fmt.Errorf("record container image source: %w", err)
	}
	return nil
}

func ensureContainerOverlayRoot(ctx context.Context, paths containerOverlayPathSet) error {
	if mounted, _, err := containerDiskHooks.mountSource(paths.merged); err != nil {
		return err
	} else if mounted {
		if err := runHostCommand(ctx, "umount", paths.merged); err != nil {
			return fmt.Errorf("reset stale container overlay rootfs: %w", err)
		}
	}
	options := "lowerdir=" + paths.lower + ",upperdir=" + paths.upper + ",workdir=" + paths.work
	if err := runHostCommand(ctx, "mount", "-t", "overlay", "overlay", "-o", options, paths.merged); err != nil {
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

func mountContainerDisk(ctx context.Context, device, mountPath string) error {
	err := runHostCommand(ctx, "mount", device, mountPath)
	if err == nil {
		return nil
	}
	if !isUnformattedDiskMountError(err) {
		return err
	}
	if containerDiskHasFilesystem(ctx, device) {
		return err
	}
	if mkfsErr := formatContainerDisk(ctx, device); mkfsErr != nil {
		return mkfsErr
	}
	return runHostCommand(ctx, "mount", device, mountPath)
}

func formatContainerDisk(ctx context.Context, device string) error {
	if err := runHostCommand(ctx, "mkfs.ext4", "-F", device); err != nil {
		return fmt.Errorf("format empty container disk after mount failed: %w", err)
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

func containerDiskHasFilesystem(ctx context.Context, device string) bool {
	return runHostCommand(ctx, "blkid", "-o", "value", "-s", "TYPE", device) == nil
}

func isUnformattedDiskMountError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "wrong fs type") || strings.Contains(text, "bad superblock")
}

func cleanupMountedContainerDiskMount(ctx context.Context, mountPath, source string) error {
	if err := runHostCommand(ctx, "umount", mountPath); err != nil {
		return fmt.Errorf("unmount container disk: %w", err)
	}
	if strings.HasPrefix(source, "/dev/nbd") {
		if err := runHostCommand(ctx, "qemu-nbd", "--disconnect", nbdBaseDevice(source)); err != nil {
			return fmt.Errorf("disconnect container disk: %w", err)
		}
	}
	return nil
}

func cleanupContainerDiskMount(ctx context.Context, l *lab.Lab, ct lab.Container, address, namespace string) error {
	if ct.Disk == "" {
		return nil
	}
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		return err
	}
	if err := cleanupContainerOverlayMount(ctx, l, ct, mountPath, address, namespace); err != nil {
		return err
	}
	mounted, source, err := containerDiskHooks.mountSource(mountPath)
	if err != nil || !mounted {
		return err
	}
	return cleanupMountedContainerDiskMount(ctx, mountPath, source)
}

func cleanupContainerOverlayMount(ctx context.Context, l *lab.Lab, ct lab.Container, diskRoot, address, namespace string) error {
	paths, err := containerOverlayPaths(l, ct, diskRoot)
	if err != nil {
		return err
	}
	if mounted, _, err := containerDiskHooks.mountSource(paths.merged); err != nil {
		return err
	} else if mounted {
		if err := runHostCommand(ctx, "umount", paths.merged); err != nil {
			return fmt.Errorf("unmount container overlay rootfs: %w", err)
		}
	}
	if mounted, _, err := containerDiskHooks.mountSource(paths.lower); err != nil {
		return err
	} else if mounted {
		if err := cleanupContainerImageMount(ctx, paths.lower, address, namespace); err != nil {
			return err
		}
	}
	return nil
}

func cleanupContainerImageMount(ctx context.Context, lower, address, namespace string) error {
	args := ctrGlobalArgs(address, namespace)
	args = append(args, "images", "unmount", "--rm", lower)
	if err := runHostCommand(ctx, "ctr", args...); err != nil {
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

func requireContainerDiskTools() error {
	for _, name := range []string{"qemu-nbd", "modprobe", "mount", "umount", "mkfs.ext4", "blkid", "ctr"} {
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
	return filepath.Join(root, "container-rootfs", ct.ID), nil
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
