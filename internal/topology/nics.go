package topology

import (
	"errors"
	"strconv"

	"foxlab-cli/internal/lab"
)

func (s *Service) AddVMNIC(ref string, request NICAddRequest) Result {
	if s.CurrentLab() == nil {
		return Failure("vm nic add needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	if err := validateNICMACArg("vm nic", request.MAC); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	for i := range s.CurrentLab().VMs {
		if s.CurrentLab().VMs[i].ID != id {
			continue
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("nic add failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.CurrentLab().VMs[i].Networks = append(s.CurrentLab().VMs[i].Networks, lab.VMNetwork{MAC: request.MAC})
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("nic add failed: "+err.Error(), err)
		}
		return Success("added nic to " + s.workloadDisplayRef("vm", id))
	}
	return Failure("vm not found: " + id)
}

func (s *Service) ConnectVMNIC(ref string, request NICConnectRequest) Result {
	if s.CurrentLab() == nil {
		return Failure("vm nic connect needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	index := request.NIC
	if index < 0 {
		return Failure("usage: vm nic connect <id> <index> to=ID")
	}
	switchRef, externalRef, err := s.resolveNICAttachmentEndpoint("vm", request.Endpoint)
	if err != nil {
		return FailureWithCause(err.Error(), err)
	}
	if err := validateNICMACArg("vm nic", request.MAC); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	for i := range s.CurrentLab().VMs {
		if s.CurrentLab().VMs[i].ID != id {
			continue
		}
		if index >= len(s.CurrentLab().VMs[i].Networks) {
			return Failure("vm nic not found: " + id + ":" + strconv.Itoa(index))
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("nic connect failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.removeNetworkLinksForEndpoint(lab.NetworkEndpoint{Type: "vm", ID: id, NIC: index})
		s.CurrentLab().VMs[i].Networks[index].Switch = switchRef
		s.CurrentLab().VMs[i].Networks[index].ExternalLink = externalRef
		if value := request.MAC; value != "" {
			s.CurrentLab().VMs[i].Networks[index].MAC = value
		}
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("nic connect failed: "+err.Error(), err)
		}
		return Success("connected nic to " + s.workloadDisplayRef("vm", id))
	}
	return Failure("vm not found: " + id)
}

func (s *Service) DeleteVMNIC(ref string, index int) Result {
	if s.CurrentLab() == nil {
		return Failure("vm nic delete needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	if index < 0 {
		return Failure("usage: vm nic delete <id> <index>")
	}
	for i := range s.CurrentLab().VMs {
		if s.CurrentLab().VMs[i].ID != id {
			continue
		}
		if index >= len(s.CurrentLab().VMs[i].Networks) {
			return Failure("vm nic not found: " + id + ":" + strconv.Itoa(index))
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("nic delete failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.CurrentLab().VMs[i].Networks = append(s.CurrentLab().VMs[i].Networks[:index], s.CurrentLab().VMs[i].Networks[index+1:]...)
		s.removeNetworkLinksForDeletedNIC("vm", id, index)
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("nic delete failed: "+err.Error(), err)
		}
		return Success("deleted nic from " + s.workloadDisplayRef("vm", id) + " nic" + strconv.Itoa(index))
	}
	return Failure("vm not found: " + id)
}

func (s *Service) AddContainerNIC(ref string, request NICAddRequest) Result {
	if s.CurrentLab() == nil {
		return Failure("container nic add needs a loaded .lab file")
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	if err := validateNICMACArg("container nic", request.MAC); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	for i := range s.CurrentLab().Containers {
		if s.CurrentLab().Containers[i].ID != id {
			continue
		}
		if lab.IsDHCPContainer(s.CurrentLab().Containers[i]) {
			return Failure("DHCP service has exactly one managed NIC")
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("container nic add failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.CurrentLab().Containers[i].Networks = append(s.CurrentLab().Containers[i].Networks, lab.ContainerNetwork{MAC: request.MAC})
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("container nic add failed: "+err.Error(), err)
		}
		return Success("added nic to " + s.workloadDisplayRef("container", id))
	}
	return Failure("container not found: " + id)
}

func (s *Service) ConnectContainerNIC(ref string, request NICConnectRequest) Result {
	if s.CurrentLab() == nil {
		return Failure("container nic connect needs a loaded .lab file")
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	index := request.NIC
	if index < 0 {
		return Failure("usage: container nic connect <id> <index> to=ID")
	}
	switchRef, externalRef, err := s.resolveNICAttachmentEndpoint("container", request.Endpoint)
	if err != nil {
		return FailureWithCause(err.Error(), err)
	}
	if err := validateNICMACArg("container nic", request.MAC); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	for i := range s.CurrentLab().Containers {
		if s.CurrentLab().Containers[i].ID != id {
			continue
		}
		if lab.IsDHCPContainer(s.CurrentLab().Containers[i]) {
			if index != 0 || len(s.CurrentLab().Containers[i].Networks) != 1 {
				return Failure("DHCP service has exactly one managed NIC")
			}
			if externalRef != "" {
				return Failure("DHCP service can connect only to a NAT switch")
			}
			if request.MAC != "" {
				return Failure("DHCP network MAC is managed by FoxLab")
			}
			if err := s.validateDHCPNetworkTarget(id, switchRef); err != nil {
				return FailureWithCause("DHCP nic connect failed: "+err.Error(), err)
			}
		}
		if index >= len(s.CurrentLab().Containers[i].Networks) {
			return Failure("container nic not found: " + id + ":" + strconv.Itoa(index))
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("container nic connect failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.removeNetworkLinksForEndpoint(lab.NetworkEndpoint{Type: "container", ID: id, NIC: index})
		s.CurrentLab().Containers[i].Networks[index].Switch = switchRef
		s.CurrentLab().Containers[i].Networks[index].ExternalLink = externalRef
		if value := request.MAC; value != "" {
			s.CurrentLab().Containers[i].Networks[index].MAC = value
		}
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("container nic connect failed: "+err.Error(), err)
		}
		return Success("connected nic to " + s.workloadDisplayRef("container", id))
	}
	return Failure("container not found: " + id)
}

func (s *Service) DeleteContainerNIC(ref string, index int) Result {
	if s.CurrentLab() == nil {
		return Failure("container nic delete needs a loaded .lab file")
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	if index < 0 {
		return Failure("usage: container nic delete <id> <index>")
	}
	for i := range s.CurrentLab().Containers {
		if s.CurrentLab().Containers[i].ID != id {
			continue
		}
		if lab.IsDHCPContainer(s.CurrentLab().Containers[i]) {
			return Failure("DHCP service has exactly one managed NIC; select another NAT switch instead")
		}
		if index >= len(s.CurrentLab().Containers[i].Networks) {
			return Failure("container nic not found: " + id + ":" + strconv.Itoa(index))
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("container nic delete failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		s.CurrentLab().Containers[i].Networks = append(s.CurrentLab().Containers[i].Networks[:index], s.CurrentLab().Containers[i].Networks[index+1:]...)
		s.removeNetworkLinksForDeletedNIC("container", id, index)
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("container nic delete failed: "+err.Error(), err)
		}
		return Success("deleted nic from " + s.workloadDisplayRef("container", id) + " nic" + strconv.Itoa(index))
	}
	return Failure("container not found: " + id)
}

func (s *Service) resolveNICAttachmentEndpoint(workloadType string, endpoint NetworkEndpointRef) (string, string, error) {
	if endpoint.ID == "" {
		return "", "", errors.New(workloadType + " nic connect needs exactly one endpoint")
	}
	switch endpoint.Type {
	case NetworkEndpointAuto:
		if id, ok := s.resolveSwitchID(endpoint.ID); ok {
			return id, "", nil
		}
		if id, ok := s.resolveExternalID(endpoint.ID); ok {
			return "", id, nil
		}
		return "", "", errors.New("endpoint not found: " + endpoint.ID)
	case NetworkEndpointSwitch:
		id, ok := s.resolveSwitchID(endpoint.ID)
		if !ok {
			return "", "", errors.New("switch not found: " + endpoint.ID)
		}
		return id, "", nil
	case NetworkEndpointUplink:
		id, ok := s.resolveExternalID(endpoint.ID)
		if !ok {
			return "", "", errors.New("uplink not found: " + endpoint.ID)
		}
		return "", id, nil
	default:
		return "", "", errors.New(workloadType + " nic connect needs exactly one endpoint")
	}
}
