package topology

import "foxlab-cli/internal/lab"

func (s *Service) activateContainerDataDisk(disk lab.Disk, targetID string) string {
	if disk.AttachedTo != "" && (disk.AttachedType != "container" || disk.AttachedTo != targetID) {
		return "disk is attached: " + disk.ID
	}
	if diskKind(disk) == "base" && s.diskHasLayers(disk.ID) {
		return "disk has layers; create a separate container disk: " + disk.ID
	}
	index, _, ok := s.diskIndexByID(disk.ID)
	if !ok {
		return "disk not found: " + disk.ID
	}
	if err := s.requireSavePath(); err != nil {
		return "disk attach failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	s.detachActiveDisk("container", targetID)
	s.Lab.Disks[index].Kind = "data"
	s.Lab.Disks[index].Base = ""
	s.Lab.Disks[index].AttachedType = "container"
	s.Lab.Disks[index].AttachedTo = targetID
	s.setWorkloadDisk("container", targetID, s.Lab.ResolvePath(disk.Path))
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "disk attach failed: " + err.Error()
	}
	return "attached disk:" + disk.ID + " to container:" + targetID
}

func (s *Service) activateContainerBaseDisk(disk lab.Disk, targetID string) string {
	return s.activateBaseDisk(disk, "container", targetID)
}

func (s *Service) activateBaseDisk(disk lab.Disk, targetType, targetID string) string {
	if disk.AttachedTo != "" && (disk.AttachedType != targetType || disk.AttachedTo != targetID) {
		return "disk is attached: " + disk.ID
	}
	index, _, ok := s.diskIndexByID(disk.ID)
	if !ok {
		return "disk not found: " + disk.ID
	}
	if err := s.requireSavePath(); err != nil {
		return "disk attach failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	s.detachActiveDisk(targetType, targetID)
	s.Lab.Disks[index].Kind = "base"
	s.Lab.Disks[index].Base = ""
	s.Lab.Disks[index].AttachedType = targetType
	s.Lab.Disks[index].AttachedTo = targetID
	s.setWorkloadDisk(targetType, targetID, s.Lab.ResolvePath(disk.Path))
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "disk attach failed: " + err.Error()
	}
	return "attached disk:" + disk.ID + " to " + targetType + ":" + targetID
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
	if err := s.requireSavePath(); err != nil {
		return "disk attach failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	s.detachActiveDisk(targetType, targetID)
	s.Lab.Disks[index].AttachedType = targetType
	s.Lab.Disks[index].AttachedTo = targetID
	s.setWorkloadDisk(targetType, targetID, s.Lab.ResolvePath(disk.Path))
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "disk attach failed: " + err.Error()
	}
	return "attached disk:" + id + " to " + targetType + ":" + targetID
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
	setLabWorkloadDisk(s.Lab, targetType, targetID, path)
}

func setLabWorkloadDisk(l *lab.Lab, targetType, targetID, path string) {
	if l == nil {
		return
	}
	switch targetType {
	case "vm":
		for i := range l.VMs {
			if l.VMs[i].ID == targetID {
				l.VMs[i].Disk = path
				return
			}
		}
	case "container":
		for i := range l.Containers {
			if l.Containers[i].ID == targetID {
				l.Containers[i].Disk = path
				return
			}
		}
	}
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
