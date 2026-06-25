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
	for _, call := range calls {
		if strings.Contains(call, " -b ") {
			t.Fatalf("vm base attach created backing layer command: %q", call)
		}
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("disk count = %d, want 1", len(reloaded.Disks))
	}
	if reloaded.Disks[0].ID != "data" || reloaded.Disks[0].Kind != "base" || reloaded.Disks[0].AttachedType != "vm" || reloaded.Disks[0].AttachedTo != "vm1" {
		t.Fatalf("attached base = %#v", reloaded.Disks[0])
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

func TestDiskCreateWithVMTargetAttachesBaseDiskWithoutLayer(t *testing.T) {
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
	for _, call := range calls {
		if strings.Contains(call, " -b ") {
			t.Fatalf("vm disk create used backing layer command: %q", call)
		}
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("disk count = %d, want 1", len(reloaded.Disks))
	}
	if reloaded.Disks[0].ID != "vm-disk" || reloaded.Disks[0].Kind != "base" || reloaded.Disks[0].AttachedType != "vm" || reloaded.Disks[0].AttachedTo != "vm1" {
		t.Fatalf("vm base disk = %#v", reloaded.Disks[0])
	}
	if reloaded.VMs[0].Disk == "" || !strings.Contains(reloaded.VMs[0].Disk, "/disks/vm-disk.qcow2") {
		t.Fatalf("vm disk = %q", reloaded.VMs[0].Disk)
	}
}

func TestDiskCreateWithContainerTargetCreatesDataDisk(t *testing.T) {
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
	if reloaded.Disks[0].ID != "web-disk" || reloaded.Disks[0].Kind != "data" || reloaded.Disks[0].Base != "" {
		t.Fatalf("container data disk = %#v", reloaded.Disks[0])
	}
	if reloaded.Disks[0].AttachedType != "container" || reloaded.Disks[0].AttachedTo != "web" {
		t.Fatalf("container data attachment = %#v", reloaded.Disks[0])
	}
	if reloaded.Containers[0].Disk == "" || !strings.Contains(reloaded.Containers[0].Disk, "/disks/web-disk.qcow2") {
		t.Fatalf("container disk = %q", reloaded.Containers[0].Disk)
	}
}

func TestDiskAttachStandaloneBaseToContainerBecomesDataDisk(t *testing.T) {
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
			ID:     "data",
			Path:   diskPath,
			Format: "qcow2",
			Kind:   "base",
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
		t.Fatalf("attach standalone container data disk ran disk commands: %#v", calls)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Disks) != 1 {
		t.Fatalf("disk count = %d, want 1", len(reloaded.Disks))
	}
	disk := reloaded.Disks[0]
	if disk.Kind != "data" || disk.AttachedType != "container" || disk.AttachedTo != "web" || disk.Base != "" {
		t.Fatalf("attached disk = %#v", disk)
	}
	if reloaded.Containers[0].Disk != diskPath {
		t.Fatalf("container disk = %q, want %q", reloaded.Containers[0].Disk, diskPath)
	}
}

func TestDiskAttachBaseWithLayersToContainerIsRejected(t *testing.T) {
	restore := stubDiskCommands(t)
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

	if got := service.DiskAttach("data", map[string]string{"to": "container:web"}); got != "disk has layers; create a container data disk: data" {
		t.Fatalf("DiskAttach = %q", got)
	}
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Disks[0].Kind != "base" || reloaded.Containers[0].Disk != "" {
		t.Fatalf("lab changed after rejected attach: disks=%#v container=%#v", reloaded.Disks, reloaded.Containers[0])
	}
}

func TestDiskAttachLayerArgumentToContainerRejected(t *testing.T) {
	restore := stubDiskCommands(t)
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

	if got := service.DiskAttach("data", map[string]string{"to": "container:web", "layer": "data-layer"}); got != "container disks do not support layers: data" {
		t.Fatalf("DiskAttach = %q", got)
	}
}

func TestDiskAttachCreatesMultipleLayerVariants(t *testing.T) {
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

	if got := service.DiskAttach("data", map[string]string{"to": "vm:vm1", "layer": "data-layer"}); got != "attached disk:data to vm:vm1" {
		t.Fatalf("first DiskAttach = %q", got)
	}
	service = NewService(service.Lab, path)
	if got := service.DiskAttach("data", map[string]string{"to": "vm:vm1", "layer": "data-layer-2"}); got != "attached disk:data to vm:vm1" {
		t.Fatalf("second DiskAttach = %q", got)
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
	if first.AttachedTo != "" || second.AttachedTo != "vm1" {
		t.Fatalf("layer attachment state: first=%#v second=%#v", first, second)
	}
	if !strings.Contains(reloaded.VMs[0].Disk, "/layers/data-layer-2.qcow2") {
		t.Fatalf("active disk path = %q", reloaded.VMs[0].Disk)
	}
}

func TestDiskAttachUsesCustomLayerName(t *testing.T) {
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

	if got := service.DiskAttach("data", map[string]string{"to": "vm:vm1", "layer": "clean-install"}); got != "attached disk:data to vm:vm1" {
		t.Fatalf("DiskAttach = %q", got)
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
			if disk.AttachedTo != "vm1" {
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
