package topology

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

const defaultDiskSizeGB = 10

var runDiskCommand = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, text)
	}
	return nil
}

func (s *Service) DiskCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "disk create needs a loaded .lab file"
	}
	if id == "" {
		return "usage: disk create <id> [size=N] [format=qcow2|raw]"
	}
	if _, ok := s.diskByID(id); ok {
		return "disk already exists: " + id
	}
	targetType, targetID, attach, ok := parseDiskCreateTarget(args)
	if !ok {
		return "usage: disk create <id> [size=N] [format=qcow2|raw] [to=vm:<id>|container:<id>]"
	}
	if attach {
		if err := s.ensureDiskTarget(targetType, targetID); err != nil {
			return err.Error()
		}
	}
	format := strings.ToLower(firstNonEmpty(args["format"], "qcow2"))
	if format != "qcow2" && format != "raw" {
		return "unsupported disk format: " + format
	}
	sizeGB := diskSizeGB(args["size"], defaultDiskSizeGB)
	path, err := s.Lab.DiskStoragePath(id, format)
	if err != nil {
		return "disk create failed: " + err.Error()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "disk create failed: " + err.Error()
	}
	if err := runDiskCommand("qemu-img", "create", "-f", format, path, strconv.Itoa(sizeGB)+"G"); err != nil {
		return "disk create failed: " + err.Error()
	}
	kind := "base"
	if attach && targetType == "container" {
		kind = "data"
	}
	disk := lab.Disk{
		ID:     id,
		Path:   path,
		SizeGB: sizeGB,
		Format: format,
		Kind:   kind,
	}
	if attach && targetType == "container" {
		disk.AttachedType = targetType
		disk.AttachedTo = targetID
		s.detachActiveDisk(targetType, targetID)
		s.setWorkloadDisk(targetType, targetID, s.Lab.ResolvePath(path))
	}
	s.Lab.Disks = append(s.Lab.Disks, disk)
	if err := s.SaveAndRefresh(); err != nil {
		return "disk create failed: " + err.Error()
	}
	if attach {
		if targetType == "container" {
			return "attached disk:" + id + " to " + targetType + ":" + targetID
		}
		return s.DiskAttach(id, map[string]string{"to": targetType + ":" + targetID})
	}
	return "created disk:" + id
}

func (s *Service) DiskAttach(id string, args map[string]string) string {
	if s.Lab == nil {
		return "disk attach needs a loaded .lab file"
	}
	disk, ok := s.diskByID(id)
	if !ok {
		return "disk not found: " + id
	}
	targetType, targetID, ok := parseDiskTarget(firstNonEmpty(args["to"], args["target"]))
	if !ok {
		return "usage: disk attach <id> to=vm:<id>|container:<id>"
	}
	if err := s.ensureDiskTarget(targetType, targetID); err != nil {
		return err.Error()
	}
	layerID := strings.TrimSpace(firstNonEmpty(args["layer"], args["layerID"], args["name"]))
	if targetType == "container" {
		if layerID != "" {
			return "container disks do not support layers: " + id
		}
		if diskKind(disk) == "layer" {
			return s.activateDiskLayer(id, disk, targetType, targetID)
		}
		return s.activateContainerDataDisk(disk, targetID)
	}
	if diskKind(disk) == "data" {
		return "container data disk cannot attach to vm: " + id
	}
	if diskKind(disk) == "layer" {
		return s.activateDiskLayer(id, disk, targetType, targetID)
	}
	if layerID == "" {
		return s.activateBaseDisk(disk, targetType, targetID)
	}
	if _, exists := s.diskByID(layerID); exists {
		return "disk already exists: " + layerID
	}
	if disk.AttachedTo != "" && (disk.AttachedType != targetType || disk.AttachedTo != targetID) {
		return "disk is attached: " + id
	}
	layerPath, err := s.layerStoragePath(layerID)
	if err != nil {
		return "disk attach failed: " + err.Error()
	}
	if err := os.MkdirAll(filepath.Dir(layerPath), 0o755); err != nil {
		return "disk attach failed: " + err.Error()
	}
	basePath := s.Lab.ResolvePath(disk.Path)
	baseFormat := diskFormat(disk)
	if err := runDiskCommand("qemu-img", "create", "-f", "qcow2", "-F", baseFormat, "-b", basePath, layerPath); err != nil {
		return "disk attach failed: " + err.Error()
	}
	layer := lab.Disk{
		ID:           layerID,
		Path:         layerPath,
		Format:       "qcow2",
		Kind:         "layer",
		Base:         id,
		AttachedType: targetType,
		AttachedTo:   targetID,
	}
	s.detachActiveDisk(targetType, targetID)
	s.Lab.Disks = append(s.Lab.Disks, layer)
	s.setWorkloadDisk(targetType, targetID, layerPath)
	if err := s.SaveAndRefresh(); err != nil {
		return "disk attach failed: " + err.Error()
	}
	return "attached disk:" + id + " to " + targetType + ":" + targetID
}

