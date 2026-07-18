package topology

import (
	"errors"

	"foxlab-cli/internal/lab"
)

func (s *Service) VMNICAdd(ref string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("vm nic add needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	if invalid := unexpectedVMNICAddArgs(args); len(invalid) > 0 {
		return Failure("unsupported vm nic add argument: " + invalid[0])
	}
	if err := validateNICMACArg("vm nic", args["mac"]); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("nic add failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.Lab.VMs[i].Networks = append(s.Lab.VMs[i].Networks, lab.VMNetwork{MAC: args["mac"]})
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("nic add failed: "+err.Error(), err)
		}
		return Success("added nic to " + s.workloadDisplayRef("vm", id))
	}
	return Failure("vm not found: " + id)
}

func (s *Service) VMNICConnect(ref, indexValue string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("vm nic connect needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	if invalid := unexpectedVMNICConnectArgs(args); len(invalid) > 0 {
		return Failure("unsupported vm nic connect argument: " + invalid[0])
	}
	index, ok := nicIndexArg(indexValue)
	if !ok {
		return Failure("usage: vm nic connect <id> <index> to=ID")
	}
	switchRef, externalRef, err := s.resolveVMNICEndpoint(args)
	if err != nil {
		return FailureWithCause(err.Error(), err)
	}
	if err := validateNICMACArg("vm nic", args["mac"]); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if index >= len(s.Lab.VMs[i].Networks) {
			return Failure("vm nic not found: " + id + ":" + indexValue)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("nic connect failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.removeNetworkLinksForEndpoint(lab.NetworkEndpoint{Type: "vm", ID: id, NIC: index})
		s.Lab.VMs[i].Networks[index].Switch = switchRef
		s.Lab.VMs[i].Networks[index].ExternalLink = externalRef
		if value := args["mac"]; value != "" {
			s.Lab.VMs[i].Networks[index].MAC = value
		}
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("nic connect failed: "+err.Error(), err)
		}
		return Success("connected nic to " + s.workloadDisplayRef("vm", id))
	}
	return Failure("vm not found: " + id)
}

