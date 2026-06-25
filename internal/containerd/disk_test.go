package containerd

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

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

	if err := mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs", false); err != nil {
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

	if err := mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs", false); err == nil {
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

	if err := mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs", false); err == nil {
		t.Fatal("mountContainerDisk returned nil error")
	}
	want := []string{"mount /dev/nbd0 /rootfs"}
	if !reflect.DeepEqual(testCommands(t), want) {
		t.Fatalf("commands = %#v, want %#v", testCommands(t), want)
	}
}

func TestMountContainerDiskForceFormatsUnusableRootFS(t *testing.T) {
	restore := stubRunHostCommand(t, func(name string, args ...string) error {
		return nil
	})
	defer restore()

	if err := mountContainerDisk(context.Background(), "/dev/nbd0", "/rootfs", true); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"mkfs.ext4 -F /dev/nbd0",
		"mount /dev/nbd0 /rootfs",
	}
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
