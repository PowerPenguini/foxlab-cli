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

type containerDiskMount struct {
	Source      string
	Destination string
}

func prepareContainerDiskMount(ctx context.Context, l *lab.Lab, ct lab.Container) (containerDiskMount, error) {
	if err := requireContainerDiskTools(); err != nil {
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
	rebuild := false
	if mounted, source, err := mountSource(mountPath); err != nil {
		return containerDiskMount{}, err
	} else if mounted {
		if !containerRootFSWritable(mountPath) {
			if err := cleanupMountedContainerDiskMount(ctx, mountPath, source); err != nil {
				return containerDiskMount{}, fmt.Errorf("reset unusable container disk mount: %w", err)
			}
			rebuild = true
		} else {
			return containerDiskMount{Source: mountPath, Destination: containerDiskDestination(l, ct)}, nil
		}
	}
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		return containerDiskMount{}, err
	}
	_ = runHostCommand(ctx, "modprobe", "nbd", "max_part=16")
	device, err := freeNBDDevice()
	if err != nil {
		return containerDiskMount{}, err
	}
	if err := runHostCommand(ctx, "qemu-nbd", "--connect="+device, diskPath); err != nil {
		return containerDiskMount{}, fmt.Errorf("connect container disk: %w", err)
	}
	if err := mountContainerDisk(ctx, device, mountPath, rebuild); err != nil {
		_ = runHostCommand(ctx, "qemu-nbd", "--disconnect", device)
		return containerDiskMount{}, fmt.Errorf("mount container disk: %w", err)
	}
	return containerDiskMount{Source: mountPath, Destination: containerDiskDestination(l, ct)}, nil
}

func mountContainerDisk(ctx context.Context, device, mountPath string, forceFormat bool) error {
	if forceFormat {
		if err := formatContainerDisk(ctx, device); err != nil {
			return err
		}
		return runHostCommand(ctx, "mount", device, mountPath)
	}
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

func cleanupContainerDiskMount(ctx context.Context, l *lab.Lab, ct lab.Container) error {
	if ct.Disk == "" {
		return nil
	}
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		return err
	}
	mounted, source, err := mountSource(mountPath)
	if err != nil || !mounted {
		return err
	}
	return cleanupMountedContainerDiskMount(ctx, mountPath, source)
}

func requireContainerDiskTools() error {
	for _, name := range []string{"qemu-img", "qemu-nbd", "modprobe", "mount", "umount", "mkfs.ext4", "blkid"} {
		if _, err := exec.LookPath(name); err != nil {
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
