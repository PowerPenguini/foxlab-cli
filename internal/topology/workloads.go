package topology

import (
	"path/filepath"

	"foxlab-cli/internal/lab"
)

func (s *Service) VMCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "vm create needs a loaded .lab file"
	}
	if s.HasLabVM(id) {
		return "vm already exists: " + id
	}
	if invalid := unexpectedVMCreateArgs(args); len(invalid) > 0 {
		return "unsupported vm create argument: " + invalid[0]
	}
	cpus := intArg(args, "cpus", 2)
	memory := intArg(args, "memory", 2048)
	if value, ok := positiveInt(args["mem"]); ok {
		memory = value
	}
	vm := lab.VM{
		ID:       id,
		Name:     firstNonEmpty(args["name"], id),
		MemoryMB: memory,
		CPUs:     cpus,
		Disk:     filepath.ToSlash(args["disk"]),
	}
	switchRef := args["switch"]
	externalRef := args["external"]
	if switchRef == "" && externalRef == "" && len(s.Lab.Switches) > 0 {
		switchRef = s.Lab.Switches[0].ID
	}
	if switchRef != "" {
		vm.Networks = append(vm.Networks, lab.VMNetwork{Switch: switchRef})
	}
	if externalRef != "" {
		vm.Networks = append(vm.Networks, lab.VMNetwork{ExternalLink: externalRef})
	}
	s.Lab.VMs = append(s.Lab.VMs, vm)
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 80, Y: 80 + len(s.Lab.VMs)*96}
	if err := s.SaveAndRefresh(); err != nil {
		return "create failed: " + err.Error()
	}
	return "created vm:" + id
}

func (s *Service) VMSet(id string, args map[string]string) string {
	if s.Lab == nil {
		return "vm set needs a loaded .lab file"
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if invalid := unexpectedVMSetArgs(args); len(invalid) > 0 {
			return "unsupported vm set argument: " + invalid[0]
		}
		if value := args["name"]; value != "" {
			s.Lab.VMs[i].Name = value
		}
		if value, ok := args["disk"]; ok {
			s.Lab.VMs[i].Disk = value
		}
		if value, ok := args["iso"]; ok {
			s.Lab.VMs[i].ISO = value
		}
		if value := args["vnc"]; value != "" {
			s.Lab.VMs[i].VNC = boolArg(value, s.Lab.VMs[i].VNC)
		}
		if value, ok := positiveInt(args["cpus"]); ok {
			s.Lab.VMs[i].CPUs = value
		}
		if value, ok := positiveInt(firstNonEmpty(args["memory"], args["mem"])); ok {
			s.Lab.VMs[i].MemoryMB = value
		}
		if value := args["switch"]; value != "" {
			s.removeNetworkLinksForNode("vm", id)
			s.Lab.VMs[i].Networks = []lab.VMNetwork{{Switch: value}}
		}
		if value := args["external"]; value != "" {
			s.removeNetworkLinksForNode("vm", id)
			s.Lab.VMs[i].Networks = []lab.VMNetwork{{ExternalLink: value}}
		}
		if err := s.SaveAndRefresh(); err != nil {
			return "config failed: " + err.Error()
		}
		return "configured vm:" + id
	}
	return "vm not found: " + id
}

func (s *Service) VMDelete(id string) string {
	if s.Lab == nil {
		return "vm delete needs a loaded .lab file"
	}
	if id == "" {
		return "usage: vm delete <id>"
	}
	found := false
	filtered := s.Lab.VMs[:0]
	for _, vm := range s.Lab.VMs {
		if vm.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, vm)
	}
	if !found {
		return "vm not found: " + id
	}
	s.Lab.VMs = filtered
	s.removeNetworkLinksForNode("vm", id)
	s.detachDisksForNode("vm", id)
	delete(s.Lab.Layout.Nodes, id)
	if err := s.SaveAndRefresh(); err != nil {
		return "delete failed: " + err.Error()
	}
	return "deleted vm:" + id
}

