package topology

import (
	"fmt"
	"strings"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

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

func (s *Service) diskAttachedRunning(disk lab.Disk) bool {
	if disk.AttachedType == "" || disk.AttachedTo == "" {
		return false
	}
	key := workload.Key(workload.Ref{Type: disk.AttachedType, ID: disk.AttachedTo})
	state := strings.ToLower(s.States[key])
	return state == "running"
}