func (s *Service) DiskDetach(target string, args map[string]string) string {
	if s.Lab == nil {
		return "disk detach needs a loaded .lab file"
	}
	targetType := strings.ToLower(args["type"])
	targetID := target
	if parsedType, parsedID, ok := parseDiskTarget(firstNonEmpty(args["from"], args["target"], target)); ok {
		targetType, targetID = parsedType, parsedID
	}
	if targetType == "" {
		var err error
		targetType, err = s.inferWorkloadType(targetID)
		if err != nil {
			return err.Error()
		}
	}
	diskIndex := s.attachedDiskIndex(targetType, targetID, args["disk"])
	if diskIndex < 0 {
		return "attached disk not found: " + targetType + ":" + targetID
	}
	s.Lab.Disks[diskIndex].AttachedType = ""
	s.Lab.Disks[diskIndex].AttachedTo = ""
	s.setWorkloadDisk(targetType, targetID, "")
	if err := s.SaveAndRefresh(); err != nil {
		return "disk detach failed: " + err.Error()
	}
	return "detached disk from " + targetType + ":" + targetID
}

func (s *Service) DiskMerge(id string) string {
	if s.Lab == nil {
		return "disk merge needs a loaded .lab file"
	}
	index, disk, ok := s.diskIndexByID(id)
	if !ok {
		return "disk not found: " + id
	}
	if diskKind(disk) != "layer" {
		return "disk is not a layer: " + id
	}
	if s.diskAttachedRunning(disk) {
		return "disk merge needs stopped workload: " + id
	}
	if _, ok := s.diskByID(disk.Base); !ok {
		return "disk base not found: " + disk.Base
	}
	layerPath := s.Lab.ResolvePath(disk.Path)
	if err := runDiskCommand("qemu-img", "commit", layerPath); err != nil {
		return "disk merge failed: " + err.Error()
	}
	if err := os.Remove(layerPath); err != nil && !os.IsNotExist(err) {
		return "disk merge failed: " + err.Error()
	}
	if disk.AttachedType != "" && disk.AttachedTo != "" {
		s.setWorkloadDisk(disk.AttachedType, disk.AttachedTo, "")
	}
	s.Lab.Disks = append(s.Lab.Disks[:index], s.Lab.Disks[index+1:]...)
	if err := s.SaveAndRefresh(); err != nil {
		return "disk merge failed: " + err.Error()
	}
	return "merged disk layer:" + id
}

func (s *Service) DiskDelete(id string) string {
	if s.Lab == nil {
		return "disk delete needs a loaded .lab file"
	}
	index, disk, ok := s.diskIndexByID(id)
	if !ok {
		return "disk not found: " + id
	}
	if disk.AttachedTo != "" {
		return "disk is attached: " + id
	}
	if diskKind(disk) == "base" {
		for _, other := range s.Lab.Disks {
			if other.Base == id {
				return "delete disk layers first: " + id
			}
		}
	}
	if err := os.Remove(s.Lab.ResolvePath(disk.Path)); err != nil && !os.IsNotExist(err) {
		return "disk delete failed: " + err.Error()
	}
	s.Lab.Disks = append(s.Lab.Disks[:index], s.Lab.Disks[index+1:]...)
	if err := s.SaveAndRefresh(); err != nil {
		return "disk delete failed: " + err.Error()
	}
	return "deleted disk:" + id
}

func (s *Service) DiskLayerDelete(id string) string {
	if s.Lab == nil {
		return "disk layer delete needs a loaded .lab file"
	}
	_, disk, ok := s.diskIndexByID(id)
	if !ok {
		return "disk not found: " + id
	}
	if diskKind(disk) != "layer" {
		return "disk is not a layer: " + id
	}
	return s.DiskDelete(id)
}

