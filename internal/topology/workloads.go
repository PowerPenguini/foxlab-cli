package topology

import (
	"path/filepath"

	"foxlab-cli/internal/lab"
)

func (s *Service) VMCreate(name string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("vm create needs a loaded .lab file")
	}
	name = firstNonEmpty(args["name"], name)
	if name == "" {
		return Failure("usage: vm create <name> [cpus=N] [memory=N] [switch=NAME|uplink=NAME]")
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return Failure(err)
	}
	if invalid := unexpectedVMCreateArgs(args); len(invalid) > 0 {
		return Failure("unsupported vm create argument: " + invalid[0])
	}
	cpus := 2
	if value, present, ok := positiveIntField(args, "cpus"); present {
		if !ok {
			return Failure("invalid vm cpus: " + args["cpus"])
		}
		cpus = value
	}
	memory := 2048
	if value, present, ok := positiveIntField(args, "memory"); present {
		if !ok {
			return Failure("invalid vm memory: " + args["memory"])
		}
		memory = value
	}
	if value, present, ok := positiveIntField(args, "mem"); present {
		if !ok {
			return Failure("invalid vm memory: " + args["mem"])
		}
		memory = value
	}
	switchRef := ""
	if value := args["switch"]; value != "" {
		var ok bool
		switchRef, ok = s.resolveSwitchID(value)
		if !ok {
			return Failure("create failed: switch not found: " + value)
		}
	}
	externalRef := ""
	if value := firstNonEmpty(args["uplink"], args["external"]); value != "" {
		var ok bool
		externalRef, ok = s.resolveExternalID(value)
		if !ok {
			return Failure("create failed: uplink not found: " + value)
		}
	}
	if switchRef == "" && externalRef == "" && len(s.Lab.Switches) > 0 {
		switchRef = s.Lab.Switches[0].ID
	}
	id := name
	if err := s.validateVMNetworkRefs(name, switchRef, externalRef); err != nil {
		return FailureWithCause("create failed: "+err.Error(), err)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("create failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	vm := lab.VM{
		ID:       id,
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
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("create failed: "+err.Error(), err)
	}
	return Success("created vm:" + name)
}

