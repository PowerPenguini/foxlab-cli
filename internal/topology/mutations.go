package topology

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

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

func (s *Service) VMNICAdd(id string, args map[string]string) string {
	if s.Lab == nil {
		return "vm nic add needs a loaded .lab file"
	}
	if invalid := unexpectedVMNICAddArgs(args); len(invalid) > 0 {
		return "unsupported vm nic add argument: " + invalid[0]
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		s.Lab.VMs[i].Networks = append(s.Lab.VMs[i].Networks, lab.VMNetwork{MAC: args["mac"]})
		if err := s.SaveAndRefresh(); err != nil {
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
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if index >= len(s.Lab.VMs[i].Networks) {
			return "vm nic not found: " + id + ":" + indexValue
		}
		s.removeNetworkLinksForEndpoint(lab.NetworkEndpoint{Type: "vm", ID: id, NIC: index})
		s.Lab.VMs[i].Networks[index].Switch = switchRef
		s.Lab.VMs[i].Networks[index].ExternalLink = externalRef
		if value := args["mac"]; value != "" {
			s.Lab.VMs[i].Networks[index].MAC = value
		}
		if err := s.SaveAndRefresh(); err != nil {
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
		s.Lab.VMs[i].Networks = append(s.Lab.VMs[i].Networks[:index], s.Lab.VMs[i].Networks[index+1:]...)
		s.removeNetworkLinksForDeletedNIC("vm", id, index)
		if err := s.SaveAndRefresh(); err != nil {
			return "nic delete failed: " + err.Error()
		}
		return "deleted nic from vm:" + id + " nic" + indexValue
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
	delete(s.Lab.Layout.Nodes, id)
	if err := s.SaveAndRefresh(); err != nil {
		return "delete failed: " + err.Error()
	}
	return "deleted vm:" + id
}

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

func (s *Service) ExternalCreate(id string, args map[string]string) string {
	if s.Lab == nil {
		return "external create needs a loaded .lab file"
	}
	if s.HasLabExternal(id) {
		return "external already exists: " + id
	}
	s.Lab.ExternalLinks = append(s.Lab.ExternalLinks, lab.ExternalLink{
		ID:        id,
		Name:      args["name"],
		Interface: args["interface"],
	})
	if s.Lab.Layout.Nodes == nil {
		s.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	s.Lab.Layout.Nodes[id] = lab.Position{X: 832, Y: 80 + len(s.Lab.ExternalLinks)*96}
	if err := s.SaveAndRefresh(); err != nil {
		return "external create failed: " + err.Error()
	}
	return "created external:" + id
}

func (s *Service) ExternalSet(id string, args map[string]string) string {
	if s.Lab == nil {
		return "external set needs a loaded .lab file"
	}
	for i := range s.Lab.ExternalLinks {
		if s.Lab.ExternalLinks[i].ID != id {
			continue
		}
		if value := args["name"]; value != "" {
			s.Lab.ExternalLinks[i].Name = value
		}
		if value := args["interface"]; value != "" {
			s.Lab.ExternalLinks[i].Interface = value
		}
		if err := s.SaveAndRefresh(); err != nil {
			return "external config failed: " + err.Error()
		}
		return "configured external:" + id
	}
	return "external not found: " + id
}

func (s *Service) ExternalDelete(id string) string {
	if s.Lab == nil {
		return "external delete needs a loaded .lab file"
	}
	if id == "" {
		return "usage: external delete <id>"
	}
	found := false
	links := s.Lab.ExternalLinks[:0]
	for _, link := range s.Lab.ExternalLinks {
		if link.ID == id {
			found = true
			continue
		}
		links = append(links, link)
	}
	if !found {
		return "external not found: " + id
	}
	s.Lab.ExternalLinks = links
	for i := range s.Lab.Switches {
		if s.Lab.Switches[i].ExternalLink == id {
			s.Lab.Switches[i].ExternalLink = ""
			if s.Lab.Switches[i].Mode == "macnat-bridge" {
				s.Lab.Switches[i].Mode = "bridge"
			}
		}
	}
	for i := range s.Lab.VMs {
		for j := range s.Lab.VMs[i].Networks {
			if s.Lab.VMs[i].Networks[j].ExternalLink == id {
				s.Lab.VMs[i].Networks[j].ExternalLink = ""
			}
		}
	}
	delete(s.Lab.Layout.Nodes, id)
	if err := s.SaveAndRefresh(); err != nil {
		return "external delete failed: " + err.Error()
	}
	return "deleted external:" + id
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
		Command: splitCommand(args["command"]),
		Env:     parseEnv(args["env"]),
	}
	switchRef := args["switch"]
	if switchRef == "" && len(s.Lab.Switches) > 0 {
		switchRef = s.Lab.Switches[0].ID
	}
	if switchRef != "" {
		ct.Networks = append(ct.Networks, lab.ContainerNetwork{Switch: switchRef, MAC: args["mac"]})
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
		if value := args["command"]; value != "" {
			s.Lab.Containers[i].Command = splitCommand(value)
		}
		if value := args["env"]; value != "" {
			s.Lab.Containers[i].Env = parseEnv(value)
		}
		if value := args["switch"]; value != "" {
			s.removeNetworkLinksForNode("container", id)
			s.Lab.Containers[i].Networks = []lab.ContainerNetwork{{Switch: value, MAC: args["mac"]}}
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

func (s *Service) ContainerNICAdd(id string, args map[string]string) string {
	if s.Lab == nil {
		return "container nic add needs a loaded .lab file"
	}
	if invalid := unexpectedContainerNICAddArgs(args); len(invalid) > 0 {
		return "unsupported container nic add argument: " + invalid[0]
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		s.Lab.Containers[i].Networks = append(s.Lab.Containers[i].Networks, lab.ContainerNetwork{MAC: args["mac"]})
		if err := s.SaveAndRefresh(); err != nil {
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
	switchRef, err := s.resolveContainerNICEndpoint(args)
	if err != nil {
		return err.Error()
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if index >= len(s.Lab.Containers[i].Networks) {
			return "container nic not found: " + id + ":" + indexValue
		}
		s.removeNetworkLinksForEndpoint(lab.NetworkEndpoint{Type: "container", ID: id, NIC: index})
		s.Lab.Containers[i].Networks[index].Switch = switchRef
		if value := args["mac"]; value != "" {
			s.Lab.Containers[i].Networks[index].MAC = value
		}
		if err := s.SaveAndRefresh(); err != nil {
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
		s.Lab.Containers[i].Networks = append(s.Lab.Containers[i].Networks[:index], s.Lab.Containers[i].Networks[index+1:]...)
		s.removeNetworkLinksForDeletedNIC("container", id, index)
		if err := s.SaveAndRefresh(); err != nil {
			return "container nic delete failed: " + err.Error()
		}
		return "deleted nic from container:" + id + " nic" + indexValue
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
	delete(s.Lab.Layout.Nodes, id)
	if err := s.SaveAndRefresh(); err != nil {
		return "container delete failed: " + err.Error()
	}
	return "deleted container:" + id
}

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

func unexpectedVMCreateArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"name":     {},
		"cpus":     {},
		"memory":   {},
		"mem":      {},
		"disk":     {},
		"switch":   {},
		"external": {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedVMSetArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"name":     {},
		"disk":     {},
		"iso":      {},
		"vnc":      {},
		"cpus":     {},
		"memory":   {},
		"mem":      {},
		"switch":   {},
		"external": {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedVMNICAddArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"mac": {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedVMNICConnectArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"to":       {},
		"target":   {},
		"switch":   {},
		"external": {},
		"mac":      {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedContainerNICAddArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"mac": {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func unexpectedContainerNICConnectArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"to":     {},
		"target": {},
		"switch": {},
		"mac":    {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
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

func (s *Service) resolveContainerNICEndpoint(args map[string]string) (string, error) {
	target := firstNonEmpty(args["to"], args["target"], args["switch"])
	if target == "" {
		return "", errors.New("container nic connect needs endpoint")
	}
	if s.HasLabExternal(target) {
		return "", errors.New("container nic connect needs switch endpoint: " + target)
	}
	if !s.HasLabSwitch(target) {
		return "", errors.New("endpoint not found: " + target)
	}
	return target, nil
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
		if ct.Networks[endpoint.NIC].Switch != "" || s.networkEndpointLinked(endpoint) {
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
			if nic.Switch == "" && !s.networkEndpointLinked(endpoint) {
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

func nicIndexArg(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

func intArg(args map[string]string, key string, fallback int) int {
	if value, ok := positiveInt(args[key]); ok {
		return value
	}
	return fallback
}

func positiveInt(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}

func boolArg(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func unexpectedContainerArgs(args map[string]string) []string {
	valid := map[string]struct{}{
		"name":    {},
		"image":   {},
		"command": {},
		"env":     {},
		"switch":  {},
		"mac":     {},
	}
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	return invalid
}

func splitCommand(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Fields(value)
}

func parseEnv(value string) map[string]string {
	if value == "" {
		return nil
	}
	out := map[string]string{}
	for _, pair := range strings.Split(value, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(key) != "" {
			out[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
