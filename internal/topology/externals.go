package topology

import "foxlab-cli/internal/lab"

func (s *Service) ExternalCreate(name string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("uplink create needs a loaded .lab file")
	}
	name = firstNonEmpty(args["name"], name)
	if name == "" {
		return Failure("usage: uplink create <name> interface=IFACE [mode=nat|direct|macnat]")
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return Failure(err)
	}
	if invalid := unexpectedExternalArgs(args); len(invalid) > 0 {
		return Failure("unsupported uplink create argument: " + invalid[0])
	}
	if firstNonEmpty(args["interface"]) == "" {
		return Failure("usage: uplink create <name> interface=IFACE [mode=nat|direct|macnat]")
	}
	mode := firstNonEmpty(args["mode"], lab.ExternalModeNAT)
	id := name
	if err := validateExternalConfig(name, mode); err != nil {
		return FailureWithCause("uplink create failed: "+err.Error(), err)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("uplink create failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	s.Lab.ExternalLinks = append(s.Lab.ExternalLinks, lab.ExternalLink{
		ID:        id,
		Interface: args["interface"],
		Mode:      mode,
	})
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 832, Y: 80 + len(s.Lab.ExternalLinks)*96}
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("uplink create failed: "+err.Error(), err)
	}
	return Success("created uplink:" + name)
}

func (s *Service) ExternalSet(ref string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("uplink set needs a loaded .lab file")
	}
	if invalid := unexpectedExternalArgs(args); len(invalid) > 0 {
		return Failure("unsupported uplink set argument: " + invalid[0])
	}
	id, ok := s.resolveExternalID(ref)
	if !ok {
		return Failure("uplink not found: " + ref)
	}
	for i := range s.Lab.ExternalLinks {
		if s.Lab.ExternalLinks[i].ID != id {
			continue
		}
		if len(args) == 0 {
			return Info("configured uplink:" + s.nodeDisplayName("uplink", id))
		}
		mode := firstNonEmpty(args["mode"], s.Lab.ExternalLinks[i].Mode)
		if err := validateExternalConfig(s.nodeDisplayName("uplink", id), mode); err != nil {
			return FailureWithCause("uplink config failed: "+err.Error(), err)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("uplink config failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		renamed := false
		if value := args["name"]; value != "" {
			if err := s.renameNodeID("external", id, value); err != nil {
				return FailureWithCause("uplink rename failed: "+err.Error(), err)
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
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("uplink config failed: "+err.Error(), err)
		}
		message := "configured uplink:" + s.nodeDisplayName("uplink", id)
		if renamed {
			message += "; runtime will be recreated"
		}
		return ChangedInfo(message)
	}
	return Failure("uplink not found: " + id)
}

func (s *Service) ExternalDelete(ref string) Result {
	if s.Lab == nil {
		return Failure("uplink delete needs a loaded .lab file")
	}
	id, ok := s.resolveExternalID(ref)
	if !ok {
		return Failure("uplink not found: " + ref)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("uplink delete failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
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
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("uplink delete failed: "+err.Error(), err)
	}
	return Success("deleted uplink:" + s.nodeDisplayName("uplink", id))
}
