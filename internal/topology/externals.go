package topology

import "foxlab-cli/internal/lab"

func (s *Service) ExternalCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "external create needs a loaded .lab file"
	}
	if s.HasLabExternal(id) {
		return "external already exists: " + id
	}
	s.Lab.ExternalLinks = append(s.Lab.ExternalLinks, lab.ExternalLink{
		ID:        id,
		Name:      args["name"],
		Interface: args["interface"],
		Mode:      firstNonEmpty(args["mode"], lab.ExternalModeNAT),
	})
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 832, Y: 80 + len(s.Lab.ExternalLinks)*96}
	if err := s.SaveAndRefresh(); err != nil {
		return "external create failed: " + err.Error()
	}
	return "created external:" + id
}

func (s *Service) ExternalSet(id string, args map[string]string) string {
	if s.Lab == nil {
		return "external set needs a loaded .lab file"
	}
	for i := range s.Lab.ExternalLinks {
		if s.Lab.ExternalLinks[i].ID != id {
			continue
		}
		if value := args["name"]; value != "" {
			s.Lab.ExternalLinks[i].Name = value
		}
		if value := args["interface"]; value != "" {
			s.Lab.ExternalLinks[i].Interface = value
		}
		if value := args["mode"]; value != "" {
			s.Lab.ExternalLinks[i].Mode = value
		}
		if err := s.SaveAndRefresh(); err != nil {
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
	links := s.Lab.ExternalLinks[:0]
	for _, link := range s.Lab.ExternalLinks {
		if link.ID == id {
			found = true
			continue
		}
		links = append(links, link)
	}
	if !found {
		return "external not found: " + id
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
	if err := s.SaveAndRefresh(); err != nil {
		return "external delete failed: " + err.Error()
	}
	return "deleted external:" + id
}
