package topology

import (
	"fmt"
	"strings"

	"foxlab-cli/internal/lab"
)

func (s *Service) CreateDHCP(request DHCPCreateRequest) Result {
	if s.CurrentLab() == nil {
		return Failure("DHCP create needs a loaded .lab file")
	}
	name := strings.TrimSpace(request.Name)
	if name == "" {
		return Failure("usage: add dhcp <name> [switch=NAME]")
	}
	if image := strings.TrimSpace(request.Image); image != "" && image != lab.DefaultDHCPImage {
		return Failure("DHCP create failed: image is managed by FoxLab")
	}
	if err := s.validateNodeName(name, ""); err != "" {
		return Failure(err)
	}
	switchID := strings.TrimSpace(request.Switch)
	if switchID != "" {
		resolved, ok := s.resolveSwitchID(switchID)
		if !ok {
			return Failure("DHCP create failed: switch not found: " + switchID)
		}
		switchID = resolved
	} else {
		switchID = s.firstDHCPCompatibleSwitchID()
		if switchID == "" {
			return Failure("DHCP create failed: no NAT switch available")
		}
	}
	sw, _ := lab.FindSwitch(s.CurrentLab(), switchID)
	if !s.dhcpCompatibleSwitch(sw) {
		return Failure("DHCP create failed: switch must use NAT mode without a MACNAT uplink: " + s.nodeDisplayName("switch", switchID))
	}
	for _, ct := range s.CurrentLab().Containers {
		if existingSwitch, ok := lab.DHCPContainerSwitch(ct); ok && existingSwitch == switchID {
			return Failure("DHCP create failed: switch already has DHCP container: " + s.nodeDisplayName("container", ct.ID))
		}
	}
	if err := s.requireSavePath(); err != nil {
		return FailureWithCause("DHCP create failed: "+err.Error(), err)
	}
	mutation := s.beginLabMutation()
	ct := lab.Container{
		ID:           name,
		DesiredState: lab.DesiredStateRunning,
		Service:      lab.ContainerServiceDHCP,
		Image:        lab.DefaultDHCPImage,
		Networks:     []lab.ContainerNetwork{{Switch: switchID}},
	}
	s.CurrentLab().Containers = append(s.CurrentLab().Containers, ct)
	if s.CurrentLab().Layout.Nodes == nil {
		s.CurrentLab().Layout.Nodes = map[string]lab.Position{}
	}
	s.CurrentLab().Layout.Nodes[name] = lab.Position{X: 80, Y: 80 + len(s.CurrentLab().Containers)*96}
	if err := mutation.Commit(); err != nil {
		return FailureWithCause("DHCP create failed: "+err.Error(), err)
	}
	return Success("created DHCP container:" + name)
}

func (s *Service) managedDHCPContainer(id string) (lab.Container, bool) {
	ct, ok := s.LabContainer(id)
	return ct, ok && lab.IsDHCPContainer(ct)
}

func managedDHCPUpdateError(update ContainerUpdate) error {
	switch {
	case update.Image.Set:
		return fmt.Errorf("DHCP image is managed by FoxLab")
	case update.Command.Set:
		return fmt.Errorf("DHCP command is managed by FoxLab")
	case update.Shell.Set:
		return fmt.Errorf("DHCP service does not expose a configurable shell")
	case update.Env.Set:
		return fmt.Errorf("DHCP environment is managed by FoxLab")
	case update.Disk.Set:
		return fmt.Errorf("DHCP service does not support disks")
	case strings.TrimSpace(update.Network.Uplink) != "":
		return fmt.Errorf("DHCP service can connect only to a NAT switch")
	case strings.TrimSpace(update.Network.MAC) != "":
		return fmt.Errorf("DHCP network MAC is managed by FoxLab")
	default:
		return nil
	}
}

func (s *Service) validateDHCPNetworkTarget(id, switchID string) error {
	sw, ok := lab.FindSwitch(s.CurrentLab(), switchID)
	if !ok {
		return fmt.Errorf("switch not found: %s", switchID)
	}
	if !s.dhcpCompatibleSwitch(sw) {
		return fmt.Errorf("DHCP container requires a NAT switch without a MACNAT uplink: %s", s.nodeDisplayName("switch", switchID))
	}
	for _, ct := range s.CurrentLab().Containers {
		if ct.ID == id {
			continue
		}
		if existingSwitch, ok := lab.DHCPContainerSwitch(ct); ok && existingSwitch == switchID {
			return fmt.Errorf("switch already has DHCP container: %s", s.nodeDisplayName("container", ct.ID))
		}
	}
	return nil
}

func (s *Service) firstDHCPCompatibleSwitchID() string {
	if s.CurrentLab() == nil {
		return ""
	}
	for _, sw := range s.CurrentLab().Switches {
		if s.dhcpCompatibleSwitch(sw) {
			return sw.ID
		}
	}
	return ""
}

func (s *Service) dhcpCompatibleSwitch(sw lab.Switch) bool {
	if sw.Mode != "nat" || s.CurrentLab() == nil {
		return false
	}
	for _, externalID := range lab.SwitchExternalLinks(sw) {
		link, ok := lab.FindExternalLink(s.CurrentLab(), externalID)
		if ok && link.Mode == lab.ExternalModeMacNAT {
			return false
		}
	}
	return true
}
