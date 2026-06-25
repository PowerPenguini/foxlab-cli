package topology

import "foxlab-cli/internal/lab"

func (s *Service) SwitchCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "switch create needs a loaded .lab file"
	}
	if s.HasLabSwitch(id) {
		return "switch already exists: " + id
	}
	s.Lab.Switches = append(s.Lab.Switches, lab.Switch{
		ID:           id,
		Name:         args["name"],
		Mode:         firstNonEmpty(args["mode"], "bridge"),
		ExternalLink: firstNonEmpty(args["external"], args["externallink"]),
	})
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 448, Y: 80 + len(s.Lab.Switches)*96}
	if err := s.SaveAndRefresh(); err != nil {
		return "switch create failed: " + err.Error()
	}
	return "created switch:" + id
}

func (s *Service) SwitchSet(id string, args map[string]string) string {
	if s.Lab == nil {
		return "switch set needs a loaded .lab file"
	}
	for i := range s.Lab.Switches {
		if s.Lab.Switches[i].ID != id {
			continue
		}
		if value := args["name"]; value != "" {
			s.Lab.Switches[i].Name = value
		}
		if value := args["mode"]; value != "" {
			s.Lab.Switches[i].Mode = value
		}
		if value := firstNonEmpty(args["external"], args["externallink"]); value != "" {
			s.Lab.Switches[i].ExternalLink = value
		}
		if err := s.SaveAndRefresh(); err != nil {
			return "switch config failed: " + err.Error()
		}
		return "configured switch:" + id
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
	switches := s.Lab.Switches[:0]
	for _, sw := range s.Lab.Switches {
		if sw.ID == id {
			found = true
			continue
		}
		switches = append(switches, sw)
	}
	if !found {
		return "switch not found: " + id
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
	if err := s.SaveAndRefresh(); err != nil {
		return "switch delete failed: " + err.Error()
	}
	return "deleted switch:" + id
}
