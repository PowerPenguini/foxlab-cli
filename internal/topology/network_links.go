package topology

import (
	"errors"
	"strconv"

	"foxlab-cli/internal/lab"
)

func (s *Service) ConnectDirectNIC(sourceRef, targetRef NetworkEndpointRef) Result {
	if s.CurrentLab() == nil {
		return Failure("nic connect needs a loaded .lab file")
	}
	sourceType, sourceTypeOK := directEndpointType(sourceRef.Type)
	targetType, targetTypeOK := directEndpointType(targetRef.Type)
	if !sourceTypeOK || !targetTypeOK {
		return Failure("direct link target must be vm or container")
	}
	sourceID, ok := s.resolveWorkloadID(sourceType, sourceRef.ID)
	if !ok {
		return Failure(sourceType + " not found: " + sourceRef.ID)
	}
	targetID, ok := s.resolveWorkloadID(targetType, targetRef.ID)
	if !ok {
		return Failure(targetType + " not found: " + targetRef.ID)
	}
	if _, managed := s.managedDHCPContainer(sourceID); sourceType == "container" && managed {
		return Failure("DHCP service can connect only to a NAT switch")
	}
	if _, managed := s.managedDHCPContainer(targetID); targetType == "container" && managed {
		return Failure("DHCP service can connect only to a NAT switch")
	}
	usage := "usage: nic connect <source> <index> <target>"
	if targetRef.NIC.Set {
		usage = "usage: nic connect <source> <index> <target> <target-index>"
	}
	if !sourceRef.NIC.Set || sourceRef.NIC.Value < 0 {
		return Failure(usage)
	}
	sourceIndex := sourceRef.NIC.Value
	source := lab.NetworkEndpoint{Type: sourceType, ID: sourceID, NIC: sourceIndex}
	targetIndex := 0
	if targetRef.NIC.Set {
		if targetRef.NIC.Value < 0 {
			return Failure(usage)
		}
		targetIndex = targetRef.NIC.Value
	} else {
		if err := s.ensureDirectEndpointAvailable(source); err != nil {
			return FailureWithCause(err.Error(), err)
		}
		var err error
		targetIndex, err = s.firstAvailableDirectNIC(targetType, targetID, source)
		if err != nil {
			return FailureWithCause(err.Error(), err)
		}
	}
	target := lab.NetworkEndpoint{Type: targetType, ID: targetID, NIC: targetIndex}
	if targetRef.NIC.Set {
		if lab.SameNetworkEndpoint(source, target) {
			return Failure("nic connect target must differ from source")
		}
		if err := s.ensureNICEndpointExists(source); err != nil {
			return FailureWithCause(err.Error(), err)
		}
		if err := s.ensureNICEndpointExists(target); err != nil {
			return FailureWithCause(err.Error(), err)
		}
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("nic connect failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	if targetRef.NIC.Set {
		if err := s.disconnectNICEndpoint(source); err != nil {
			return FailureWithCause(err.Error(), err)
		}
		if err := s.disconnectNICEndpoint(target); err != nil {
			return FailureWithCause(err.Error(), err)
		}
	}
	s.CurrentLab().NetworkLinks = append(s.CurrentLab().NetworkLinks, lab.NetworkLink{From: source, To: target})
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("nic connect failed: "+err.Error(), err)
	}
	return Success("connected direct " + sourceType + ":" + sourceID + " nic" + strconv.Itoa(sourceIndex) + " to " + targetType + ":" + targetID + " nic" + strconv.Itoa(targetIndex))
}

