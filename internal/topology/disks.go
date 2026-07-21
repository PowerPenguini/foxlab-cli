package topology

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"foxlab-cli/internal/lab"
)

const defaultDiskSizeGB = 10

type importedDiskInfo struct {
	VirtualSize int64  `json:"virtual-size"`
	Format      string `json:"format"`
}

func (s *Service) DiskImport(source string) Result {
	if s.Lab == nil {
		return Failure("disk import needs a loaded .lab file")
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk import failed: "+err.Error(), err)
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return Failure("disk import needs a source path")
	}
	if source == "~" || strings.HasPrefix(source, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return FailureWithCause("disk import failed: "+err.Error(), err)
		}
		source = filepath.Join(home, strings.TrimPrefix(source, "~/"))
	}
	absSource, err := filepath.Abs(source)
	if err != nil {
		return FailureWithCause("disk import failed: "+err.Error(), err)
	}
	fileInfo, err := os.Lstat(absSource)
	if err != nil {
		return FailureWithCause("disk import failed: "+err.Error(), err)
	}
	if !fileInfo.Mode().IsRegular() {
		return Failure("disk import needs a regular file: " + absSource)
	}
	out, err := s.diskCommands().Output("qemu-img", "info", "--output=json", absSource)
	if err != nil {
		return FailureWithCause("disk import failed: "+err.Error(), err)
	}
	var info importedDiskInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return FailureWithCause("disk import failed: invalid qemu-img info: "+err.Error(), err)
	}
	format := strings.ToLower(strings.TrimSpace(info.Format))
	if format != "qcow2" && format != "raw" {
		return Failure("disk import failed: unsupported disk format: " + format)
	}
	id, destination, err := s.importedDiskTarget(absSource, format)
	if err != nil {
		return FailureWithCause("disk import failed: "+err.Error(), err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return FailureWithCause("disk import failed: "+err.Error(), err)
	}
	if err := ensureDiskDirectoryWritable(filepath.Dir(destination)); err != nil {
		return FailureWithCause("disk import failed: "+err.Error(), err)
	}
	samePath := filepath.Clean(absSource) == filepath.Clean(destination)
	if !samePath {
		if err := moveDiskFile(absSource, destination); err != nil {
			return FailureWithCause("disk import failed: "+err.Error(), err)
		}
	}
	sizeGB := 0
	if info.VirtualSize > 0 {
		const gib = int64(1024 * 1024 * 1024)
		sizeGB = int((info.VirtualSize + gib - 1) / gib)
	}
	mutation := s.beginLabMutation()
	s.Lab.Disks = append(s.Lab.Disks, lab.Disk{
		ID: id, Path: destination, SizeGB: sizeGB, Format: format, Kind: "base",
	})
	if err := mutation.Commit(); err != nil {
		if !samePath {
			if restoreErr := moveDiskFile(destination, absSource); restoreErr != nil {
				return FailureWithCause("disk import failed: "+err.Error()+"; restore failed: "+restoreErr.Error(), err)
			}
		}
		return FailureWithCause("disk import failed: "+err.Error(), err)
	}
	return Success("imported disk:" + id)
}

func (s *Service) importedDiskTarget(source, format string) (string, string, error) {
	base := importedDiskID(filepath.Base(source))
	for suffix := 1; ; suffix++ {
		id := base
		if suffix > 1 {
			id += "-" + strconv.Itoa(suffix)
		}
		if _, exists := s.diskByID(id); exists {
			continue
		}
		path, err := s.Lab.DiskStoragePath(id, format)
		if err != nil {
			return "", "", err
		}
		if filepath.Clean(source) == filepath.Clean(path) {
			return id, path, nil
		}
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return id, path, nil
		} else if err != nil {
			return "", "", err
		}
	}
}

func importedDiskID(filename string) string {
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	var out strings.Builder
	separator := false
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			if r <= unicode.MaxASCII {
				out.WriteRune(r)
				separator = false
				continue
			}
		}
		if out.Len() > 0 && !separator {
			out.WriteByte('-')
			separator = true
		}
	}
	id := strings.Trim(out.String(), "-_")
	if !lab.ValidID(id) {
		return "disk"
	}
	return id
}

