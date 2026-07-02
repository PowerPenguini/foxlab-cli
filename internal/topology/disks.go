package topology

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"foxlab-cli/internal/lab"
)

const defaultDiskSizeGB = 10

func (s *Service) DiskCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "disk create needs a loaded .lab file"
	}
	if id == "" {
		return "usage: disk create <id> [size=N] [format=qcow2|raw]"
	}
	if !lab.ValidID(id) {
		return "invalid disk id: " + id
	}
	if _, ok := s.diskByID(id); ok {
		return "disk already exists: " + id
	}
	if invalid := unexpectedDiskCreateArgs(args); len(invalid) > 0 {
		return "unsupported disk create argument: " + invalid[0]
	}
	targetType, targetID, attach, ok := parseDiskCreateTarget(args)
	if !ok {
		return "usage: disk create <id> [size=N] [format=qcow2|raw] [to=vm:<id>|container:<id>]"
	}
	if attach {
		var err error
		targetType, targetID, err = s.resolveDiskTarget(targetType, targetID)
		if err != nil {
			return err.Error()
		}
	}
	format := strings.ToLower(firstNonEmpty(args["format"], "qcow2"))
	if format != "qcow2" && format != "raw" {
		return "unsupported disk format: " + format
	}
	sizeGB, ok := diskSizeGB(args["size"], defaultDiskSizeGB)
	if !ok {
		return "invalid disk size: " + args["size"]
	}
	if err := s.requireSavePath(); err != nil {
		return "disk create failed: " + err.Error()
	}
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
	snapshot := lab.Clone(s.Lab)
	disk := lab.Disk{
		ID:     id,
		Path:   path,
		SizeGB: sizeGB,
		Format: format,
		Kind:   "base",
	}
	if attach {
		disk.AttachedType = targetType
		disk.AttachedTo = targetID
		s.detachActiveDisk(targetType, targetID)
		s.setWorkloadDisk(targetType, targetID, s.Lab.ResolvePath(path))
	}
	s.Lab.Disks = append(s.Lab.Disks, disk)
	if err := s.SaveAndRefresh(); err != nil {
		s.Lab = snapshot
		_ = os.Remove(path)
		return "disk create failed: " + err.Error()
	}
	if attach {
		return "attached disk:" + id + " to " + targetType + ":" + targetID
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
	if invalid := unexpectedDiskAttachArgs(args); len(invalid) > 0 {
		return "unsupported disk attach argument: " + invalid[0]
	}
	targetType, targetID, ok := parseDiskTarget(firstNonEmpty(args["to"], args["target"]))
	if !ok {
		return "usage: disk attach <id> to=vm:<id>|container:<id>"
	}
	var err error
	targetType, targetID, err = s.resolveDiskTarget(targetType, targetID)
	if err != nil {
		return err.Error()
	}
	if targetType == "container" {
		if diskKind(disk) == "layer" {
			return s.activateDiskLayer(id, disk, targetType, targetID)
		}
		if diskKind(disk) == "data" {
			return s.activateContainerDataDisk(disk, targetID)
		}
		return s.activateContainerBaseDisk(disk, targetID)
	}
	if diskKind(disk) == "data" {
		return "container data disk cannot attach to vm: " + id
	}
	if diskKind(disk) == "layer" {
		return s.activateDiskLayer(id, disk, targetType, targetID)
	}
	return s.activateBaseDisk(disk, targetType, targetID)
}

func (s *Service) DiskLayerCreateAndAttach(baseID, layerID, targetType, targetID string) string {
	if s.Lab == nil {
		return "disk layer create needs a loaded .lab file"
	}
	base, ok := s.diskByID(baseID)
	if !ok {
		return "disk not found: " + baseID
	}
	if diskKind(base) != "base" {
		return "disk is not a base: " + baseID
	}
	layerID = strings.TrimSpace(layerID)
	if !lab.ValidID(layerID) {
		return "invalid disk id: " + layerID
	}
	if _, exists := s.diskByID(layerID); exists {
		return "disk already exists: " + layerID
	}
	var err error
	targetType, targetID, err = s.resolveDiskTarget(targetType, targetID)
	if err != nil {
		return err.Error()
	}
	if err := s.requireSavePath(); err != nil {
		return "disk layer create failed: " + err.Error()
	}
	layerPath, err := s.layerStoragePath(layerID)
	if err != nil {
		return "disk layer create failed: " + err.Error()
	}
	if err := os.MkdirAll(filepath.Dir(layerPath), 0o755); err != nil {
		return "disk layer create failed: " + err.Error()
	}
	basePath := s.Lab.ResolvePath(base.Path)
	baseFormat := diskFormat(base)
	if err := runDiskCommand("qemu-img", "create", "-f", "qcow2", "-F", baseFormat, "-b", basePath, layerPath); err != nil {
		return "disk layer create failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	layer := lab.Disk{
		ID:           layerID,
		Path:         layerPath,
		Format:       "qcow2",
		Kind:         "layer",
		Base:         baseID,
		MountPath:    base.MountPath,
		AttachedType: targetType,
		AttachedTo:   targetID,
	}
	s.detachActiveDisk(targetType, targetID)
	s.Lab.Disks = append(s.Lab.Disks, layer)
	s.setWorkloadDisk(targetType, targetID, layerPath)
	if err := s.SaveAndRefresh(); err != nil {
		s.Lab = snapshot
		_ = os.Remove(layerPath)
		return "disk layer create failed: " + err.Error()
	}
	return "attached disk:" + layerID + " to " + targetType + ":" + targetID
}

