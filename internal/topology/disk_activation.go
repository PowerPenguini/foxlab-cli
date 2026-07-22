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
	s.CurrentLab().Disks[index].Kind = "data"
	s.CurrentLab().Disks[index].Base = ""
	s.CurrentLab().Disks[index].AttachedType = "container"
	s.CurrentLab().Disks[index].AttachedTo = targetID
	s.setWorkloadDisk("container", targetID, s.CurrentLab().ResolvePath(disk.Path))
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
	s.CurrentLab().Disks[index].Kind = "base"
	s.CurrentLab().Disks[index].Base = ""
	s.CurrentLab().Disks[index].AttachedType = targetType
	s.CurrentLab().Disks[index].AttachedTo = targetID
	s.setWorkloadDisk(targetType, targetID, s.CurrentLab().ResolvePath(disk.Path))
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
	s.CurrentLab().Disks[index].AttachedType = targetType
	s.CurrentLab().Disks[index].AttachedTo = targetID
	s.setWorkloadDisk(targetType, targetID, s.CurrentLab().ResolvePath(disk.Path))
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("disk attach failed: "+err.Error(), err)
	}
	return Success("attached disk:" + id + " to " + s.workloadDisplayRef(targetType, targetID))
}

func (s *Service) detachActiveDisk(targetType, targetID string) {
	for i := range s.CurrentLab().Disks {
		if s.CurrentLab().Disks[i].AttachedType == targetType && s.CurrentLab().Disks[i].AttachedTo == targetID {
			s.CurrentLab().Disks[i].AttachedType = ""
			s.CurrentLab().Disks[i].AttachedTo = ""
		}
	}
	s.setWorkloadDisk(targetType, targetID, "")
}

func (s *Service) setWorkloadDisk(targetType, targetID, path string) {
	setLabWorkloadDisk(s.CurrentLab(), targetType, targetID, path)
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
	if s.CurrentLab() == nil {
		return
	}
	for i := range s.CurrentLab().Disks {
		if s.CurrentLab().Disks[i].AttachedType == targetType && s.CurrentLab().Disks[i].AttachedTo == targetID {
			s.CurrentLab().Disks[i].AttachedType = ""
			s.CurrentLab().Disks[i].AttachedTo = ""
		}
	}
}
