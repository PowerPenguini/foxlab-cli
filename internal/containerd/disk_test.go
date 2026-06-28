package containerd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

const testImageRef = "docker.io/library/bash:latest"

func TestMountContainerDiskFormatsAfterWrongFSType(t *testing.T) {
	restore := stubRunHostCommand(t, func(name string, args ...string) error {
		if name == "mount" && len(args) == 2 && args[0] == "/dev/nbd0" {
			if mountCalls := testCommandCount(t, "mount"); mountCalls == 1 {
				return fmt.Errorf("exit status 32: mount: /rootfs: wrong fs type, bad superblock on /dev/nbd0")
			}
		}
		if name == "blkid" {
			return fmt.Errorf("exit status 2")
		}
		return nil
	})
	defer restore()

	if err := mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"mount /dev/nbd0 /rootfs",
		"blkid -o value -s TYPE /dev/nbd0",
		"mkfs.ext4 -F /dev/nbd0",
		"mount /dev/nbd0 /rootfs",
	}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestMountContainerDiskDoesNotFormatExistingFilesystem(t *testing.T) {
	restore := stubRunHostCommand(t, func(name string, args ...string) error {
		if name == "mount" {
			return fmt.Errorf("exit status 32: mount: /rootfs: wrong fs type, bad superblock on /dev/nbd0")
		}
		return nil
	})
	defer restore()

	if err := mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs"); err == nil {
		t.Fatal("mountContainerDisk returned nil error")
	}
	want := []string{
		"mount /dev/nbd0 /rootfs",
		"blkid -o value -s TYPE /dev/nbd0",
	}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestMountContainerDiskDoesNotFormatOtherMountErrors(t *testing.T) {
	restore := stubRunHostCommand(t, func(name string, args ...string) error {
		if name == "mount" {
			return fmt.Errorf("exit status 32: mount: permission denied")
		}
		return nil
	})
	defer restore()

	if err := mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs"); err == nil {
		t.Fatal("mountContainerDisk returned nil error")
	}
	want := []string{"mount /dev/nbd0 /rootfs"}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestRequireContainerDiskToolsDoesNotRequireUnusedQemuImg(t *testing.T) {
	oldLookPath := lookPath
	lookPath = func(file string) (string, error) {
		if file == "qemu-img" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}
	t.Cleanup(func() { lookPath = oldLookPath })

	if err := requireContainerDiskTools(); err != nil {
		t.Fatalf("requireContainerDiskTools() = %v, want qemu-img absence ignored", err)
	}
}

func TestRequireContainerDiskToolsReportsMissingUsedTool(t *testing.T) {
	oldLookPath := lookPath
	lookPath = func(file string) (string, error) {
		if file == "qemu-nbd" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}
	t.Cleanup(func() { lookPath = oldLookPath })

	err := requireContainerDiskTools()
	if err == nil || !strings.Contains(err.Error(), "requires qemu-nbd") {
		t.Fatalf("requireContainerDiskTools() = %v, want qemu-nbd error", err)
	}
}

func TestCtrGlobalArgsIncludesCustomAddressAndNamespace(t *testing.T) {
	got := ctrGlobalArgs("/tmp/containerd.sock", "foxlab")
	want := []string{"--address", "/tmp/containerd.sock", "-n", "foxlab"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ctr args = %#v, want %#v", got, want)
	}
	if got := ctrGlobalArgs(DefaultAddress, "foxlab"); !reflect.DeepEqual(got, []string{"-n", "foxlab"}) {
		t.Fatalf("default-address ctr args = %#v, want namespace only", got)
	}
}

