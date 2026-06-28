package topology

import "foxlab-cli/internal/lab"

func (s *Service) ExternalCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "external create needs a loaded .lab file"
	}
	if id == "" {
		return "usage: external create <id> interface=IFACE [mode=nat|direct|macnat]"
	}
	if !lab.ValidID(id) {
		return "invalid external id: " + id
	}
	if s.HasLabExternal(id) {
		return "external already exists: " + id
	}
	if kind := s.existingNodeKind(id); kind != "" {
		return "node id already exists as " + kind + ": " + id
	}
	if invalid := unexpectedExternalArgs(args); len(invalid) > 0 {
		return "unsupported external create argument: " + invalid[0]
	}
	if firstNonEmpty(args["interface"]) == "" {
		return "usage: external create <id> interface=IFACE [mode=nat|direct|macnat]"
	}
	mode := firstNonEmpty(args["mode"], lab.ExternalModeNAT)
	if err := validateExternalConfig(id, mode); err != nil {
		return "external create failed: " + err.Error()
	}
	if err := s.requireSavePath(); err != nil {
		return "external create failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	s.Lab.ExternalLinks = append(s.Lab.ExternalLinks, lab.ExternalLink{
		ID:        id,
		Name:      args["name"],
		Interface: args["interface"],
		Mode:      mode,
	})
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 832, Y: 80 + len(s.Lab.ExternalLinks)*96}
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "external create failed: " + err.Error()
	}
	return "created external:" + id
}

func (s *Service) ExternalSet(id string, args map[string]string) string {
	if s.Lab == nil {
		return "external set needs a loaded .lab file"
	}
	if invalid := unexpectedExternalArgs(args); len(invalid) > 0 {
		return "unsupported external set argument: " + invalid[0]
	}
	for i := range s.Lab.ExternalLinks {
		if s.Lab.ExternalLinks[i].ID != id {
			continue
		}
		if len(args) == 0 {
			return "configured external:" + id
		}
		mode := firstNonEmpty(args["mode"], s.Lab.ExternalLinks[i].Mode)
		if err := validateExternalConfig(id, mode); err != nil {
			return "external config failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		if value := args["name"]; value != "" {
			if err := s.requireSavePath(); err != nil {
				return "external config failed: " + err.Error()
			}
			s.Lab.ExternalLinks[i].Name = value
		}
		if value := args["interface"]; value != "" {
			if err := s.requireSavePath(); err != nil {
				return "external config failed: " + err.Error()
			}
			s.Lab.ExternalLinks[i].Interface = value
		}
		if value := args["mode"]; value != "" {
			if err := s.requireSavePath(); err != nil {
				return "external config failed: " + err.Error()
			}
			s.Lab.ExternalLinks[i].Mode = value
		}
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "external config failed: " + err.Error()
		}
		return "configured external:" + id
	}
	return "external not found: " + id
}

func (s *Service) ExternalDelete(id string) string {
	if s.Lab == nil {
		return "external delete needs a loaded .lab file"
	}
	if id == "" {
		return "usage: external delete <id>"
	}
	found := false
	for _, link := range s.Lab.ExternalLinks {
		if link.ID == id {
			found = true
		}
	}
	if !found {
		return "external not found: " + id
	}
	if err := s.requireSavePath(); err != nil {
		return "external delete failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	links := s.Lab.ExternalLinks[:0]
	for _, link := range s.Lab.ExternalLinks {
		if link.ID == id {
			continue
		}
		links = append(links, link)
	}
	s.Lab.ExternalLinks = links
	for i := range s.Lab.Switches {
		if s.Lab.Switches[i].ExternalLink == id {
			s.Lab.Switches[i].ExternalLink = ""
			if s.Lab.Switches[i].Mode == "macnat-bridge" {
				s.Lab.Switches[i].Mode = "bridge"
			}
		}
	}
	for i := range s.Lab.VMs {
		for j := range s.Lab.VMs[i].Networks {
			if s.Lab.VMs[i].Networks[j].ExternalLink == id {
				s.Lab.VMs[i].Networks[j].ExternalLink = ""
			}
		}
	}
	for i := range s.Lab.Containers {
		for j := range s.Lab.Containers[i].Networks {
			if s.Lab.Containers[i].Networks[j].ExternalLink == id {
				s.Lab.Containers[i].Networks[j].ExternalLink = ""
			}
		}
	}
	delete(s.Lab.Layout.Nodes, id)
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "external delete failed: " + err.Error()
	}
	return "deleted external:" + id
}
