package containerd

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	containerdapi "github.com/containerd/containerd"
	cdocontainers "github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/identifiers"
	coci "github.com/containerd/containerd/oci"

	"foxlab-cli/internal/lab"
)

func TestWaitTaskExitTimesOut(t *testing.T) {
	err := waitTaskExit(context.Background(), make(<-chan containerdapi.ExitStatus), time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timed out waiting for container task to exit") {
		t.Fatalf("waitTaskExit error = %v, want timeout", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = waitTaskExit(ctx, make(<-chan containerdapi.ExitStatus), time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitTaskExit canceled error = %v, want context canceled", err)
	}
}

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
	changedID := base
	changedID.ID = "web-renamed"
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
	rootMount := containerDiskMount{Source: "/host/rootfs/merged", Destination: "/"}
	changedRootMount := containerDiskMount{Source: "/host/other-rootfs/merged", Destination: "/"}

	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedImage, containerDiskMount{}) {
		t.Fatal("image change did not change hash")
	}
	if containerConfigHash(base, containerDiskMount{}) == containerConfigHash(changedID, containerDiskMount{}) {
		t.Fatal("id change did not change hash")
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
	if containerConfigHash(base, rootMount) == containerConfigHash(base, changedRootMount) {
		t.Fatal("disk rootfs source change did not change hash")
	}
}

func TestDesiredContainerDiskMountMatchesOverlayRootShape(t *testing.T) {
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
	wantSource := filepath.Join(root, "container-data", "web", "merged")
	if mount.Source != wantSource || mount.Destination != "/" {
		t.Fatalf("desired mount = %#v, want source %q destination /", mount, wantSource)
	}
	preparedShape := containerDiskMount{Source: wantSource, Destination: "/"}
	if containerConfigHash(l.Containers[0], mount) != containerConfigHash(l.Containers[0], preparedShape) {
		t.Fatal("desired disk mount does not match prepared mount hash shape")
	}
	if mount.CleanupDiskOnFailure || mount.CleanupOverlayOnFailure {
		t.Fatalf("desired mount = %#v, want no cleanup marker for side-effect-free descriptor", mount)
	}
}

func TestDesiredContainerNamesAndManagedPrefix(t *testing.T) {
	l := &lab.Lab{
		ID: "default",
		Containers: []lab.Container{
			{ID: "kali"},
			{ID: "other"},
		},
	}

	if got, want := managedContainerPrefix(l), "foxlab-default-"; got != want {
		t.Fatalf("managedContainerPrefix() = %q, want %q", got, want)
	}
	names := desiredContainerNames(l)
	for _, want := range []string{
		"foxlab-default-kali",
		"foxlab-default-other",
	} {
		if !names[want] {
			t.Fatalf("desired names = %#v, missing %q", names, want)
		}
	}
	if names["foxlab-default-ct2"] {
		t.Fatalf("desired names = %#v, want old ct2 to be orphan", names)
	}
}

func TestManagedContainerOwnershipLabelsSeparateOverlappingLabNames(t *testing.T) {
	current := &lab.Lab{ID: "demo"}
	other := &lab.Lab{ID: "demo-extra"}
	otherName := other.ManagedContainerName(lab.Container{ID: "web"})
	if !strings.HasPrefix(otherName, managedContainerPrefix(current)) {
		t.Fatalf("test setup needs overlapping managed prefixes: %q and %q", managedContainerPrefix(current), otherName)
	}
	if managedContainerOwnedByLab(map[string]string{labLabel: other.ID}, current) {
		t.Fatalf("container for lab %q was claimed by lab %q", other.ID, current.ID)
	}
	if !managedContainerOwnedByLab(map[string]string{labLabel: current.ID}, current) {
		t.Fatalf("container ownership label for lab %q was not recognized", current.ID)
	}
	if managedContainerOwnedByLab(nil, current) {
		t.Fatal("unlabelled ambiguous container was claimed by current lab")
	}
}

func TestManagedContainerNamesSatisfyContainerdIdentifierContract(t *testing.T) {
	tests := []struct {
		name string
		lab  string
		id   string
	}{
		{name: "long lab", lab: strings.Repeat("lab", 30), id: "web"},
		{name: "long container", lab: "demo", id: strings.Repeat("container", 12)},
		{name: "repeated separators", lab: "demo__edge", id: "web--api"},
		{name: "trailing separator", lab: "demo-", id: "web_"},
	}
	seen := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := (&lab.Lab{ID: tt.lab}).ManagedContainerName(lab.Container{ID: tt.id})
			if err := identifiers.Validate(name); err != nil {
				t.Fatalf("managed container name %q is invalid: %v", name, err)
			}
			key := tt.lab + "\x00" + tt.id
			if previous, exists := seen[name]; exists && previous != key {
				t.Fatalf("managed container name %q collides for %q and %q", name, previous, key)
			}
			seen[name] = key
		})
	}
}

func TestContainerExecIDSatisfiesContainerdIdentifierContract(t *testing.T) {
	resourceID := strings.Repeat("container--", 12) + "_"
	first := containerExecID("shell", resourceID, time.Unix(1, 2))
	second := containerExecID("shell", resourceID, time.Unix(1, 3))
	for _, id := range []string{first, second} {
		if err := identifiers.Validate(id); err != nil {
			t.Fatalf("container exec id %q is invalid: %v", id, err)
		}
	}
	if first == second {
		t.Fatalf("container exec ids are not unique across timestamps: %q", first)
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

func TestContainerSpecOptsUsesContainerDiskAsRootFS(t *testing.T) {
	diskMount := containerDiskMount{Source: "/host/rootfs/merged", Destination: "/"}
	opts := containerSpecOpts(nil, lab.Container{}, diskMount)
	if len(opts) != 4 {
		t.Fatalf("containerSpecOpts returned %d options, want image config, process args, resolv.conf, and rootfs path", len(opts))
	}
	var spec coci.Spec
	if err := opts[0](context.Background(), nil, &cdocontainers.Container{}, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Root == nil || spec.Root.Path != diskMount.Source {
		t.Fatalf("spec root = %#v, want rootfs path %q", spec.Root, diskMount.Source)
	}
	if spec.Root.Readonly {
		t.Fatalf("spec root = %#v, want writable rootfs", spec.Root)
	}
	if len(spec.Mounts) != 0 {
		t.Fatalf("spec mounts = %#v, want disk rootfs without bind mount", spec.Mounts)
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
