package topology

import (
	"path/filepath"

	"foxlab-cli/internal/lab"
)

func (s *Service) VMCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "vm create needs a loaded .lab file"
	}
	if id == "" {
		return "usage: vm create <id> [cpus=N] [memory=N] [switch=ID|external=ID]"
	}
	if !lab.ValidID(id) {
		return "invalid vm id: " + id
	}
	if s.HasLabVM(id) {
		return "vm already exists: " + id
	}
	if kind := s.existingNodeKind(id); kind != "" {
		return "node id already exists as " + kind + ": " + id
	}
	if invalid := unexpectedVMCreateArgs(args); len(invalid) > 0 {
		return "unsupported vm create argument: " + invalid[0]
	}
	cpus := 2
	if value, present, ok := positiveIntField(args, "cpus"); present {
		if !ok {
			return "invalid vm cpus: " + args["cpus"]
		}
		cpus = value
	}
	memory := 2048
	if value, present, ok := positiveIntField(args, "memory"); present {
		if !ok {
			return "invalid vm memory: " + args["memory"]
		}
		memory = value
	}
	if value, present, ok := positiveIntField(args, "mem"); present {
		if !ok {
			return "invalid vm memory: " + args["mem"]
		}
		memory = value
	}
	switchRef := args["switch"]
	externalRef := args["external"]
	if switchRef == "" && externalRef == "" && len(s.Lab.Switches) > 0 {
		switchRef = s.Lab.Switches[0].ID
	}
	if err := s.validateVMNetworkRefs(id, switchRef, externalRef); err != nil {
		return "create failed: " + err.Error()
	}
	if err := s.requireSavePath(); err != nil {
		return "create failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	vm := lab.VM{
		ID:       id,
		Name:     firstNonEmpty(args["name"], id),
		MemoryMB: memory,
		CPUs:     cpus,
		Disk:     filepath.ToSlash(args["disk"]),
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
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
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
		if len(args) == 0 {
			return "configured vm:" + id
		}
		vncEnabled := false
		if value, ok := args["vnc"]; ok {
			var valid bool
			vncEnabled, valid = parseBool(value)
			if !valid {
				return "invalid vm vnc: " + value
			}
		}
		cpus := 0
		if value, present, ok := positiveIntField(args, "cpus"); present {
			if !ok {
				return "invalid vm cpus: " + args["cpus"]
			}
			cpus = value
		}
		memory := 0
		if value, present, ok := positiveIntField(args, "memory"); present {
			if !ok {
				return "invalid vm memory: " + args["memory"]
			}
			memory = value
		}
		if value, present, ok := positiveIntField(args, "mem"); present {
			if !ok {
				return "invalid vm memory: " + args["mem"]
			}
			memory = value
		}
		if err := s.validateVMNetworkRefs(id, args["switch"], args["external"]); err != nil {
			return "config failed: " + err.Error()
		}
		if err := s.requireSavePath(); err != nil {
			return "config failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		if value := args["name"]; value != "" {
			s.Lab.VMs[i].Name = value
		}
		if value, ok := args["disk"]; ok {
			s.Lab.VMs[i].Disk = value
		}
		if value, ok := args["iso"]; ok {
			s.Lab.VMs[i].ISO = value
		}
		if _, ok := args["vnc"]; ok {
			s.Lab.VMs[i].VNC = vncEnabled
		}
		if cpus > 0 {
			s.Lab.VMs[i].CPUs = cpus
		}
		if memory > 0 {
			s.Lab.VMs[i].MemoryMB = memory
		}
		if value := args["switch"]; value != "" {
			s.removeNetworkLinksForNode("vm", id)
			s.Lab.VMs[i].Networks = []lab.VMNetwork{{Switch: value}}
		}
		if value := args["external"]; value != "" {
			s.removeNetworkLinksForNode("vm", id)
			s.Lab.VMs[i].Networks = []lab.VMNetwork{{ExternalLink: value}}
		}
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
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
	for _, vm := range s.Lab.VMs {
		if vm.ID == id {
			found = true
		}
	}
	if !found {
		return "vm not found: " + id
	}
	if err := s.requireSavePath(); err != nil {
		return "delete failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	filtered := s.Lab.VMs[:0]
	for _, vm := range s.Lab.VMs {
		if vm.ID == id {
			continue
		}
		filtered = append(filtered, vm)
	}
	s.Lab.VMs = filtered
	s.removeNetworkLinksForNode("vm", id)
	s.detachDisksForNode("vm", id)
	delete(s.Lab.Layout.Nodes, id)
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "delete failed: " + err.Error()
	}
	return "deleted vm:" + id
}

func (s *Service) ContainerCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "container create needs a loaded .lab file"
	}
	if id == "" {
		return "usage: container create <id> [image=REF] [command=CMD] [switch=ID]"
	}
	if !lab.ValidID(id) {
		return "invalid container id: " + id
	}
	if s.HasLabContainer(id) {
		return "container already exists: " + id
	}
	if kind := s.existingNodeKind(id); kind != "" {
		return "node id already exists as " + kind + ": " + id
	}
	if invalid := unexpectedContainerArgs(args); len(invalid) > 0 {
		return "unsupported container create argument: " + invalid[0]
	}
	if err := validateNICMACArg("container nic", args["mac"]); err != nil {
		return err.Error()
	}
	switchRef := args["switch"]
	externalRef := args["external"]
	if switchRef == "" && externalRef == "" && len(s.Lab.Switches) > 0 {
		switchRef = s.Lab.Switches[0].ID
	}
	if err := s.validateContainerNetworkRefs(id, switchRef, externalRef); err != nil {
		return "container create failed: " + err.Error()
	}
	if err := s.requireSavePath(); err != nil {
		return "container create failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	ct := lab.Container{
		ID:      id,
		Name:    firstNonEmpty(args["name"], id),
		Image:   firstNonEmpty(args["image"], "?"),
		Disk:    args["disk"],
		Command: splitCommand(args["command"]),
		Env:     parseEnv(args["env"]),
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
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
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
		if len(args) == 0 {
			return "configured container:" + id
		}
		if err := validateNICMACArg("container nic", args["mac"]); err != nil {
			return err.Error()
		}
		if err := s.validateContainerNetworkRefs(id, args["switch"], args["external"]); err != nil {
			return "container config failed: " + err.Error()
		}
		if err := s.requireSavePath(); err != nil {
			return "container config failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
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
		if value, ok := args["env"]; ok {
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
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
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
	for _, ct := range s.Lab.Containers {
		if ct.ID == id {
			found = true
		}
	}
	if !found {
		return "container not found: " + id
	}
	if err := s.requireSavePath(); err != nil {
		return "container delete failed: " + err.Error()
	}
	snapshot := lab.Clone(s.Lab)
	containers := s.Lab.Containers[:0]
	for _, ct := range s.Lab.Containers {
		if ct.ID == id {
			continue
		}
		containers = append(containers, ct)
	}
	s.Lab.Containers = containers
	s.removeNetworkLinksForNode("container", id)
	s.detachDisksForNode("container", id)
	delete(s.Lab.Layout.Nodes, id)
	if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
		return "container delete failed: " + err.Error()
	}
	return "deleted container:" + id
}
