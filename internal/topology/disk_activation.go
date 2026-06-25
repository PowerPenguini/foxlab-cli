package topology

import "foxlab-cli/internal/lab"

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
