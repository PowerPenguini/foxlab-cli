package topologyui

import "strings"

type commandLinkEndpoint struct {
	Type string
	ID   string
	NIC  string
}

func parseLinkEndpoint(value string, requireNIC bool) (commandLinkEndpoint, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return commandLinkEndpoint{}, false
	}
	typ := normalizeLinkEndpointType(parts[0])
	id := strings.TrimSpace(parts[1])
	if typ == "" || id == "" {
		return commandLinkEndpoint{}, false
	}
	nic := ""
	if len(parts) == 3 {
		nic = normalizeLinkEndpointNIC(parts[2])
		if nic == "" {
			return commandLinkEndpoint{}, false
		}
	}
	if requireNIC && nic == "" {
		return commandLinkEndpoint{}, false
	}
	return commandLinkEndpoint{Type: typ, ID: id, NIC: nic}, true
}

func normalizeLinkEndpointType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "vm":
		return "vm"
	case "container", "ct":
		return "container"
	default:
		return ""
	}
}

func normalizeLinkEndpointNIC(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "nic")
	if value == "" {
		return ""
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return value
}
