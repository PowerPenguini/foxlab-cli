package lab

import (
	"errors"
	"fmt"
	"strings"
)

// ValidateManagedDHCPContainer rejects durable settings owned by FoxLab.
// The runtime supplies the dnsmasq command and required capabilities.
func ValidateManagedDHCPContainer(ct Container) error {
	if !IsDHCPContainer(ct) {
		return nil
	}
	problems := managedDHCPContainerProblems(ct, displayNodeRef(ct.ID, ct.Name))
	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "; "))
}

func managedDHCPContainerProblems(ct Container, ctRef string) []string {
	var problems []string
	if ct.Image != DefaultDHCPImage {
		problems = append(problems, fmt.Sprintf("DHCP container %q image is managed by FoxLab and must be %q", ctRef, DefaultDHCPImage))
	}
	if len(ct.Command) != 0 {
		problems = append(problems, fmt.Sprintf("DHCP container %q command is managed by FoxLab", ctRef))
	}
	if strings.TrimSpace(ct.Shell) != "" {
		problems = append(problems, fmt.Sprintf("DHCP container %q does not expose a configurable shell", ctRef))
	}
	if len(ct.Env) != 0 {
		problems = append(problems, fmt.Sprintf("DHCP container %q environment is managed by FoxLab", ctRef))
	}
	if ct.Capabilities != nil {
		problems = append(problems, fmt.Sprintf("DHCP container %q capabilities are managed by FoxLab", ctRef))
	}
	if strings.TrimSpace(ct.Disk) != "" {
		problems = append(problems, fmt.Sprintf("DHCP container %q does not support disks", ctRef))
	}
	for _, network := range ct.Networks {
		if strings.TrimSpace(network.MAC) != "" {
			problems = append(problems, fmt.Sprintf("DHCP container %q network MAC is managed by FoxLab", ctRef))
			break
		}
	}
	return problems
}

func validateDHCPContainers(l *Lab) []string {
	if l == nil {
		return nil
	}
	var problems []string
	serversBySwitch := map[string]string{}
	for _, ct := range l.Containers {
		service := ct.Service
		if service == "" {
			continue
		}
		ctRef := displayNodeRef(ct.ID, ct.Name)
		if service != ContainerServiceDHCP {
			problems = append(problems, fmt.Sprintf("container %q uses unsupported service %q; supported service is dhcp", ctRef, service))
			continue
		}
		problems = append(problems, managedDHCPContainerProblems(ct, ctRef)...)
		if len(ct.Networks) != 1 || ct.Networks[0].Switch == "" || ct.Networks[0].ExternalLink != "" {
			problems = append(problems, fmt.Sprintf("DHCP container %q must have exactly one network connected to a switch", ctRef))
			continue
		}
		switchID := ct.Networks[0].Switch
		sw, ok := FindSwitch(l, switchID)
		if !ok {
			continue
		}
		if sw.Mode != "nat" {
			problems = append(problems, fmt.Sprintf("DHCP container %q requires NAT switch %q", ctRef, displayNodeRef(sw.ID, sw.Name)))
		}
		for _, externalID := range SwitchExternalLinks(sw) {
			link, ok := FindExternalLink(l, externalID)
			if ok && link.Mode == ExternalModeMacNAT {
				problems = append(problems, fmt.Sprintf("DHCP container %q cannot use switch %q with a MACNAT uplink", ctRef, displayNodeRef(sw.ID, sw.Name)))
			}
		}
		if existing, exists := serversBySwitch[switchID]; exists {
			problems = append(problems, fmt.Sprintf("switch %q has more than one DHCP container: %q and %q", displayNodeRef(sw.ID, sw.Name), existing, ctRef))
		} else {
			serversBySwitch[switchID] = ctRef
		}
	}
	for _, link := range l.NetworkLinks {
		for _, endpoint := range []NetworkEndpoint{link.From, link.To} {
			if endpoint.Type != "container" {
				continue
			}
			for _, ct := range l.Containers {
				if ct.ID == endpoint.ID && IsDHCPContainer(ct) {
					problems = append(problems, fmt.Sprintf("DHCP container %q cannot use direct network links", displayNodeRef(ct.ID, ct.Name)))
				}
			}
		}
	}
	return problems
}