func (s *Service) DisconnectNIC(sourceRef NetworkEndpointRef) Result {
	if s.CurrentLab() == nil {
		return Failure("nic disconnect needs a loaded .lab file")
	}
	sourceType, ok := directEndpointType(sourceRef.Type)
	if !ok {
		return Failure("direct link target must be vm or container")
	}
	sourceID, ok := s.resolveWorkloadID(sourceType, sourceRef.ID)
	if !ok {
		return Failure(sourceType + " not found: " + sourceRef.ID)
	}
	if _, managed := s.managedDHCPContainer(sourceID); sourceType == "container" && managed {
		return Failure("DHCP service NIC cannot be disconnected; select another NAT switch instead")
	}
	if !sourceRef.NIC.Set || sourceRef.NIC.Value < 0 {
		return Failure("usage: nic disconnect <source> <index>")
	}
	index := sourceRef.NIC.Value
	endpoint := lab.NetworkEndpoint{Type: sourceType, ID: sourceID, NIC: index}
	if err := s.ensureNICEndpointExists(endpoint); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("nic disconnect failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	if err := s.disconnectNICEndpoint(endpoint); err != nil {
		return FailureWithCause(err.Error(), err)
	}
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("nic disconnect failed: "+err.Error(), err)
	}
	return Success("disconnected nic from " + sourceType + ":" + sourceID + " nic" + strconv.Itoa(index))
}

func directEndpointType(typ NetworkEndpointType) (string, bool) {
	switch typ {
	case NetworkEndpointVM:
		return "vm", true
	case NetworkEndpointContainer:
		return "container", true
	default:
		return "", false
	}
}

func (s *Service) ensureDirectEndpointAvailable(endpoint lab.NetworkEndpoint) error {
	if err := s.ensureNICEndpointExists(endpoint); err != nil {
		return err
	}
	switch endpoint.Type {
	case "vm":
		vm, _ := s.LabVM(endpoint.ID)
		nic := vm.Networks[endpoint.NIC]
		if nic.Switch != "" || nic.ExternalLink != "" || s.networkEndpointLinked(endpoint) {
			return errors.New("vm nic already connected: " + endpoint.ID + ":" + strconv.Itoa(endpoint.NIC))
		}
	case "container":
		ct, _ := s.LabContainer(endpoint.ID)
		nic := ct.Networks[endpoint.NIC]
		if nic.Switch != "" || nic.ExternalLink != "" || s.networkEndpointLinked(endpoint) {
			return errors.New("container nic already connected: " + endpoint.ID + ":" + strconv.Itoa(endpoint.NIC))
		}
	default:
		return errors.New("direct link target must be vm or container")
	}
	return nil
}

func (s *Service) ensureNICEndpointExists(endpoint lab.NetworkEndpoint) error {
	switch endpoint.Type {
	case "vm":
		vm, ok := s.LabVM(endpoint.ID)
		if !ok {
			return errors.New("vm not found: " + endpoint.ID)
		}
		if endpoint.NIC < 0 || endpoint.NIC >= len(vm.Networks) {
			return errors.New("vm nic not found: " + endpoint.ID + ":" + strconv.Itoa(endpoint.NIC))
		}
	case "container":
		ct, ok := s.LabContainer(endpoint.ID)
		if !ok {
			return errors.New("container not found: " + endpoint.ID)
		}
		if endpoint.NIC < 0 || endpoint.NIC >= len(ct.Networks) {
			return errors.New("container nic not found: " + endpoint.ID + ":" + strconv.Itoa(endpoint.NIC))
		}
	default:
		return errors.New("direct link target must be vm or container")
	}
	return nil
}

func (s *Service) disconnectNICEndpoint(endpoint lab.NetworkEndpoint) error {
	if err := s.ensureNICEndpointExists(endpoint); err != nil {
		return err
	}
	s.removeNetworkLinksForEndpoint(endpoint)
	switch endpoint.Type {
	case "vm":
		for i := range s.CurrentLab().VMs {
			if s.CurrentLab().VMs[i].ID != endpoint.ID {
				continue
			}
			s.CurrentLab().VMs[i].Networks[endpoint.NIC].Switch = ""
			s.CurrentLab().VMs[i].Networks[endpoint.NIC].ExternalLink = ""
			return nil
		}
	case "container":
		for i := range s.CurrentLab().Containers {
			if s.CurrentLab().Containers[i].ID != endpoint.ID {
				continue
			}
			s.CurrentLab().Containers[i].Networks[endpoint.NIC].Switch = ""
			s.CurrentLab().Containers[i].Networks[endpoint.NIC].ExternalLink = ""
			return nil
		}
	}
	return nil
}

