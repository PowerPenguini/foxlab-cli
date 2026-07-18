package lab

func FindSwitch(l *Lab, id string) (Switch, bool) {
	if l == nil {
		return Switch{}, false
	}
	for _, sw := range l.Switches {
		if sw.ID == id {
			return sw, true
		}
	}
	return Switch{}, false
}

func FindExternalLink(l *Lab, id string) (ExternalLink, bool) {
	if l == nil {
		return ExternalLink{}, false
	}
	for _, link := range l.ExternalLinks {
		if link.ID == id {
			return link, true
		}
	}
	return ExternalLink{}, false
}

func FindNetworkLinkForEndpoint(l *Lab, endpoint NetworkEndpoint) (NetworkLink, bool) {
	if l == nil {
		return NetworkLink{}, false
	}
	for _, link := range l.NetworkLinks {
		if SameNetworkEndpoint(link.From, endpoint) || SameNetworkEndpoint(link.To, endpoint) {
			return link, true
		}
	}
	return NetworkLink{}, false
}

func SameNetworkEndpoint(a, b NetworkEndpoint) bool {
	return a.Type == b.Type && a.ID == b.ID && a.NIC == b.NIC
}
