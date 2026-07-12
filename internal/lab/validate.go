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

	diskIDs := map[string]struct{}{}
	diskKinds := map[string]string{}
	for _, disk := range l.Disks {
		if !validID(disk.ID) {
			problems = append(problems, fmt.Sprintf("disk %q has invalid id", disk.ID))
		}
		if _, exists := diskIDs[disk.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate disk id %q", disk.ID))
		}
		diskIDs[disk.ID] = struct{}{}
		diskKinds[disk.ID] = normalizedDiskKind(disk)
	}
	for _, disk := range l.Disks {
		if disk.Path == "" {
			problems = append(problems, fmt.Sprintf("disk %q path is required", disk.ID))
		}
		if disk.SizeGB < 0 {
			problems = append(problems, fmt.Sprintf("disk %q sizeGB must not be negative", disk.ID))
		}
		if disk.Format != "" && disk.Format != "qcow2" && disk.Format != "raw" {
			problems = append(problems, fmt.Sprintf("disk %q format must be qcow2 or raw", disk.ID))
		}
		if disk.Kind != "" && disk.Kind != "base" && disk.Kind != "layer" && disk.Kind != "data" {
			problems = append(problems, fmt.Sprintf("disk %q kind must be base, layer or data", disk.ID))
		}
		kind := normalizedDiskKind(disk)
		if kind == "layer" && disk.Base == "" {
			problems = append(problems, fmt.Sprintf("disk %q layer requires base", disk.ID))
		}
		if kind == "data" && disk.Base != "" {
			problems = append(problems, fmt.Sprintf("disk %q data disk must not reference base", disk.ID))
		}
		if kind == "base" && disk.Base != "" {
			problems = append(problems, fmt.Sprintf("disk %q base disk must not reference base", disk.ID))
		}
		if disk.Base != "" {
			baseKind, baseExists := diskKinds[disk.Base]
			switch {
			case disk.Base == disk.ID:
				problems = append(problems, fmt.Sprintf("disk %q must not use itself as base", disk.ID))
			case !baseExists:
				problems = append(problems, fmt.Sprintf("disk %q references missing base disk %q", disk.ID, disk.Base))
			case baseKind != "base":
				problems = append(problems, fmt.Sprintf("disk %q base disk %q must be a base disk", disk.ID, disk.Base))
			}
		}
		if disk.AttachedType == "" && disk.AttachedTo != "" {
			problems = append(problems, fmt.Sprintf("disk %q attachedTo requires attachedType", disk.ID))
		}
		switch disk.AttachedType {
		case "":
		case "vm":
			if _, ok := vmIDs[disk.AttachedTo]; !ok {
				problems = append(problems, fmt.Sprintf("disk %q references missing vm %q", disk.ID, disk.AttachedTo))
			}
			if disk.Kind == "data" {
				problems = append(problems, fmt.Sprintf("disk %q data disk cannot attach to vm", disk.ID))
			}
		case "container":
			if _, ok := containerIDs[disk.AttachedTo]; !ok {
				problems = append(problems, fmt.Sprintf("disk %q references missing container %q", disk.ID, disk.AttachedTo))
			}
		default:
			problems = append(problems, fmt.Sprintf("disk %q attachedType must be vm or container", disk.ID))
		}
	}
	attachedDisks := map[string]string{}
	for _, disk := range l.Disks {
		if disk.AttachedType == "" || disk.AttachedTo == "" {
			continue
		}
		key := disk.AttachedType + ":" + disk.AttachedTo
		displayKey := workloadDisplayRef(disk.AttachedType, disk.AttachedTo, vmNames, containerNames)
		if existing, exists := attachedDisks[key]; exists {
			problems = append(problems, fmt.Sprintf("disks %q and %q are both attached to %s", existing, disk.ID, displayKey))
			continue
		}
		attachedDisks[key] = disk.ID
		if workloadDisk := l.attachedWorkloadDiskPath(disk.AttachedType, disk.AttachedTo); workloadDisk == "" {
			problems = append(problems, fmt.Sprintf("disk %q is attached to %s but workload disk is empty", disk.ID, displayKey))
		} else if l.ResolvePath(workloadDisk) != l.ResolvePath(disk.Path) {
			problems = append(problems, fmt.Sprintf("disk %q attachment path does not match %s disk", disk.ID, displayKey))
		}
	}

	linkedNICs := map[string]struct{}{}
	for _, link := range l.NetworkLinks {
		endpoints := []NetworkEndpoint{link.From, link.To}
		if networkEndpointKey(link.From) == networkEndpointKey(link.To) {
			problems = append(problems, "network link endpoints must be different")
			continue
		}
		for _, endpoint := range endpoints {
			key := networkEndpointKey(endpoint)
			if _, exists := linkedNICs[key]; exists {
				problems = append(problems, fmt.Sprintf("network endpoint %s is linked more than once", key))
			}
			switch endpoint.Type {
			case "vm":
				vm, ok := findVMByID(l.VMs, endpoint.ID)
				if !ok {
					problems = append(problems, fmt.Sprintf("network link references missing vm %q", endpoint.ID))
					continue
				}
				if endpoint.NIC < 0 || endpoint.NIC >= len(vm.Networks) {
					problems = append(problems, fmt.Sprintf("network link references missing vm nic %q:%d", endpoint.ID, endpoint.NIC))
					continue
				}
				nic := vm.Networks[endpoint.NIC]
				if nic.Switch != "" || nic.ExternalLink != "" {
					problems = append(problems, fmt.Sprintf("network link endpoint vm %q nic %d is already connected", endpoint.ID, endpoint.NIC))
					continue
				}
			case "container":
				ct, ok := findContainerByID(l.Containers, endpoint.ID)
				if !ok {
					problems = append(problems, fmt.Sprintf("network link references missing container %q", endpoint.ID))
					continue
				}
				if endpoint.NIC < 0 || endpoint.NIC >= len(ct.Networks) {
					problems = append(problems, fmt.Sprintf("network link references missing container nic %q:%d", endpoint.ID, endpoint.NIC))
					continue
				}
				nic := ct.Networks[endpoint.NIC]
				if nic.Switch != "" || nic.ExternalLink != "" {
					problems = append(problems, fmt.Sprintf("network link endpoint container %q nic %d is already connected", endpoint.ID, endpoint.NIC))
					continue
				}
			default:
				problems = append(problems, fmt.Sprintf("network link references unknown endpoint type %q", endpoint.Type))
				continue
			}
			linkedNICs[key] = struct{}{}
		}
	}

	for id := range l.Layout.Nodes {
		if _, ok := vmIDs[id]; ok {
			continue
		}
		if _, ok := switchIDs[id]; ok {
			continue
		}
		if _, ok := externalLinkIDs[id]; ok {
			continue
		}
		if _, ok := containerIDs[id]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf("layout references missing node %q", id))
	}
	for _, link := range l.Layout.Links {
		for _, endpoint := range []LayoutEndpoint{link.From, link.To} {
			switch endpoint.Type {
			case "vm":
				if _, ok := vmIDs[endpoint.ID]; !ok {
					problems = append(problems, fmt.Sprintf("layout link references missing vm %q", endpoint.ID))
				}
			case "switch":
				if _, ok := switchIDs[endpoint.ID]; !ok {
					problems = append(problems, fmt.Sprintf("layout link references missing switch %q", endpoint.ID))
				}
			case "external":
				if _, ok := externalLinkIDs[endpoint.ID]; !ok {
					problems = append(problems, fmt.Sprintf("layout link references missing external link %q", endpoint.ID))
				}
			case "container":
				if _, ok := containerIDs[endpoint.ID]; !ok {
					problems = append(problems, fmt.Sprintf("layout link references missing container %q", endpoint.ID))
				}
			default:
				problems = append(problems, fmt.Sprintf("layout link references unknown node type %q", endpoint.Type))
			}
		}
	}

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
