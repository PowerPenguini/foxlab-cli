package topology

import (
	"maps"
	"path/filepath"
	"strconv"
	"strings"

	"foxlab-cli/internal/lab"
)

func (s *Service) CreateVM(request VMCreateRequest) Result {
	if s.CurrentLab() == nil {
		return Failure("vm create needs a loaded .lab file")
	}
	name := request.Name
	if name == "" {
		return Failure("usage: vm create <name> [cpus=N] [memory=N] [switch=NAME|uplink=NAME]")
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return Failure(err)
	}
	cpus := 2
	if request.CPUs.Set {
		if request.CPUs.Value <= 0 {
			return Failure("invalid vm cpus: " + strconv.Itoa(request.CPUs.Value))
		}
		cpus = request.CPUs.Value
	}
	memory := 2048
	if request.MemoryMB.Set {
		if request.MemoryMB.Value <= 0 {
			return Failure("invalid vm memory: " + strconv.Itoa(request.MemoryMB.Value))
		}
		memory = request.MemoryMB.Value
	}
	switchRef := ""
	if value := request.Network.Switch; value != "" {
		var ok bool
		switchRef, ok = s.resolveSwitchID(value)
		if !ok {
			return Failure("create failed: switch not found: " + value)
		}
	}
	externalRef := ""
	if value := request.Network.Uplink; value != "" {
		var ok bool
		externalRef, ok = s.resolveExternalID(value)
		if !ok {
			return Failure("create failed: uplink not found: " + value)
		}
	}
	if switchRef == "" && externalRef == "" && len(s.CurrentLab().Switches) > 0 {
		switchRef = s.CurrentLab().Switches[0].ID
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
		Disk:     filepath.ToSlash(request.Disk),
	}
	if switchRef != "" {
		vm.Networks = append(vm.Networks, lab.VMNetwork{Switch: switchRef})
	}
	if externalRef != "" {
		vm.Networks = append(vm.Networks, lab.VMNetwork{ExternalLink: externalRef})
	}
	s.CurrentLab().VMs = append(s.CurrentLab().VMs, vm)
	if s.CurrentLab().Layout.Nodes == nil {
		s.CurrentLab().Layout.Nodes = map[string]lab.Position{}
	}
	s.CurrentLab().Layout.Nodes[id] = lab.Position{X: 80, Y: 80 + len(s.CurrentLab().VMs)*96}
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("create failed: "+err.Error(), err)
	}
	return Success("created vm:" + name)
}