func (s *Service) diskByID(id string) (lab.Disk, bool) {
	_, disk, ok := s.diskIndexByID(id)
	return disk, ok
}

func (s *Service) diskIndexByID(id string) (int, lab.Disk, bool) {
	if s.Lab == nil {
		return -1, lab.Disk{}, false
	}
	for i, disk := range s.Lab.Disks {
		if disk.ID == id {
			return i, disk, true
		}
	}
	return -1, lab.Disk{}, false
}

func (s *Service) activateBaseDisk(disk lab.Disk, targetType, targetID string) string {
	if disk.AttachedTo != "" && (disk.AttachedType != targetType || disk.AttachedTo != targetID) {
		return "disk is attached: " + disk.ID
	}
	index, _, ok := s.diskIndexByID(disk.ID)
	if !ok {
		return "disk not found: " + disk.ID
	}
	s.detachActiveDisk(targetType, targetID)
	s.Lab.Disks[index].AttachedType = targetType
	s.Lab.Disks[index].AttachedTo = targetID
	s.setWorkloadDisk(targetType, targetID, s.Lab.ResolvePath(disk.Path))
	if err := s.SaveAndRefresh(); err != nil {
		return "disk attach failed: " + err.Error()
	}
	return "attached disk:" + disk.ID + " to " + targetType + ":" + targetID
}

func (s *Service) activateContainerDataDisk(disk lab.Disk, targetID string) string {
	if disk.AttachedTo != "" && (disk.AttachedType != "container" || disk.AttachedTo != targetID) {
		return "disk is attached: " + disk.ID
	}
	if diskKind(disk) == "base" && s.diskHasLayers(disk.ID) {
		return "disk has layers; create a container data disk: " + disk.ID
	}
	index, _, ok := s.diskIndexByID(disk.ID)
	if !ok {
		return "disk not found: " + disk.ID
	}
	s.detachActiveDisk("container", targetID)
	s.Lab.Disks[index].Kind = "data"
	s.Lab.Disks[index].Base = ""
	s.Lab.Disks[index].AttachedType = "container"
	s.Lab.Disks[index].AttachedTo = targetID
	s.setWorkloadDisk("container", targetID, s.Lab.ResolvePath(disk.Path))
	if err := s.SaveAndRefresh(); err != nil {
		return "disk attach failed: " + err.Error()
	}
	return "attached disk:" + disk.ID + " to container:" + targetID
}

func (s *Service) activateDiskLayer(id string, disk lab.Disk, targetType, targetID string) string {
	if disk.Base == "" {
		return "disk layer base missing: " + id
	}
	if _, ok := s.diskByID(disk.Base); !ok {
		return "disk base not found: " + disk.Base
	}
	if disk.AttachedTo != "" && (disk.AttachedType != targetType || disk.AttachedTo != targetID) {
		return "disk layer is attached: " + id
	}
	index, _, ok := s.diskIndexByID(id)
	if !ok {
		return "disk not found: " + id
	}
	s.detachActiveDisk(targetType, targetID)
	s.Lab.Disks[index].AttachedType = targetType
	s.Lab.Disks[index].AttachedTo = targetID
	s.setWorkloadDisk(targetType, targetID, s.Lab.ResolvePath(disk.Path))
	if err := s.SaveAndRefresh(); err != nil {
		return "disk attach failed: " + err.Error()
	}
	return "attached disk:" + id + " to " + targetType + ":" + targetID
}

func (s *Service) nextLayerID(baseID string) string {
	base := baseID + "-layer"
	if _, exists := s.diskByID(base); !exists {
		return base
	}
	for i := 2; ; i++ {
		id := base + "-" + strconv.Itoa(i)
		if _, exists := s.diskByID(id); !exists {
			return id
		}
	}
}

func (s *Service) diskHasLayers(baseID string) bool {
	for _, disk := range s.Lab.Disks {
		if disk.Base == baseID && diskKind(disk) == "layer" {
			return true
		}
	}
	return false
}

func (s *Service) layerStoragePath(layerID string) (string, error) {
	root, err := s.Lab.StorageRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "layers", layerID+".qcow2"), nil
}

