package topology

import (
	"errors"

	"foxlab-cli/internal/lab"
)

func (s *Service) VMNICAdd(id string, args map[string]string) string {
	if s.Lab == nil {
		return "vm nic add needs a loaded .lab file"
	}
	if invalid := unexpectedVMNICAddArgs(args); len(invalid) > 0 {
		return "unsupported vm nic add argument: " + invalid[0]
	}
	if err := validateNICMACArg("vm nic", args["mac"]); err != nil {
		return err.Error()
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if err := s.requireSavePath(); err != nil {
			return "nic add failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		s.Lab.VMs[i].Networks = append(s.Lab.VMs[i].Networks, lab.VMNetwork{MAC: args["mac"]})
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "nic add failed: " + err.Error()
		}
		return "added nic to vm:" + id
	}
	return "vm not found: " + id
}

func (s *Service) VMNICConnect(id, indexValue string, args map[string]string) string {
	if s.Lab == nil {
		return "vm nic connect needs a loaded .lab file"
	}
	if invalid := unexpectedVMNICConnectArgs(args); len(invalid) > 0 {
		return "unsupported vm nic connect argument: " + invalid[0]
	}
	index, ok := nicIndexArg(indexValue)
	if !ok {
		return "usage: vm nic connect <id> <index> to=ID"
	}
	switchRef, externalRef, err := s.resolveVMNICEndpoint(args)
	if err != nil {
		return err.Error()
	}
	if err := validateNICMACArg("vm nic", args["mac"]); err != nil {
		return err.Error()
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if index >= len(s.Lab.VMs[i].Networks) {
			return "vm nic not found: " + id + ":" + indexValue
		}
		if err := s.requireSavePath(); err != nil {
			return "nic connect failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		s.removeNetworkLinksForEndpoint(lab.NetworkEndpoint{Type: "vm", ID: id, NIC: index})
		s.Lab.VMs[i].Networks[index].Switch = switchRef
		s.Lab.VMs[i].Networks[index].ExternalLink = externalRef
		if value := args["mac"]; value != "" {
			s.Lab.VMs[i].Networks[index].MAC = value
		}
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "nic connect failed: " + err.Error()
		}
		return "connected nic to vm:" + id
	}
	return "vm not found: " + id
}

func (s *Service) VMNICDelete(id, indexValue string) string {
	if s.Lab == nil {
		return "vm nic delete needs a loaded .lab file"
	}
	index, ok := nicIndexArg(indexValue)
	if !ok {
		return "usage: vm nic delete <id> <index>"
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if index >= len(s.Lab.VMs[i].Networks) {
			return "vm nic not found: " + id + ":" + indexValue
		}
		if err := s.requireSavePath(); err != nil {
			return "nic delete failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		s.Lab.VMs[i].Networks = append(s.Lab.VMs[i].Networks[:index], s.Lab.VMs[i].Networks[index+1:]...)
		s.removeNetworkLinksForDeletedNIC("vm", id, index)
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "nic delete failed: " + err.Error()
		}
		return "deleted nic from vm:" + id + " nic" + indexValue
	}
	return "vm not found: " + id
}

func (s *Service) ContainerNICAdd(id string, args map[string]string) string {
	if s.Lab == nil {
		return "container nic add needs a loaded .lab file"
	}
	if invalid := unexpectedContainerNICAddArgs(args); len(invalid) > 0 {
		return "unsupported container nic add argument: " + invalid[0]
	}
	if err := validateNICMACArg("container nic", args["mac"]); err != nil {
		return err.Error()
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if err := s.requireSavePath(); err != nil {
			return "container nic add failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		s.Lab.Containers[i].Networks = append(s.Lab.Containers[i].Networks, lab.ContainerNetwork{MAC: args["mac"]})
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "container nic add failed: " + err.Error()
		}
		return "added nic to container:" + id
	}
	return "container not found: " + id
}

