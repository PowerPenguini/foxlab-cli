package hostnet

import "foxlab-cli/internal/lab"

func findSwitch(l *lab.Lab, id string) (lab.Switch, bool) {
	if l == nil {
		return lab.Switch{}, false
	}
	for _, sw := range l.Switches {
		if sw.ID == id {
			return sw, true
		}
	}
	return lab.Switch{}, false
}

func findExternalLink(l *lab.Lab, id string) (lab.ExternalLink, bool) {
	if l == nil {
		return lab.ExternalLink{}, false
	}
	for _, link := range l.ExternalLinks {
		if link.ID == id {
			return link, true
		}
	}
	return lab.ExternalLink{}, false
}

func findNetworkLinkForEndpoint(l *lab.Lab, endpoint lab.NetworkEndpoint) (lab.NetworkLink, bool) {
	if l == nil {
		return lab.NetworkLink{}, false
	}
	for _, link := range l.NetworkLinks {
		if sameNetworkEndpoint(link.From, endpoint) || sameNetworkEndpoint(link.To, endpoint) {
			return link, true
		}
	}
	return lab.NetworkLink{}, false
}

func endpointHasNetworkLink(l *lab.Lab, endpoint lab.NetworkEndpoint) bool {
	_, ok := findNetworkLinkForEndpoint(l, endpoint)
	return ok
}

func vmExternalUsesManagedBridge(l *lab.Lab, nic lab.VMNetwork) bool {
	if nic.ExternalLink == "" {
		return false
	}
	link, ok := findExternalLink(l, nic.ExternalLink)
	if !ok {
		return false
	}
	return link.Mode == lab.ExternalModeNAT || link.Mode == lab.ExternalModeMacNAT
}

func sameNetworkEndpoint(a, b lab.NetworkEndpoint) bool {
	return a.Type == b.Type && a.ID == b.ID && a.NIC == b.NIC
}