func (s *Service) VMSet(ref string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("vm set needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if invalid := unexpectedVMSetArgs(args); len(invalid) > 0 {
			return Failure("unsupported vm set argument: " + invalid[0])
		}
		if len(args) == 0 {
			return Info("configured " + s.workloadDisplayRef("vm", id))
		}
		vncEnabled := false
		if value, ok := args["vnc"]; ok {
			var valid bool
			vncEnabled, valid = parseBool(value)
			if !valid {
				return Failure("invalid vm vnc: " + value)
			}
		}
		cpus := 0
		if value, present, ok := positiveIntField(args, "cpus"); present {
			if !ok {
				return Failure("invalid vm cpus: " + args["cpus"])
			}
			cpus = value
		}
		memory := 0
		if value, present, ok := positiveIntField(args, "memory"); present {
			if !ok {
				return Failure("invalid vm memory: " + args["memory"])
			}
			memory = value
		}
		if value, present, ok := positiveIntField(args, "mem"); present {
			if !ok {
				return Failure("invalid vm memory: " + args["mem"])
			}
			memory = value
		}
		switchRef := ""
		if value := args["switch"]; value != "" {
			var ok bool
			switchRef, ok = s.resolveSwitchID(value)
			if !ok {
				return Failure("config failed: switch not found: " + value)
			}
		}
		externalRef := ""
		if value := firstNonEmpty(args["uplink"], args["external"]); value != "" {
			var ok bool
			externalRef, ok = s.resolveExternalID(value)
			if !ok {
				return Failure("config failed: uplink not found: " + value)
			}
		}
		if err := s.validateVMNetworkRefs(s.nodeDisplayName("vm", id), switchRef, externalRef); err != nil {
			return FailureWithCause("config failed: "+err.Error(), err)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("config failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		renamed := false
		if value := args["name"]; value != "" {
			if err := s.renameNodeID("vm", id, value); err != nil {
				return FailureWithCause("vm rename failed: "+err.Error(), err)
			}
			renamed = id != value
			id = value
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
		if switchRef != "" {
			s.removeNetworkLinksForNode("vm", id)
			s.Lab.VMs[i].Networks = []lab.VMNetwork{{Switch: switchRef}}
		}
		if externalRef != "" {
			s.removeNetworkLinksForNode("vm", id)
			s.Lab.VMs[i].Networks = []lab.VMNetwork{{ExternalLink: externalRef}}
		}
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("config failed: "+err.Error(), err)
		}
		message := "configured " + s.workloadDisplayRef("vm", id)
		if renamed {
			message += "; runtime will be recreated"
		}
		return ChangedInfo(message)
	}
	return Failure("vm not found: " + id)
}

func (s *Service) VMDelete(ref string) Result {
	if s.Lab == nil {
		return Failure("vm delete needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("delete failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
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
	s.removeLayoutLinksForNode("vm", id)
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("delete failed: "+err.Error(), err)
	}
	return Success("deleted " + s.workloadDisplayRef("vm", id))
}

func (s *Service) ContainerCreate(name string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("container create needs a loaded .lab file")
	}
	name = firstNonEmpty(args["name"], name)
	if name == "" {
		return Failure("usage: container create <name> [image=REF] [command=CMD] [switch=NAME|uplink=NAME]")
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return Failure(err)
	}
	if invalid := unexpectedContainerArgs(args); len(invalid) > 0 {
		return Failure("unsupported container create argument: " + invalid[0])
	}
	if err := validateNICMACArg("container nic", args["mac"]); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	switchRef := ""
	if value := args["switch"]; value != "" {
		var ok bool
		switchRef, ok = s.resolveSwitchID(value)
		if !ok {
			return Failure("container create failed: switch not found: " + value)
		}
	}
	externalRef := ""
	if value := firstNonEmpty(args["uplink"], args["external"]); value != "" {
		var ok bool
		externalRef, ok = s.resolveExternalID(value)
		if !ok {
			return Failure("container create failed: uplink not found: " + value)
		}
	}
	if switchRef == "" && externalRef == "" && len(s.Lab.Switches) > 0 {
		switchRef = s.Lab.Switches[0].ID
	}
	id := name
	if err := s.validateContainerNetworkRefs(name, switchRef, externalRef); err != nil {
		return FailureWithCause("container create failed: "+err.Error(), err)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("container create failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	ct := lab.Container{
		ID:      id,
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
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("container create failed: "+err.Error(), err)
	}
	return Success("created container:" + name)
}

func (s *Service) ContainerSet(ref string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("container set needs a loaded .lab file")
	}
	if invalid := unexpectedContainerArgs(args); len(invalid) > 0 {
		return Failure("unsupported container set argument: " + invalid[0])
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if len(args) == 0 {
			return Info("configured " + s.workloadDisplayRef("container", id))
		}
		if err := validateNICMACArg("container nic", args["mac"]); err != nil {
			return FailureWithCause(err.Error(), err)
		}
		switchRef := ""
		if value := args["switch"]; value != "" {
			var ok bool
			switchRef, ok = s.resolveSwitchID(value)
			if !ok {
				return Failure("container config failed: switch not found: " + value)
			}
		}
		externalRef := ""
		if value := firstNonEmpty(args["uplink"], args["external"]); value != "" {
			var ok bool
			externalRef, ok = s.resolveExternalID(value)
			if !ok {
				return Failure("container config failed: uplink not found: " + value)
			}
		}
		if err := s.validateContainerNetworkRefs(s.nodeDisplayName("container", id), switchRef, externalRef); err != nil {
			return FailureWithCause("container config failed: "+err.Error(), err)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("container config failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		renamed := false
		if value := args["name"]; value != "" {
			if err := s.renameNodeID("container", id, value); err != nil {
				return FailureWithCause("container rename failed: "+err.Error(), err)
			}
			renamed = id != value
			id = value
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
		if switchRef != "" {
			s.removeNetworkLinksForNode("container", id)
			s.Lab.Containers[i].Networks = []lab.ContainerNetwork{{Switch: switchRef, MAC: args["mac"]}}
		}
		if externalRef != "" {
			s.removeNetworkLinksForNode("container", id)
			s.Lab.Containers[i].Networks = []lab.ContainerNetwork{{ExternalLink: externalRef, MAC: args["mac"]}}
		} else if value := args["mac"]; value != "" && len(s.Lab.Containers[i].Networks) > 0 {
			s.Lab.Containers[i].Networks[0].MAC = value
		}
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("container config failed: "+err.Error(), err)
		}
		message := "configured " + s.workloadDisplayRef("container", id)
		if renamed {
			message += "; runtime will be recreated"
		}
		return ChangedInfo(message)
	}
	return Failure("container not found: " + id)
}

func (s *Service) ContainerDelete(ref string) Result {
	if s.Lab == nil {
		return Failure("container delete needs a loaded .lab file")
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("container delete failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
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
	s.removeLayoutLinksForNode("container", id)
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("container delete failed: "+err.Error(), err)
	}
	return Success("deleted " + s.workloadDisplayRef("container", id))
}
