package topology

import "foxlab-cli/internal/lab"

func (s *Service) SwitchCreate(name string, args map[string]string) string {
	if s.Lab == nil {
		return "switch create needs a loaded .lab file"
	}
	name = firstNonEmpty(args["name"], name)
	if name == "" {
		return "usage: switch create <name> [mode=bridge|nat|macnat-bridge] [uplink=NAME]"
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return err
	}
	if invalid := unexpectedSwitchArgs(args); len(invalid) > 0 {
		return "unsupported switch create argument: " + invalid[0]
	}
	mode := firstNonEmpty(args["mode"], "bridge")
	externals, err := s.resolveExternalRefs(switchExternalArgs(args))
	if err != nil {
		return "switch create failed: " + err.Error()
	}
	id := newNodeID()
	if err := s.validateSwitchConfig(name, mode, externals); err != nil {
		return "switch create failed: " + err.Error()
	}
	if err := s.requireSavePath(); err != nil {
		return "switch create failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	s.Lab.Switches = append(s.Lab.Switches, lab.Switch{
		ID:            id,
		Name:          name,
		Mode:          mode,
		ExternalLinks: externals,
	})
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 448, Y: 80 + len(s.Lab.Switches)*96}
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "switch create failed: " + err.Error()
	}
	return "created switch:" + name
}

func (s *Service) SwitchSet(ref string, args map[string]string) string {
	if s.Lab == nil {
		return "switch set needs a loaded .lab file"
	}
	if invalid := unexpectedSwitchArgs(args); len(invalid) > 0 {
		return "unsupported switch set argument: " + invalid[0]
	}
	id, ok := s.resolveSwitchID(ref)
	if !ok {
		return "switch not found: " + ref
	}
	for i := range s.Lab.Switches {
		if s.Lab.Switches[i].ID != id {
			continue
		}
		if len(args) == 0 {
			return "configured switch:" + s.nodeDisplayName("switch", id)
		}
		mode := firstNonEmpty(args["mode"], s.Lab.Switches[i].Mode)
		externals := lab.SwitchExternalLinks(s.Lab.Switches[i])
		nextExternalRefs, err := s.resolveExternalRefs(switchExternalArgs(args))
		if err != nil {
			return "switch config failed: " + err.Error()
		}
		externals = appendSwitchExternalLinks(externals, nextExternalRefs...)
		if err := s.validateSwitchConfig(s.nodeDisplayName("switch", id), mode, externals); err != nil {
			return "switch config failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		if value := args["name"]; value != "" {
			if err := s.validateNodeName(value, id); err != "" {
				return err
			}
			if err := s.requireSavePath(); err != nil {
				return "switch config failed: " + err.Error()
			}
			s.Lab.Switches[i].Name = value
		}
		if value := args["mode"]; value != "" {
			if err := s.requireSavePath(); err != nil {
				return "switch config failed: " + err.Error()
			}
			s.Lab.Switches[i].Mode = value
		}
		if value := firstNonEmpty(args["uplink"], args["external"], args["externallink"]); value != "" {
			if err := s.requireSavePath(); err != nil {
				return "switch config failed: " + err.Error()
			}
			s.Lab.Switches[i].ExternalLinks = externals
			s.Lab.Switches[i].ExternalLink = ""
		}
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "switch config failed: " + err.Error()
		}
		return "configured switch:" + s.nodeDisplayName("switch", id)
	}
	return "switch not found: " + id
}

func (s *Service) SwitchDisconnectExternal(ref string, externalIDs ...string) string {
	if s.Lab == nil {
		return "switch set needs a loaded .lab file"
	}
	id, ok := s.resolveSwitchID(ref)
	if !ok {
		return "switch not found: " + ref
	}
	for i := range s.Lab.Switches {
		if s.Lab.Switches[i].ID != id {
			continue
		}
		externals := lab.SwitchExternalLinks(s.Lab.Switches[i])
		if len(externals) == 0 {
			return "switch uplink already empty:" + id
		}
		removeID := firstNonEmpty(externalIDs...)
		if removeID != "" {
			resolved, ok := s.resolveExternalID(removeID)
			if !ok {
				return "uplink not found: " + removeID
			}
			removeID = resolved
		}
		if removeID != "" && !containsString(externals, removeID) {
			return "switch uplink not attached:" + id + ":" + removeID
		}
		if err := s.requireSavePath(); err != nil {
			return "switch config failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
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
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "switch config failed: " + err.Error()
		}
		return "disconnected uplink from switch:" + id
	}
	return "switch not found: " + id
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

func (s *Service) SwitchDelete(ref string) string {
	if s.Lab == nil {
		return "switch delete needs a loaded .lab file"
	}
	id, ok := s.resolveSwitchID(ref)
	if !ok {
		return "switch not found: " + ref
	}
	name := s.nodeDisplayName("switch", id)
	if err := s.requireSavePath(); err != nil {
		return "switch delete failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
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
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "switch delete failed: " + err.Error()
	}
	return "deleted switch:" + name
}