func (s *Service) ContainerNICConnect(id, indexValue string, args map[string]string) string {
	if s.Lab == nil {
		return "container nic connect needs a loaded .lab file"
	}
	if invalid := unexpectedContainerNICConnectArgs(args); len(invalid) > 0 {
		return "unsupported container nic connect argument: " + invalid[0]
	}
	index, ok := nicIndexArg(indexValue)
	if !ok {
		return "usage: container nic connect <id> <index> to=ID"
	}
	switchRef, externalRef, err := s.resolveContainerNICEndpoint(args)
	if err != nil {
		return err.Error()
	}
	if err := validateNICMACArg("container nic", args["mac"]); err != nil {
		return err.Error()
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if index >= len(s.Lab.Containers[i].Networks) {
			return "container nic not found: " + id + ":" + indexValue
		}
		if err := s.requireSavePath(); err != nil {
			return "container nic connect failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		s.removeNetworkLinksForEndpoint(lab.NetworkEndpoint{Type: "container", ID: id, NIC: index})
		s.Lab.Containers[i].Networks[index].Switch = switchRef
		s.Lab.Containers[i].Networks[index].ExternalLink = externalRef
		if value := args["mac"]; value != "" {
			s.Lab.Containers[i].Networks[index].MAC = value
		}
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "container nic connect failed: " + err.Error()
		}
		return "connected nic to container:" + id
	}
	return "container not found: " + id
}

func (s *Service) ContainerNICDelete(id, indexValue string) string {
	if s.Lab == nil {
		return "container nic delete needs a loaded .lab file"
	}
	index, ok := nicIndexArg(indexValue)
	if !ok {
		return "usage: container nic delete <id> <index>"
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if index >= len(s.Lab.Containers[i].Networks) {
			return "container nic not found: " + id + ":" + indexValue
		}
		if err := s.requireSavePath(); err != nil {
			return "container nic delete failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		s.Lab.Containers[i].Networks = append(s.Lab.Containers[i].Networks[:index], s.Lab.Containers[i].Networks[index+1:]...)
		s.removeNetworkLinksForDeletedNIC("container", id, index)
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "container nic delete failed: " + err.Error()
		}
		return "deleted nic from container:" + id + " nic" + indexValue
	}
	return "container not found: " + id
}

func (s *Service) resolveVMNICEndpoint(args map[string]string) (string, string, error) {
	target := firstNonEmpty(args["to"], args["target"])
	switchRef := args["switch"]
	externalRef := args["external"]
	if target != "" {
		if switchRef != "" || externalRef != "" {
			return "", "", errors.New("vm nic connect accepts to=ID or a compatibility alias, not both")
		}
		switch {
		case s.HasLabSwitch(target):
			return target, "", nil
		case s.HasLabExternal(target):
			return "", target, nil
		default:
			return "", "", errors.New("endpoint not found: " + target)
		}
	}
	if (switchRef == "") == (externalRef == "") {
		return "", "", errors.New("vm nic connect needs exactly one endpoint")
	}
	if switchRef != "" && !s.HasLabSwitch(switchRef) {
		return "", "", errors.New("switch not found: " + switchRef)
	}
	if externalRef != "" && !s.HasLabExternal(externalRef) {
		return "", "", errors.New("external not found: " + externalRef)
	}
	return switchRef, externalRef, nil
}

func (s *Service) resolveContainerNICEndpoint(args map[string]string) (string, string, error) {
	target := firstNonEmpty(args["to"], args["target"])
	switchRef := args["switch"]
	externalRef := args["external"]
	if target == "" {
		if (switchRef == "") == (externalRef == "") {
			return "", "", errors.New("container nic connect needs exactly one endpoint")
		}
		if switchRef != "" && !s.HasLabSwitch(switchRef) {
			return "", "", errors.New("switch not found: " + switchRef)
		}
		if externalRef != "" && !s.HasLabExternal(externalRef) {
			return "", "", errors.New("external not found: " + externalRef)
		}
		return switchRef, externalRef, nil
	}
	if switchRef != "" || externalRef != "" {
		return "", "", errors.New("container nic connect accepts to=ID or a compatibility alias, not both")
	}
	switch {
	case s.HasLabSwitch(target):
		return target, "", nil
	case s.HasLabExternal(target):
		return "", target, nil
	default:
		return "", "", errors.New("endpoint not found: " + target)
	}
}