func (s *Service) detachActiveDisk(targetType, targetID string) {
	for i := range s.Lab.Disks {
		if s.Lab.Disks[i].AttachedType == targetType && s.Lab.Disks[i].AttachedTo == targetID {
			s.Lab.Disks[i].AttachedType = ""
			s.Lab.Disks[i].AttachedTo = ""
		}
	}
	s.setWorkloadDisk(targetType, targetID, "")
}

func (s *Service) setWorkloadDisk(targetType, targetID, path string) {
	switch targetType {
	case "vm":
		for i := range s.Lab.VMs {
			if s.Lab.VMs[i].ID == targetID {
				s.Lab.VMs[i].Disk = path
				return
			}
		}
	case "container":
		for i := range s.Lab.Containers {
			if s.Lab.Containers[i].ID == targetID {
				s.Lab.Containers[i].Disk = path
				return
			}
		}
	}
}

func (s *Service) workloadDisk(targetType, targetID string) string {
	switch targetType {
	case "vm":
		for _, vm := range s.Lab.VMs {
			if vm.ID == targetID {
				return vm.Disk
			}
		}
	case "container":
		for _, ct := range s.Lab.Containers {
			if ct.ID == targetID {
				return ct.Disk
			}
		}
	}
	return ""
}

func (s *Service) ensureDiskTarget(targetType, targetID string) error {
	switch targetType {
	case "vm":
		if s.HasLabVM(targetID) {
			return nil
		}
		return fmt.Errorf("vm not found: %s", targetID)
	case "container":
		if s.HasLabContainer(targetID) {
			return nil
		}
		return fmt.Errorf("container not found: %s", targetID)
	default:
		return fmt.Errorf("disk target must be vm or container")
	}
}

func (s *Service) inferWorkloadType(id string) (string, error) {
	hasVM := s.HasLabVM(id)
	hasContainer := s.HasLabContainer(id)
	switch {
	case hasVM && hasContainer:
		return "", fmt.Errorf("workload id is ambiguous; pass type=vm or type=container")
	case hasVM:
		return "vm", nil
	case hasContainer:
		return "container", nil
	default:
		return "", fmt.Errorf("workload not found: %s", id)
	}
}

func (s *Service) attachedDiskIndex(targetType, targetID, diskID string) int {
	for i, disk := range s.Lab.Disks {
		if disk.AttachedType != targetType || disk.AttachedTo != targetID {
			continue
		}
		if diskID == "" || disk.ID == diskID || disk.Base == diskID {
			return i
		}
	}
	return -1
}

func (s *Service) detachDisksForNode(targetType, targetID string) {
	if s.Lab == nil {
		return
	}
	for i := range s.Lab.Disks {
		if s.Lab.Disks[i].AttachedType == targetType && s.Lab.Disks[i].AttachedTo == targetID {
			s.Lab.Disks[i].AttachedType = ""
			s.Lab.Disks[i].AttachedTo = ""
		}
	}
}

func (s *Service) diskAttachedRunning(disk lab.Disk) bool {
	if disk.AttachedType == "" || disk.AttachedTo == "" {
		return false
	}
	key := workload.Key(workload.Ref{Type: disk.AttachedType, ID: disk.AttachedTo})
	state := strings.ToLower(s.States[key])
	return state == "running"
}

func parseDiskTarget(value string) (string, string, bool) {
	typ, id, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || id == "" {
		return "", "", false
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	switch typ {
	case "vm", "container":
		return typ, strings.TrimSpace(id), true
	case "ct":
		return "container", strings.TrimSpace(id), true
	default:
		return "", "", false
	}
}

func parseDiskCreateTarget(args map[string]string) (string, string, bool, bool) {
	target := firstNonEmpty(args["to"], args["target"], args["attach"])
	if target == "" {
		return "", "", false, true
	}
	targetType, targetID, ok := parseDiskTarget(target)
	return targetType, targetID, true, ok
}

func diskKind(disk lab.Disk) string {
	if disk.Kind == "" {
		return "base"
	}
	return disk.Kind
}

func diskFormat(disk lab.Disk) string {
	if disk.Format != "" {
		return disk.Format
	}
	ext := strings.ToLower(filepath.Ext(disk.Path))
	if ext == ".img" || ext == ".raw" {
		return "raw"
	}
	return "qcow2"
}

func diskSizeGB(value string, fallback int) int {
	value = strings.TrimSpace(strings.ToUpper(value))
	value = strings.TrimSuffix(value, "GB")
	value = strings.TrimSuffix(value, "G")
	if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
}
