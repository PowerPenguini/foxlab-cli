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
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands

	if got := service.CreateDisk(DiskCreateRequest{ID: "data", SizeGB: SetField(2)}).Message; got != "created disk:data" {
		t.Fatalf("DiskCreate = %q", got)
	}
	if got := service.AttachDisk(DiskAttachRequest{DiskID: "data", Target: workload.Ref{Type: workload.TypeVM, ID: "vm1"}}).Message; got != "attached disk:data to vm:vm1" {
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
	if got := service.DetachDisk(DiskDetachRequest{Target: workload.Ref{ID: "vm1"}}).Message; got != "detached disk from vm:vm1" {
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
	if got := service.DiskDelete("data").Message; got != "deleted disk:data" {
		t.Fatalf("delete base = %q", got)
	}
}

func TestDiskImportMovesImageIntoLabStorage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := []string{}
	path := filepath.Join(t.TempDir(), "demo.lab")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "router disk.qcow2")
	if err := os.WriteFile(source, []byte("image-data"), 0o640); err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	service.DiskCommands = stubDiskCommandsWithCalls(t, &calls)

	result := service.DiskImport(source)
	if !result.OK() || result.Message != "imported disk:router-disk" {
		t.Fatalf("DiskImport = %#v", result)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("source still exists after import: %v", err)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("imported disks = %#v", reloaded.Disks)
	}
	disk := reloaded.Disks[0]
	if disk.ID != "router-disk" || disk.Format != "qcow2" || disk.Kind != "base" || disk.SizeGB != 10 {
		t.Fatalf("imported disk metadata = %#v", disk)
	}
	content, err := os.ReadFile(reloaded.ResolvePath(disk.Path))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "image-data" {
		t.Fatalf("imported disk content = %q", content)
	}
	if got := strings.Join(calls, "\n"); !strings.Contains(got, "qemu-img info --output=json "+source) {
		t.Fatalf("disk import calls = %#v", calls)
	}
}

func TestDiskImportRestoresSourceWhenLabSaveFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "data.raw")
	if err := os.WriteFile(source, []byte("raw-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(&lab.Lab{ID: "demo"}, filepath.Join(blocker, "demo.lab"))
	service.DiskCommands = DiskCommandFuncs{OutputFunc: func(string, ...string) ([]byte, error) {
		return []byte(`{"virtual-size":2147483648,"format":"raw"}`), nil
	}}

	result := service.DiskImport(source)
	if result.OK() || !strings.Contains(result.Message, "disk import failed:") {
		t.Fatalf("DiskImport = %#v, want save failure", result)
	}
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("source was not restored: %v", err)
	}
	if string(content) != "raw-data" || len(service.CurrentLab().Disks) != 0 {
		t.Fatalf("failed import state: content=%q disks=%#v", content, service.CurrentLab().Disks)
	}
}

