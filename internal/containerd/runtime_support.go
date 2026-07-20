package containerd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	distref "github.com/distribution/reference"
	specs "github.com/opencontainers/runtime-spec/specs-go"

	"foxlab-cli/internal/lab"
)

func (r *Runtime) client(ctx context.Context) (*containerd.Client, context.Context, func(), error) {
	client, err := containerd.New(r.containerdAddress())
	if err != nil {
		return nil, nil, nil, err
	}
	return client, namespaces.WithNamespace(ctx, r.containerdNamespace()), func() { _ = client.Close() }, nil
}

func (r *Runtime) containerdNamespace() string {
	if strings.TrimSpace(r.Namespace) == "" {
		return DefaultNamespace
	}
	return r.Namespace
}

func (r *Runtime) containerdAddress() string {
	if strings.TrimSpace(r.Address) == "" {
		return DefaultAddress
	}
	return r.Address
}

func containerSpecOpts(image containerd.Image, ct lab.Container, diskMount containerDiskMount, resolvconfPath string) []oci.SpecOpts {
	opts := []oci.SpecOpts{}
	if diskMount.Source != "" && diskMount.Destination == "/" {
		opts = append(opts, oci.WithRootFSPath(diskMount.Source))
	}
	opts = append(opts, oci.WithImageConfig(image), oci.WithProcessArgs(containerProcessArgs(ct)...))
	if ct.Capabilities != nil {
		if added := ociCapabilityNames(ct.Capabilities.Add); len(added) > 0 {
			opts = append(opts, oci.WithAddedCapabilities(added))
		}
		if dropped := ociCapabilityNames(ct.Capabilities.Drop); len(dropped) > 0 {
			opts = append(opts, oci.WithDroppedCapabilities(dropped))
		}
	}
	env := []string{}
	for key, value := range ct.Env {
		env = append(env, key+"="+value)
	}
	if len(env) > 0 {
		opts = append(opts, oci.WithEnv(env))
	}
	opts = append(opts, oci.WithMounts([]specs.Mount{{Type: "bind", Source: resolvconfPath, Destination: "/etc/resolv.conf", Options: []string{"rbind", "ro"}}}))
	if diskMount.Source != "" && diskMount.Destination != "/" {
		opts = append(opts, oci.WithMounts([]specs.Mount{{Type: "bind", Source: diskMount.Source, Destination: diskMount.Destination, Options: []string{"rbind", "rw"}}}))
	}
	return opts
}

func containerImageRef(image string) (string, error) {
	image = strings.TrimSpace(image)
	if image == "" || image == "?" || image == "empty" {
		return "", fmt.Errorf("container image is empty")
	}
	named, err := distref.ParseDockerRef(image)
	if err != nil {
		return "", fmt.Errorf("invalid container image %q: %w", image, err)
	}
	return named.String(), nil
}

func containerImage(ctx context.Context, client *containerd.Client, imageRef string) (containerd.Image, error) {
	image, err := client.GetImage(ctx, imageRef)
	if err == nil {
		if unpacked, unpackErr := image.IsUnpacked(ctx, containerd.DefaultSnapshotter); unpackErr != nil || !unpacked {
			if err := image.Unpack(ctx, containerd.DefaultSnapshotter); err != nil {
				return nil, fmt.Errorf("unpack local image %q: %w", imageRef, err)
			}
		}
		return image, nil
	}
	if !errdefs.IsNotFound(err) {
		return nil, fmt.Errorf("load local image %q: %w", imageRef, err)
	}
	image, err = client.Pull(ctx, imageRef, containerd.WithPullUnpack, containerd.WithPullSnapshotter(containerd.DefaultSnapshotter))
	if err != nil {
		return nil, fmt.Errorf("pull image %q: %w", imageRef, err)
	}
	return image, nil
}

func createContainer(ctx context.Context, client *containerd.Client, name string, image containerd.Image, labID string, ct lab.Container, diskMount containerDiskMount, resolvconfPath string) (containerd.Container, error) {
	opts := []containerd.NewContainerOpts{containerd.WithImage(image)}
	if containerUsesManagedSnapshot(diskMount) {
		opts = append(opts, containerd.WithNewSnapshot(name+"-rootfs", image))
	}
	opts = append(opts,
		containerd.WithNewSpec(containerSpecOpts(image, ct, diskMount, resolvconfPath)...),
		containerd.WithContainerLabels(map[string]string{configLabel: containerConfigHash(ct, diskMount), labLabel: labID, containerIDLabel: ct.ID}),
	)
	return client.NewContainer(ctx, name, opts...)
}

func containerUsesManagedSnapshot(diskMount containerDiskMount) bool {
	return diskMount.Source == "" || diskMount.Destination != "/"
}

