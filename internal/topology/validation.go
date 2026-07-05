package topology

import (
	"fmt"
	"strings"

	"foxlab-cli/internal/lab"
)

func validSwitchMode(mode string) bool {
	switch mode {
	case "bridge", "nat", "macnat-bridge":
		return true
	default:
		return false
	}
}

func validExternalMode(mode string) bool {
	switch mode {
	case "nat", "direct", "macnat":
		return true
	default:
		return false
	}
}

func (s *Service) validateSwitchConfig(name, mode string, externals []string) error {
	if !validSwitchMode(mode) {
		return fmt.Errorf("switch %q uses unsupported mode %q; supported modes are bridge, nat and macnat-bridge", name, mode)
	}
	if mode == "macnat-bridge" && len(externals) == 0 {
		return fmt.Errorf("switch %q macnat-bridge mode requires externalLinks", name)
	}
	for _, external := range externals {
		if external != "" && !s.HasLabExternal(external) {
			return fmt.Errorf("switch %q references missing uplink %q", name, external)
		}
	}
	return nil
}

func (s *Service) resolveExternalRefs(refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref == "" {
			continue
		}
		id, ok := s.resolveExternalID(ref)
		if !ok {
			return nil, fmt.Errorf("uplink not found: %s", ref)
		}
		out = append(out, id)
	}
	return out, nil
}

func validateExternalConfig(id, mode string) error {
	if !validExternalMode(mode) {
		return fmt.Errorf("uplink %q uses unsupported mode %q; supported modes are nat, direct and macnat", id, mode)
	}
	return nil
}

func (s *Service) validateVMNetworkRefs(id, switchRef, externalRef string) error {
	if switchRef != "" && externalRef != "" {
		return fmt.Errorf("vm %q network must not reference both switch and externalLink", id)
	}
	if switchRef != "" && !s.HasLabSwitch(switchRef) {
		return fmt.Errorf("vm %q references missing switch %q", id, switchRef)
	}
	if externalRef != "" && !s.HasLabExternal(externalRef) {
		return fmt.Errorf("vm %q references missing uplink %q", id, externalRef)
	}
	return nil
}

func (s *Service) validateContainerNetworkRefs(id, switchRef, externalRef string) error {
	if switchRef != "" && externalRef != "" {
		return fmt.Errorf("container %q network must not reference both switch and externalLink", id)
	}
	if switchRef != "" && !s.HasLabSwitch(switchRef) {
		return fmt.Errorf("container %q references missing switch %q", id, switchRef)
	}
	if externalRef != "" && !s.HasLabExternal(externalRef) {
		return fmt.Errorf("container %q references missing uplink %q", id, externalRef)
	}
	return nil
}

func validateNICMACArg(scope, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if !lab.ValidMAC(value) {
		return fmt.Errorf("invalid %s mac: %s", scope, value)
	}
	return nil
}
