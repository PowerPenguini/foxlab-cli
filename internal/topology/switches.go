package topology

import "foxlab-cli/internal/lab"

func (s *Service) CreateSwitch(request SwitchCreateRequest) Result {
	if s.CurrentLab() == nil {
		return Failure("switch create needs a loaded .lab file")
	}
	name := request.Name
	if name == "" {
		return Failure("usage: switch create <name> [mode=bridge|nat|macnat-bridge] [uplink=NAME]")
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return Failure(err)
	}
	mode := firstNonEmpty(request.Mode, "bridge")
	externals, err := s.resolveExternalRefs(nonEmptyStrings(request.Uplink))
	if err != nil {
		return FailureWithCause("switch create failed: "+err.Error(), err)
	}
	id := name
	if err := s.validateSwitchConfig(name, mode, externals); err != nil {
		return FailureWithCause("switch create failed: "+err.Error(), err)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("switch create failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	s.CurrentLab().Switches = append(s.CurrentLab().Switches, lab.Switch{
		ID:            id,
		Mode:          mode,
		ExternalLinks: externals,
	})
	if s.CurrentLab().Layout.Nodes == nil {
		s.CurrentLab().Layout.Nodes = map[string]lab.Position{}
	}
	s.CurrentLab().Layout.Nodes[id] = lab.Position{X: 448, Y: 80 + len(s.CurrentLab().Switches)*96}
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("switch create failed: "+err.Error(), err)
	}
	return Success("created switch:" + name)
}

func (s *Service) UpdateSwitch(ref string, update SwitchUpdate) Result {
	if s.CurrentLab() == nil {
		return Failure("switch set needs a loaded .lab file")
	}
	id, ok := s.resolveSwitchID(ref)
	if !ok {
		return Failure("switch not found: " + ref)
	}
	for i := range s.CurrentLab().Switches {
		if s.CurrentLab().Switches[i].ID != id {
			continue
		}
		if !switchUpdateRequested(update) {
			return Info("configured switch:" + s.nodeDisplayName("switch", id))
		}
		mode := s.CurrentLab().Switches[i].Mode
		if update.Mode.Set && update.Mode.Value != "" {
			mode = update.Mode.Value
		}
		externals := lab.SwitchExternalLinks(s.CurrentLab().Switches[i])
		if update.AttachUplink.Set && update.AttachUplink.Value != "" {
			nextExternalRefs, err := s.resolveExternalRefs(nonEmptyStrings(update.AttachUplink.Value))
			if err != nil {
				return FailureWithCause("switch config failed: "+err.Error(), err)
			}
			externals = appendSwitchExternalLinks(externals, nextExternalRefs...)
		}
		if err := s.validateSwitchConfig(s.nodeDisplayName("switch", id), mode, externals); err != nil {
			return FailureWithCause("switch config failed: "+err.Error(), err)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("switch config failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		renamed := false
		if value := update.Name.Value; update.Name.Set && value != "" {
			if err := s.renameNodeID("switch", id, value); err != nil {
				return FailureWithCause("switch rename failed: "+err.Error(), err)
			}
			renamed = id != value
			id = value
		}
		if value := update.Mode.Value; update.Mode.Set && value != "" {
			s.CurrentLab().Switches[i].Mode = value
		}
		if value := update.AttachUplink.Value; update.AttachUplink.Set && value != "" {
			s.CurrentLab().Switches[i].ExternalLinks = externals
			s.CurrentLab().Switches[i].ExternalLink = ""
		}
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("switch config failed: "+err.Error(), err)
		}
		message := "configured switch:" + s.nodeDisplayName("switch", id)
		if renamed {
			message += "; runtime will be recreated"
		}
		return ChangedInfo(message)
	}
	return Failure("switch not found: " + id)
}

func (s *Service) SwitchDisconnectExternal(ref string, externalIDs ...string) Result {
	if s.CurrentLab() == nil {
		return Failure("switch set needs a loaded .lab file")
	}
	id, ok := s.resolveSwitchID(ref)
	if !ok {
		return Failure("switch not found: " + ref)
	}
	for i := range s.CurrentLab().Switches {
		if s.CurrentLab().Switches[i].ID != id {
			continue
		}
		externals := lab.SwitchExternalLinks(s.CurrentLab().Switches[i])
		if len(externals) == 0 {
			return Info("switch uplink already empty:" + id)
		}
		removeID := firstNonEmpty(externalIDs...)
		if removeID != "" {
			resolved, ok := s.resolveExternalID(removeID)
			if !ok {
				return Failure("uplink not found: " + removeID)
			}
			removeID = resolved
		}
		if removeID != "" && !containsString(externals, removeID) {
			return Failure("switch uplink not attached:" + id + ":" + removeID)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("switch config failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		if removeID == "" {
			externals = nil
		} else {
			next := externals[:0]
			for _, externalID := range externals {
				if externalID != removeID {
					next = append(next, externalID)
				}
			}
			externals = next
		}
		s.CurrentLab().Switches[i].ExternalLinks = append([]string(nil), externals...)
		s.CurrentLab().Switches[i].ExternalLink = ""
		if len(externals) == 0 && s.CurrentLab().Switches[i].Mode == "macnat-bridge" {
			s.CurrentLab().Switches[i].Mode = "bridge"
		}
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("switch config failed: "+err.Error(), err)
		}
		return Success("disconnected uplink from switch:" + id)
	}
	return Failure("switch not found: " + id)
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func nonEmptyStrings(values ...string) []string {
	var out []string
	for _, value := range values {
		if value = firstNonEmpty(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func switchUpdateRequested(update SwitchUpdate) bool {
	return update.Name.Set || update.Mode.Set || update.AttachUplink.Set
}

func appendSwitchExternalLinks(existing []string, ids ...string) []string {
	out := append([]string(nil), existing...)
	seen := map[string]struct{}{}
	for _, id := range out {
		seen[id] = struct{}{}
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *Service) SwitchDelete(ref string) Result {
	if s.CurrentLab() == nil {
		return Failure("switch delete needs a loaded .lab file")
	}
	id, ok := s.resolveSwitchID(ref)
	if !ok {
		return Failure("switch not found: " + ref)
	}
	name := s.nodeDisplayName("switch", id)
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("switch delete failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	switches := s.CurrentLab().Switches[:0]
	for _, sw := range s.CurrentLab().Switches {
		if sw.ID == id {
			continue
		}
		switches = append(switches, sw)
	}
	s.CurrentLab().Switches = switches
	for i := range s.CurrentLab().VMs {
		for j := range s.CurrentLab().VMs[i].Networks {
			if s.CurrentLab().VMs[i].Networks[j].Switch == id {
				s.CurrentLab().VMs[i].Networks[j].Switch = ""
			}
		}
	}
	for i := range s.CurrentLab().Containers {
		for j := range s.CurrentLab().Containers[i].Networks {
			if s.CurrentLab().Containers[i].Networks[j].Switch == id {
				s.CurrentLab().Containers[i].Networks[j].Switch = ""
			}
		}
	}
	delete(s.CurrentLab().Layout.Nodes, id)
	s.removeLayoutLinksForNode("switch", id)
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("switch delete failed: "+err.Error(), err)
	}
	return Success("deleted switch:" + name)
}