func containerConfigChanged(ctx context.Context, container containerd.Container, labID string, ct lab.Container, diskMount containerDiskMount) (bool, error) {
	labels, err := container.Labels(ctx)
	if err != nil {
		return false, err
	}
	if owner := labels[labLabel]; owner != "" && owner != labID {
		return false, fmt.Errorf("container %s belongs to lab %q, not %q", container.ID(), owner, labID)
	}
	if resourceID := labels[containerIDLabel]; resourceID != "" && resourceID != ct.ID {
		return false, fmt.Errorf("container %s has workload id %q, not %q", container.ID(), resourceID, ct.ID)
	}
	missingLabels := map[string]string{}
	if labels[labLabel] == "" {
		missingLabels[labLabel] = labID
	}
	if labels[containerIDLabel] == "" {
		missingLabels[containerIDLabel] = ct.ID
	}
	if len(missingLabels) > 0 {
		if _, err := container.SetLabels(ctx, missingLabels); err != nil {
			return false, fmt.Errorf("label managed container %s: %w", container.ID(), err)
		}
	}
	return labels[configLabel] != containerConfigHash(ct, diskMount), nil
}

func deleteContainer(ctx context.Context, container containerd.Container) error {
	task, err := container.Task(ctx, nil)
	if err == nil {
		if err := deleteTask(ctx, task); err != nil {
			return err
		}
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	if err := container.Delete(ctx); err != nil && !errdefs.IsNotFound(err) {
		return err
	}
	return nil
}

func containerConfigHash(ct lab.Container, diskMount containerDiskMount) string {
	parts := []string{"id=" + ct.ID, "image=" + ct.Image, "shell=" + ct.Shell, "disk=" + ct.Disk, "diskSource=" + diskMount.Source, "diskDestination=" + diskMount.Destination, "dns=" + containerDNSMode, "command=" + strings.Join(containerProcessArgs(ct), "\x00")}
	if ct.Capabilities != nil {
		parts = append(parts, "capabilities:add="+strings.Join(sortedCapabilityNames(ct.Capabilities.Add), ","))
		parts = append(parts, "capabilities:drop="+strings.Join(sortedCapabilityNames(ct.Capabilities.Drop), ","))
	}
	for i, nic := range ct.Networks {
		parts = append(parts, fmt.Sprintf("network:%d:switch=%s", i, nic.Switch), fmt.Sprintf("network:%d:external=%s", i, nic.ExternalLink), fmt.Sprintf("network:%d:mac=%s", i, nic.MAC))
	}
	keys := make([]string, 0, len(ct.Env))
	for key := range ct.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		parts = append(parts, "env:"+key+"="+ct.Env[key])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return hex.EncodeToString(sum[:])
}

func ociCapabilityNames(capabilities []string) []string {
	names := sortedCapabilityNames(capabilities)
	for i := range names {
		names[i] = "CAP_" + names[i]
	}
	return names
}

func sortedCapabilityNames(capabilities []string) []string {
	out := make([]string, 0, len(capabilities))
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		capability = lab.NormalizeContainerCapability(capability)
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
}

func desiredContainerDiskMount(l *lab.Lab, ct lab.Container) (containerDiskMount, error) {
	if ct.Disk == "" {
		return containerDiskMount{}, nil
	}
	mountPath, err := containerDiskMountPath(l, ct)
	if err != nil {
		return containerDiskMount{}, err
	}
	return containerDiskMount{Source: filepath.Join(mountPath, "merged"), Destination: "/"}, nil
}

func desiredContainerNames(l *lab.Lab) map[string]bool {
	names := map[string]bool{}
	for name := range desiredContainerIDs(l) {
		names[name] = true
	}
	return names
}

func desiredContainerIDs(l *lab.Lab) map[string]string {
	ids := map[string]string{}
	if l != nil {
		for _, ct := range l.Containers {
			ids[l.ManagedContainerName(ct)] = ct.ID
		}
	}
	return ids
}

func managedContainerPrefix(l *lab.Lab) string {
	if l == nil {
		return ""
	}
	return strings.ToLower(lab.ManagedPrefix + "-" + l.ID + "-")
}

func managedContainerOwnedByLab(labels map[string]string, l *lab.Lab) bool {
	return l != nil && labels[labLabel] != "" && labels[labLabel] == l.ID
}

func containerExecID(kind, resourceID string, now time.Time) string {
	sum := sha256.Sum256([]byte(resourceID))
	return fmt.Sprintf("foxlab-%s-%s-%s", kind, hex.EncodeToString(sum[:8]), now.UTC().Format("20060102T150405.000000000"))
}
