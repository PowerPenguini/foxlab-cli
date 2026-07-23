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
	if s.CurrentLab() == nil {
		return -1, lab.Disk{}, false
	}
	for i, disk := range s.CurrentLab().Disks {
		if disk.ID == id {
			return i, disk, true
		}
	}
	return -1, lab.Disk{}, false
}

func (s *Service) workloadDisk(targetType, targetID string) string {
	switch targetType {
	case "vm":
		for _, vm := range s.CurrentLab().VMs {
			if vm.ID == targetID {
				return vm.Disk
			}
		}
	case "container":
		for _, ct := range s.CurrentLab().Containers {
			if ct.ID == targetID {
				return ct.Disk
			}
		}
	}
	return ""
}

func (s *Service) ensureDiskTarget(targetType, targetID string) error {
	resolvedID, ok := s.resolveWorkloadID(targetType, targetID)
	if ok {
		targetID = resolvedID
	}
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
	_, hasVM := s.resolveVMID(id)
	_, hasContainer := s.resolveContainerID(id)
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

func (s *Service) resolveDiskTarget(targetType, targetID string) (string, string, error) {
	resolvedID, ok := s.resolveWorkloadID(targetType, targetID)
	if !ok {
		return "", "", s.ensureDiskTarget(targetType, targetID)
	}
	return targetType, resolvedID, nil
}

func (s *Service) rejectManagedDHCPDiskTarget(targetType, targetID string) error {
	if targetType != "container" {
		return nil
	}
	if _, managed := s.managedDHCPContainer(targetID); managed {
		return fmt.Errorf("DHCP service does not support disks")
	}
	return nil
}

func validDiskTargetRef(ref workload.Ref) bool {
	return (ref.Type == workload.TypeVM || ref.Type == workload.TypeContainer) && strings.TrimSpace(ref.ID) != ""
}

func (s *Service) attachedDiskIndex(targetType, targetID, diskID string) int {
	for i, disk := range s.CurrentLab().Disks {
		if disk.AttachedType != targetType || disk.AttachedTo != targetID {
			continue
		}
		if diskID == "" || disk.ID == diskID || disk.Base == diskID {
			return i
		}
	}
	return -1
}

func (s *Service) requireDiskOffline(disk lab.Disk) error {
	if disk.AttachedType == "" || disk.AttachedTo == "" {
		return nil
	}
	desired := ""
	switch disk.AttachedType {
	case workload.TypeVM:
		for _, vm := range s.CurrentLab().VMs {
			if vm.ID == disk.AttachedTo {
				desired = lab.DesiredState(vm.DesiredState)
				break
			}
		}
	case workload.TypeContainer:
		for _, ct := range s.CurrentLab().Containers {
			if ct.ID == disk.AttachedTo {
				desired = lab.DesiredState(ct.DesiredState)
				break
			}
		}
	}
	if desired != lab.DesiredStateStopped {
		return fmt.Errorf("disk operation needs desired workload state stopped: %s", disk.ID)
	}
	if !s.StatesConfirmed {
		return fmt.Errorf("disk operation needs confirmed stopped runtime state: %s", disk.ID)
	}
	key := workload.Key(workload.Ref{Type: disk.AttachedType, ID: disk.AttachedTo})
	state := strings.ToLower(strings.TrimSpace(s.States[key]))
	if state == "" {
		name := s.nodeDisplayName(disk.AttachedType, disk.AttachedTo)
		key = workload.Key(workload.Ref{Type: disk.AttachedType, ID: name})
		state = strings.ToLower(strings.TrimSpace(s.States[key]))
	}
	safe := false
	switch disk.AttachedType {
	case workload.TypeVM:
		safe = state == "missing" || state == "shutoff" || state == "stopped"
	case workload.TypeContainer:
		safe = state == "missing" || state == "created" || state == "stopped"
	}
	if !safe {
		if state == "" {
			state = "unknown"
		}
		return fmt.Errorf("disk operation needs stopped workload; %s is %s", s.workloadDisplayRef(disk.AttachedType, disk.AttachedTo), state)
	}
	return nil
}
