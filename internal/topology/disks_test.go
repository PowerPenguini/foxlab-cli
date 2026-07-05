package topology

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

func TestDiskCreateAttachDetachDelete(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskCreate("data", map[string]string{"size": "2"}); got != "created disk:data" {
		t.Fatalf("DiskCreate = %q", got)
	}
	if got := service.DiskAttach("data", map[string]string{"to": "vm:vm1"}); got != "attached disk:data to vm:vm1" {
		t.Fatalf("DiskAttach = %q", got)
	}
	if len(calls) != 1 {
		t.Fatalf("vm base attach ran disk commands: %#v", calls)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("disk count = %d, want base only", len(reloaded.Disks))
	}
	if reloaded.Disks[0].ID != "data" || reloaded.Disks[0].Kind != "base" || reloaded.Disks[0].AttachedType != "vm" || reloaded.Disks[0].AttachedTo != reloaded.VMs[0].ID {
		t.Fatalf("base disk not attached directly: %#v", reloaded.Disks[0])
	}
	if reloaded.VMs[0].Disk == "" || !strings.Contains(reloaded.VMs[0].Disk, "/disks/data.qcow2") {
		t.Fatalf("vm disk = %q", reloaded.VMs[0].Disk)
	}

	service = NewService(reloaded, path)
	if got := service.DiskDetach("vm1", nil); got != "detached disk from vm:vm1" {
		t.Fatalf("DiskDetach = %q", got)
	}
	reloaded, err = lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Disk != "" {
		t.Fatalf("vm disk after detach = %q", reloaded.VMs[0].Disk)
	}
	service = NewService(reloaded, path)
	if got := service.DiskDelete("data"); got != "deleted disk:data" {
		t.Fatalf("delete base = %q", got)
	}
}

