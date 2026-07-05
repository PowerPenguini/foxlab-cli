package topology

import (
	"fmt"

	"github.com/google/uuid"
)

func newNodeID() string {
	return uuid.NewString()
}

func (s *Service) nextNodeName(prefix string) string {
	for i := 1; ; i++ {
		name := fmt.Sprintf("%s-%d", prefix, i)
		if s.existingNodeNameKind(name, "") == "" {
			return name
		}
	}
}

func (s *Service) existingNodeNameKind(name, selfID string) string {
	if s.Lab == nil || name == "" {
		return ""
	}
	for _, vm := range s.Lab.VMs {
		if vm.ID != selfID && firstNonEmpty(vm.Name, vm.ID) == name {
			return "vm"
		}
	}
	for _, ct := range s.Lab.Containers {
		if ct.ID != selfID && firstNonEmpty(ct.Name, ct.ID) == name {
			return "container"
		}
	}
	for _, sw := range s.Lab.Switches {
		if sw.ID != selfID && firstNonEmpty(sw.Name, sw.ID) == name {
			return "switch"
		}
	}
	for _, link := range s.Lab.ExternalLinks {
		if link.ID != selfID && firstNonEmpty(link.Name, link.ID) == name {
			return "uplink"
		}
	}
	return ""
}

func (s *Service) validateNodeName(name, selfID string) string {
	if name == "" {
		return "node name is required"
	}
	if kind := s.existingNodeNameKind(name, selfID); kind != "" {
		return "node name already exists as " + kind + ": " + name
	}
	return ""
}

func (s *Service) resolveVMID(ref string) (string, bool) {
	if vm, ok := s.LabVM(ref); ok {
		return vm.ID, true
	}
	if s.Lab == nil {
		return "", false
	}
	for _, vm := range s.Lab.VMs {
		if vm.Name == ref {
			return vm.ID, true
		}
	}
	return "", false
}

func (s *Service) resolveContainerID(ref string) (string, bool) {
	if ct, ok := s.LabContainer(ref); ok {
		return ct.ID, true
	}
	if s.Lab == nil {
		return "", false
	}
	for _, ct := range s.Lab.Containers {
		if ct.Name == ref {
			return ct.ID, true
		}
	}
	return "", false
}

func (s *Service) resolveSwitchID(ref string) (string, bool) {
	if sw, ok := s.LabSwitch(ref); ok {
		return sw.ID, true
	}
	if s.Lab == nil {
		return "", false
	}
	for _, sw := range s.Lab.Switches {
		if sw.Name == ref {
			return sw.ID, true
		}
	}
	return "", false
}

func (s *Service) resolveExternalID(ref string) (string, bool) {
	if link, ok := s.LabExternal(ref); ok {
		return link.ID, true
	}
	if s.Lab == nil {
		return "", false
	}
	for _, link := range s.Lab.ExternalLinks {
		if link.Name == ref {
			return link.ID, true
		}
	}
	return "", false
}

func (s *Service) resolveWorkloadID(typ, ref string) (string, bool) {
	switch typ {
	case "vm":
		return s.resolveVMID(ref)
	case "container":
		return s.resolveContainerID(ref)
	default:
		return "", false
	}
}

func (s *Service) ResolveWorkloadID(typ, ref string) (string, bool) {
	return s.resolveWorkloadID(typ, ref)
}