func (s *Service) ContainerCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "container create needs a loaded .lab file"
	}
	if s.HasLabContainer(id) {
		return "container already exists: " + id
	}
	if invalid := unexpectedContainerArgs(args); len(invalid) > 0 {
		return "unsupported container create argument: " + invalid[0]
	}
	ct := lab.Container{
		ID:      id,
		Name:    firstNonEmpty(args["name"], id),
		Image:   firstNonEmpty(args["image"], "?"),
		Disk:    args["disk"],
		Command: splitCommand(args["command"]),
		Env:     parseEnv(args["env"]),
	}
	switchRef := args["switch"]
	externalRef := args["external"]
	if switchRef == "" && externalRef == "" && len(s.Lab.Switches) > 0 {
		switchRef = s.Lab.Switches[0].ID
	}
	if switchRef != "" {
		ct.Networks = append(ct.Networks, lab.ContainerNetwork{Switch: switchRef, MAC: args["mac"]})
	}
	if externalRef != "" {
		ct.Networks = append(ct.Networks, lab.ContainerNetwork{ExternalLink: externalRef, MAC: args["mac"]})
	}
	s.Lab.Containers = append(s.Lab.Containers, ct)
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 80, Y: 80 + len(s.Lab.Containers)*96}
	if err := s.SaveAndRefresh(); err != nil {
		return "container create failed: " + err.Error()
	}
	return "created container:" + id
}

func (s *Service) ContainerSet(id string, args map[string]string) string {
	if s.Lab == nil {
		return "container set needs a loaded .lab file"
	}
	if invalid := unexpectedContainerArgs(args); len(invalid) > 0 {
		return "unsupported container set argument: " + invalid[0]
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if value := args["name"]; value != "" {
			s.Lab.Containers[i].Name = value
		}
		if value := args["image"]; value != "" {
			s.Lab.Containers[i].Image = value
		}
		if value, ok := args["disk"]; ok {
			s.Lab.Containers[i].Disk = value
		}
		if value, ok := args["command"]; ok {
			s.Lab.Containers[i].Command = splitCommand(value)
		}
		if value := args["env"]; value != "" {
			s.Lab.Containers[i].Env = parseEnv(value)
		}
		if value := args["switch"]; value != "" {
			s.removeNetworkLinksForNode("container", id)
			s.Lab.Containers[i].Networks = []lab.ContainerNetwork{{Switch: value, MAC: args["mac"]}}
		}
		if value := args["external"]; value != "" {
			s.removeNetworkLinksForNode("container", id)
			s.Lab.Containers[i].Networks = []lab.ContainerNetwork{{ExternalLink: value, MAC: args["mac"]}}
		} else if value := args["mac"]; value != "" && len(s.Lab.Containers[i].Networks) > 0 {
			s.Lab.Containers[i].Networks[0].MAC = value
		}
		if err := s.SaveAndRefresh(); err != nil {
			return "container config failed: " + err.Error()
		}
		return "configured container:" + id
	}
	return "container not found: " + id
}

func (s *Service) ContainerDelete(id string) string {
	if s.Lab == nil {
		return "container delete needs a loaded .lab file"
	}
	if id == "" {
		return "usage: container delete <id>"
	}
	found := false
	containers := s.Lab.Containers[:0]
	for _, ct := range s.Lab.Containers {
		if ct.ID == id {
			found = true
			continue
		}
		containers = append(containers, ct)
	}
	if !found {
		return "container not found: " + id
	}
	s.Lab.Containers = containers
	s.removeNetworkLinksForNode("container", id)
	s.detachDisksForNode("container", id)
	delete(s.Lab.Layout.Nodes, id)
	if err := s.SaveAndRefresh(); err != nil {
		return "container delete failed: " + err.Error()
	}
	return "deleted container:" + id
}
