package topology

import (
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
	diskPath := firstNonEmpty(args["disk"], filepath.ToSlash(filepath.Join("labs", s.Lab.ID, "disks", id+".qcow2")))
	vm := lab.VM{
		ID:       id,
		Name:     firstNonEmpty(args["name"], id),
		MemoryMB: memory,
		CPUs:     cpus,
		Disk:     diskPath,
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
		if value := args["disk"]; value != "" {
			s.Lab.VMs[i].Disk = value
		}
		if value := args["iso"]; value != "" {
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
			s.Lab.VMs[i].Networks = []lab.VMNetwork{{Switch: value}}
		}
		if value := args["external"]; value != "" {
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
		networks := s.Lab.VMs[i].Networks[:0]
		for _, nic := range s.Lab.VMs[i].Networks {
			if nic.Switch != id {
				networks = append(networks, nic)
			}
		}
		s.Lab.VMs[i].Networks = networks
	}
	for i := range s.Lab.Containers {
		networks := s.Lab.Containers[i].Networks[:0]
		for _, nic := range s.Lab.Containers[i].Networks {
			if nic.Switch != id {
				networks = append(networks, nic)
			}
		}
		s.Lab.Containers[i].Networks = networks
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
		networks := s.Lab.VMs[i].Networks[:0]
		for _, nic := range s.Lab.VMs[i].Networks {
			if nic.ExternalLink != id {
				networks = append(networks, nic)
			}
		}
		s.Lab.VMs[i].Networks = networks
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
	image := args["image"]
	if image == "" {
		return "container image is required"
	}
	ct := lab.Container{
		ID:      id,
		Name:    firstNonEmpty(args["name"], id),
		Image:   image,
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
	delete(s.Lab.Layout.Nodes, id)
	if err := s.SaveAndRefresh(); err != nil {
		return "container delete failed: " + err.Error()
	}
	return "deleted container:" + id
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