func TestDiskCreateRejectsInvalidIDBeforeSideEffects(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{ID: "demo"}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskCreate("bad/id", map[string]string{}); got != "invalid disk id: bad/id" {
		t.Fatalf("DiskCreate = %q, want invalid id", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk command calls = %#v, want none", calls)
	}
	if len(service.Lab.Disks) != 0 {
		t.Fatalf("invalid disk create mutated lab: %#v", service.Lab.Disks)
	}
}

func TestDiskAttachRejectsLayerArgumentBeforeSideEffects(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:    "demo",
		VMs:   []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}},
		Disks: []lab.Disk{{ID: "base", Path: "disks/base.qcow2", Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskAttach("base", map[string]string{"to": "vm:vm1", "layer": "bad/id"}); got != "unsupported disk attach argument: layer" {
		t.Fatalf("DiskAttach = %q, want unsupported layer arg", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk command calls = %#v, want none", calls)
	}
	if len(service.Lab.Disks) != 1 || service.Lab.Disks[0].ID != "base" {
		t.Fatalf("invalid layer attach mutated disks: %#v", service.Lab.Disks)
	}
	if service.Lab.VMs[0].Disk != "" {
		t.Fatalf("invalid layer attach mutated vm disk: %q", service.Lab.VMs[0].Disk)
	}
}

func TestDiskCommandsRejectUnsupportedArgsBeforeMutating(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1, Disk: "disks/data.qcow2"}},
		Disks: []lab.Disk{{
			ID:           "data",
			Path:         "disks/data.qcow2",
			Format:       "qcow2",
			Kind:         "base",
			AttachedType: "vm",
			AttachedTo:   "vm1",
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskCreate("newdisk", map[string]string{"siz": "20"}); got != "unsupported disk create argument: siz" {
		t.Fatalf("DiskCreate = %q, want unsupported arg", got)
	}
	if got := service.DiskCreate("newdisk", map[string]string{"size": "0"}); got != "invalid disk size: 0" {
		t.Fatalf("DiskCreate size zero = %q, want invalid size", got)
	}
	if got := service.DiskCreate("newdisk", map[string]string{"size": "abc"}); got != "invalid disk size: abc" {
		t.Fatalf("DiskCreate bad size = %q, want invalid size", got)
	}
	if got := service.DiskAttach("data", map[string]string{"to": "vm:vm1", "layr": "data-layer"}); got != "unsupported disk attach argument: layr" {
		t.Fatalf("DiskAttach = %q, want unsupported arg", got)
	}
	if got := service.DiskDetach("vm1", map[string]string{"diskid": "data"}); got != "unsupported disk detach argument: diskid" {
		t.Fatalf("DiskDetach = %q, want unsupported arg", got)
	}
	if got := service.DiskDetach("vm1", map[string]string{"type": "pod"}); got != "disk target must be vm or container" {
		t.Fatalf("DiskDetach invalid type = %q, want disk target validation", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk command calls = %#v, want none", calls)
	}
	if len(service.Lab.Disks) != 1 || service.Lab.Disks[0].ID != "data" || service.Lab.Disks[0].AttachedTo != service.Lab.VMs[0].ID {
		t.Fatalf("unsupported args mutated disks: %#v", service.Lab.Disks)
	}
	if service.Lab.VMs[0].Disk != "disks/data.qcow2" {
		t.Fatalf("unsupported args mutated vm disk: %q", service.Lab.VMs[0].Disk)
	}
}

func TestDiskCreateRequiresSavePathBeforeSideEffects(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	service := NewService(&lab.Lab{ID: "demo"}, "")

	if got := service.DiskCreate("data", map[string]string{}); got != "disk create failed: missing lab path" {
		t.Fatalf("DiskCreate = %q, want missing path", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk command calls = %#v, want none", calls)
	}
	if len(service.Lab.Disks) != 0 {
		t.Fatalf("missing-path disk create mutated lab: %#v", service.Lab.Disks)
	}
}

func TestEnsureDiskDirectoryWritableReportsPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write to read-only directories")
	}
	dir := filepath.Join(t.TempDir(), "disks")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureDiskDirectoryWritable(dir); err != nil {
		t.Fatalf("ensureDiskDirectoryWritable writable dir = %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	err := ensureDiskDirectoryWritable(dir)
	if err == nil || !strings.Contains(err.Error(), "disk storage directory is not writable:") {
		t.Fatalf("ensureDiskDirectoryWritable error = %v, want writable diagnostic", err)
	}
}

func TestDiskCreateRemovesImageAndRestoresLabOnSaveFailure(t *testing.T) {
	restore := stubDiskCommands(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{ID: "demo"}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	diskPath, err := loaded.DiskStoragePath("data", "qcow2")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, t.TempDir())

	if got := service.DiskCreate("data", map[string]string{}); !strings.Contains(got, "disk create failed:") {
		t.Fatalf("DiskCreate = %q, want save failure", got)
	}
	if _, err := os.Stat(diskPath); !os.IsNotExist(err) {
		t.Fatalf("created disk path still exists or stat failed: %v", err)
	}
	if len(service.Lab.Disks) != 0 {
		t.Fatalf("failed disk create mutated lab: %#v", service.Lab.Disks)
	}
}

func TestDiskAttachRestoresLabOnSaveFailure(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:    "demo",
		VMs:   []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}},
		Disks: []lab.Disk{{ID: "base", Path: "disks/base.qcow2", Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, t.TempDir())

	if got := service.DiskAttach("base", map[string]string{"to": "vm:vm1"}); !strings.Contains(got, "disk attach failed:") {
		t.Fatalf("DiskAttach = %q, want save failure", got)
	}
	if len(calls) != 0 {
		t.Fatalf("direct attach ran disk commands: %#v", calls)
	}
	if len(service.Lab.Disks) != 1 || service.Lab.Disks[0].ID != "base" {
		t.Fatalf("failed attach mutated disks: %#v", service.Lab.Disks)
	}
	if service.Lab.VMs[0].Disk != "" {
		t.Fatalf("failed attach mutated vm disk: %q", service.Lab.VMs[0].Disk)
	}
}

func TestDiskCreateWithVMTargetRemovesBaseWhenSaveFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := []string{}
	old := runDiskCommand
	runDiskCommand = func(name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if name != "qemu-img" || len(args) == 0 || args[0] != "create" {
			return nil
		}
		path := args[len(args)-1]
		if strings.HasSuffix(args[len(args)-1], "G") || strings.HasSuffix(args[len(args)-1], "GB") {
			path = args[len(args)-2]
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, nil, 0o644)
	}
	defer func() { runDiskCommand = old }()

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	basePath, err := loaded.DiskStoragePath("vm-disk", "qcow2")
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, t.TempDir())

	if got := service.DiskCreate("vm-disk", map[string]string{"size": "3", "to": "vm:vm1"}); !strings.Contains(got, "disk create failed:") {
		t.Fatalf("DiskCreate attached = %q, want save failure", got)
	}
	if len(calls) != 1 || strings.Contains(calls[0], " -b ") {
		t.Fatalf("qemu-img create calls = %#v, want base create only", calls)
	}
	if _, err := os.Stat(basePath); !os.IsNotExist(err) {
		t.Fatalf("base path still exists or stat failed: %v", err)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 0 {
		t.Fatalf("failed disk create persisted disks: %#v", reloaded.Disks)
	}
	if reloaded.VMs[0].Disk != "" {
		t.Fatalf("failed disk create persisted vm disk: %q", reloaded.VMs[0].Disk)
	}
	if len(service.Lab.Disks) != 0 || service.Lab.VMs[0].Disk != "" {
		t.Fatalf("failed disk create mutated service lab: disks=%#v vmDisk=%q", service.Lab.Disks, service.Lab.VMs[0].Disk)
	}
}

func TestDiskCreateWithVMTargetAttachesBaseDirectly(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskCreate("vm-disk", map[string]string{"size": "3", "to": "vm:vm1"}); got != "attached disk:vm-disk to vm:vm1" {
		t.Fatalf("DiskCreate attached = %q", got)
	}
	if len(calls) != 1 || strings.Contains(calls[0], " -b ") {
		t.Fatalf("vm disk create calls = %#v, want base create only", calls)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("disk count = %d, want base only", len(reloaded.Disks))
	}
	if reloaded.Disks[0].ID != "vm-disk" || reloaded.Disks[0].Kind != "base" || reloaded.Disks[0].AttachedType != "vm" || reloaded.Disks[0].AttachedTo != reloaded.VMs[0].ID {
		t.Fatalf("vm base disk = %#v", reloaded.Disks[0])
	}
	if reloaded.VMs[0].Disk == "" || !strings.Contains(reloaded.VMs[0].Disk, "/disks/vm-disk.qcow2") {
		t.Fatalf("vm disk = %q", reloaded.VMs[0].Disk)
	}
}

func TestDiskCreateWithContainerTargetCreatesBaseRootLayer(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/alpine:latest"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskCreate("web-disk", map[string]string{"size": "3", "to": "container:web"}); got != "attached disk:web-disk to container:web" {
		t.Fatalf("DiskCreate attached = %q", got)
	}
	for _, call := range calls {
		if strings.Contains(call, " -b ") {
			t.Fatalf("container disk create used backing layer command: %q", call)
		}
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("disk count = %d, want 1", len(reloaded.Disks))
	}
	if reloaded.Disks[0].ID != "web-disk" || reloaded.Disks[0].Kind != "base" || reloaded.Disks[0].Base != "" {
		t.Fatalf("container root layer disk = %#v", reloaded.Disks[0])
	}
	if reloaded.Disks[0].AttachedType != "container" || reloaded.Disks[0].AttachedTo != reloaded.Containers[0].ID {
		t.Fatalf("container data attachment = %#v", reloaded.Disks[0])
	}
	if reloaded.Containers[0].Disk == "" || !strings.Contains(reloaded.Containers[0].Disk, "/disks/web-disk.qcow2") {
		t.Fatalf("container disk = %q", reloaded.Containers[0].Disk)
	}
}

func TestDiskAttachStandaloneBaseToContainerUsesBaseAsRootLayer(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	diskPath := filepath.Join(t.TempDir(), "base.qcow2")
	if err := os.WriteFile(diskPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	initial := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/alpine:latest"}},
		Disks: []lab.Disk{{
			ID:        "data",
			Path:      diskPath,
			Format:    "qcow2",
			Kind:      "base",
			MountPath: "/srv/data",
		}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskAttach("data", map[string]string{"to": "container:web"}); got != "attached disk:data to container:web" {
		t.Fatalf("DiskAttach = %q", got)
	}
	if len(calls) != 0 {
		t.Fatalf("attach standalone container base ran disk commands: %#v", calls)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("disk count = %d, want base only", len(reloaded.Disks))
	}
	baseDisk := reloaded.Disks[0]
	if baseDisk.Kind != "base" || baseDisk.AttachedType != "container" || baseDisk.AttachedTo != reloaded.Containers[0].ID || baseDisk.Base != "" {
		t.Fatalf("base disk not attached as container root layer: %#v", baseDisk)
	}
	if reloaded.Containers[0].Disk != diskPath {
		t.Fatalf("container disk = %q, want base path %q", reloaded.Containers[0].Disk, diskPath)
	}
}

func TestDiskAttachBaseWithLayersToContainerUsesBaseDirectly(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	base := filepath.Join(dir, "base.qcow2")
	layer := filepath.Join(dir, "layer.qcow2")
	for _, path := range []string{base, layer} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/alpine:latest"}},
		Disks: []lab.Disk{
			{ID: "data", Path: base, Format: "qcow2", Kind: "base"},
			{ID: "data-layer", Path: layer, Format: "qcow2", Kind: "layer", Base: "data"},
		},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskAttach("data", map[string]string{"to": "container:web"}); got != "attached disk:data to container:web" {
		t.Fatalf("DiskAttach = %q", got)
	}
	if len(calls) != 0 {
		t.Fatalf("base attach with existing layers ran disk commands: %#v", calls)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 2 {
		t.Fatalf("disks = %#v, want base plus existing layer", reloaded.Disks)
	}
	if reloaded.Disks[0].Kind != "base" || reloaded.Disks[0].AttachedType != "container" || reloaded.Disks[0].AttachedTo != reloaded.Containers[0].ID {
		t.Fatalf("base disk not attached: %#v", reloaded.Disks[0])
	}
	if reloaded.Disks[1].Kind != "layer" || reloaded.Disks[1].AttachedTo != "" {
		t.Fatalf("existing layer mutated: %#v", reloaded.Disks[1])
	}
	if reloaded.Containers[0].Disk != base {
		t.Fatalf("container disk = %q, want base path %q", reloaded.Containers[0].Disk, base)
	}
}

func TestDiskAttachLayerArgumentToContainerDoesNotCreateLayer(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	diskPath := filepath.Join(t.TempDir(), "base.qcow2")
	if err := os.WriteFile(diskPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	initial := &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/alpine:latest"}},
		Disks:      []lab.Disk{{ID: "data", Path: diskPath, Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskAttach("data", map[string]string{"to": "container:web", "layer": "data-layer"}); got != "unsupported disk attach argument: layer" {
		t.Fatalf("DiskAttach = %q", got)
	}
	if len(calls) != 0 {
		t.Fatalf("container base attach with layer arg ran disk commands: %#v", calls)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 || reloaded.Disks[0].ID != "data" || reloaded.Disks[0].Kind != "base" || reloaded.Disks[0].AttachedTo != "" {
		t.Fatalf("disks = %#v, want unchanged base disk", reloaded.Disks)
	}
	if reloaded.Containers[0].Disk != "" {
		t.Fatalf("container disk = %q, want unchanged empty disk", reloaded.Containers[0].Disk)
	}
}

func TestDiskLayerCreateAndAttachCreatesMultipleLayerVariants(t *testing.T) {
	restore := stubDiskCommands(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}},
		Disks: []lab.Disk{{
			ID:     "data",
			Path:   filepath.Join(t.TempDir(), "base.qcow2"),
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	if err := os.WriteFile(initial.Disks[0].Path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskLayerCreateAndAttach("data", "data-layer", "vm", "vm1"); got != "attached disk:data-layer to vm:vm1" {
		t.Fatalf("first DiskLayerCreateAndAttach = %q", got)
	}
	service = NewService(service.Lab, path)
	if got := service.DiskLayerCreateAndAttach("data", "data-layer-2", "vm", "vm1"); got != "attached disk:data-layer-2 to vm:vm1" {
		t.Fatalf("second DiskLayerCreateAndAttach = %q", got)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 3 {
		t.Fatalf("disk count = %d, want base plus two layers", len(reloaded.Disks))
	}
	var first, second lab.Disk
	for _, disk := range reloaded.Disks {
		switch disk.ID {
		case "data-layer":
			first = disk
		case "data-layer-2":
			second = disk
		}
	}
	if first.ID == "" || second.ID == "" {
		t.Fatalf("layers missing: %#v", reloaded.Disks)
	}
	if first.AttachedTo != "" || second.AttachedTo != reloaded.VMs[0].ID {
		t.Fatalf("layer attachment state: first=%#v second=%#v", first, second)
	}
	if !strings.Contains(reloaded.VMs[0].Disk, "/layers/data-layer-2.qcow2") {
		t.Fatalf("active disk path = %q", reloaded.VMs[0].Disk)
	}
}

func TestDiskLayerCreateAndAttachUsesCustomLayerName(t *testing.T) {
	restore := stubDiskCommands(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}},
		Disks: []lab.Disk{{
			ID:     "data",
			Path:   filepath.Join(t.TempDir(), "base.qcow2"),
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	if err := os.WriteFile(initial.Disks[0].Path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskLayerCreateAndAttach("data", "clean-install", "vm", "vm1"); got != "attached disk:clean-install to vm:vm1" {
		t.Fatalf("DiskLayerCreateAndAttach = %q", got)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 2 || reloaded.Disks[1].ID != "clean-install" {
		t.Fatalf("disks = %#v, want custom layer", reloaded.Disks)
	}
	if !strings.Contains(reloaded.VMs[0].Disk, "/layers/clean-install.qcow2") {
		t.Fatalf("vm disk = %q", reloaded.VMs[0].Disk)
	}
}

func TestDiskAttachRejectsLayerIDAlias(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1}},
		Disks: []lab.Disk{{
			ID:     "data",
			Path:   filepath.Join(t.TempDir(), "base.qcow2"),
			Format: "qcow2",
			Kind:   "base",
		}},
	}
	if err := os.WriteFile(initial.Disks[0].Path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskAttach("data", map[string]string{"to": "vm:vm1", "layerid": "from-command"}); got != "unsupported disk attach argument: layerid" {
		t.Fatalf("DiskAttach = %q", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk attach layerid alias ran disk commands: %#v", calls)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 || reloaded.Disks[0].ID != "data" || reloaded.Disks[0].AttachedTo != "" {
		t.Fatalf("disks = %#v, want unchanged base disk", reloaded.Disks)
	}
	if reloaded.VMs[0].Disk != "" {
		t.Fatalf("vm disk = %q, want unchanged empty disk", reloaded.VMs[0].Disk)
	}
}

func TestDiskAttachExistingLayerSwitchesActiveLayer(t *testing.T) {
	restore := stubDiskCommands(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	dir := t.TempDir()
	base := filepath.Join(dir, "base.qcow2")
	layer1 := filepath.Join(dir, "layer1.qcow2")
	layer2 := filepath.Join(dir, "layer2.qcow2")
	for _, path := range []string{base, layer1, layer2} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1, Disk: layer1}},
		Disks: []lab.Disk{
			{ID: "data", Path: base, Format: "qcow2", Kind: "base"},
			{ID: "data-layer", Path: layer1, Format: "qcow2", Kind: "layer", Base: "data", AttachedType: "vm", AttachedTo: "vm1"},
			{ID: "data-layer-2", Path: layer2, Format: "qcow2", Kind: "layer", Base: "data"},
		},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskAttach("data-layer-2", map[string]string{"to": "vm:vm1"}); got != "attached disk:data-layer-2 to vm:vm1" {
		t.Fatalf("DiskAttach layer = %q", got)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Disk != layer2 {
		t.Fatalf("active disk = %q, want %q", reloaded.VMs[0].Disk, layer2)
	}
	for _, disk := range reloaded.Disks {
		switch disk.ID {
		case "data-layer":
			if disk.AttachedTo != "" {
				t.Fatalf("old layer still attached: %#v", disk)
			}
		case "data-layer-2":
			if disk.AttachedTo != reloaded.VMs[0].ID {
				t.Fatalf("new layer not attached: %#v", disk)
			}
		}
	}
}

func TestDiskMergeRefusesRunningWorkload(t *testing.T) {
	restore := stubDiskCommands(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	base := filepath.Join(t.TempDir(), "base.qcow2")
	layer := filepath.Join(t.TempDir(), "layer.qcow2")
	if err := os.WriteFile(base, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layer, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1, Disk: layer}},
		Disks: []lab.Disk{
			{ID: "data", Path: base, Format: "qcow2", Kind: "base"},
			{ID: "data-layer", Path: layer, Format: "qcow2", Kind: "layer", Base: "data", AttachedType: "vm", AttachedTo: "vm1"},
		},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	service.States = map[string]string{workload.Key(workload.Ref{Type: workload.TypeVM, ID: "vm1"}): "running"}

	if got := service.DiskMerge("data-layer"); got != "disk merge needs stopped workload: data-layer" {
		t.Fatalf("DiskMerge running = %q", got)
	}
}

func TestDiskMergeRemovesAttachedLayer(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	base := filepath.Join(t.TempDir(), "base.qcow2")
	layer := filepath.Join(t.TempDir(), "layer.qcow2")
	if err := os.WriteFile(base, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layer, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1, Disk: layer}},
		Disks: []lab.Disk{
			{ID: "data", Path: base, Format: "qcow2", Kind: "base"},
			{ID: "data-layer", Path: layer, Format: "qcow2", Kind: "layer", Base: "data", AttachedType: "vm", AttachedTo: "vm1"},
		},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)

	if got := service.DiskMerge("data-layer"); got != "merged disk layer:data-layer" {
		t.Fatalf("DiskMerge = %q", got)
	}
	if _, err := os.Stat(layer); !os.IsNotExist(err) {
		t.Fatalf("layer file still exists or stat failed: %v", err)
	}
	if strings.Join(calls, "\n") != "qemu-img commit "+layer {
		t.Fatalf("calls = %#v", calls)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.VMs[0].Disk != "" {
		t.Fatalf("vm disk after merge = %q, want detached", reloaded.VMs[0].Disk)
	}
	if len(reloaded.Disks) != 1 || reloaded.Disks[0].ID != "data" {
		t.Fatalf("disks after merge = %#v, want only base", reloaded.Disks)
	}
}

func TestDiskMergeValidatesFinalLabBeforeCommit(t *testing.T) {
	calls := []string{}
	restore := stubDiskCommandsWithCalls(t, &calls)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	base := filepath.Join(t.TempDir(), "base.qcow2")
	layer := filepath.Join(t.TempDir(), "layer.qcow2")
	if err := os.WriteFile(base, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layer, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewService(&lab.Lab{
		ID:  "",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1, Disk: layer}},
		Disks: []lab.Disk{
			{ID: "data", Path: base, Format: "qcow2", Kind: "base"},
			{ID: "data-layer", Path: layer, Format: "qcow2", Kind: "layer", Base: "data", AttachedType: "vm", AttachedTo: "vm1"},
		},
	}, path)

	if got := service.DiskMerge("data-layer"); !strings.Contains(got, "disk merge failed:") {
		t.Fatalf("DiskMerge = %q, want validation failure", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk commands = %#v, want none before validation succeeds", calls)
	}
	if _, err := os.Stat(layer); err != nil {
		t.Fatalf("layer file missing after failed preflight: %v", err)
	}
	if len(service.Lab.Disks) != 2 || service.Lab.Disks[1].ID != "data-layer" {
		t.Fatalf("failed merge preflight mutated disks: %#v", service.Lab.Disks)
	}
}

func TestDiskMergeKeepsLayerAndRestoresLabOnSaveFailure(t *testing.T) {
	restore := stubDiskCommands(t)
	defer restore()
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	base := filepath.Join(t.TempDir(), "base.qcow2")
	layer := filepath.Join(t.TempDir(), "layer.qcow2")
	if err := os.WriteFile(base, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layer, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	initial := &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 1024, CPUs: 1, Disk: layer}},
		Disks: []lab.Disk{
			{ID: "data", Path: base, Format: "qcow2", Kind: "base"},
			{ID: "data-layer", Path: layer, Format: "qcow2", Kind: "layer", Base: "data", AttachedType: "vm", AttachedTo: "vm1"},
		},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, t.TempDir())

	if got := service.DiskMerge("data-layer"); !strings.Contains(got, "disk merge failed:") {
		t.Fatalf("DiskMerge = %q, want save failure", got)
	}
	if _, err := os.Stat(layer); err != nil {
		t.Fatalf("layer file missing after failed save: %v", err)
	}
	if len(service.Lab.Disks) != 2 || service.Lab.Disks[1].ID != "data-layer" {
		t.Fatalf("failed merge mutated disks: %#v", service.Lab.Disks)
	}
	if service.Lab.VMs[0].Disk != layer {
		t.Fatalf("failed merge mutated vm disk: %q", service.Lab.VMs[0].Disk)
	}
}

func TestDiskDeleteKeepsFileAndRestoresLabOnSaveFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	diskPath := filepath.Join(t.TempDir(), "data.qcow2")
	if err := os.WriteFile(diskPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	initial := &lab.Lab{
		ID:    "demo",
		Disks: []lab.Disk{{ID: "data", Path: diskPath, Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, t.TempDir())

	if got := service.DiskDelete("data"); !strings.Contains(got, "disk delete failed:") {
		t.Fatalf("DiskDelete = %q, want save failure", got)
	}
	if _, err := os.Stat(diskPath); err != nil {
		t.Fatalf("disk file missing after failed save: %v", err)
	}
	if len(service.Lab.Disks) != 1 || service.Lab.Disks[0].ID != "data" {
		t.Fatalf("failed delete mutated disks: %#v", service.Lab.Disks)
	}
}

func stubDiskCommands(t *testing.T) func() {
	t.Helper()
	return stubDiskCommandsWithCalls(t, nil)
}

func stubDiskCommandsWithCalls(t *testing.T, calls *[]string) func() {
	t.Helper()
	old := runDiskCommand
	runDiskCommand = func(name string, args ...string) error {
		if calls != nil {
			*calls = append(*calls, name+" "+strings.Join(args, " "))
		}
		if name == "qemu-img" && len(args) > 0 && args[0] == "create" {
			path := args[len(args)-1]
			for _, arg := range args {
				if arg == "-b" {
					path = args[len(args)-1]
					break
				}
			}
			if strings.HasSuffix(args[len(args)-1], "G") {
				path = args[len(args)-2]
			}
			if strings.HasSuffix(args[len(args)-1], "GB") {
				path = args[len(args)-2]
			}
			if path == "" {
				path = args[len(args)-1]
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			return os.WriteFile(path, nil, 0o644)
		}
		return nil
	}
	return func() { runDiskCommand = old }
}
