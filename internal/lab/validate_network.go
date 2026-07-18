package lab

import "fmt"

func validateNetworkLinks(l *Lab) []string {
	var problems []string
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
	return problems
}

func validateLayout(l *Lab, index validationIndex) []string {
	var problems []string
	vmIDs := index.vmIDs
	switchIDs := index.switchIDs
	externalLinkIDs := index.externalLinkIDs
	containerIDs := index.containerIDs
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
	return problems
}
