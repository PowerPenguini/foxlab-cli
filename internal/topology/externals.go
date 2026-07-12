package topology

import "foxlab-cli/internal/lab"

func (s *Service) ExternalCreate(name string, args map[string]string) string {
	if s.Lab == nil {
		return "uplink create needs a loaded .lab file"
	}
	name = firstNonEmpty(args["name"], name)
	if name == "" {
		return "usage: uplink create <name> interface=IFACE [mode=nat|direct|macnat]"
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return err
	}
	if invalid := unexpectedExternalArgs(args); len(invalid) > 0 {
		return "unsupported uplink create argument: " + invalid[0]
	}
	if firstNonEmpty(args["interface"]) == "" {
		return "usage: uplink create <name> interface=IFACE [mode=nat|direct|macnat]"
	}
	mode := firstNonEmpty(args["mode"], lab.ExternalModeNAT)
	id := name
	if err := validateExternalConfig(name, mode); err != nil {
		return "uplink create failed: " + err.Error()
	}
	if err := s.requireSavePath(); err != nil {
		return "uplink create failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	s.Lab.ExternalLinks = append(s.Lab.ExternalLinks, lab.ExternalLink{
		ID:        id,
		Interface: args["interface"],
		Mode:      mode,
	})
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 832, Y: 80 + len(s.Lab.ExternalLinks)*96}
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "uplink create failed: " + err.Error()
	}
	return "created uplink:" + name
}

func (s *Service) ExternalSet(ref string, args map[string]string) string {
	if s.Lab == nil {
		return "uplink set needs a loaded .lab file"
	}
	if invalid := unexpectedExternalArgs(args); len(invalid) > 0 {
		return "unsupported uplink set argument: " + invalid[0]
	}
	id, ok := s.resolveExternalID(ref)
	if !ok {
		return "uplink not found: " + ref
	}
	for i := range s.Lab.ExternalLinks {
		if s.Lab.ExternalLinks[i].ID != id {
			continue
		}
		if len(args) == 0 {
			return "configured uplink:" + s.nodeDisplayName("uplink", id)
		}
		mode := firstNonEmpty(args["mode"], s.Lab.ExternalLinks[i].Mode)
		if err := validateExternalConfig(s.nodeDisplayName("uplink", id), mode); err != nil {
			return "uplink config failed: " + err.Error()
		}
		if err := s.requireSavePath(); err != nil {
			return "uplink config failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		renamed := false
		if value := args["name"]; value != "" {
			if err := s.renameNodeID("external", id, value); err != nil {
				return "uplink rename failed: " + err.Error()
			}
			renamed = id != value
			id = value
		}
		if value := args["interface"]; value != "" {
			s.Lab.ExternalLinks[i].Interface = value
		}
		if value := args["mode"]; value != "" {
			s.Lab.ExternalLinks[i].Mode = value
		}
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "uplink config failed: " + err.Error()
		}
		message := "configured uplink:" + s.nodeDisplayName("uplink", id)
		if renamed {
			message += "; runtime will be recreated"
		}
		return message
	}
	return "uplink not found: " + id
}

func (s *Service) ExternalDelete(ref string) string {
	if s.Lab == nil {
		return "uplink delete needs a loaded .lab file"
	}
	id, ok := s.resolveExternalID(ref)
	if !ok {
		return "uplink not found: " + ref
	}
	if err := s.requireSavePath(); err != nil {
		return "uplink delete failed: " + err.Error()
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
		externals := lab.SwitchExternalLinks(s.Lab.Switches[i])
		next := externals[:0]
		for _, externalID := range externals {
			if externalID != id {
				next = append(next, externalID)
			}
		}
		if len(next) != len(externals) {
			s.Lab.Switches[i].ExternalLinks = append([]string(nil), next...)
			s.Lab.Switches[i].ExternalLink = ""
			if len(next) == 0 && s.Lab.Switches[i].Mode == "macnat-bridge" {
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
	s.removeLayoutLinksForNode("external", id)
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "uplink delete failed: " + err.Error()
	}
	return "deleted uplink:" + s.nodeDisplayName("uplink", id)
}
