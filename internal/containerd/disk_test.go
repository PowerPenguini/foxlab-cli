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
	manager := stubDiskManager(t, func(name string, args ...string) error {
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

	if err := manager.mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs"); err != nil {
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
	manager := stubDiskManager(t, func(name string, args ...string) error {
		if name == "mount" {
			return fmt.Errorf("exit status 32: mount: /rootfs: wrong fs type, bad superblock on /dev/nbd0")
		}
		return nil
	})

	if err := manager.mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs"); err == nil {
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
	manager := stubDiskManager(t, func(name string, args ...string) error {
		if name == "mount" {
			return fmt.Errorf("exit status 32: mount: permission denied")
		}
		return nil
	})

	if err := manager.mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs"); err == nil {
		t.Fatal("mountContainerDisk returned nil error")
	}
	want := []string{"mount /dev/nbd0 /rootfs"}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestGrowMountedContainerDiskFilesystemExpandsExt4(t *testing.T) {
	manager := stubDiskManager(t, func(name string, args ...string) error { return nil })
	manager.ops.filesystemType = func(path string) (string, error) {
		if path != "/rootfs" {
			t.Fatalf("filesystem type path = %q, want /rootfs", path)
		}
		return "ext4", nil
	}

	if err := manager.growMountedContainerDiskFilesystem(context.Background(), "/dev/nbd0", "/rootfs"); err != nil {
		t.Fatal(err)
	}
	want := []string{"resize2fs /dev/nbd0"}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestGrowMountedContainerDiskFilesystemSkipsNonExtFilesystem(t *testing.T) {
	manager := stubDiskManager(t, func(name string, args ...string) error { return nil })
	manager.ops.filesystemType = func(string) (string, error) { return "xfs", nil }

	if err := manager.growMountedContainerDiskFilesystem(context.Background(), "/dev/nbd0", "/rootfs"); err != nil {
		t.Fatal(err)
	}
	if got := testCommands(t); len(got) != 0 {
		t.Fatalf("commands = %#v, want none", got)
	}
}

func TestGrowMountedContainerDiskFilesystemReportsResizeFailure(t *testing.T) {
	manager := stubDiskManager(t, func(name string, args ...string) error {
		return fmt.Errorf("resize failed")
	})
	manager.ops.filesystemType = func(string) (string, error) { return "ext4", nil }

	err := manager.growMountedContainerDiskFilesystem(context.Background(), "/dev/nbd0", "/rootfs")
	if err == nil || !strings.Contains(err.Error(), "grow container disk filesystem: resize failed") {
		t.Fatalf("grow error = %v", err)
	}
}

func TestRequireContainerDiskToolsDoesNotRequireUnusedQemuImg(t *testing.T) {
	lookup := func(file string) (string, error) {
		if file == "qemu-img" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}

	if err := requireContainerDiskTools(lookup); err != nil {
		t.Fatalf("requireContainerDiskTools() = %v, want qemu-img absence ignored", err)
	}
}

func TestRequireContainerDiskToolsReportsMissingUsedTool(t *testing.T) {
	lookup := func(file string) (string, error) {
		if file == "qemu-nbd" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}

	err := requireContainerDiskTools(lookup)
	if err == nil || !strings.Contains(err.Error(), "requires qemu-nbd") {
		t.Fatalf("requireContainerDiskTools() = %v, want qemu-nbd error", err)
	}
}

func TestRequireContainerDiskToolsReportsMissingResize2FS(t *testing.T) {
	lookup := func(file string) (string, error) {
		if file == "resize2fs" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + file, nil
	}

	err := requireContainerDiskTools(lookup)
	if err == nil || !strings.Contains(err.Error(), "requires resize2fs") {
		t.Fatalf("requireContainerDiskTools() = %v, want resize2fs error", err)
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

func TestContainerDiskMountActiveUsesManagedMountPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SUDO_USER", "")
	l := &lab.Lab{ID: "demo"}
	ct := lab.Container{ID: "web", Disk: "/tmp/web.qcow2"}
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		t.Fatal(err)
	}
	manager := newDiskManager()
	manager.ops.mountSource = func(path string) (bool, string, error) {
		if path != mountPath {
			t.Fatalf("mount path = %q, want %q", path, mountPath)
		}
		return true, "/dev/nbd0", nil
	}
	mounted, err := manager.containerDiskMountActive(l, ct)
	if err != nil || !mounted {
		t.Fatalf("containerDiskMountActive = %t, %v", mounted, err)
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
	mountPath := filepath.Join(root, "container-data", "web")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	overlayOptions := "lowerdir=" + lowerPath + ",upperdir=" + filepath.Join(mountPath, "upper") + ",workdir=" + filepath.Join(mountPath, "work")

	manager := newDiskManager()
	manager.ops.requireTools = func() error { return nil }
	manager.ops.mountSource = func(path string) (bool, string, error) {
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
	manager.ops.rootWritable = func(path string) bool {
		if path != mountPath {
			t.Fatalf("rootWritable path = %q, want %q", path, mountPath)
		}
		return false
	}
	manager.ops.sourceHealthy = func(source string) bool { return true }
	manager.ops.freeNBD = func() (string, error) { return "/dev/nbd1", nil }
	stubDiskManagerCommands(t, manager, func(name string, args ...string) error {
		return nil
	})

	mount, err := manager.prepareContainerDiskMount(context.Background(), l, ct, testImageRef, "", DefaultNamespace)
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
		testNBDConnectCommand("/dev/nbd1", diskPath),
		"mount /dev/nbd1 " + mountPath,
		"ctr -n foxlab images mount --snapshotter overlayfs " + testImageRef + " " + lowerPath,
		"mount -t overlay overlay -o " + overlayOptions + " " + mergedPath,
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
	mountPath := filepath.Join(root, "container-data", "web")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	overlayOptions := "lowerdir=" + lowerPath + ",upperdir=" + filepath.Join(mountPath, "upper") + ",workdir=" + filepath.Join(mountPath, "work")
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeContainerDiskSourceMarker(mountPath, diskPath); err != nil {
		t.Fatal(err)
	}

	manager := newDiskManager()
	manager.ops.requireTools = func() error { return nil }
	manager.ops.mountSource = func(path string) (bool, string, error) {
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
	manager.ops.rootWritable = func(path string) bool { return true }
	manager.ops.sourceHealthy = func(source string) bool { return true }
	manager.ops.freeNBD = func() (string, error) {
		t.Fatal("freeNBD called for matching mounted disk")
		return "", nil
	}
	stubDiskManagerCommands(t, manager, func(name string, args ...string) error {
		return nil
	})

	mount, err := manager.prepareContainerDiskMount(context.Background(), l, ct, testImageRef, "", DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if mount.Source != mergedPath || mount.Destination != "/" {
		t.Fatalf("mount = %#v, want source %q destination /", mount, mergedPath)
	}
	if mount.CleanupDiskOnFailure || !mount.CleanupOverlayOnFailure {
		t.Fatalf("mount = %#v, want reused disk preserved and overlay cleaned on start failure", mount)
	}
	want := []string{
		"ctr -n foxlab images mount --snapshotter overlayfs " + testImageRef + " " + lowerPath,
		"mount -t overlay overlay -o " + overlayOptions + " " + mergedPath,
	}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestPrepareContainerDiskMountReplacesMountWhenSourceUnhealthy(t *testing.T) {
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
	mountPath := filepath.Join(root, "container-data", "web")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	overlayOptions := "lowerdir=" + lowerPath + ",upperdir=" + filepath.Join(mountPath, "upper") + ",workdir=" + filepath.Join(mountPath, "work")
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeContainerDiskSourceMarker(mountPath, diskPath); err != nil {
		t.Fatal(err)
	}

	manager := newDiskManager()
	manager.ops.requireTools = func() error { return nil }
	manager.ops.mountSource = func(path string) (bool, string, error) {
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
	manager.ops.rootWritable = func(path string) bool { return true }
	manager.ops.sourceHealthy = func(source string) bool {
		switch source {
		case "/dev/nbd0":
			return false
		case "/dev/nbd1":
			return true
		default:
			t.Fatalf("unexpected sourceHealthy source = %q", source)
			return false
		}
	}
	manager.ops.freeNBD = func() (string, error) { return "/dev/nbd1", nil }
	stubDiskManagerCommands(t, manager, func(name string, args ...string) error {
		return nil
	})

	mount, err := manager.prepareContainerDiskMount(context.Background(), l, ct, testImageRef, "", DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if mount.Source != mergedPath || mount.Destination != "/" {
		t.Fatalf("mount = %#v, want source %q destination /", mount, mergedPath)
	}
	if !mount.CleanupDiskOnFailure || !mount.CleanupOverlayOnFailure {
		t.Fatalf("mount = %#v, want cleanup on failure for replaced unhealthy disk mount and overlay", mount)
	}
	want := []string{
		"umount " + mountPath,
		"qemu-nbd --disconnect /dev/nbd0",
		"modprobe nbd max_part=16",
		testNBDConnectCommand("/dev/nbd1", diskPath),
		"mount /dev/nbd1 " + mountPath,
		"ctr -n foxlab images mount --snapshotter overlayfs " + testImageRef + " " + lowerPath,
		"mount -t overlay overlay -o " + overlayOptions + " " + mergedPath,
	}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestPrepareContainerDiskMountReplacesWritableMountWithoutMarker(t *testing.T) {
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
	mountPath := filepath.Join(root, "container-data", "web")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	overlayOptions := "lowerdir=" + lowerPath + ",upperdir=" + filepath.Join(mountPath, "upper") + ",workdir=" + filepath.Join(mountPath, "work")
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}

	manager := newDiskManager()
	manager.ops.requireTools = func() error { return nil }
	manager.ops.mountSource = func(path string) (bool, string, error) {
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
	manager.ops.rootWritable = func(path string) bool { return true }
	manager.ops.sourceHealthy = func(source string) bool { return true }
	manager.ops.freeNBD = func() (string, error) { return "/dev/nbd1", nil }
	stubDiskManagerCommands(t, manager, func(name string, args ...string) error {
		return nil
	})

	mount, err := manager.prepareContainerDiskMount(context.Background(), l, ct, testImageRef, "", DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if mount.Source != mergedPath || mount.Destination != "/" {
		t.Fatalf("mount = %#v, want source %q destination /", mount, mergedPath)
	}
	if !mount.CleanupDiskOnFailure || !mount.CleanupOverlayOnFailure {
		t.Fatalf("mount = %#v, want cleanup on failure for replaced untracked disk mount and overlay", mount)
	}
	marker, err := os.ReadFile(containerDiskSourceMarkerPath(mountPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(marker)) != filepath.Clean(diskPath) {
		t.Fatalf("marker = %q, want %q", strings.TrimSpace(string(marker)), filepath.Clean(diskPath))
	}
	want := []string{
		"umount " + mountPath,
		"qemu-nbd --disconnect /dev/nbd0",
		"modprobe nbd max_part=16",
		testNBDConnectCommand("/dev/nbd1", diskPath),
		"mount /dev/nbd1 " + mountPath,
		"ctr -n foxlab images mount --snapshotter overlayfs " + testImageRef + " " + lowerPath,
		"mount -t overlay overlay -o " + overlayOptions + " " + mergedPath,
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
	mountPath := filepath.Join(root, "container-data", "web")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	overlayOptions := "lowerdir=" + lowerPath + ",upperdir=" + filepath.Join(mountPath, "upper") + ",workdir=" + filepath.Join(mountPath, "work")
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeContainerDiskSourceMarker(mountPath, oldDiskPath); err != nil {
		t.Fatal(err)
	}

	manager := newDiskManager()
	manager.ops.requireTools = func() error { return nil }
	manager.ops.mountSource = func(path string) (bool, string, error) {
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
	manager.ops.rootWritable = func(path string) bool { return true }
	manager.ops.sourceHealthy = func(source string) bool { return true }
	manager.ops.freeNBD = func() (string, error) { return "/dev/nbd1", nil }
	stubDiskManagerCommands(t, manager, func(name string, args ...string) error {
		return nil
	})

	mount, err := manager.prepareContainerDiskMount(context.Background(), l, ct, testImageRef, "", DefaultNamespace)
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
		testNBDConnectCommand("/dev/nbd1", newDiskPath),
		"mount /dev/nbd1 " + mountPath,
		"ctr -n foxlab images mount --snapshotter overlayfs " + testImageRef + " " + lowerPath,
		"mount -t overlay overlay -o " + overlayOptions + " " + mergedPath,
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
	mountPath := filepath.Join(root, "container-data", "web")
	mergedPath := filepath.Join(mountPath, "merged")
	lowerPath := filepath.Join(root, "container-image-rootfs", "web")

	manager := newDiskManager()
	manager.ops.mountSource = func(path string) (bool, string, error) {
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
	stubDiskManagerCommands(t, manager, func(name string, args ...string) error {
		if name == "umount" {
			return fmt.Errorf("busy")
		}
		return nil
	})

	runtime := &Runtime{disks: manager}
	err = runtime.cleanupPreparedContainerDiskMount(l, ct, containerDiskMount{CleanupDiskOnFailure: true}, "", DefaultNamespace)

	if err == nil || !strings.Contains(err.Error(), "unmount container disk") {
		t.Fatalf("cleanup error = %v, want unmount error", err)
	}
	want := []string{"umount " + mountPath}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestCleanupContainerDiskMountCleansStaleMountWhenDesiredDiskDetached(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SUDO_USER", "")
	l := &lab.Lab{ID: "demo"}
	ct := lab.Container{ID: "web"}
	root, err := l.StorageRoot()
	if err != nil {
		t.Fatal(err)
	}
	mountPath := filepath.Join(root, "container-data", "web")

	manager := newDiskManager()
	manager.ops.mountSource = func(path string) (bool, string, error) {
		switch path {
		case filepath.Join(mountPath, "merged"), filepath.Join(root, "container-image-rootfs", "web"):
			return false, "", nil
		case mountPath:
			return true, "/dev/nbd0", nil
		default:
			t.Fatalf("unexpected mountSource path = %q", path)
			return false, "", nil
		}
	}
	stubDiskManagerCommands(t, manager, func(name string, args ...string) error {
		return nil
	})

	if err := manager.cleanupContainerDiskMount(context.Background(), l, ct, "", DefaultNamespace); err != nil {
		t.Fatal(err)
	}

	want := []string{"umount " + mountPath, "qemu-nbd --disconnect /dev/nbd0"}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestCleanupOrphanContainerDiskMountsRemovesRenamedContainerMount(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SUDO_USER", "")
	l := &lab.Lab{ID: "default", Containers: []lab.Container{{ID: "Kali-a"}}}
	root, err := l.StorageRoot()
	if err != nil {
		t.Fatal(err)
	}
	staleID := "Kali"
	mountPath := filepath.Join(root, "container-data", staleID)
	if err := os.MkdirAll(mountPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeContainerDiskSourceMarker(mountPath, "/disks/kali.qcow2"); err != nil {
		t.Fatal(err)
	}

	manager := newDiskManager()
	manager.ops.mountSource = func(path string) (bool, string, error) {
		switch path {
		case filepath.Join(mountPath, "merged"), filepath.Join(root, "container-image-rootfs", staleID):
			return false, "", nil
		case mountPath:
			return true, "/dev/nbd0", nil
		default:
			t.Fatalf("unexpected mountSource path = %q", path)
			return false, "", nil
		}
	}
	stubDiskManagerCommands(t, manager, func(name string, args ...string) error { return nil })

	actions, err := manager.cleanupOrphanContainerDiskMounts(context.Background(), l, nil, "", DefaultNamespace)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actions, []string{"cleaned orphan container disk:Kali"}) {
		t.Fatalf("actions = %#v", actions)
	}
	want := []string{"umount " + mountPath, "qemu-nbd --disconnect /dev/nbd0"}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestCleanupOrphanContainerDiskMountsKeepsMountAfterDeleteFailure(t *testing.T) {
	if !skippedContainerDiskID(map[string]bool{"kali": true}, "Kali") {
		t.Fatal("case-normalized failed orphan id did not protect its mounted disk")
	}
}

func stubDiskManager(t *testing.T, fn func(name string, args ...string) error) *diskManager {
	t.Helper()
	manager := newDiskManager()
	stubDiskManagerCommands(t, manager, fn)
	return manager
}

func stubDiskManagerCommands(t *testing.T, manager *diskManager, fn func(name string, args ...string) error) {
	t.Helper()
	var commands []string
	manager.ops.runCommand = func(ctx context.Context, name string, args ...string) error {
		commands = append(commands, strings.Join(append([]string{name}, args...), " "))
		return fn(name, args...)
	}
	t.Cleanup(func() {
		testCommandLog = nil
	})
	testCommandLog = &commands
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

func testNBDConnectCommand(device, diskPath string) string {
	return "systemd-run --scope --collect --quiet --unit " + containerDiskScopeUnit(device) + " qemu-nbd --fork --connect=" + device + " " + diskPath
}