func (s *Service) VMNICDelete(ref, indexValue string) Result {
	if s.Lab == nil {
		return Failure("vm nic delete needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	index, ok := nicIndexArg(indexValue)
	if !ok {
		return Failure("usage: vm nic delete <id> <index>")
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if index >= len(s.Lab.VMs[i].Networks) {
			return Failure("vm nic not found: " + id + ":" + indexValue)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("nic delete failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.Lab.VMs[i].Networks = append(s.Lab.VMs[i].Networks[:index], s.Lab.VMs[i].Networks[index+1:]...)
		s.removeNetworkLinksForDeletedNIC("vm", id, index)
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("nic delete failed: "+err.Error(), err)
		}
		return Success("deleted nic from " + s.workloadDisplayRef("vm", id) + " nic" + indexValue)
	}
	return Failure("vm not found: " + id)
}

func (s *Service) ContainerNICAdd(ref string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("container nic add needs a loaded .lab file")
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	if invalid := unexpectedContainerNICAddArgs(args); len(invalid) > 0 {
		return Failure("unsupported container nic add argument: " + invalid[0])
	}
	if err := validateNICMACArg("container nic", args["mac"]); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("container nic add failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.Lab.Containers[i].Networks = append(s.Lab.Containers[i].Networks, lab.ContainerNetwork{MAC: args["mac"]})
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("container nic add failed: "+err.Error(), err)
		}
		return Success("added nic to " + s.workloadDisplayRef("container", id))
	}
	return Failure("container not found: " + id)
}

func (s *Service) ContainerNICConnect(ref, indexValue string, args map[string]string) Result {
	if s.Lab == nil {
		return Failure("container nic connect needs a loaded .lab file")
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	if invalid := unexpectedContainerNICConnectArgs(args); len(invalid) > 0 {
		return Failure("unsupported container nic connect argument: " + invalid[0])
	}
	index, ok := nicIndexArg(indexValue)
	if !ok {
		return Failure("usage: container nic connect <id> <index> to=ID")
	}
	switchRef, externalRef, err := s.resolveContainerNICEndpoint(args)
	if err != nil {
		return FailureWithCause(err.Error(), err)
	}
	if err := validateNICMACArg("container nic", args["mac"]); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if index >= len(s.Lab.Containers[i].Networks) {
			return Failure("container nic not found: " + id + ":" + indexValue)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("container nic connect failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.removeNetworkLinksForEndpoint(lab.NetworkEndpoint{Type: "container", ID: id, NIC: index})
		s.Lab.Containers[i].Networks[index].Switch = switchRef
		s.Lab.Containers[i].Networks[index].ExternalLink = externalRef
		if value := args["mac"]; value != "" {
			s.Lab.Containers[i].Networks[index].MAC = value
		}
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("container nic connect failed: "+err.Error(), err)
		}
		return Success("connected nic to " + s.workloadDisplayRef("container", id))
	}
	return Failure("container not found: " + id)
}

func (s *Service) ContainerNICDelete(ref, indexValue string) Result {
	if s.Lab == nil {
		return Failure("container nic delete needs a loaded .lab file")
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	index, ok := nicIndexArg(indexValue)
	if !ok {
		return Failure("usage: container nic delete <id> <index>")
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if index >= len(s.Lab.Containers[i].Networks) {
			return Failure("container nic not found: " + id + ":" + indexValue)
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("container nic delete failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.Lab.Containers[i].Networks = append(s.Lab.Containers[i].Networks[:index], s.Lab.Containers[i].Networks[index+1:]...)
		s.removeNetworkLinksForDeletedNIC("container", id, index)
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("container nic delete failed: "+err.Error(), err)
		}
		return Success("deleted nic from " + s.workloadDisplayRef("container", id) + " nic" + indexValue)
	}
	return Failure("container not found: " + id)
}

func (s *Service) resolveVMNICEndpoint(args map[string]string) (string, string, error) {
	target := firstNonEmpty(args["to"], args["target"])
	switchRef := args["switch"]
	externalRef := firstNonEmpty(args["uplink"], args["external"])
	if target != "" {
		if switchRef != "" || externalRef != "" {
			return "", "", errors.New("vm nic connect accepts to=ID or a compatibility alias, not both")
		}
		if id, ok := s.resolveSwitchID(target); ok {
			return id, "", nil
		}
		if id, ok := s.resolveExternalID(target); ok {
			return "", id, nil
		}
		{
			return "", "", errors.New("endpoint not found: " + target)
		}
	}
	if (switchRef == "") == (externalRef == "") {
		return "", "", errors.New("vm nic connect needs exactly one endpoint")
	}
	if switchRef != "" {
		resolved, ok := s.resolveSwitchID(switchRef)
		if !ok {
			return "", "", errors.New("switch not found: " + switchRef)
		}
		switchRef = resolved
	}
	if externalRef != "" {
		resolved, ok := s.resolveExternalID(externalRef)
		if !ok {
			return "", "", errors.New("uplink not found: " + externalRef)
		}
		externalRef = resolved
	}
	return switchRef, externalRef, nil
}

func (s *Service) resolveContainerNICEndpoint(args map[string]string) (string, string, error) {
	target := firstNonEmpty(args["to"], args["target"])
	switchRef := args["switch"]
	externalRef := firstNonEmpty(args["uplink"], args["external"])
	if target == "" {
		if (switchRef == "") == (externalRef == "") {
			return "", "", errors.New("container nic connect needs exactly one endpoint")
		}
		if switchRef != "" {
			resolved, ok := s.resolveSwitchID(switchRef)
			if !ok {
				return "", "", errors.New("switch not found: " + switchRef)
			}
			switchRef = resolved
		}
		if externalRef != "" {
			resolved, ok := s.resolveExternalID(externalRef)
			if !ok {
				return "", "", errors.New("uplink not found: " + externalRef)
			}
			externalRef = resolved
		}
		return switchRef, externalRef, nil
	}
	if switchRef != "" || externalRef != "" {
		return "", "", errors.New("container nic connect accepts to=ID or a compatibility alias, not both")
	}
	if id, ok := s.resolveSwitchID(target); ok {
		return id, "", nil
	}
	if id, ok := s.resolveExternalID(target); ok {
		return "", id, nil
	}
	{
		return "", "", errors.New("endpoint not found: " + target)
	}
}
