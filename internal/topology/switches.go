package topology

import "foxlab-cli/internal/lab"

func (s *Service) SwitchCreate(name string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("switch create needs a loaded .lab file")
	}
	name = firstNonEmpty(args["name"], name)
	if name == "" {
		return Failure("usage: switch create <name> [mode=bridge|nat|macnat-bridge] [uplink=NAME]")
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return Failure(err)
	}
	if invalid := unexpectedSwitchArgs(args); len(invalid) > 0 {
		return Failure("unsupported switch create argument: " + invalid[0])
	}
	mode := firstNonEmpty(args["mode"], "bridge")
	externals, err := s.resolveExternalRefs(switchExternalArgs(args))
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
	s.Lab.Switches = append(s.Lab.Switches, lab.Switch{
		ID:            id,
		Mode:          mode,
		ExternalLinks: externals,
	})
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 448, Y: 80 + len(s.Lab.Switches)*96}
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("switch create failed: "+err.Error(), err)
	}
	return Success("created switch:" + name)
}

func (s *Service) SwitchSet(ref string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("switch set needs a loaded .lab file")
	}
	if invalid := unexpectedSwitchArgs(args); len(invalid) > 0 {
		return Failure("unsupported switch set argument: " + invalid[0])
	}
	id, ok := s.resolveSwitchID(ref)
	if !ok {
		return Failure("switch not found: " + ref)
	}
	for i := range s.Lab.Switches {
		if s.Lab.Switches[i].ID != id {
			continue
		}
		if len(args) == 0 {
			return Info("configured switch:" + s.nodeDisplayName("switch", id))
		}
		mode := firstNonEmpty(args["mode"], s.Lab.Switches[i].Mode)
		externals := lab.SwitchExternalLinks(s.Lab.Switches[i])
		nextExternalRefs, err := s.resolveExternalRefs(switchExternalArgs(args))
		if err != nil {
			return FailureWithCause("switch config failed: "+err.Error(), err)
		}
		externals = appendSwitchExternalLinks(externals, nextExternalRefs...)
		if err := s.validateSwitchConfig(s.nodeDisplayName("switch", id), mode, externals); err != nil {
			return FailureWithCause("switch config failed: "+err.Error(), err)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("switch config failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		renamed := false
		if value := args["name"]; value != "" {
			if err := s.renameNodeID("switch", id, value); err != nil {
				return FailureWithCause("switch rename failed: "+err.Error(), err)
			}
			renamed = id != value
			id = value
		}
		if value := args["mode"]; value != "" {
			s.Lab.Switches[i].Mode = value
		}
		if value := firstNonEmpty(args["uplink"], args["external"], args["externallink"]); value != "" {
			s.Lab.Switches[i].ExternalLinks = externals
			s.Lab.Switches[i].ExternalLink = ""
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
	if s.Lab == nil {
		return Failure("switch set needs a loaded .lab file")
	}
	id, ok := s.resolveSwitchID(ref)
	if !ok {
		return Failure("switch not found: " + ref)
	}
	for i := range s.Lab.Switches {
		if s.Lab.Switches[i].ID != id {
			continue
		}
		externals := lab.SwitchExternalLinks(s.Lab.Switches[i])
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
		s.Lab.Switches[i].ExternalLinks = append([]string(nil), externals...)
		s.Lab.Switches[i].ExternalLink = ""
		if len(externals) == 0 && s.Lab.Switches[i].Mode == "macnat-bridge" {
			s.Lab.Switches[i].Mode = "bridge"
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

func switchExternalArgs(args map[string]string) []string {
	id := firstNonEmpty(args["uplink"], args["external"], args["externallink"])
	if id == "" {
		return nil
	}
	return []string{id}
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
	if s.Lab == nil {
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
	switches := s.Lab.Switches[:0]
	for _, sw := range s.Lab.Switches {
		if sw.ID == id {
			continue
		}
		switches = append(switches, sw)
	}
	s.Lab.Switches = switches
	for i := range s.Lab.VMs {
		for j := range s.Lab.VMs[i].Networks {
			if s.Lab.VMs[i].Networks[j].Switch == id {
				s.Lab.VMs[i].Networks[j].Switch = ""
			}
		}
	}
	for i := range s.Lab.Containers {
		for j := range s.Lab.Containers[i].Networks {
			if s.Lab.Containers[i].Networks[j].Switch == id {
				s.Lab.Containers[i].Networks[j].Switch = ""
			}
		}
	}
	delete(s.Lab.Layout.Nodes, id)
	s.removeLayoutLinksForNode("switch", id)
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("switch delete failed: "+err.Error(), err)
	}
	return Success("deleted switch:" + name)
}
