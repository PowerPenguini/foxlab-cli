package topology

import "foxlab-cli/internal/lab"

func (s *Service) SwitchCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "switch create needs a loaded .lab file"
	}
	if id == "" {
		return "usage: switch create <id> [mode=bridge|nat|macnat-bridge] [external=ID]"
	}
	if !lab.ValidID(id) {
		return "invalid switch id: " + id
	}
	if s.HasLabSwitch(id) {
		return "switch already exists: " + id
	}
	if kind := s.existingNodeKind(id); kind != "" {
		return "node id already exists as " + kind + ": " + id
	}
	if invalid := unexpectedSwitchArgs(args); len(invalid) > 0 {
		return "unsupported switch create argument: " + invalid[0]
	}
	mode := firstNonEmpty(args["mode"], "bridge")
	external := firstNonEmpty(args["external"], args["externallink"])
	if err := s.validateSwitchConfig(id, mode, external); err != nil {
		return "switch create failed: " + err.Error()
	}
	if err := s.requireSavePath(); err != nil {
		return "switch create failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	s.Lab.Switches = append(s.Lab.Switches, lab.Switch{
		ID:           id,
		Name:         args["name"],
		Mode:         mode,
		ExternalLink: external,
	})
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 448, Y: 80 + len(s.Lab.Switches)*96}
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "switch create failed: " + err.Error()
	}
	return "created switch:" + id
}

func (s *Service) SwitchSet(id string, args map[string]string) string {
	if s.Lab == nil {
		return "switch set needs a loaded .lab file"
	}
	if invalid := unexpectedSwitchArgs(args); len(invalid) > 0 {
		return "unsupported switch set argument: " + invalid[0]
	}
	for i := range s.Lab.Switches {
		if s.Lab.Switches[i].ID != id {
			continue
		}
		if len(args) == 0 {
			return "configured switch:" + id
		}
		mode := firstNonEmpty(args["mode"], s.Lab.Switches[i].Mode)
		external := firstNonEmpty(args["external"], args["externallink"], s.Lab.Switches[i].ExternalLink)
		if err := s.validateSwitchConfig(id, mode, external); err != nil {
			return "switch config failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		if value := args["name"]; value != "" {
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
		if value := firstNonEmpty(args["external"], args["externallink"]); value != "" {
			if err := s.requireSavePath(); err != nil {
				return "switch config failed: " + err.Error()
			}
			s.Lab.Switches[i].ExternalLink = value
		}
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "switch config failed: " + err.Error()
		}
		return "configured switch:" + id
	}
	return "switch not found: " + id
}

func (s *Service) SwitchDisconnectExternal(id string) string {
	if s.Lab == nil {
		return "switch set needs a loaded .lab file"
	}
	for i := range s.Lab.Switches {
		if s.Lab.Switches[i].ID != id {
			continue
		}
		if s.Lab.Switches[i].ExternalLink == "" {
			return "switch uplink already empty:" + id
		}
		if err := s.requireSavePath(); err != nil {
			return "switch config failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		s.Lab.Switches[i].ExternalLink = ""
		if s.Lab.Switches[i].Mode == "macnat-bridge" {
			s.Lab.Switches[i].Mode = "bridge"
		}
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "switch config failed: " + err.Error()
		}
		return "disconnected uplink from switch:" + id
	}
	return "switch not found: " + id
}

func (s *Service) SwitchDelete(id string) string {
	if s.Lab == nil {
		return "switch delete needs a loaded .lab file"
	}
	if id == "" {
		return "usage: switch delete <id>"
	}
	found := false
	for _, sw := range s.Lab.Switches {
		if sw.ID == id {
			found = true
		}
	}
	if !found {
		return "switch not found: " + id
	}
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
	return "deleted switch:" + id
}