func (s *Service) DiskCreate(id string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("disk create needs a loaded .lab file")
	}
	if id == "" {
		return Failure("usage: disk create <id> [size=N] [format=qcow2|raw]")
	}
	if !lab.ValidID(id) {
		return Failure("invalid disk id: " + id)
	}
	if _, ok := s.diskByID(id); ok {
		return Failure("disk already exists: " + id)
	}
	if invalid := unexpectedDiskCreateArgs(args); len(invalid) > 0 {
		return Failure("unsupported disk create argument: " + invalid[0])
	}
	targetType, targetID, attach, ok := parseDiskCreateTarget(args)
	if !ok {
		return Failure("usage: disk create <id> [size=N] [format=qcow2|raw] [to=vm:<id>|container:<id>]")
	}
	if attach {
		var err error
		targetType, targetID, err = s.resolveDiskTarget(targetType, targetID)
		if err != nil {
			return FailureWithCause(err.Error(), err)
		}
	}
	format := strings.ToLower(firstNonEmpty(args["format"], "qcow2"))
	if format != "qcow2" && format != "raw" {
		return Failure("unsupported disk format: " + format)
	}
	sizeGB, ok := diskSizeGB(args["size"], defaultDiskSizeGB)
	if !ok {
		return Failure("invalid disk size: " + args["size"])
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk create failed: "+err.Error(), err)
	}
	path, err := s.Lab.DiskStoragePath(id, format)
	if err != nil {
		return FailureWithCause("disk create failed: "+err.Error(), err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return FailureWithCause("disk create failed: "+err.Error(), err)
	}
	if err := ensureDiskDirectoryWritable(filepath.Dir(path)); err != nil {
		return FailureWithCause("disk create failed: "+err.Error(), err)
	}
	if err := reserveDiskPath(path); err != nil {
		return FailureWithCause("disk create failed: "+err.Error(), err)
	}
	if err := s.diskCommands().Run("qemu-img", "create", "-f", format, path, strconv.Itoa(sizeGB)+"G"); err != nil {
		_ = os.Remove(path)
		return FailureWithCause("disk create failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
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
	if err := mutation.Commit(); err != nil {
		_ = os.Remove(path)
		return FailureWithCause("disk create failed: "+err.Error(), err)
	}
	if attach {
		return Success("attached disk:" + id + " to " + s.workloadDisplayRef(targetType, targetID))
	}
	return Success("created disk:" + id)
}

func (s *Service) DiskAttach(id string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("disk attach needs a loaded .lab file")
	}
	disk, ok := s.diskByID(id)
	if !ok {
		return Failure("disk not found: " + id)
	}
	if invalid := unexpectedDiskAttachArgs(args); len(invalid) > 0 {
		return Failure("unsupported disk attach argument: " + invalid[0])
	}
	targetType, targetID, ok := parseDiskTarget(firstNonEmpty(args["to"], args["target"]))
	if !ok {
		return Failure("usage: disk attach <id> to=vm:<id>|container:<id>")
	}
	var err error
	targetType, targetID, err = s.resolveDiskTarget(targetType, targetID)
	if err != nil {
		return FailureWithCause(err.Error(), err)
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
		return Failure("container data disk cannot attach to vm: " + id)
	}
	if diskKind(disk) == "layer" {
		return s.activateDiskLayer(id, disk, targetType, targetID)
	}
	return s.activateBaseDisk(disk, targetType, targetID)
}

func (s *Service) DiskLayerCreateAndAttach(baseID, layerID, targetType, targetID string) Result {
	if s.Lab == nil {
		return Failure("disk layer create needs a loaded .lab file")
	}
	base, ok := s.diskByID(baseID)
	if !ok {
		return Failure("disk not found: " + baseID)
	}
	if diskKind(base) != "base" {
		return Failure("disk is not a base: " + baseID)
	}
	layerID = strings.TrimSpace(layerID)
	if !lab.ValidID(layerID) {
		return Failure("invalid disk id: " + layerID)
	}
	if _, exists := s.diskByID(layerID); exists {
		return Failure("disk already exists: " + layerID)
	}
	var err error
	targetType, targetID, err = s.resolveDiskTarget(targetType, targetID)
	if err != nil {
		return FailureWithCause(err.Error(), err)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	layerPath, err := s.layerStoragePath(layerID)
	if err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	if err := os.MkdirAll(filepath.Dir(layerPath), 0o755); err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	if err := ensureDiskDirectoryWritable(filepath.Dir(layerPath)); err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	if err := reserveDiskPath(layerPath); err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	basePath := s.Lab.ResolvePath(base.Path)
	baseFormat := diskFormat(base)
	if err := s.diskCommands().Run("qemu-img", "create", "-f", "qcow2", "-F", baseFormat, "-b", basePath, layerPath); err != nil {
		_ = os.Remove(layerPath)
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
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
	if err := mutation.Commit(); err != nil {
		_ = os.Remove(layerPath)
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	return Success("attached disk:" + layerID + " to " + s.workloadDisplayRef(targetType, targetID))
}

func (s *Service) DiskLayerCreate(baseID, layerID string) Result {
	if s.Lab == nil {
		return Failure("disk layer create needs a loaded .lab file")
	}
	base, ok := s.diskByID(baseID)
	if !ok {
		return Failure("disk not found: " + baseID)
	}
	if diskKind(base) != "base" {
		return Failure("disk is not a base: " + baseID)
	}
	layerID = strings.TrimSpace(layerID)
	if !lab.ValidID(layerID) {
		return Failure("invalid disk id: " + layerID)
	}
	if _, exists := s.diskByID(layerID); exists {
		return Failure("disk already exists: " + layerID)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	layerPath, err := s.layerStoragePath(layerID)
	if err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	if err := os.MkdirAll(filepath.Dir(layerPath), 0o755); err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	if err := ensureDiskDirectoryWritable(filepath.Dir(layerPath)); err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	if err := reserveDiskPath(layerPath); err != nil {
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	basePath := s.Lab.ResolvePath(base.Path)
	baseFormat := diskFormat(base)
	if err := s.diskCommands().Run("qemu-img", "create", "-f", "qcow2", "-F", baseFormat, "-b", basePath, layerPath); err != nil {
		_ = os.Remove(layerPath)
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	layer := lab.Disk{
		ID:        layerID,
		Path:      layerPath,
		Format:    "qcow2",
		Kind:      "layer",
		Base:      baseID,
		MountPath: base.MountPath,
	}
	s.Lab.Disks = append(s.Lab.Disks, layer)
	if err := mutation.Commit(); err != nil {
		_ = os.Remove(layerPath)
		return FailureWithCause("disk layer create failed: "+err.Error(), err)
	}
	return Success("created disk layer:" + layerID)
}

func (s *Service) DiskDetach(target string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("disk detach needs a loaded .lab file")
	}
	if invalid := unexpectedDiskDetachArgs(args); len(invalid) > 0 {
		return Failure("unsupported disk detach argument: " + invalid[0])
	}
	targetType := strings.ToLower(strings.TrimSpace(args["type"]))
	if targetType != "" && targetType != "vm" && targetType != "container" {
		return Failure("disk target must be vm or container")
	}
	targetID := target
	if parsedType, parsedID, ok := parseDiskTarget(firstNonEmpty(args["from"], args["target"], target)); ok {
		targetType, targetID = parsedType, parsedID
	}
	if targetType == "" {
		var err error
		targetType, err = s.inferWorkloadType(targetID)
		if err != nil {
			return FailureWithCause(err.Error(), err)
		}
	}
	var err error
	targetType, targetID, err = s.resolveDiskTarget(targetType, targetID)
	if err != nil {
		return FailureWithCause(err.Error(), err)
	}
	diskIndex := s.attachedDiskIndex(targetType, targetID, args["disk"])
	if diskIndex < 0 {
		return FailureWithCode(ResultCodeDiskNotAttached, "attached disk not found: "+s.workloadDisplayRef(targetType, targetID))
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk detach failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	s.Lab.Disks[diskIndex].AttachedType = ""
	s.Lab.Disks[diskIndex].AttachedTo = ""
	s.setWorkloadDisk(targetType, targetID, "")
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("disk detach failed: "+err.Error(), err)
	}
	return Success("detached disk from " + s.workloadDisplayRef(targetType, targetID))
}

func (s *Service) DiskMerge(id string) Result {
	if s.Lab == nil {
		return Failure("disk merge needs a loaded .lab file")
	}
	index, disk, ok := s.diskIndexByID(id)
	if !ok {
		return Failure("disk not found: " + id)
	}
	if diskKind(disk) != "layer" {
		return Failure("disk is not a layer: " + id)
	}
	if err := s.requireDiskOffline(disk); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	if _, ok := s.diskByID(disk.Base); !ok {
		return Failure("disk base not found: " + disk.Base)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk merge failed: "+err.Error(), err)
	}
	candidate := lab.Clone(s.Lab)
	applyDiskMerge(candidate, index, disk)
	candidate.Normalize()
	if err := candidate.Validate(); err != nil {
		return FailureWithCause("disk merge failed: "+err.Error(), err)
	}
	layerPath := s.Lab.ResolvePath(disk.Path)
	if err := s.diskCommands().Run("qemu-img", "commit", layerPath); err != nil {
		return FailureWithCause("disk merge failed: "+err.Error(), err)
	}
	layerBackup, err := moveFileAside(layerPath)
	if err != nil {
		return FailureWithCause("disk merge failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	applyDiskMerge(s.Lab, index, disk)
	if err := mutation.Commit(); err != nil {
		restoreMovedFile(layerBackup, layerPath)
		return FailureWithCause("disk merge failed: "+err.Error(), err)
	}
	if err := removeMovedFile(layerBackup); err != nil {
		return FailureWithCause("disk merge failed: "+err.Error(), err)
	}
	return Success("merged disk layer:" + id)
}

func (s *Service) DiskResize(id string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("disk resize needs a loaded .lab file")
	}
	index, disk, ok := s.diskIndexByID(id)
	if !ok {
		return Failure("disk not found: " + id)
	}
	if invalid := unexpectedDiskResizeArgs(args); len(invalid) > 0 {
		return Failure("unsupported disk resize argument: " + invalid[0])
	}
	sizeGB, present, ok := positiveIntField(args, "size")
	if !present || !ok {
		return Failure("usage: disk resize <id> size=N [force=true]")
	}
	if err := s.requireDiskOffline(disk); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk resize failed: "+err.Error(), err)
	}
	oldSizeGB := disk.SizeGB
	force := false
	if value, present := args["force"]; present {
		var valid bool
		force, valid = parseBool(value)
		if !valid {
			return Failure("invalid disk resize force: " + value)
		}
	}
	shrinking := oldSizeGB > 0 && sizeGB < oldSizeGB
	if shrinking && !force {
		return Failure("disk shrink is destructive; shrink the guest filesystem first, then rerun with force=true")
	}
	resizeArgs := []string{"resize"}
	if shrinking {
		resizeArgs = append(resizeArgs, "--shrink")
	}
	diskPath := s.Lab.ResolvePath(disk.Path)
	resizeArgs = append(resizeArgs, diskPath, strconv.Itoa(sizeGB)+"G")
	mutation := s.beginLabMutation()
	s.Lab.Disks[index].SizeGB = sizeGB
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("disk resize failed: "+err.Error(), err)
	}
	if err := s.diskCommands().Run("qemu-img", resizeArgs...); err != nil {
		commandErr := err
		mutation.Rollback()
		if rollbackErr := s.SaveAndRefresh(); rollbackErr != nil {
			return FailureWithCause("disk resize failed: "+commandErr.Error()+"; metadata rollback failed: "+rollbackErr.Error(), errors.Join(commandErr, rollbackErr))
		}
		return FailureWithCause("disk resize failed: "+commandErr.Error(), commandErr)
	}
	return Success("resized disk:" + id)
}

type DiskInfo struct {
	Disk     lab.Disk
	Path     string
	QemuInfo string
}

func (s *Service) DiskInfo(id string) (DiskInfo, Result) {
	if s.Lab == nil {
		return DiskInfo{}, FailureWithCode(ResultCodeDiskInfoInvalid, "disk info needs a loaded .lab file")
	}
	disk, ok := s.diskByID(id)
	if !ok {
		return DiskInfo{}, FailureWithCode(ResultCodeDiskInfoInvalid, "disk not found: "+id)
	}
	info := DiskInfo{
		Disk: disk,
		Path: s.Lab.ResolvePath(disk.Path),
	}
	out, err := s.diskCommands().Output("qemu-img", "info", "--output=json", info.Path)
	if err != nil {
		return info, FailureWithCause("disk info failed: "+err.Error(), err)
	}
	info.QemuInfo = strings.TrimSpace(string(out))
	return info, Info("disk info:" + id)
}

func (s *Service) DiskRename(id, newID string) Result {
	if s.Lab == nil {
		return Failure("disk rename needs a loaded .lab file")
	}
	index, disk, ok := s.diskIndexByID(id)
	if !ok {
		return Failure("disk not found: " + id)
	}
	newID = strings.TrimSpace(newID)
	if !lab.ValidID(newID) {
		return Failure("invalid disk id: " + newID)
	}
	if _, exists := s.diskByID(newID); exists {
		return Failure("disk already exists: " + newID)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk rename failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	s.Lab.Disks[index].ID = newID
	if diskKind(disk) == "base" {
		for i := range s.Lab.Disks {
			if s.Lab.Disks[i].Base == id {
				s.Lab.Disks[i].Base = newID
			}
		}
	}
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("disk rename failed: "+err.Error(), err)
	}
	return Success("renamed disk:" + id + " to " + newID)
}

func applyDiskMerge(l *lab.Lab, index int, disk lab.Disk) {
	if disk.AttachedType != "" && disk.AttachedTo != "" {
		setLabWorkloadDisk(l, disk.AttachedType, disk.AttachedTo, "")
	}
	l.Disks = append(l.Disks[:index], l.Disks[index+1:]...)
}

func (s *Service) DiskDelete(id string) Result {
	if s.Lab == nil {
		return Failure("disk delete needs a loaded .lab file")
	}
	index, disk, ok := s.diskIndexByID(id)
	if !ok {
		return Failure("disk not found: " + id)
	}
	if disk.AttachedTo != "" {
		return Failure("disk is attached: " + id)
	}
	if diskKind(disk) == "base" {
		for _, other := range s.Lab.Disks {
			if other.Base == id {
				return Failure("delete disk layers first: " + id)
			}
		}
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk delete failed: "+err.Error(), err)
	}
	diskPath := s.Lab.ResolvePath(disk.Path)
	diskBackup, err := moveFileAside(diskPath)
	if err != nil {
		return FailureWithCause("disk delete failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	s.Lab.Disks = append(s.Lab.Disks[:index], s.Lab.Disks[index+1:]...)
	if err := mutation.Commit(); err != nil {
		restoreMovedFile(diskBackup, diskPath)
		return FailureWithCause("disk delete failed: "+err.Error(), err)
	}
	if err := removeMovedFile(diskBackup); err != nil {
		return FailureWithCause("disk delete failed: "+err.Error(), err)
	}
	return Success("deleted disk:" + id)
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

func (s *Service) DiskLayerDelete(id string) Result {
	if s.Lab == nil {
		return Failure("disk layer delete needs a loaded .lab file")
	}
	_, disk, ok := s.diskIndexByID(id)
	if !ok {
		return Failure("disk not found: " + id)
	}
	if diskKind(disk) != "layer" {
		return Failure("disk is not a layer: " + id)
	}
	return s.DiskDelete(id)
}
