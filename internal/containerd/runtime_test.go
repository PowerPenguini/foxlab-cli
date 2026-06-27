package containerd

import (
	"context"
	"reflect"
	"testing"

	cdocontainers "github.com/containerd/containerd/containers"
	coci "github.com/containerd/containerd/oci"

	"foxlab-cli/internal/lab"
)

type nilCheckInterface interface {
	value() string
}

type nilCheckValue struct{}

func (*nilCheckValue) value() string { return "" }

func TestNilInterfaceDetectsTypedNil(t *testing.T) {
	var typedNil *nilCheckValue
	var iface nilCheckInterface = typedNil
	if !nilInterface(iface) {
		t.Fatal("nilInterface returned false for typed nil interface")
	}
	if nilInterface(nilCheckInterface(&nilCheckValue{})) {
		t.Fatal("nilInterface returned true for non-nil interface")
	}
}

func TestContainerConfigHashIncludesShellAndImage(t *testing.T) {
	base := lab.Container{ID: "web", Image: "docker.io/library/bash:latest", Shell: "/usr/local/bin/bash", Command: []string{"/usr/local/bin/bash", "-lc", "sleep infinity"}}
	changedImage := base
	changedImage.Image = "docker.io/kalilinux/kali-rolling:latest"
	changedShell := base
	changedShell.Shell = "/bin/bash"
	changedDisk := base
	changedDisk.Disk = "/tmp/rootfs.qcow2"
	dataMount := containerDiskMount{Source: "/host/data", Destination: "/data"}
	changedDataMount := containerDiskMount{Source: "/host/data", Destination: "/var/lib/foxlab"}

	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedImage, containerDiskMount{}) {
		t.Fatal("image change did not change hash")
	}
	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedShell, containerDiskMount{}) {
		t.Fatal("shell change did not change hash")
	}
	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedDisk, containerDiskMount{}) {
		t.Fatal("disk change did not change hash")
	}
	if containerConfigHash(base, dataMount) == containerConfigHash(base, changedDataMount) {
		t.Fatal("disk mount destination change did not change hash")
	}
}

func TestContainerImageRefAddsDefaultTag(t *testing.T) {
	got, err := containerImageRef("docker.io/kalilinux/kali-rolling")
	if err != nil {
		t.Fatal(err)
	}
	if got != "docker.io/kalilinux/kali-rolling:latest" {
		t.Fatalf("image ref = %q, want docker.io/kalilinux/kali-rolling:latest", got)
	}
}

func TestContainerImageRefRejectsPlaceholder(t *testing.T) {
	for _, image := range []string{"", "?", "empty"} {
		if _, err := containerImageRef(image); err == nil {
			t.Fatalf("containerImageRef(%q) returned nil error", image)
		}
	}
}

func TestContainerProcessArgsDefaultKeepsContainerRunning(t *testing.T) {
	got := containerProcessArgs(lab.Container{})
	want := []string{"/bin/sh", "-lc", "sleep infinity"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containerProcessArgs = %#v, want %#v", got, want)
	}
	explicit := []string{"/usr/bin/bash", "-lc", "sleep infinity"}
	got = containerProcessArgs(lab.Container{Command: explicit})
	if !reflect.DeepEqual(got, explicit) {
		t.Fatalf("containerProcessArgs explicit = %#v, want %#v", got, explicit)
	}
}

func TestContainerSpecOptsMountsDataDiskOverImageRootFS(t *testing.T) {
	diskMount := containerDiskMount{Source: "/host/rootfs", Destination: "/data"}
	opts := containerSpecOpts(nil, lab.Container{}, diskMount)
	if len(opts) != 4 {
		t.Fatalf("containerSpecOpts returned %d options, want image config, process args, resolv.conf, and data mount", len(opts))
	}
	var spec coci.Spec
	if err := opts[len(opts)-1](context.Background(), nil, &cdocontainers.Container{}, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Root != nil {
		t.Fatalf("spec root = %#v, want image snapshot rootfs", spec.Root)
	}
	if len(spec.Mounts) != 1 {
		t.Fatalf("spec mounts = %#v, want one data disk mount", spec.Mounts)
	}
	mount := spec.Mounts[0]
	if mount.Type != "bind" || mount.Source != diskMount.Source || mount.Destination != diskMount.Destination {
		t.Fatalf("spec mount = %#v, want bind %s to %s", mount, diskMount.Source, diskMount.Destination)
	}
	if !reflect.DeepEqual(mount.Options, []string{"rbind", "rw"}) {
		t.Fatalf("spec mount options = %#v", mount.Options)
	}
}

func TestContainerSpecOptsMountsHostResolvconf(t *testing.T) {
	opts := containerSpecOpts(nil, lab.Container{}, containerDiskMount{})
	if len(opts) != 3 {
		t.Fatalf("containerSpecOpts returned %d options, want image config, process args, and resolv.conf", len(opts))
	}
	var spec coci.Spec
	if err := opts[len(opts)-1](context.Background(), nil, &cdocontainers.Container{}, &spec); err != nil {
		t.Fatal(err)
	}
	if len(spec.Mounts) != 1 {
		t.Fatalf("spec mounts = %#v, want one resolv.conf mount", spec.Mounts)
	}
	mount := spec.Mounts[0]
	if mount.Type != "bind" || mount.Source != "/etc/resolv.conf" || mount.Destination != "/etc/resolv.conf" {
		t.Fatalf("spec mount = %#v, want host resolv.conf bind mount", mount)
	}
	if !reflect.DeepEqual(mount.Options, []string{"rbind", "ro"}) {
		t.Fatalf("spec mount options = %#v", mount.Options)
	}
}

func TestContainerDiskDestinationUsesAttachedDiskMountPath(t *testing.T) {
	l := &lab.Lab{
		Containers: []lab.Container{{ID: "web", Disk: "/tmp/web-layer.qcow2"}},
		Disks: []lab.Disk{{
			ID:           "web-layer",
			Path:         "/tmp/web-layer.qcow2",
			AttachedType: "container",
			AttachedTo:   "web",
			MountPath:    "var/lib/web",
		}},
	}
	if got := containerDiskDestination(l, l.Containers[0]); got != "/var/lib/web" {
		t.Fatalf("containerDiskDestination = %q, want /var/lib/web", got)
	}
}

func TestContainerDiskDestinationDefaultsToData(t *testing.T) {
	l := &lab.Lab{
		Containers: []lab.Container{{ID: "web", Disk: "/tmp/web-layer.qcow2"}},
		Disks: []lab.Disk{{
			ID:           "web-layer",
			Path:         "/tmp/web-layer.qcow2",
			AttachedType: "container",
			AttachedTo:   "web",
		}},
	}
	if got := containerDiskDestination(l, l.Containers[0]); got != "/data" {
		t.Fatalf("containerDiskDestination = %q, want /data", got)
	}
}
