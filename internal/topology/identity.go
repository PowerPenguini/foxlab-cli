package topology

import (
	"fmt"
	"strings"

	"foxlab-cli/internal/lab"
)

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
		if vm.ID != selfID && nodeMatchesRef(vm.ID, vm.Name, name) {
			return "vm"
		}
	}
	for _, ct := range s.Lab.Containers {
		if ct.ID != selfID && nodeMatchesRef(ct.ID, ct.Name, name) {
			return "container"
		}
	}
	for _, sw := range s.Lab.Switches {
		if sw.ID != selfID && nodeMatchesRef(sw.ID, sw.Name, name) {
			return "switch"
		}
	}
	for _, link := range s.Lab.ExternalLinks {
		if link.ID != selfID && nodeMatchesRef(link.ID, link.Name, name) {
			return "uplink"
		}
	}
	return ""
}

func (s *Service) validateNodeName(name, selfID string) string {
	if name == "" {
		return "node id is required"
	}
	if !lab.ValidNodeID(name) {
		return "invalid mnemonic node id: " + name
	}
	if kind := s.existingNodeNameKind(name, selfID); kind != "" {
		return "node id already exists as " + kind + ": " + name
	}
	return ""
}

func nodeMatchesRef(id, name, ref string) bool {
	return strings.EqualFold(id, ref) || name != "" && strings.EqualFold(name, ref)
}

func (s *Service) renameNodeID(kind, oldID, newID string) error {
	if s.Lab == nil {
		return fmt.Errorf("missing loaded lab")
	}
	if oldID == newID {
		s.clearRedundantNodeName(kind, oldID)
		return nil
	}
	if problem := s.validateNodeName(newID, oldID); problem != "" {
		return fmt.Errorf("%s", problem)
	}
	found := false
	switch kind {
	case "vm":
		for i := range s.Lab.VMs {
			if s.Lab.VMs[i].ID == oldID {
				s.Lab.VMs[i].ID = newID
				s.Lab.VMs[i].Name = ""
				found = true
				break
			}
		}
	case "container":
		for i := range s.Lab.Containers {
			if s.Lab.Containers[i].ID == oldID {
				s.Lab.Containers[i].ID = newID
				s.Lab.Containers[i].Name = ""
				found = true
				break
			}
		}
	case "switch":
		for i := range s.Lab.Switches {
			if s.Lab.Switches[i].ID == oldID {
				s.Lab.Switches[i].ID = newID
				s.Lab.Switches[i].Name = ""
				found = true
				break
			}
		}
	case "external":
		for i := range s.Lab.ExternalLinks {
			if s.Lab.ExternalLinks[i].ID == oldID {
				s.Lab.ExternalLinks[i].ID = newID
				s.Lab.ExternalLinks[i].Name = ""
				found = true
				break
			}
		}
	default:
		return fmt.Errorf("unsupported node type: %s", kind)
	}
	if !found {
		return fmt.Errorf("%s not found: %s", kind, oldID)
	}
	s.rewriteNodeReferences(kind, oldID, newID)
	return nil
}

func (s *Service) clearRedundantNodeName(kind, id string) {
	switch kind {
	case "vm":
		for i := range s.Lab.VMs {
			if s.Lab.VMs[i].ID == id && s.Lab.VMs[i].Name == id {
				s.Lab.VMs[i].Name = ""
			}
		}
	case "container":
		for i := range s.Lab.Containers {
			if s.Lab.Containers[i].ID == id && s.Lab.Containers[i].Name == id {
				s.Lab.Containers[i].Name = ""
			}
		}
	case "switch":
		for i := range s.Lab.Switches {
			if s.Lab.Switches[i].ID == id && s.Lab.Switches[i].Name == id {
				s.Lab.Switches[i].Name = ""
			}
		}
	case "external":
		for i := range s.Lab.ExternalLinks {
			if s.Lab.ExternalLinks[i].ID == id && s.Lab.ExternalLinks[i].Name == id {
				s.Lab.ExternalLinks[i].Name = ""
			}
		}
	}
}

func (s *Service) rewriteNodeReferences(kind, oldID, newID string) {
	if position, ok := s.Lab.Layout.Nodes[oldID]; ok {
		delete(s.Lab.Layout.Nodes, oldID)
		s.Lab.Layout.Nodes[newID] = position
	}
	for i := range s.Lab.Layout.Links {
		rewriteLayoutEndpoint(&s.Lab.Layout.Links[i].From, kind, oldID, newID)
		rewriteLayoutEndpoint(&s.Lab.Layout.Links[i].To, kind, oldID, newID)
	}
	switch kind {
	case "vm", "container":
		for i := range s.Lab.NetworkLinks {
			rewriteNetworkEndpoint(&s.Lab.NetworkLinks[i].From, kind, oldID, newID)
			rewriteNetworkEndpoint(&s.Lab.NetworkLinks[i].To, kind, oldID, newID)
		}
		for i := range s.Lab.Disks {
			if s.Lab.Disks[i].AttachedType == kind && s.Lab.Disks[i].AttachedTo == oldID {
				s.Lab.Disks[i].AttachedTo = newID
			}
		}
	case "switch":
		for i := range s.Lab.VMs {
			for j := range s.Lab.VMs[i].Networks {
				if s.Lab.VMs[i].Networks[j].Switch == oldID {
					s.Lab.VMs[i].Networks[j].Switch = newID
				}
			}
		}
		for i := range s.Lab.Containers {
			for j := range s.Lab.Containers[i].Networks {
				if s.Lab.Containers[i].Networks[j].Switch == oldID {
					s.Lab.Containers[i].Networks[j].Switch = newID
				}
			}
		}
	case "external":
		for i := range s.Lab.Switches {
			for j := range s.Lab.Switches[i].ExternalLinks {
				if s.Lab.Switches[i].ExternalLinks[j] == oldID {
					s.Lab.Switches[i].ExternalLinks[j] = newID
				}
			}
			if s.Lab.Switches[i].ExternalLink == oldID {
				s.Lab.Switches[i].ExternalLink = newID
			}
		}
		for i := range s.Lab.VMs {
			for j := range s.Lab.VMs[i].Networks {
				if s.Lab.VMs[i].Networks[j].ExternalLink == oldID {
					s.Lab.VMs[i].Networks[j].ExternalLink = newID
				}
			}
		}
		for i := range s.Lab.Containers {
			for j := range s.Lab.Containers[i].Networks {
				if s.Lab.Containers[i].Networks[j].ExternalLink == oldID {
					s.Lab.Containers[i].Networks[j].ExternalLink = newID
				}
			}
		}
	}
}

func rewriteNetworkEndpoint(endpoint *lab.NetworkEndpoint, kind, oldID, newID string) {
	if endpoint.Type == kind && endpoint.ID == oldID {
		endpoint.ID = newID
	}
}

func rewriteLayoutEndpoint(endpoint *lab.LayoutEndpoint, kind, oldID, newID string) {
	layoutKind := kind
	if kind == "external" {
		layoutKind = "external"
	}
	if endpoint.Type == layoutKind && endpoint.ID == oldID {
		endpoint.ID = newID
	}
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