func (s *Service) firstAvailableDirectNIC(typ, id string, source lab.NetworkEndpoint) (int, error) {
	switch typ {
	case "vm":
		vm, ok := s.LabVM(id)
		if !ok {
			return 0, errors.New("vm not found: " + id)
		}
		for i, nic := range vm.Networks {
			endpoint := lab.NetworkEndpoint{Type: typ, ID: id, NIC: i}
			if lab.SameNetworkEndpoint(endpoint, source) {
				continue
			}
			if nic.Switch == "" && nic.ExternalLink == "" && !s.networkEndpointLinked(endpoint) {
				return i, nil
			}
		}
		return 0, errors.New("add nic to vm:" + id + " first")
	case "container":
		ct, ok := s.LabContainer(id)
		if !ok {
			return 0, errors.New("container not found: " + id)
		}
		for i, nic := range ct.Networks {
			endpoint := lab.NetworkEndpoint{Type: typ, ID: id, NIC: i}
			if lab.SameNetworkEndpoint(endpoint, source) {
				continue
			}
			if nic.Switch == "" && nic.ExternalLink == "" && !s.networkEndpointLinked(endpoint) {
				return i, nil
			}
		}
		return 0, errors.New("add nic to container:" + id + " first")
	default:
		return 0, errors.New("direct link target must be vm or container")
	}
}

func (s *Service) networkEndpointLinked(endpoint lab.NetworkEndpoint) bool {
	for _, link := range s.CurrentLab().NetworkLinks {
		if lab.SameNetworkEndpoint(link.From, endpoint) || lab.SameNetworkEndpoint(link.To, endpoint) {
			return true
		}
	}
	return false
}

func (s *Service) removeNetworkLinksForEndpoint(endpoint lab.NetworkEndpoint) {
	links := s.CurrentLab().NetworkLinks[:0]
	for _, link := range s.CurrentLab().NetworkLinks {
		if lab.SameNetworkEndpoint(link.From, endpoint) || lab.SameNetworkEndpoint(link.To, endpoint) {
			continue
		}
		links = append(links, link)
	}
	s.CurrentLab().NetworkLinks = links
}

func (s *Service) removeNetworkLinksForNode(typ, id string) {
	links := s.CurrentLab().NetworkLinks[:0]
	for _, link := range s.CurrentLab().NetworkLinks {
		if (link.From.Type == typ && link.From.ID == id) || (link.To.Type == typ && link.To.ID == id) {
			continue
		}
		links = append(links, link)
	}
	s.CurrentLab().NetworkLinks = links
}

func (s *Service) removeNetworkLinksForDeletedNIC(typ, id string, deletedIndex int) {
	links := s.CurrentLab().NetworkLinks[:0]
	for _, link := range s.CurrentLab().NetworkLinks {
		keepFrom, from := reindexEndpointAfterNICDelete(link.From, typ, id, deletedIndex)
		keepTo, to := reindexEndpointAfterNICDelete(link.To, typ, id, deletedIndex)
		if !keepFrom || !keepTo {
			continue
		}
		link.From = from
		link.To = to
		links = append(links, link)
	}
	s.CurrentLab().NetworkLinks = links
}

func reindexEndpointAfterNICDelete(endpoint lab.NetworkEndpoint, typ, id string, deletedIndex int) (bool, lab.NetworkEndpoint) {
	if endpoint.Type != typ || endpoint.ID != id {
		return true, endpoint
	}
	if endpoint.NIC == deletedIndex {
		return false, endpoint
	}
	if endpoint.NIC > deletedIndex {
		endpoint.NIC--
	}
	return true, endpoint
}
