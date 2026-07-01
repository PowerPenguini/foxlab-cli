package lab

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func (l *Lab) Validate() error {
	var problems []string
	if l.LegacyID != "" {
		problems = append(problems, "lab must use name, not both name and id")
	}
	if !validID(l.ID) {
		problems = append(problems, "lab name must start with a letter/number and contain only letters, numbers, '_' or '-'")
	}

	switchIDs := map[string]struct{}{}
	for _, sw := range l.Switches {
		if !validID(sw.ID) {
			problems = append(problems, fmt.Sprintf("switch %q has invalid id", sw.ID))
		}
		if _, exists := switchIDs[sw.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate switch id %q", sw.ID))
		}
		switchIDs[sw.ID] = struct{}{}
		if sw.Mode != "bridge" && sw.Mode != "nat" && sw.Mode != "macnat-bridge" {
			problems = append(problems, fmt.Sprintf("switch %q uses unsupported mode %q; supported modes are bridge, nat and macnat-bridge", sw.ID, sw.Mode))
		}
		if sw.Mode == "macnat-bridge" && len(SwitchExternalLinks(sw)) == 0 {
			problems = append(problems, fmt.Sprintf("switch %q macnat-bridge mode requires externalLinks", sw.ID))
		}
	}

	externalLinkIDs := map[string]struct{}{}
	for _, link := range l.ExternalLinks {
		if !validID(link.ID) {
			problems = append(problems, fmt.Sprintf("external link %q has invalid id", link.ID))
		}
		if _, exists := externalLinkIDs[link.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate external link id %q", link.ID))
		}
		externalLinkIDs[link.ID] = struct{}{}
		if link.Interface == "" {
			problems = append(problems, fmt.Sprintf("external link %q interface is required", link.ID))
		}
		if !validExternalMode(link.Mode) {
			problems = append(problems, fmt.Sprintf("external link %q uses unsupported mode %q; supported modes are nat, direct and macnat", link.ID, link.Mode))
		}
	}

	for _, sw := range l.Switches {
		for _, externalID := range SwitchExternalLinks(sw) {
			if _, ok := externalLinkIDs[externalID]; !ok {
				problems = append(problems, fmt.Sprintf("switch %q references missing external link %q", sw.ID, externalID))
			}
		}
	}

	vmIDs := map[string]struct{}{}
	for _, vm := range l.VMs {
		if !validID(vm.ID) {
			problems = append(problems, fmt.Sprintf("vm %q has invalid id", vm.ID))
		}
		if _, exists := vmIDs[vm.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate vm id %q", vm.ID))
		}
		vmIDs[vm.ID] = struct{}{}
		if vm.MemoryMB <= 0 {
			problems = append(problems, fmt.Sprintf("vm %q memoryMB must be greater than zero", vm.ID))
		}
		if vm.CPUs <= 0 {
			problems = append(problems, fmt.Sprintf("vm %q cpus must be greater than zero", vm.ID))
		}
		if !validDesiredState(vm.DesiredState) {
			problems = append(problems, fmt.Sprintf("vm %q desiredState must be running or stopped", vm.ID))
		}
		for _, nic := range vm.Networks {
			if !validMAC(nic.MAC) {
				problems = append(problems, fmt.Sprintf("vm %q network mac %q is invalid", vm.ID, nic.MAC))
			}
			switchRef := nic.Switch != ""
			externalRef := nic.ExternalLink != ""
			if switchRef && externalRef {
				problems = append(problems, fmt.Sprintf("vm %q network must not reference both switch and externalLink", vm.ID))
				continue
			}
			if switchRef {
				if _, ok := switchIDs[nic.Switch]; !ok {
					problems = append(problems, fmt.Sprintf("vm %q references missing switch %q", vm.ID, nic.Switch))
				}
			}
			if externalRef {
				if _, ok := externalLinkIDs[nic.ExternalLink]; !ok {
					problems = append(problems, fmt.Sprintf("vm %q references missing external link %q", vm.ID, nic.ExternalLink))
				}
			}
		}
	}

	containerIDs := map[string]struct{}{}
	for _, ct := range l.Containers {
		if !validID(ct.ID) {
			problems = append(problems, fmt.Sprintf("container %q has invalid id", ct.ID))
		}
		if _, exists := containerIDs[ct.ID]; exists {
			problems = append(problems, fmt.Sprintf("duplicate container id %q", ct.ID))
		}
		containerIDs[ct.ID] = struct{}{}
		if ct.Image == "" {
			problems = append(problems, fmt.Sprintf("container %q image is required", ct.ID))
		}
		if !validDesiredState(ct.DesiredState) {
			problems = append(problems, fmt.Sprintf("container %q desiredState must be running or stopped", ct.ID))
		}
		for _, nic := range ct.Networks {
			if !validMAC(nic.MAC) {
				problems = append(problems, fmt.Sprintf("container %q network mac %q is invalid", ct.ID, nic.MAC))
			}
			switchRef := nic.Switch != ""
			externalRef := nic.ExternalLink != ""
			if switchRef && externalRef {
				problems = append(problems, fmt.Sprintf("container %q network must not reference both switch and externalLink", ct.ID))
				continue
			}
			if switchRef {
				if _, ok := switchIDs[nic.Switch]; !ok {
					problems = append(problems, fmt.Sprintf("container %q references missing switch %q", ct.ID, nic.Switch))
				}
			}
			if externalRef {
				if _, ok := externalLinkIDs[nic.ExternalLink]; !ok {
					problems = append(problems, fmt.Sprintf("container %q references missing external link %q", ct.ID, nic.ExternalLink))
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
		if existing, exists := attachedDisks[key]; exists {
			problems = append(problems, fmt.Sprintf("disks %q and %q are both attached to %s", existing, disk.ID, key))
			continue
		}
		attachedDisks[key] = disk.ID
		if workloadDisk := l.attachedWorkloadDiskPath(disk.AttachedType, disk.AttachedTo); workloadDisk == "" {
			problems = append(problems, fmt.Sprintf("disk %q is attached to %s but workload disk is empty", disk.ID, key))
		} else if l.ResolvePath(workloadDisk) != l.ResolvePath(disk.Path) {
			problems = append(problems, fmt.Sprintf("disk %q attachment path does not match %s disk", disk.ID, key))
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

func normalizedDiskKind(disk Disk) string {
	if disk.Kind == "" {
		return "base"
	}
	return disk.Kind
}

func ValidID(id string) bool {
	return validID(id)
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