func TestPrepareContainerDiskMountDoesNotForceFormatAfterStaleMountCleanup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SUDO_USER", "")
	diskPath := filepath.Join(t.TempDir(), "web.qcow2")
	if err := os.WriteFile(diskPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	l := &lab.Lab{ID: "demo"}
	ct := lab.Container{ID: "web", Disk: diskPath}
	root, err := l.StorageRoot()
	if err != nil {
		t.Fatal(err)
	}
	mountPath := filepath.Join(root, "container-rootfs", "web")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")

	oldHooks := containerDiskHooks
	containerDiskHooks.requireTools = func() error { return nil }
	containerDiskHooks.mountSource = func(path string) (bool, string, error) {
		switch path {
		case mountPath:
			return true, "/dev/nbd0", nil
		case lowerPath, mergedPath:
			return false, "", nil
		default:
			t.Fatalf("unexpected mountSource path = %q", path)
			return false, "", nil
		}
	}
	containerDiskHooks.rootWritable = func(path string) bool {
		if path != mountPath {
			t.Fatalf("rootWritable path = %q, want %q", path, mountPath)
		}
		return false
	}
	containerDiskHooks.freeNBD = func() (string, error) { return "/dev/nbd1", nil }
	t.Cleanup(func() { containerDiskHooks = oldHooks })
	restore := stubRunHostCommand(t, func(name string, args ...string) error {
		return nil
	})
	defer restore()

	mount, err := prepareContainerDiskMount(context.Background(), l, ct, testImageRef, "", DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if mount.Source != mergedPath || mount.Destination != "/" {
		t.Fatalf("mount = %#v, want source %q destination /", mount, mergedPath)
	}
	if !mount.CleanupDiskOnFailure || !mount.CleanupOverlayOnFailure {
		t.Fatalf("mount = %#v, want cleanup on failure for newly mounted disk and overlay", mount)
	}
	for _, command := range testCommands(t) {
		if strings.HasPrefix(command, "mkfs.ext4 ") {
			t.Fatalf("prepare formatted existing disk after stale mount cleanup; commands=%#v", testCommands(t))
		}
	}
	want := []string{
		"umount " + mountPath,
		"qemu-nbd --disconnect /dev/nbd0",
		"modprobe nbd max_part=16",
		"qemu-nbd --connect=/dev/nbd1 " + diskPath,
		"mount /dev/nbd1 " + mountPath,
		"ctr -n foxlab images mount --snapshotter overlayfs " + testImageRef + " " + lowerPath,
		"mount -t overlay overlay -o lowerdir=" + lowerPath + ",upperdir=" + filepath.Join(mountPath, "upper") + ",workdir=" + filepath.Join(mountPath, "work") + " " + mergedPath,
	}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestPrepareContainerDiskMountReusesMountedDiskWhenMarkerMatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SUDO_USER", "")
	diskPath := filepath.Join(t.TempDir(), "web.qcow2")
	if err := os.WriteFile(diskPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	l := &lab.Lab{ID: "demo"}
	ct := lab.Container{ID: "web", Disk: diskPath}
	root, err := l.StorageRoot()
	if err != nil {
		t.Fatal(err)
	}
	mountPath := filepath.Join(root, "container-rootfs", "web")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeContainerDiskSourceMarker(mountPath, diskPath); err != nil {
		t.Fatal(err)
	}

	oldHooks := containerDiskHooks
	containerDiskHooks.requireTools = func() error { return nil }
	containerDiskHooks.mountSource = func(path string) (bool, string, error) {
		switch path {
		case mountPath:
			return true, "/dev/nbd0", nil
		case lowerPath, mergedPath:
			return false, "", nil
		default:
			t.Fatalf("unexpected mountSource path = %q", path)
			return false, "", nil
		}
	}
	containerDiskHooks.rootWritable = func(path string) bool { return true }
	containerDiskHooks.freeNBD = func() (string, error) {
		t.Fatal("freeNBD called for matching mounted disk")
		return "", nil
	}
	t.Cleanup(func() { containerDiskHooks = oldHooks })
	restore := stubRunHostCommand(t, func(name string, args ...string) error {
		return nil
	})
	defer restore()

	mount, err := prepareContainerDiskMount(context.Background(), l, ct, testImageRef, "", DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if mount.Source != mergedPath || mount.Destination != "/" {
		t.Fatalf("mount = %#v, want source %q destination /", mount, mergedPath)
	}
	if mount.CleanupDiskOnFailure || !mount.CleanupOverlayOnFailure {
		t.Fatalf("mount = %#v, want reused disk preserved and overlay cleaned on later failure", mount)
	}
	want := []string{
		"ctr -n foxlab images mount --snapshotter overlayfs " + testImageRef + " " + lowerPath,
		"mount -t overlay overlay -o lowerdir=" + lowerPath + ",upperdir=" + filepath.Join(mountPath, "upper") + ",workdir=" + filepath.Join(mountPath, "work") + " " + mergedPath,
	}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestPrepareContainerDiskMountReusesWritableLegacyMountWithoutMarker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SUDO_USER", "")
	diskPath := filepath.Join(t.TempDir(), "web.qcow2")
	if err := os.WriteFile(diskPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	l := &lab.Lab{ID: "demo"}
	ct := lab.Container{ID: "web", Disk: diskPath}
	root, err := l.StorageRoot()
	if err != nil {
		t.Fatal(err)
	}
	mountPath := filepath.Join(root, "container-rootfs", "web")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}

	oldHooks := containerDiskHooks
	containerDiskHooks.requireTools = func() error { return nil }
	containerDiskHooks.mountSource = func(path string) (bool, string, error) {
		switch path {
		case mountPath:
			return true, "/dev/nbd0", nil
		case lowerPath, mergedPath:
			return false, "", nil
		default:
			t.Fatalf("unexpected mountSource path = %q", path)
			return false, "", nil
		}
	}
	containerDiskHooks.rootWritable = func(path string) bool { return true }
	containerDiskHooks.freeNBD = func() (string, error) {
		t.Fatal("freeNBD called for writable legacy mount")
		return "", nil
	}
	t.Cleanup(func() { containerDiskHooks = oldHooks })
	restore := stubRunHostCommand(t, func(name string, args ...string) error {
		return nil
	})
	defer restore()

	mount, err := prepareContainerDiskMount(context.Background(), l, ct, testImageRef, "", DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if mount.Source != mergedPath || mount.Destination != "/" {
		t.Fatalf("mount = %#v, want source %q destination /", mount, mergedPath)
	}
	if mount.CleanupDiskOnFailure || !mount.CleanupOverlayOnFailure {
		t.Fatalf("mount = %#v, want reused legacy disk preserved and overlay cleaned on later failure", mount)
	}
	want := []string{
		"ctr -n foxlab images mount --snapshotter overlayfs " + testImageRef + " " + lowerPath,
		"mount -t overlay overlay -o lowerdir=" + lowerPath + ",upperdir=" + filepath.Join(mountPath, "upper") + ",workdir=" + filepath.Join(mountPath, "work") + " " + mergedPath,
	}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestPrepareContainerDiskMountReplacesMountedDiskWhenMarkerDiffers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SUDO_USER", "")
	dir := t.TempDir()
	oldDiskPath := filepath.Join(dir, "old.qcow2")
	newDiskPath := filepath.Join(dir, "new.qcow2")
	for _, path := range []string{oldDiskPath, newDiskPath} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l := &lab.Lab{ID: "demo"}
	ct := lab.Container{ID: "web", Disk: newDiskPath}
	root, err := l.StorageRoot()
	if err != nil {
		t.Fatal(err)
	}
	mountPath := filepath.Join(root, "container-rootfs", "web")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeContainerDiskSourceMarker(mountPath, oldDiskPath); err != nil {
		t.Fatal(err)
	}

	oldHooks := containerDiskHooks
	containerDiskHooks.requireTools = func() error { return nil }
	containerDiskHooks.mountSource = func(path string) (bool, string, error) {
		switch path {
		case mountPath:
			return true, "/dev/nbd0", nil
		case lowerPath, mergedPath:
			return false, "", nil
		default:
			t.Fatalf("unexpected mountSource path = %q", path)
			return false, "", nil
		}
	}
	containerDiskHooks.rootWritable = func(path string) bool { return true }
	containerDiskHooks.freeNBD = func() (string, error) { return "/dev/nbd1", nil }
	t.Cleanup(func() { containerDiskHooks = oldHooks })
	restore := stubRunHostCommand(t, func(name string, args ...string) error {
		return nil
	})
	defer restore()

	mount, err := prepareContainerDiskMount(context.Background(), l, ct, testImageRef, "", DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if mount.Source != mergedPath || mount.Destination != "/" {
		t.Fatalf("mount = %#v, want source %q destination /", mount, mergedPath)
	}
	if !mount.CleanupDiskOnFailure || !mount.CleanupOverlayOnFailure {
		t.Fatalf("mount = %#v, want cleanup on failure for replaced disk mount and overlay", mount)
	}
	marker, err := os.ReadFile(containerDiskSourceMarkerPath(mountPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(marker)) != filepath.Clean(newDiskPath) {
		t.Fatalf("marker = %q, want %q", strings.TrimSpace(string(marker)), filepath.Clean(newDiskPath))
	}
	want := []string{
		"umount " + mountPath,
		"qemu-nbd --disconnect /dev/nbd0",
		"modprobe nbd max_part=16",
		"qemu-nbd --connect=/dev/nbd1 " + newDiskPath,
		"mount /dev/nbd1 " + mountPath,
		"ctr -n foxlab images mount --snapshotter overlayfs " + testImageRef + " " + lowerPath,
		"mount -t overlay overlay -o lowerdir=" + lowerPath + ",upperdir=" + filepath.Join(mountPath, "upper") + ",workdir=" + filepath.Join(mountPath, "work") + " " + mergedPath,
	}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestCleanupPreparedContainerDiskMountReturnsCleanupError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SUDO_USER", "")
	l := &lab.Lab{ID: "demo"}
	ct := lab.Container{ID: "web", Disk: filepath.Join(t.TempDir(), "web.qcow2")}
	root, err := l.StorageRoot()
	if err != nil {
		t.Fatal(err)
	}
	mountPath := filepath.Join(root, "container-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")

	oldHooks := containerDiskHooks
	containerDiskHooks.mountSource = func(path string) (bool, string, error) {
		switch path {
		case mergedPath, lowerPath:
			return false, "", nil
		case mountPath:
			return true, "/dev/nbd0", nil
		default:
			t.Fatalf("unexpected mountSource path = %q", path)
			return false, "", nil
		}
	}
	t.Cleanup(func() { containerDiskHooks = oldHooks })
	restore := stubRunHostCommand(t, func(name string, args ...string) error {
		if name == "umount" {
			return fmt.Errorf("busy")
		}
		return nil
	})
	defer restore()

	err = cleanupPreparedContainerDiskMount(l, ct, containerDiskMount{CleanupDiskOnFailure: true}, "", DefaultNamespace)

	if err == nil || !strings.Contains(err.Error(), "unmount container disk") {
		t.Fatalf("cleanup error = %v, want unmount error", err)
	}
	want := []string{"umount " + mountPath}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func stubRunHostCommand(t *testing.T, fn func(name string, args ...string) error) func() {
	t.Helper()
	old := runHostCommand
	var commands []string
	runHostCommand = func(ctx context.Context, name string, args ...string) error {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		return fn(name, args...)
	}
	t.Cleanup(func() {
		runHostCommand = old
		testCommandLog = nil
	})
	testCommandLog = &commands
	return func() {
		runHostCommand = old
		testCommandLog = nil
	}
}

var testCommandLog *[]string

func testCommands(t *testing.T) []string {
	t.Helper()
	if testCommandLog == nil {
		t.Fatal("test command log is not installed")
	}
	return append([]string(nil), (*testCommandLog)...)
}

func testCommandCount(t *testing.T, name string) int {
	t.Helper()
	count := 0
	for _, command := range testCommands(t) {
		if command == name || len(command) > len(name) && command[:len(name)+1] == name+" " {
			count++
		}
	}
	return count
}