func TestDiskCreateRejectsInvalidIDBeforeSideEffects(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands

	if got := service.CreateDisk(DiskCreateRequest{ID: "bad/id"}).Message; got != "invalid disk id: bad/id" {
		t.Fatalf("DiskCreate = %q, want invalid id", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk command calls = %#v, want none", calls)
	}
	if len(service.CurrentLab().Disks) != 0 {
		t.Fatalf("invalid disk create mutated lab: %#v", service.CurrentLab().Disks)
	}
}

func TestTypedDiskRequestsRejectInvalidValuesBeforeMutating(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands

	if got := service.CreateDisk(DiskCreateRequest{ID: "newdisk", SizeGB: SetField(0)}).Message; got != "invalid disk size: 0" {
		t.Fatalf("DiskCreate size zero = %q, want invalid size", got)
	}
	if got := service.CreateDisk(DiskCreateRequest{ID: "newdisk", Format: DiskFormat("vmdk")}).Message; got != "unsupported disk format: vmdk" {
		t.Fatalf("DiskCreate format = %q, want unsupported format", got)
	}
	if got := service.DetachDisk(DiskDetachRequest{Target: workload.Ref{Type: "pod", ID: "vm1"}}).Message; got != "disk target must be vm or container" {
		t.Fatalf("DiskDetach invalid type = %q, want disk target validation", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk command calls = %#v, want none", calls)
	}
	if len(service.CurrentLab().Disks) != 1 || service.CurrentLab().Disks[0].ID != "data" || service.CurrentLab().Disks[0].AttachedTo != service.CurrentLab().VMs[0].ID {
		t.Fatalf("invalid requests mutated disks: %#v", service.CurrentLab().Disks)
	}
	if service.CurrentLab().VMs[0].Disk != "disks/data.qcow2" {
		t.Fatalf("invalid requests mutated vm disk: %q", service.CurrentLab().VMs[0].Disk)
	}
}

func TestDiskCreateRequiresSavePathBeforeSideEffects(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
	t.Setenv("HOME", t.TempDir())

	service := NewService(&lab.Lab{ID: "demo"}, "")
	service.DiskCommands = diskCommands

	if got := service.CreateDisk(DiskCreateRequest{ID: "data"}).Message; got != "disk create failed: missing lab path" {
		t.Fatalf("DiskCreate = %q, want missing path", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk command calls = %#v, want none", calls)
	}
	if len(service.CurrentLab().Disks) != 0 {
		t.Fatalf("missing-path disk create mutated lab: %#v", service.CurrentLab().Disks)
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
	diskCommands := stubDiskCommands(t)
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
	service.DiskCommands = diskCommands

	if got := service.CreateDisk(DiskCreateRequest{ID: "data"}).Message; !strings.Contains(got, "disk create failed:") {
		t.Fatalf("DiskCreate = %q, want save failure", got)
	}
	if _, err := os.Stat(diskPath); !os.IsNotExist(err) {
		t.Fatalf("created disk path still exists or stat failed: %v", err)
	}
	if len(service.CurrentLab().Disks) != 0 {
		t.Fatalf("failed disk create mutated lab: %#v", service.CurrentLab().Disks)
	}
}

func TestDiskAttachRestoresLabOnSaveFailure(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands

	request := DiskAttachRequest{DiskID: "base", Target: workload.Ref{Type: workload.TypeVM, ID: "vm1"}}
	if got := service.AttachDisk(request).Message; !strings.Contains(got, "disk attach failed:") {
		t.Fatalf("DiskAttach = %q, want save failure", got)
	}
	if len(calls) != 0 {
		t.Fatalf("direct attach ran disk commands: %#v", calls)
	}
	if len(service.CurrentLab().Disks) != 1 || service.CurrentLab().Disks[0].ID != "base" {
		t.Fatalf("failed attach mutated disks: %#v", service.CurrentLab().Disks)
	}
	if service.CurrentLab().VMs[0].Disk != "" {
		t.Fatalf("failed attach mutated vm disk: %q", service.CurrentLab().VMs[0].Disk)
	}
}

func TestDiskCreateWithVMTargetRemovesBaseWhenSaveFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	calls := []string{}
	diskCommands := DiskCommandFuncs{RunFunc: func(name string, args ...string) error {
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
	}}

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
	service.DiskCommands = diskCommands

	request := DiskCreateRequest{
		ID:       "vm-disk",
		SizeGB:   SetField(3),
		AttachTo: SetField(workload.Ref{Type: workload.TypeVM, ID: "vm1"}),
	}
	if got := service.CreateDisk(request).Message; !strings.Contains(got, "disk create failed:") {
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
	if len(service.CurrentLab().Disks) != 0 || service.CurrentLab().VMs[0].Disk != "" {
		t.Fatalf("failed disk create mutated service lab: disks=%#v vmDisk=%q", service.CurrentLab().Disks, service.CurrentLab().VMs[0].Disk)
	}
}

func TestDiskCreateWithVMTargetAttachesBaseDirectly(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands

	request := DiskCreateRequest{
		ID:       "vm-disk",
		SizeGB:   SetField(3),
		AttachTo: SetField(workload.Ref{Type: workload.TypeVM, ID: "vm1"}),
	}
	if got := service.CreateDisk(request).Message; got != "attached disk:vm-disk to vm:vm1" {
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
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands

	request := DiskCreateRequest{
		ID:       "web-disk",
		SizeGB:   SetField(3),
		AttachTo: SetField(workload.Ref{Type: workload.TypeContainer, ID: "web"}),
	}
	if got := service.CreateDisk(request).Message; got != "attached disk:web-disk to container:web" {
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
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands

	request := DiskAttachRequest{DiskID: "data", Target: workload.Ref{Type: workload.TypeContainer, ID: "web"}}
	if got := service.AttachDisk(request).Message; got != "attached disk:data to container:web" {
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
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands

	request := DiskAttachRequest{DiskID: "data", Target: workload.Ref{Type: workload.TypeContainer, ID: "web"}}
	if got := service.AttachDisk(request).Message; got != "attached disk:data to container:web" {
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

func TestDiskLayerCreateAndAttachCreatesMultipleLayerVariants(t *testing.T) {
	diskCommands := stubDiskCommands(t)
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
	service.DiskCommands = diskCommands

	if got := service.DiskLayerCreateAndAttach("data", "data-layer", "vm", "vm1").Message; got != "attached disk:data-layer to vm:vm1" {
		t.Fatalf("first DiskLayerCreateAndAttach = %q", got)
	}
	service = NewService(service.CurrentLab(), path)
	service.DiskCommands = diskCommands
	if got := service.DiskLayerCreateAndAttach("data", "data-layer-2", "vm", "vm1").Message; got != "attached disk:data-layer-2 to vm:vm1" {
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
	diskCommands := stubDiskCommands(t)
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
	service.DiskCommands = diskCommands

	if got := service.DiskLayerCreateAndAttach("data", "clean-install", "vm", "vm1").Message; got != "attached disk:clean-install to vm:vm1" {
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

func TestDiskAttachExistingLayerSwitchesActiveLayer(t *testing.T) {
	diskCommands := stubDiskCommands(t)
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
	service.DiskCommands = diskCommands

	request := DiskAttachRequest{DiskID: "data-layer-2", Target: workload.Ref{Type: workload.TypeVM, ID: "vm1"}}
	if got := service.AttachDisk(request).Message; got != "attached disk:data-layer-2 to vm:vm1" {
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
	diskCommands := stubDiskCommands(t)
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
	service.DiskCommands = diskCommands
	service.States = map[string]string{workload.Key(workload.Ref{Type: workload.TypeVM, ID: "vm1"}): "running"}
	service.StatesConfirmed = true

	if got := service.DiskMerge("data-layer").Message; got != "disk operation needs stopped workload; vm:vm1 is running" {
		t.Fatalf("DiskMerge running = %q", got)
	}
}

func TestDiskMergeRemovesAttachedLayer(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands
	confirmAttachedWorkloadsStopped(service)

	if got := service.DiskMerge("data-layer").Message; got != "merged disk layer:data-layer" {
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
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
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
	service.DiskCommands = diskCommands
	confirmAttachedWorkloadsStopped(service)

	if got := service.DiskMerge("data-layer").Message; !strings.Contains(got, "disk merge failed:") {
		t.Fatalf("DiskMerge = %q, want validation failure", got)
	}
	if len(calls) != 0 {
		t.Fatalf("disk commands = %#v, want none before validation succeeds", calls)
	}
	if _, err := os.Stat(layer); err != nil {
		t.Fatalf("layer file missing after failed preflight: %v", err)
	}
	if len(service.CurrentLab().Disks) != 2 || service.CurrentLab().Disks[1].ID != "data-layer" {
		t.Fatalf("failed merge preflight mutated disks: %#v", service.CurrentLab().Disks)
	}
}

func TestDiskMergeKeepsLayerAndRestoresLabOnSaveFailure(t *testing.T) {
	diskCommands := stubDiskCommands(t)
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
	service.DiskCommands = diskCommands
	confirmAttachedWorkloadsStopped(service)

	if got := service.DiskMerge("data-layer").Message; !strings.Contains(got, "disk merge failed:") {
		t.Fatalf("DiskMerge = %q, want save failure", got)
	}
	if _, err := os.Stat(layer); err != nil {
		t.Fatalf("layer file missing after failed save: %v", err)
	}
	if len(service.CurrentLab().Disks) != 2 || service.CurrentLab().Disks[1].ID != "data-layer" {
		t.Fatalf("failed merge mutated disks: %#v", service.CurrentLab().Disks)
	}
	if service.CurrentLab().VMs[0].Disk != layer {
		t.Fatalf("failed merge mutated vm disk: %q", service.CurrentLab().VMs[0].Disk)
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

	if got := service.DiskDelete("data").Message; !strings.Contains(got, "disk delete failed:") {
		t.Fatalf("DiskDelete = %q, want save failure", got)
	}
	if _, err := os.Stat(diskPath); err != nil {
		t.Fatalf("disk file missing after failed save: %v", err)
	}
	if len(service.CurrentLab().Disks) != 1 || service.CurrentLab().Disks[0].ID != "data" {
		t.Fatalf("failed delete mutated disks: %#v", service.CurrentLab().Disks)
	}
}

func TestDiskResizeGrowsAndShrinks(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)

	path := filepath.Join(t.TempDir(), "demo.lab")
	diskPath := filepath.Join(t.TempDir(), "data.qcow2")
	initial := &lab.Lab{
		ID:    "demo",
		Disks: []lab.Disk{{ID: "data", Path: diskPath, SizeGB: 10, Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	service.DiskCommands = diskCommands

	if got := service.ResizeDisk(DiskResizeRequest{DiskID: "data", SizeGB: 12}).Message; got != "resized disk:data" {
		t.Fatalf("grow DiskResize = %q", got)
	}
	if got := service.ResizeDisk(DiskResizeRequest{DiskID: "data", SizeGB: 8, Force: true}).Message; got != "resized disk:data" {
		t.Fatalf("shrink DiskResize = %q", got)
	}
	want := []string{
		"qemu-img resize " + diskPath + " 12G",
		"qemu-img resize --shrink " + diskPath + " 8G",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("resize calls = %#v, want %#v", calls, want)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Disks[0].SizeGB != 8 {
		t.Fatalf("sizeGB = %d, want 8", reloaded.Disks[0].SizeGB)
	}
}

func TestDiskResizeRequiresForceToShrink(t *testing.T) {
	diskCommands := stubDiskCommands(t)
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{ID: "demo", Disks: []lab.Disk{{ID: "data", Path: "data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"}}}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	service.DiskCommands = diskCommands
	if got := service.ResizeDisk(DiskResizeRequest{DiskID: "data", SizeGB: 8}).Message; !strings.Contains(got, "force=true") {
		t.Fatalf("unforced shrink = %q", got)
	}
}

func TestDiskCreatePreservesExistingManagedPath(t *testing.T) {
	diskCommands := stubDiskCommands(t)
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
	if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(diskPath, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	service.DiskCommands = diskCommands
	if got := service.CreateDisk(DiskCreateRequest{ID: "data"}).Message; !strings.Contains(got, "disk path already exists") {
		t.Fatalf("DiskCreate = %q", got)
	}
	data, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("existing disk data = %q", data)
	}
}

func TestDiskResizeRejectsInvalidAndRunning(t *testing.T) {
	diskCommands := stubDiskCommands(t)

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:    "demo",
		VMs:   []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "data.qcow2"}},
		Disks: []lab.Disk{{ID: "data", Path: "data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base", AttachedType: "vm", AttachedTo: "vm1"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	service.DiskCommands = diskCommands
	service.States = map[string]string{workload.Key(workload.Ref{Type: "vm", ID: "vm1"}): "running"}
	service.StatesConfirmed = true

	if got := service.ResizeDisk(DiskResizeRequest{DiskID: "data"}).Message; got != "usage: disk resize <id> size=N [force=true]" {
		t.Fatalf("missing size = %q", got)
	}
	if got := service.ResizeDisk(DiskResizeRequest{DiskID: "data", SizeGB: 12}).Message; got != "disk operation needs stopped workload; vm:vm1 is running" {
		t.Fatalf("running resize = %q", got)
	}
}

func TestDiskOfflineGuardFailsClosed(t *testing.T) {
	disk := lab.Disk{ID: "data", AttachedType: workload.TypeVM, AttachedTo: "vm1"}
	base := &lab.Lab{
		ID:    "demo",
		VMs:   []lab.VM{{ID: "vm1", DesiredState: lab.DesiredStateStopped, MemoryMB: 512, CPUs: 1}},
		Disks: []lab.Disk{disk},
	}
	tests := []struct {
		name      string
		desired   string
		state     string
		confirmed bool
		wantErr   bool
	}{
		{name: "unconfirmed", state: "shutoff", wantErr: true},
		{name: "missing state", confirmed: true, wantErr: true},
		{name: "paused", state: "paused", confirmed: true, wantErr: true},
		{name: "unknown", state: "unknown", confirmed: true, wantErr: true},
		{name: "desired running", desired: lab.DesiredStateRunning, state: "shutoff", confirmed: true, wantErr: true},
		{name: "confirmed shutoff", state: "shutoff", confirmed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loaded := lab.Clone(base)
			if tt.desired != "" {
				loaded.VMs[0].DesiredState = tt.desired
			}
			service := NewService(loaded, "demo.lab")
			service.StatesConfirmed = tt.confirmed
			service.States = map[string]string{workload.Key(workload.Ref{Type: workload.TypeVM, ID: "vm1"}): tt.state}
			err := service.requireDiskOffline(disk)
			if (err != nil) != tt.wantErr {
				t.Fatalf("requireDiskOffline error = %v, wantErr=%t", err, tt.wantErr)
			}
		})
	}
}

func TestDiskResizeRestoresSizeOnSaveFailure(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)

	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	diskPath := filepath.Join(t.TempDir(), "data.qcow2")
	initial := &lab.Lab{
		ID:    "demo",
		Disks: []lab.Disk{{ID: "data", Path: diskPath, SizeGB: 10, Format: "qcow2", Kind: "base"}},
	}
	service := NewService(lab.Clone(initial), filepath.Join(blocker, "demo.lab"))
	service.DiskCommands = diskCommands

	if got := service.ResizeDisk(DiskResizeRequest{DiskID: "data", SizeGB: 12}).Message; !strings.Contains(got, "disk resize failed:") {
		t.Fatalf("DiskResize = %q, want save failure", got)
	}
	if len(calls) != 0 {
		t.Fatalf("resize commands = %#v, want none before metadata persists", calls)
	}
	if len(service.CurrentLab().Disks) != 1 || service.CurrentLab().Disks[0].SizeGB != 10 {
		t.Fatalf("failed resize mutated disks: %#v", service.CurrentLab().Disks)
	}
}

func TestDiskInfoReturnsMetadataAndQemuInfo(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)

	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID:    "demo",
		Disks: []lab.Disk{{ID: "data", Path: "data.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	service.DiskCommands = diskCommands

	info, result := service.DiskInfo("data")
	if result.Message != "disk info:data" {
		t.Fatalf("DiskInfo message = %q", result.Message)
	}
	if info.Disk.ID != "data" || !strings.HasSuffix(info.Path, "data.qcow2") || !strings.Contains(info.QemuInfo, "virtual-size") {
		t.Fatalf("DiskInfo = %#v", info)
	}
	if got := strings.Join(calls, "\n"); !strings.Contains(got, "qemu-img info --output=json") {
		t.Fatalf("DiskInfo calls = %#v", calls)
	}
}

func TestDiskLayerCreateCreatesUnattachedLayer(t *testing.T) {
	calls := []string{}
	diskCommands := stubDiskCommandsWithCalls(t, &calls)
	t.Setenv("HOME", t.TempDir())

	path := filepath.Join(t.TempDir(), "demo.lab")
	basePath := filepath.Join(t.TempDir(), "base.qcow2")
	initial := &lab.Lab{
		ID:    "demo",
		Disks: []lab.Disk{{ID: "base", Path: basePath, SizeGB: 10, Format: "qcow2", Kind: "base", MountPath: "/data"}},
	}
	if err := lab.SaveFile(path, initial); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(loaded, path)
	service.DiskCommands = diskCommands

	if got := service.DiskLayerCreate("base", "base-layer").Message; got != "created disk layer:base-layer" {
		t.Fatalf("DiskLayerCreate = %q", got)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 2 {
		t.Fatalf("disk count = %d, want base plus layer", len(reloaded.Disks))
	}
	layer := reloaded.Disks[1]
	if layer.ID != "base-layer" || layer.Kind != "layer" || layer.Base != "base" || layer.AttachedTo != "" || layer.MountPath != "/data" {
		t.Fatalf("created layer = %#v", layer)
	}
	if got := strings.Join(calls, "\n"); !strings.Contains(got, "qemu-img create -f qcow2 -F qcow2 -b "+basePath) {
		t.Fatalf("DiskLayerCreate calls = %#v", calls)
	}
}

func TestDiskRenameUpdatesBaseReferencesWithoutMovingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	basePath := filepath.Join(t.TempDir(), "base.qcow2")
	layerPath := filepath.Join(t.TempDir(), "layer.qcow2")
	initial := &lab.Lab{
		ID: "demo",
		Disks: []lab.Disk{
			{ID: "base", Path: basePath, SizeGB: 10, Format: "qcow2", Kind: "base"},
			{ID: "base-layer", Path: layerPath, Format: "qcow2", Kind: "layer", Base: "base"},
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

	if got := service.DiskRename("base", "system").Message; got != "renamed disk:base to system" {
		t.Fatalf("DiskRename = %q", got)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Disks[0].ID != "system" || reloaded.Disks[0].Path != basePath {
		t.Fatalf("renamed base = %#v", reloaded.Disks[0])
	}
	if reloaded.Disks[1].Base != "system" {
		t.Fatalf("layer base = %q, want system", reloaded.Disks[1].Base)
	}
}

func TestDiskRenameRejectsInvalidAndDuplicateID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	initial := &lab.Lab{
		ID: "demo",
		Disks: []lab.Disk{
			{ID: "base", Path: "disks/base.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"},
			{ID: "other", Path: "disks/other.qcow2", SizeGB: 10, Format: "qcow2", Kind: "base"},
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

	if got := service.DiskRename("base", "bad/id").Message; got != "invalid disk id: bad/id" {
		t.Fatalf("invalid rename = %q", got)
	}
	if got := service.DiskRename("base", "other").Message; got != "disk already exists: other" {
		t.Fatalf("duplicate rename = %q", got)
	}
}

func stubDiskCommands(t *testing.T) DiskCommandRunner {
	t.Helper()
	return stubDiskCommandsWithCalls(t, nil)
}

func confirmAttachedWorkloadsStopped(service *Service) {
	service.StatesConfirmed = true
	service.States = map[string]string{}
	for _, disk := range service.CurrentLab().Disks {
		if disk.AttachedType == "" || disk.AttachedTo == "" {
			continue
		}
		state := "stopped"
		if disk.AttachedType == workload.TypeVM {
			state = "shutoff"
		} else if disk.AttachedType == workload.TypeContainer {
			state = "created"
		}
		service.States[workload.Key(workload.Ref{Type: disk.AttachedType, ID: disk.AttachedTo})] = state
	}
}

func stubDiskCommandsWithCalls(t *testing.T, calls *[]string) DiskCommandRunner {
	t.Helper()
	return DiskCommandFuncs{
		RunFunc: func(name string, args ...string) error {
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
		},
		OutputFunc: func(name string, args ...string) ([]byte, error) {
			if calls != nil {
				*calls = append(*calls, name+" "+strings.Join(args, " "))
			}
			return []byte(`{"virtual-size":10737418240,"actual-size":4096,"format":"qcow2"}`), nil
		},
	}
}