func (s *Service) DiskDetach(target string, args map[string]string) string {
	if s.Lab == nil {
		return "disk detach needs a loaded .lab file"
	}
	if invalid := unexpectedDiskDetachArgs(args); len(invalid) > 0 {
		return "unsupported disk detach argument: " + invalid[0]
	}
	targetType := strings.ToLower(strings.TrimSpace(args["type"]))
	if targetType != "" && targetType != "vm" && targetType != "container" {
		return "disk target must be vm or container"
	}
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
	var err error
	targetType, targetID, err = s.resolveDiskTarget(targetType, targetID)
	if err != nil {
		return err.Error()
	}
	diskIndex := s.attachedDiskIndex(targetType, targetID, args["disk"])
	if diskIndex < 0 {
		return "attached disk not found: " + targetType + ":" + targetID
	}
	if err := s.requireSavePath(); err != nil {
		return "disk detach failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	s.Lab.Disks[diskIndex].AttachedType = ""
	s.Lab.Disks[diskIndex].AttachedTo = ""
	s.setWorkloadDisk(targetType, targetID, "")
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
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
	if err := s.requireSavePath(); err != nil {
		return "disk merge failed: " + err.Error()
	}
	candidate := lab.Clone(s.Lab)
	applyDiskMerge(candidate, index, disk)
	candidate.Normalize()
	if err := candidate.Validate(); err != nil {
		return "disk merge failed: " + err.Error()
	}
	layerPath := s.Lab.ResolvePath(disk.Path)
	if err := runDiskCommand("qemu-img", "commit", layerPath); err != nil {
		return "disk merge failed: " + err.Error()
	}
	layerBackup, err := moveFileAside(layerPath)
	if err != nil {
		return "disk merge failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	applyDiskMerge(s.Lab, index, disk)
	if err := s.SaveAndRefresh(); err != nil {
		s.Lab = snapshot
		restoreMovedFile(layerBackup, layerPath)
		return "disk merge failed: " + err.Error()
	}
	if err := removeMovedFile(layerBackup); err != nil {
		return "disk merge failed: " + err.Error()
	}
	return "merged disk layer:" + id
}

func applyDiskMerge(l *lab.Lab, index int, disk lab.Disk) {
	if disk.AttachedType != "" && disk.AttachedTo != "" {
		setLabWorkloadDisk(l, disk.AttachedType, disk.AttachedTo, "")
	}
	l.Disks = append(l.Disks[:index], l.Disks[index+1:]...)
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
	if err := s.requireSavePath(); err != nil {
		return "disk delete failed: " + err.Error()
	}
	diskPath := s.Lab.ResolvePath(disk.Path)
	diskBackup, err := moveFileAside(diskPath)
	if err != nil {
		return "disk delete failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	s.Lab.Disks = append(s.Lab.Disks[:index], s.Lab.Disks[index+1:]...)
	if err := s.SaveAndRefresh(); err != nil {
		s.Lab = snapshot
		restoreMovedFile(diskBackup, diskPath)
		return "disk delete failed: " + err.Error()
	}
	if err := removeMovedFile(diskBackup); err != nil {
		return "disk delete failed: " + err.Error()
	}
	return "deleted disk:" + id
}

func moveFileAside(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".delete-*")
	if err != nil {
		return "", err
	}
	backup := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(backup)
		return "", err
	}
	if err := os.Remove(backup); err != nil {
		return "", err
	}
	if err := os.Rename(path, backup); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return backup, nil
}

func restoreMovedFile(backup, path string) {
	if backup == "" {
		return
	}
	_ = os.Rename(backup, path)
}

func removeMovedFile(backup string) error {
	if backup == "" {
		return nil
	}
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
