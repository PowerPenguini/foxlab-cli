package hostnet

import "foxlab-cli/internal/lab"

func endpointHasNetworkLink(l *lab.Lab, endpoint lab.NetworkEndpoint) bool {
	_, ok := lab.FindNetworkLinkForEndpoint(l, endpoint)
	return ok
}

func vmExternalUsesManagedBridge(l *lab.Lab, nic lab.VMNetwork) bool {
	if nic.ExternalLink == "" {
		return false
	}
	link, ok := lab.FindExternalLink(l, nic.ExternalLink)
	if !ok {
		return false
	}
	return link.Mode == lab.ExternalModeNAT || link.Mode == lab.ExternalModeMacNAT
}
