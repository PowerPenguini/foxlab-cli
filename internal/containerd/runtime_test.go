package containerd

import (
	"context"
	"path/filepath"
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
	base := lab.Container{
		ID:      "web",
		Image:   "docker.io/library/bash:latest",
		Shell:   "/usr/local/bin/bash",
		Command: []string{"/usr/local/bin/bash", "-lc", "sleep infinity"},
		Networks: []lab.ContainerNetwork{{
			Switch: "lan",
			MAC:    "02:00:00:00:00:10",
		}},
	}
	changedImage := base
	changedImage.Image = "docker.io/kalilinux/kali-rolling:latest"
	changedShell := base
	changedShell.Shell = "/bin/bash"
	changedDisk := base
	changedDisk.Disk = "/tmp/rootfs.qcow2"
	changedSwitch := cloneContainerForHashTest(base)
	changedSwitch.Networks[0].Switch = "dmz"
	changedExternal := cloneContainerForHashTest(base)
	changedExternal.Networks[0].Switch = ""
	changedExternal.Networks[0].ExternalLink = "uplink"
	changedMAC := cloneContainerForHashTest(base)
	changedMAC.Networks[0].MAC = "02:00:00:00:00:11"
	removedNIC := base
	removedNIC.Networks = nil
	dataMount := containerDiskMount{Source: "/host/data", Destination: "/data"}
	changedDataMount := containerDiskMount{Source: "/host/other-data", Destination: "/data"}

	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedImage, containerDiskMount{}) {
		t.Fatal("image change did not change hash")
	}
	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedShell, containerDiskMount{}) {
		t.Fatal("shell change did not change hash")
	}
	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedDisk, containerDiskMount{}) {
		t.Fatal("disk change did not change hash")
	}
	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedSwitch, containerDiskMount{}) {
		t.Fatal("network switch change did not change hash")
	}
	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedExternal, containerDiskMount{}) {
		t.Fatal("network external link change did not change hash")
	}
	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedMAC, containerDiskMount{}) {
		t.Fatal("network MAC change did not change hash")
	}
	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(removedNIC, containerDiskMount{}) {
		t.Fatal("network removal did not change hash")
	}
	if containerConfigHash(base, dataMount) == containerConfigHash(base, changedDataMount) {
		t.Fatal("disk data source change did not change hash")
	}
}

func TestDesiredContainerDiskMountMatchesPreparedMountShape(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SUDO_USER", "")
	l := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Disk: "layers/web.qcow2"}},
		Disks: []lab.Disk{{
			ID:           "web-layer",
			Path:         "layers/web.qcow2",
			AttachedType: "container",
			AttachedTo:   "web",
			MountPath:    "srv/web",
		}},
	}

	mount, err := desiredContainerDiskMount(l, l.Containers[0])
	if err != nil {
		t.Fatal(err)
	}
	root, err := l.StorageRoot()
	if err != nil {
		t.Fatal(err)
	}
	wantSource := filepath.Join(root, "container-data", "web")
	if mount.Source != wantSource || mount.Destination != "/srv/web" {
		t.Fatalf("desired mount = %#v, want source %q destination /srv/web", mount, wantSource)
	}
	preparedShape := containerDiskMount{Source: wantSource, Destination: "/srv/web"}
	if containerConfigHash(l.Containers[0], mount) != containerConfigHash(l.Containers[0], preparedShape) {
		t.Fatal("desired disk mount does not match prepared mount hash shape")
	}
	if mount.CleanupDiskOnFailure || mount.CleanupOverlayOnFailure {
		t.Fatalf("desired mount = %#v, want no cleanup marker for side-effect-free descriptor", mount)
	}
}

func cloneContainerForHashTest(ct lab.Container) lab.Container {
	ct.Command = append([]string(nil), ct.Command...)
	ct.Networks = append([]lab.ContainerNetwork(nil), ct.Networks...)
	return ct
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

func TestContainerSpecOptsBindMountsContainerDiskAsData(t *testing.T) {
	diskMount := containerDiskMount{Source: "/host/data", Destination: "/data"}
	opts := containerSpecOpts(nil, lab.Container{}, diskMount)
	if len(opts) != 4 {
		t.Fatalf("containerSpecOpts returned %d options, want image config, process args, resolv.conf, and data mount", len(opts))
	}
	var spec coci.Spec
	if err := opts[len(opts)-1](context.Background(), nil, &cdocontainers.Container{}, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Root != nil {
		t.Fatalf("spec root = %#v, want image snapshot rootfs unchanged", spec.Root)
	}
	if len(spec.Mounts) != 1 {
		t.Fatalf("spec mounts = %#v, want one data disk bind mount", spec.Mounts)
	}
	mount := spec.Mounts[0]
	if mount.Type != "bind" || mount.Source != diskMount.Source || mount.Destination != diskMount.Destination {
		t.Fatalf("spec mount = %#v, want data disk bind mount", mount)
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
