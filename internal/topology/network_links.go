package topology

import (
	"errors"
	"strconv"

	"foxlab-cli/internal/lab"
)

func (s *Service) NICConnectDirect(sourceType, sourceID, indexValue, targetType, targetID string) string {
	if s.Lab == nil {
		return "nic connect needs a loaded .lab file"
	}
	sourceIndex, ok := nicIndexArg(indexValue)
	if !ok {
		return "usage: nic connect <source> <index> <target>"
	}
	source := lab.NetworkEndpoint{Type: sourceType, ID: sourceID, NIC: sourceIndex}
	if err := s.ensureDirectEndpointAvailable(source); err != nil {
		return err.Error()
	}
	targetIndex, err := s.firstAvailableDirectNIC(targetType, targetID, source)
	if err != nil {
		return err.Error()
	}
	target := lab.NetworkEndpoint{Type: targetType, ID: targetID, NIC: targetIndex}
	s.Lab.NetworkLinks = append(s.Lab.NetworkLinks, lab.NetworkLink{From: source, To: target})
	if err := s.SaveAndRefresh(); err != nil {
		return "nic connect failed: " + err.Error()
	}
	return "connected direct " + sourceType + ":" + sourceID + " nic" + indexValue + " to " + targetType + ":" + targetID + " nic" + strconv.Itoa(targetIndex)
}

func (s *Service) NICConnectDirectTo(sourceType, sourceID, sourceIndexValue, targetType, targetID, targetIndexValue string) string {
	if s.Lab == nil {
		return "nic connect needs a loaded .lab file"
	}
	sourceIndex, ok := nicIndexArg(sourceIndexValue)
	if !ok {
		return "usage: nic connect <source> <index> <target> <target-index>"
	}
	targetIndex, ok := nicIndexArg(targetIndexValue)
	if !ok {
		return "usage: nic connect <source> <index> <target> <target-index>"
	}
	source := lab.NetworkEndpoint{Type: sourceType, ID: sourceID, NIC: sourceIndex}
	target := lab.NetworkEndpoint{Type: targetType, ID: targetID, NIC: targetIndex}
	if sameNetworkEndpoint(source, target) {
		return "nic connect target must differ from source"
	}
	if err := s.ensureNICEndpointExists(source); err != nil {
		return err.Error()
	}
	if err := s.ensureNICEndpointExists(target); err != nil {
		return err.Error()
	}
	if err := s.disconnectNICEndpoint(source); err != nil {
		return err.Error()
	}
	if err := s.disconnectNICEndpoint(target); err != nil {
		return err.Error()
	}
	s.Lab.NetworkLinks = append(s.Lab.NetworkLinks, lab.NetworkLink{From: source, To: target})
	if err := s.SaveAndRefresh(); err != nil {
		return "nic connect failed: " + err.Error()
	}
	return "connected direct " + sourceType + ":" + sourceID + " nic" + sourceIndexValue + " to " + targetType + ":" + targetID + " nic" + targetIndexValue
}

func (s *Service) NICDisconnect(sourceType, sourceID, indexValue string) string {
	if s.Lab == nil {
		return "nic disconnect needs a loaded .lab file"
	}
	index, ok := nicIndexArg(indexValue)
	if !ok {
		return "usage: nic disconnect <source> <index>"
	}
	endpoint := lab.NetworkEndpoint{Type: sourceType, ID: sourceID, NIC: index}
	if err := s.disconnectNICEndpoint(endpoint); err != nil {
		return err.Error()
	}
	if err := s.SaveAndRefresh(); err != nil {
		return "nic disconnect failed: " + err.Error()
	}
	return "disconnected nic from " + sourceType + ":" + sourceID + " nic" + indexValue
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
		for i := range s.Lab.VMs {
			if s.Lab.VMs[i].ID != endpoint.ID {
				continue
			}
			s.Lab.VMs[i].Networks[endpoint.NIC].Switch = ""
			s.Lab.VMs[i].Networks[endpoint.NIC].ExternalLink = ""
			return nil
		}
	case "container":
		for i := range s.Lab.Containers {
			if s.Lab.Containers[i].ID != endpoint.ID {
				continue
			}
			s.Lab.Containers[i].Networks[endpoint.NIC].Switch = ""
			s.Lab.Containers[i].Networks[endpoint.NIC].ExternalLink = ""
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
			if sameNetworkEndpoint(endpoint, source) {
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
			if sameNetworkEndpoint(endpoint, source) {
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
	for _, link := range s.Lab.NetworkLinks {
		if sameNetworkEndpoint(link.From, endpoint) || sameNetworkEndpoint(link.To, endpoint) {
			return true
		}
	}
	return false
}

func (s *Service) removeNetworkLinksForEndpoint(endpoint lab.NetworkEndpoint) {
	links := s.Lab.NetworkLinks[:0]
	for _, link := range s.Lab.NetworkLinks {
		if sameNetworkEndpoint(link.From, endpoint) || sameNetworkEndpoint(link.To, endpoint) {
			continue
		}
		links = append(links, link)
	}
	s.Lab.NetworkLinks = links
}

func (s *Service) removeNetworkLinksForNode(typ, id string) {
	links := s.Lab.NetworkLinks[:0]
	for _, link := range s.Lab.NetworkLinks {
		if (link.From.Type == typ && link.From.ID == id) || (link.To.Type == typ && link.To.ID == id) {
			continue
		}
		links = append(links, link)
	}
	s.Lab.NetworkLinks = links
}

func (s *Service) removeNetworkLinksForDeletedNIC(typ, id string, deletedIndex int) {
	links := s.Lab.NetworkLinks[:0]
	for _, link := range s.Lab.NetworkLinks {
		keepFrom, from := reindexEndpointAfterNICDelete(link.From, typ, id, deletedIndex)
		keepTo, to := reindexEndpointAfterNICDelete(link.To, typ, id, deletedIndex)
		if !keepFrom || !keepTo {
			continue
		}
		link.From = from
		link.To = to
		links = append(links, link)
	}
	s.Lab.NetworkLinks = links
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

func sameNetworkEndpoint(a, b lab.NetworkEndpoint) bool {
	return a.Type == b.Type && a.ID == b.ID && a.NIC == b.NIC
}
