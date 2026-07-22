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
	if s.CurrentLab() == nil || name == "" {
		return ""
	}
	for _, vm := range s.CurrentLab().VMs {
		if vm.ID != selfID && nodeMatchesRef(vm.ID, vm.Name, name) {
			return "vm"
		}
	}
	for _, ct := range s.CurrentLab().Containers {
		if ct.ID != selfID && nodeMatchesRef(ct.ID, ct.Name, name) {
			return "container"
		}
	}
	for _, sw := range s.CurrentLab().Switches {
		if sw.ID != selfID && nodeMatchesRef(sw.ID, sw.Name, name) {
			return "switch"
		}
	}
	for _, link := range s.CurrentLab().ExternalLinks {
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
	if s.CurrentLab() == nil {
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
		for i := range s.CurrentLab().VMs {
			if s.CurrentLab().VMs[i].ID == oldID {
				s.CurrentLab().VMs[i].ID = newID
				s.CurrentLab().VMs[i].Name = ""
				found = true
				break
			}
		}
	case "container":
		for i := range s.CurrentLab().Containers {
			if s.CurrentLab().Containers[i].ID == oldID {
				s.CurrentLab().Containers[i].ID = newID
				s.CurrentLab().Containers[i].Name = ""
				found = true
				break
			}
		}
	case "switch":
		for i := range s.CurrentLab().Switches {
			if s.CurrentLab().Switches[i].ID == oldID {
				s.CurrentLab().Switches[i].ID = newID
				s.CurrentLab().Switches[i].Name = ""
				found = true
				break
			}
		}
	case "external":
		for i := range s.CurrentLab().ExternalLinks {
			if s.CurrentLab().ExternalLinks[i].ID == oldID {
				s.CurrentLab().ExternalLinks[i].ID = newID
				s.CurrentLab().ExternalLinks[i].Name = ""
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
		for i := range s.CurrentLab().VMs {
			if s.CurrentLab().VMs[i].ID == id && s.CurrentLab().VMs[i].Name == id {
				s.CurrentLab().VMs[i].Name = ""
			}
		}
	case "container":
		for i := range s.CurrentLab().Containers {
			if s.CurrentLab().Containers[i].ID == id && s.CurrentLab().Containers[i].Name == id {
				s.CurrentLab().Containers[i].Name = ""
			}
		}
	case "switch":
		for i := range s.CurrentLab().Switches {
			if s.CurrentLab().Switches[i].ID == id && s.CurrentLab().Switches[i].Name == id {
				s.CurrentLab().Switches[i].Name = ""
			}
		}
	case "external":
		for i := range s.CurrentLab().ExternalLinks {
			if s.CurrentLab().ExternalLinks[i].ID == id && s.CurrentLab().ExternalLinks[i].Name == id {
				s.CurrentLab().ExternalLinks[i].Name = ""
			}
		}
	}
}

func (s *Service) rewriteNodeReferences(kind, oldID, newID string) {
	if position, ok := s.CurrentLab().Layout.Nodes[oldID]; ok {
		delete(s.CurrentLab().Layout.Nodes, oldID)
		s.CurrentLab().Layout.Nodes[newID] = position
	}
	for i := range s.CurrentLab().Layout.Links {
		rewriteLayoutEndpoint(&s.CurrentLab().Layout.Links[i].From, kind, oldID, newID)
		rewriteLayoutEndpoint(&s.CurrentLab().Layout.Links[i].To, kind, oldID, newID)
	}
	switch kind {
	case "vm", "container":
		for i := range s.CurrentLab().NetworkLinks {
			rewriteNetworkEndpoint(&s.CurrentLab().NetworkLinks[i].From, kind, oldID, newID)
			rewriteNetworkEndpoint(&s.CurrentLab().NetworkLinks[i].To, kind, oldID, newID)
		}
		for i := range s.CurrentLab().Disks {
			if s.CurrentLab().Disks[i].AttachedType == kind && s.CurrentLab().Disks[i].AttachedTo == oldID {
				s.CurrentLab().Disks[i].AttachedTo = newID
			}
		}
	case "switch":
		for i := range s.CurrentLab().VMs {
			for j := range s.CurrentLab().VMs[i].Networks {
				if s.CurrentLab().VMs[i].Networks[j].Switch == oldID {
					s.CurrentLab().VMs[i].Networks[j].Switch = newID
				}
			}
		}
		for i := range s.CurrentLab().Containers {
			for j := range s.CurrentLab().Containers[i].Networks {
				if s.CurrentLab().Containers[i].Networks[j].Switch == oldID {
					s.CurrentLab().Containers[i].Networks[j].Switch = newID
				}
			}
		}
	case "external":
		for i := range s.CurrentLab().Switches {
			for j := range s.CurrentLab().Switches[i].ExternalLinks {
				if s.CurrentLab().Switches[i].ExternalLinks[j] == oldID {
					s.CurrentLab().Switches[i].ExternalLinks[j] = newID
				}
			}
			if s.CurrentLab().Switches[i].ExternalLink == oldID {
				s.CurrentLab().Switches[i].ExternalLink = newID
			}
		}
		for i := range s.CurrentLab().VMs {
			for j := range s.CurrentLab().VMs[i].Networks {
				if s.CurrentLab().VMs[i].Networks[j].ExternalLink == oldID {
					s.CurrentLab().VMs[i].Networks[j].ExternalLink = newID
				}
			}
		}
		for i := range s.CurrentLab().Containers {
			for j := range s.CurrentLab().Containers[i].Networks {
				if s.CurrentLab().Containers[i].Networks[j].ExternalLink == oldID {
					s.CurrentLab().Containers[i].Networks[j].ExternalLink = newID
				}
			}
		}
	}
}

func (s *Service) removeLayoutLinksForNode(kind, id string) {
	links := s.CurrentLab().Layout.Links[:0]
	for _, link := range s.CurrentLab().Layout.Links {
		if layoutEndpointMatchesNode(link.From, kind, id) || layoutEndpointMatchesNode(link.To, kind, id) {
			continue
		}
		links = append(links, link)
	}
	s.CurrentLab().Layout.Links = links
}

func layoutEndpointMatchesNode(endpoint lab.LayoutEndpoint, kind, id string) bool {
	return endpoint.Type == kind && endpoint.ID == id
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
	resolved, err := lab.ResolveNode(s.CurrentLab(), ref, lab.NodeKindVM)
	return resolved.ID, err == nil
}

func (s *Service) resolveContainerID(ref string) (string, bool) {
	resolved, err := lab.ResolveNode(s.CurrentLab(), ref, lab.NodeKindContainer)
	return resolved.ID, err == nil
}

func (s *Service) resolveSwitchID(ref string) (string, bool) {
	resolved, err := lab.ResolveNode(s.CurrentLab(), ref, lab.NodeKindSwitch)
	return resolved.ID, err == nil
}

func (s *Service) resolveExternalID(ref string) (string, bool) {
	resolved, err := lab.ResolveNode(s.CurrentLab(), ref, lab.NodeKindExternal)
	return resolved.ID, err == nil
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