func (s *Service) UpdateVM(ref string, update VMUpdate) Result {
	if s.CurrentLab() == nil {
		return Failure("vm set needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	for i := range s.CurrentLab().VMs {
		if s.CurrentLab().VMs[i].ID != id {
			continue
		}
		if !vmUpdateRequested(update) {
			return Info("configured " + s.workloadDisplayRef("vm", id))
		}
		if update.CPUs.Set && update.CPUs.Value <= 0 {
			return Failure("invalid vm cpus: " + strconv.Itoa(update.CPUs.Value))
		}
		if update.MemoryMB.Set && update.MemoryMB.Value <= 0 {
			return Failure("invalid vm memory: " + strconv.Itoa(update.MemoryMB.Value))
		}
		switchRef := ""
		if value := update.Network.Switch; value != "" {
			var ok bool
			switchRef, ok = s.resolveSwitchID(value)
			if !ok {
				return Failure("config failed: switch not found: " + value)
			}
		}
		externalRef := ""
		if value := update.Network.Uplink; value != "" {
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
		if value := update.Name.Value; update.Name.Set && value != "" {
			if err := s.renameNodeID("vm", id, value); err != nil {
				return FailureWithCause("vm rename failed: "+err.Error(), err)
			}
			renamed = id != value
			id = value
		}
		if update.Disk.Set {
			s.CurrentLab().VMs[i].Disk = update.Disk.Value
		}
		if update.ISO.Set {
			s.CurrentLab().VMs[i].ISO = update.ISO.Value
		}
		if update.VNC.Set {
			s.CurrentLab().VMs[i].VNC = update.VNC.Value
		}
		if update.CPUs.Set {
			s.CurrentLab().VMs[i].CPUs = update.CPUs.Value
		}
		if update.MemoryMB.Set {
			s.CurrentLab().VMs[i].MemoryMB = update.MemoryMB.Value
		}
		if switchRef != "" {
			s.removeNetworkLinksForNode("vm", id)
			s.CurrentLab().VMs[i].Networks = []lab.VMNetwork{{Switch: switchRef}}
		}
		if externalRef != "" {
			s.removeNetworkLinksForNode("vm", id)
			s.CurrentLab().VMs[i].Networks = []lab.VMNetwork{{ExternalLink: externalRef}}
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
	if s.CurrentLab() == nil {
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
	filtered := s.CurrentLab().VMs[:0]
	for _, vm := range s.CurrentLab().VMs {
		if vm.ID == id {
			continue
		}
		filtered = append(filtered, vm)
	}
	s.CurrentLab().VMs = filtered
	s.removeNetworkLinksForNode("vm", id)
	s.detachDisksForNode("vm", id)
	delete(s.CurrentLab().Layout.Nodes, id)
	s.removeLayoutLinksForNode("vm", id)
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("delete failed: "+err.Error(), err)
	}
	return Success("deleted " + s.workloadDisplayRef("vm", id))
}

func (s *Service) CreateContainer(request ContainerCreateRequest) Result {
	if s.CurrentLab() == nil {
		return Failure("container create needs a loaded .lab file")
	}
	name := request.Name
	if name == "" {
		return Failure("usage: container create <name> [image=REF] [command=CMD] [switch=NAME|uplink=NAME]")
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return Failure(err)
	}
	if err := validateNICMACArg("container nic", request.Network.MAC); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	switchRef := ""
	if value := request.Network.Switch; value != "" {
		var ok bool
		switchRef, ok = s.resolveSwitchID(value)
		if !ok {
			return Failure("container create failed: switch not found: " + value)
		}
	}
	externalRef := ""
	if value := request.Network.Uplink; value != "" {
		var ok bool
		externalRef, ok = s.resolveExternalID(value)
		if !ok {
			return Failure("container create failed: uplink not found: " + value)
		}
	}
	if switchRef == "" && externalRef == "" && len(s.CurrentLab().Switches) > 0 {
		switchRef = s.CurrentLab().Switches[0].ID
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
		Image:   firstNonEmpty(request.Image, "?"),
		Disk:    request.Disk,
		Command: append([]string(nil), request.Command...),
		Shell:   strings.TrimSpace(request.Shell),
		Env:     maps.Clone(request.Env),
	}
	if switchRef != "" {
		ct.Networks = append(ct.Networks, lab.ContainerNetwork{Switch: switchRef, MAC: request.Network.MAC})
	}
	if externalRef != "" {
		ct.Networks = append(ct.Networks, lab.ContainerNetwork{ExternalLink: externalRef, MAC: request.Network.MAC})
	}
	s.CurrentLab().Containers = append(s.CurrentLab().Containers, ct)
	if s.CurrentLab().Layout.Nodes == nil {
		s.CurrentLab().Layout.Nodes = map[string]lab.Position{}
	}
	s.CurrentLab().Layout.Nodes[id] = lab.Position{X: 80, Y: 80 + len(s.CurrentLab().Containers)*96}
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("container create failed: "+err.Error(), err)
	}
	return Success("created container:" + name)
}

func (s *Service) UpdateContainer(ref string, update ContainerUpdate) Result {
	if s.CurrentLab() == nil {
		return Failure("container set needs a loaded .lab file")
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	for i := range s.CurrentLab().Containers {
		if s.CurrentLab().Containers[i].ID != id {
			continue
		}
		if !containerUpdateRequested(update) {
			return Info("configured " + s.workloadDisplayRef("container", id))
		}
		if err := validateNICMACArg("container nic", update.Network.MAC); err != nil {
			return FailureWithCause(err.Error(), err)
		}
		switchRef := ""
		if value := update.Network.Switch; value != "" {
			var ok bool
			switchRef, ok = s.resolveSwitchID(value)
			if !ok {
				return Failure("container config failed: switch not found: " + value)
			}
		}
		externalRef := ""
		if value := update.Network.Uplink; value != "" {
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
		if value := update.Name.Value; update.Name.Set && value != "" {
			if err := s.renameNodeID("container", id, value); err != nil {
				return FailureWithCause("container rename failed: "+err.Error(), err)
			}
			renamed = id != value
			id = value
		}
		if value := update.Image.Value; update.Image.Set && value != "" {
			s.CurrentLab().Containers[i].Image = value
		}
		if update.Disk.Set {
			s.CurrentLab().Containers[i].Disk = update.Disk.Value
		}
		if update.Command.Set {
			s.CurrentLab().Containers[i].Command = append([]string(nil), update.Command.Value...)
		}
		if update.Shell.Set {
			s.CurrentLab().Containers[i].Shell = strings.TrimSpace(update.Shell.Value)
		}
		if update.Env.Set {
			s.CurrentLab().Containers[i].Env = maps.Clone(update.Env.Value)
		}
		if switchRef != "" {
			s.removeNetworkLinksForNode("container", id)
			s.CurrentLab().Containers[i].Networks = []lab.ContainerNetwork{{Switch: switchRef, MAC: update.Network.MAC}}
		}
		if externalRef != "" {
			s.removeNetworkLinksForNode("container", id)
			s.CurrentLab().Containers[i].Networks = []lab.ContainerNetwork{{ExternalLink: externalRef, MAC: update.Network.MAC}}
		} else if value := update.Network.MAC; value != "" && len(s.CurrentLab().Containers[i].Networks) > 0 {
			s.CurrentLab().Containers[i].Networks[0].MAC = value
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

func vmUpdateRequested(update VMUpdate) bool {
	return update.Name.Set || update.CPUs.Set || update.MemoryMB.Set || update.Disk.Set || update.ISO.Set || update.VNC.Set ||
		update.Network.Switch != "" || update.Network.Uplink != ""
}

func containerUpdateRequested(update ContainerUpdate) bool {
	return update.Name.Set || update.Image.Set || update.Disk.Set || update.Command.Set || update.Shell.Set || update.Env.Set ||
		update.Network.Switch != "" || update.Network.Uplink != "" || update.Network.MAC != ""
}

func (s *Service) ContainerDelete(ref string) Result {
	if s.CurrentLab() == nil {
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
	containers := s.CurrentLab().Containers[:0]
	for _, ct := range s.CurrentLab().Containers {
		if ct.ID == id {
			continue
		}
		containers = append(containers, ct)
	}
	s.CurrentLab().Containers = containers
	s.removeNetworkLinksForNode("container", id)
	s.detachDisksForNode("container", id)
	delete(s.CurrentLab().Layout.Nodes, id)
	s.removeLayoutLinksForNode("container", id)
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("container delete failed: "+err.Error(), err)
	}
	return Success("deleted " + s.workloadDisplayRef("container", id))
}
