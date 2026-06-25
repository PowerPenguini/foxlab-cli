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
