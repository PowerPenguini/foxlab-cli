package topology

import "foxlab-cli/internal/lab"

func (s *Service) CreateExternal(request ExternalCreateRequest) Result {
	if s.CurrentLab() == nil {
		return Failure("uplink create needs a loaded .lab file")
	}
	name := request.Name
	if name == "" {
		return Failure("usage: uplink create <name> interface=IFACE [mode=nat|direct|macnat]")
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return Failure(err)
	}
	if firstNonEmpty(request.Interface) == "" {
		return Failure("usage: uplink create <name> interface=IFACE [mode=nat|direct|macnat]")
	}
	mode := firstNonEmpty(request.Mode, lab.ExternalModeNAT)
	id := name
	if err := validateExternalConfig(name, mode); err != nil {
		return FailureWithCause("uplink create failed: "+err.Error(), err)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("uplink create failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	s.CurrentLab().ExternalLinks = append(s.CurrentLab().ExternalLinks, lab.ExternalLink{
		ID:        id,
		Interface: request.Interface,
		Mode:      mode,
	})
	if s.CurrentLab().Layout.Nodes == nil {
		s.CurrentLab().Layout.Nodes = map[string]lab.Position{}
	}
	s.CurrentLab().Layout.Nodes[id] = lab.Position{X: 832, Y: 80 + len(s.CurrentLab().ExternalLinks)*96}
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("uplink create failed: "+err.Error(), err)
	}
	return Success("created uplink:" + name)
}

func (s *Service) UpdateExternal(ref string, update ExternalUpdate) Result {
	if s.CurrentLab() == nil {
		return Failure("uplink set needs a loaded .lab file")
	}
	id, ok := s.resolveExternalID(ref)
	if !ok {
		return Failure("uplink not found: " + ref)
	}
	for i := range s.CurrentLab().ExternalLinks {
		if s.CurrentLab().ExternalLinks[i].ID != id {
			continue
		}
		if !externalUpdateRequested(update) {
			return Info("configured uplink:" + s.nodeDisplayName("uplink", id))
		}
		mode := s.CurrentLab().ExternalLinks[i].Mode
		if update.Mode.Set && update.Mode.Value != "" {
			mode = update.Mode.Value
		}
		if err := validateExternalConfig(s.nodeDisplayName("uplink", id), mode); err != nil {
			return FailureWithCause("uplink config failed: "+err.Error(), err)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("uplink config failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		renamed := false
		if value := update.Name.Value; update.Name.Set && value != "" {
			if err := s.renameNodeID("external", id, value); err != nil {
				return FailureWithCause("uplink rename failed: "+err.Error(), err)
			}
			renamed = id != value
			id = value
		}
		if value := update.Interface.Value; update.Interface.Set && value != "" {
			s.CurrentLab().ExternalLinks[i].Interface = value
		}
		if value := update.Mode.Value; update.Mode.Set && value != "" {
			s.CurrentLab().ExternalLinks[i].Mode = value
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

func externalUpdateRequested(update ExternalUpdate) bool {
	return update.Name.Set || update.Interface.Set || update.Mode.Set
}

func (s *Service) ExternalDelete(ref string) Result {
	if s.CurrentLab() == nil {
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
	links := s.CurrentLab().ExternalLinks[:0]
	for _, link := range s.CurrentLab().ExternalLinks {
		if link.ID == id {
			continue
		}
		links = append(links, link)
	}
	s.CurrentLab().ExternalLinks = links
	for i := range s.CurrentLab().Switches {
		externals := lab.SwitchExternalLinks(s.CurrentLab().Switches[i])
		next := externals[:0]
		for _, externalID := range externals {
			if externalID != id {
				next = append(next, externalID)
			}
		}
		if len(next) != len(externals) {
			s.CurrentLab().Switches[i].ExternalLinks = append([]string(nil), next...)
			s.CurrentLab().Switches[i].ExternalLink = ""
			if len(next) == 0 && s.CurrentLab().Switches[i].Mode == "macnat-bridge" {
				s.CurrentLab().Switches[i].Mode = "bridge"
			}
		}
	}
	for i := range s.CurrentLab().VMs {
		for j := range s.CurrentLab().VMs[i].Networks {
			if s.CurrentLab().VMs[i].Networks[j].ExternalLink == id {
				s.CurrentLab().VMs[i].Networks[j].ExternalLink = ""
			}
		}
	}
	for i := range s.CurrentLab().Containers {
		for j := range s.CurrentLab().Containers[i].Networks {
			if s.CurrentLab().Containers[i].Networks[j].ExternalLink == id {
				s.CurrentLab().Containers[i].Networks[j].ExternalLink = ""
			}
		}
	}
	delete(s.CurrentLab().Layout.Nodes, id)
	s.removeLayoutLinksForNode("external", id)
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("uplink delete failed: "+err.Error(), err)
	}
	return Success("deleted uplink:" + s.nodeDisplayName("uplink", id))
}
