package topology

import "foxlab-cli/internal/lab"

func (s *Service) activateContainerDataDisk(disk lab.Disk, targetID string) Result {
	if disk.AttachedTo != "" && (disk.AttachedType != "container" || disk.AttachedTo != targetID) {
		return Failure("disk is attached: " + disk.ID)
	}
	if diskKind(disk) == "base" && s.diskHasLayers(disk.ID) {
		return Failure("disk has layers; create a separate container disk: " + disk.ID)
	}
	index, _, ok := s.diskIndexByID(disk.ID)
	if !ok {
		return Failure("disk not found: " + disk.ID)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk attach failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	s.detachActiveDisk("container", targetID)
	s.Lab.Disks[index].Kind = "data"
	s.Lab.Disks[index].Base = ""
	s.Lab.Disks[index].AttachedType = "container"
	s.Lab.Disks[index].AttachedTo = targetID
	s.setWorkloadDisk("container", targetID, s.Lab.ResolvePath(disk.Path))
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("disk attach failed: "+err.Error(), err)
	}
	return Success("attached disk:" + disk.ID + " to " + s.workloadDisplayRef("container", targetID))
}

func (s *Service) activateContainerBaseDisk(disk lab.Disk, targetID string) Result {
	return s.activateBaseDisk(disk, "container", targetID)
}

func (s *Service) activateBaseDisk(disk lab.Disk, targetType, targetID string) Result {
	if disk.AttachedTo != "" && (disk.AttachedType != targetType || disk.AttachedTo != targetID) {
		return Failure("disk is attached: " + disk.ID)
	}
	index, _, ok := s.diskIndexByID(disk.ID)
	if !ok {
		return Failure("disk not found: " + disk.ID)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk attach failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	s.detachActiveDisk(targetType, targetID)
	s.Lab.Disks[index].Kind = "base"
	s.Lab.Disks[index].Base = ""
	s.Lab.Disks[index].AttachedType = targetType
	s.Lab.Disks[index].AttachedTo = targetID
	s.setWorkloadDisk(targetType, targetID, s.Lab.ResolvePath(disk.Path))
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("disk attach failed: "+err.Error(), err)
	}
	return Success("attached disk:" + disk.ID + " to " + s.workloadDisplayRef(targetType, targetID))
}

func (s *Service) activateDiskLayer(id string, disk lab.Disk, targetType, targetID string) Result {
	if disk.Base == "" {
		return Failure("disk layer base missing: " + id)
	}
	if _, ok := s.diskByID(disk.Base); !ok {
		return Failure("disk base not found: " + disk.Base)
	}
	if disk.AttachedTo != "" && (disk.AttachedType != targetType || disk.AttachedTo != targetID) {
		return Failure("disk layer is attached: " + id)
	}
	index, _, ok := s.diskIndexByID(id)
	if !ok {
		return Failure("disk not found: " + id)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("disk attach failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	s.detachActiveDisk(targetType, targetID)
	s.Lab.Disks[index].AttachedType = targetType
	s.Lab.Disks[index].AttachedTo = targetID
	s.setWorkloadDisk(targetType, targetID, s.Lab.ResolvePath(disk.Path))
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("disk attach failed: "+err.Error(), err)
	}
	return Success("attached disk:" + id + " to " + s.workloadDisplayRef(targetType, targetID))
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
