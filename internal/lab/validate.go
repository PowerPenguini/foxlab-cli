package lab

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	uuidPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

func (l *Lab) Validate() error {
	var problems []string
	if l.LegacyID != "" {
		problems = append(problems, "lab must use name, not both name and id")
	}
	if !validID(l.ID) {
		problems = append(problems, "lab name must start with a letter/number and contain only letters, numbers, '_' or '-'")
	}

	nodeRefs := map[string]string{}
	switchIDs := map[string]struct{}{}
	for _, sw := range l.Switches {
		swRef := displayNodeRef(sw.ID, sw.Name)
		if problem := nodeIDProblem("switch", sw.ID); problem != "" {
			problems = append(problems, problem)
		}
		problems = append(problems, validateNodeIdentity("switch", sw.ID, sw.Name, nodeRefs)...)
		if _, exists := switchIDs[sw.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate switch id %q", sw.ID))
		}
		switchIDs[sw.ID] = struct{}{}
		if sw.Mode != "bridge" && sw.Mode != "nat" && sw.Mode != "macnat-bridge" {
			problems = append(problems, fmt.Sprintf("switch %q uses unsupported mode %q; supported modes are bridge, nat and macnat-bridge", swRef, sw.Mode))
		}
		if sw.Mode == "macnat-bridge" && len(SwitchExternalLinks(sw)) == 0 {
			problems = append(problems, fmt.Sprintf("switch %q macnat-bridge mode requires externalLinks", swRef))
		}
	}

	externalLinkIDs := map[string]struct{}{}
	for _, link := range l.ExternalLinks {
		linkRef := displayNodeRef(link.ID, link.Name)
		if problem := nodeIDProblem("external link", link.ID); problem != "" {
			problems = append(problems, problem)
		}
		problems = append(problems, validateNodeIdentity("external link", link.ID, link.Name, nodeRefs)...)
		if _, exists := externalLinkIDs[link.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate external link id %q", link.ID))
		}
		externalLinkIDs[link.ID] = struct{}{}
		if link.Interface == "" {
			problems = append(problems, fmt.Sprintf("external link %q interface is required", linkRef))
		}
		if !validExternalMode(link.Mode) {
			problems = append(problems, fmt.Sprintf("external link %q uses unsupported mode %q; supported modes are nat, direct and macnat", linkRef, link.Mode))
		}
	}

	for _, sw := range l.Switches {
		swRef := displayNodeRef(sw.ID, sw.Name)
		for _, externalID := range SwitchExternalLinks(sw) {
			if _, ok := externalLinkIDs[externalID]; !ok {
				problems = append(problems, fmt.Sprintf("switch %q references missing external link %q", swRef, externalID))
			}
		}
	}

	vmIDs := map[string]struct{}{}
	vmNames := map[string]string{}
	for _, vm := range l.VMs {
		vmRef := displayNodeRef(vm.ID, vm.Name)
		if problem := nodeIDProblem("vm", vm.ID); problem != "" {
			problems = append(problems, problem)
		}
		problems = append(problems, validateNodeIdentity("vm", vm.ID, vm.Name, nodeRefs)...)
		if _, exists := vmIDs[vm.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate vm id %q", vm.ID))
		}
		vmIDs[vm.ID] = struct{}{}
		vmNames[vm.ID] = vmRef
		if vm.MemoryMB <= 0 {
			problems = append(problems, fmt.Sprintf("vm %q memoryMB must be greater than zero", vmRef))
		}
		if vm.CPUs <= 0 {
			problems = append(problems, fmt.Sprintf("vm %q cpus must be greater than zero", vmRef))
		}
		if !validDesiredState(vm.DesiredState) {
			problems = append(problems, fmt.Sprintf("vm %q desiredState must be running or stopped", vmRef))
		}
		for _, nic := range vm.Networks {
			if !validMAC(nic.MAC) {
				problems = append(problems, fmt.Sprintf("vm %q network mac %q is invalid", vmRef, nic.MAC))
			}
			switchRef := nic.Switch != ""
			externalRef := nic.ExternalLink != ""
			if switchRef && externalRef {
				problems = append(problems, fmt.Sprintf("vm %q network must not reference both switch and externalLink", vmRef))
				continue
			}
			if switchRef {
				if _, ok := switchIDs[nic.Switch]; !ok {
					problems = append(problems, fmt.Sprintf("vm %q references missing switch %q", vmRef, nic.Switch))
				}
			}
			if externalRef {
				if _, ok := externalLinkIDs[nic.ExternalLink]; !ok {
					problems = append(problems, fmt.Sprintf("vm %q references missing external link %q", vmRef, nic.ExternalLink))
				}
			}
		}
	}

	containerIDs := map[string]struct{}{}
	containerNames := map[string]string{}
	for _, ct := range l.Containers {
		ctRef := displayNodeRef(ct.ID, ct.Name)
		if problem := nodeIDProblem("container", ct.ID); problem != "" {
			problems = append(problems, problem)
		}
		problems = append(problems, validateNodeIdentity("container", ct.ID, ct.Name, nodeRefs)...)
		if _, exists := containerIDs[ct.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate container id %q", ct.ID))
		}
		containerIDs[ct.ID] = struct{}{}
		containerNames[ct.ID] = ctRef
		if ct.Image == "" {
			problems = append(problems, fmt.Sprintf("container %q image is required", ctRef))
		}
		if !validDesiredState(ct.DesiredState) {
			problems = append(problems, fmt.Sprintf("container %q desiredState must be running or stopped", ctRef))
		}
		for _, nic := range ct.Networks {
			if !validMAC(nic.MAC) {
				problems = append(problems, fmt.Sprintf("container %q network mac %q is invalid", ctRef, nic.MAC))
			}
			switchRef := nic.Switch != ""
			externalRef := nic.ExternalLink != ""
			if switchRef && externalRef {
				problems = append(problems, fmt.Sprintf("container %q network must not reference both switch and externalLink", ctRef))
				continue
			}
			if switchRef {
				if _, ok := switchIDs[nic.Switch]; !ok {
					problems = append(problems, fmt.Sprintf("container %q references missing switch %q", ctRef, nic.Switch))
				}
			}
			if externalRef {
				if _, ok := externalLinkIDs[nic.ExternalLink]; !ok {
					problems = append(problems, fmt.Sprintf("container %q references missing external link %q", ctRef, nic.ExternalLink))
				}
			}
		}
	}

	nodeIDs := map[string]string{}
	for _, node := range []struct {
		kind string
		ids  map[string]struct{}
	}{
		{kind: "switch", ids: switchIDs},
		{kind: "external link", ids: externalLinkIDs},
		{kind: "vm", ids: vmIDs},
		{kind: "container", ids: containerIDs},
	} {
		for id := range node.ids {
			if existing, exists := nodeIDs[id]; exists {
				problems = append(problems, fmt.Sprintf("node id %q is used by both %s and %s", id, existing, node.kind))
				continue
			}
			nodeIDs[id] = node.kind
		}
	}
	problems = append(problems, validateManagedNameCollisions(l)...)

	index := validationIndex{
		switchIDs:       switchIDs,
		externalLinkIDs: externalLinkIDs,
		vmIDs:           vmIDs,
		containerIDs:    containerIDs,
		vmNames:         vmNames,
		containerNames:  containerNames,
	}
	problems = append(problems, validateDisks(l, index)...)
	problems = append(problems, validateNetworkLinks(l)...)
	problems = append(problems, validateLayout(l, index)...)

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func validID(id string) bool {
	return idPattern.MatchString(id)
}

func validNodeID(id string) bool {
	return validID(id) && !uuidPattern.MatchString(id)
}

func nodeIDProblem(kind, id string) string {
	if uuidPattern.MatchString(id) {
		return fmt.Sprintf("%s %q uses a UUID id; use a mnemonic id", kind, id)
	}
	if !validNodeID(id) {
		return fmt.Sprintf("%s %q has invalid mnemonic id", kind, id)
	}
	return ""
}

func validateNodeIdentity(kind, id, name string, seen map[string]string) []string {
	var problems []string
	register := func(value, role string) {
		key := strings.ToLower(value)
		if existing, exists := seen[key]; exists {
			problems = append(problems, fmt.Sprintf("duplicate node reference %q used by %s and %s %q", value, existing, kind, id))
			return
		}
		seen[key] = kind + " " + id + " " + role
	}
	if id != "" {
		register(id, "id")
	}
	if name == "" || strings.EqualFold(name, id) {
		return problems
	}
	if !validNodeID(name) {
		problems = append(problems, fmt.Sprintf("%s %q has invalid mnemonic name %q", kind, id, name))
		return problems
	}
	register(name, "name")
	return problems
}

func validateManagedNameCollisions(l *Lab) []string {
	if l == nil {
		return nil
	}
	var problems []string
	register := func(seen map[string]string, name, ref string) {
		key := strings.ToLower(name)
		if existing, exists := seen[key]; exists {
			problems = append(problems, fmt.Sprintf("managed runtime name %q collides for %s and %s", name, existing, ref))
			return
		}
		seen[key] = ref
	}
	domains := map[string]string{}
	for _, vm := range l.VMs {
		register(domains, l.ManagedDomainName(vm), "vm:"+vm.ID)
	}
	containers := map[string]string{}
	for _, ct := range l.Containers {
		register(containers, l.ManagedContainerName(ct), "container:"+ct.ID)
	}
	bridges := map[string]string{}
	for _, sw := range l.Switches {
		register(bridges, l.ManagedSwitchBridgeName(sw), "switch:"+sw.ID)
	}
	for _, link := range l.ExternalLinks {
		register(bridges, l.ManagedExternalBridgeName(link), "external:"+link.ID)
	}
	for _, link := range l.NetworkLinks {
		register(bridges, l.ManagedNetworkLinkBridgeName(link), "network link:"+networkEndpointKey(link.From)+"-"+networkEndpointKey(link.To))
	}
	return problems
}

func displayNodeRef(id, name string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	return id
}

func workloadDisplayRef(kind, id string, vmNames, containerNames map[string]string) string {
	switch kind {
	case "vm":
		if name := vmNames[id]; name != "" {
			return "vm:" + name
		}
	case "container":
		if name := containerNames[id]; name != "" {
			return "container:" + name
		}
	}
	return kind + ":" + id
}

func normalizedDiskKind(disk Disk) string {
	if disk.Kind == "" {
		return "base"
	}
	return disk.Kind
}

func ValidID(id string) bool {
	return validID(id)
}

func ValidNodeID(id string) bool {
	return validNodeID(id)
}

func ValidMAC(value string) bool {
	return validMAC(value)
}

func validDesiredState(value string) bool {
	switch normalizeDesiredState(value) {
	case "", DesiredStateRunning, DesiredStateStopped:
		return true
	default:
		return false
	}
}

func validExternalMode(value string) bool {
	switch normalizeExternalMode(value) {
	case ExternalModeNAT, ExternalModeDirect, ExternalModeMacNAT:
		return true
	default:
		return false
	}
}

func validMAC(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	_, ok := parseNICMAC(value)
	return ok
}

func findVMByID(vms []VM, id string) (VM, bool) {
	for _, vm := range vms {
		if vm.ID == id {
			return vm, true
		}
	}
	return VM{}, false
}

func findContainerByID(containers []Container, id string) (Container, bool) {
	for _, ct := range containers {
		if ct.ID == id {
			return ct, true
		}
	}
	return Container{}, false
}

func (l *Lab) attachedWorkloadDiskPath(attachedType, attachedTo string) string {
	switch attachedType {
	case "vm":
		if vm, ok := findVMByID(l.VMs, attachedTo); ok {
			return vm.Disk
		}
	case "container":
		if ct, ok := findContainerByID(l.Containers, attachedTo); ok {
			return ct.Disk
		}
	}
	return ""
}
