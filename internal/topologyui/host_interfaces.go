package topologyui

import (
	"net"
	"sort"
	"strings"
)

var hostInterfaceNames = realHostInterfaceNames

func realHostInterfaceNames() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		name := strings.TrimSpace(iface.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func defaultExternalInterfaceName() string {
	names := hostInterfaceNames()
	for _, name := range names {
		if isLikelyPhysicalInterface(name) {
			return name
		}
	}
	for _, name := range names {
		if strings.TrimSpace(name) != "" && name != "lo" {
			return name
		}
	}
	return "?"
}

func isLikelyPhysicalInterface(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == "lo" {
		return false
	}
	lower := strings.ToLower(name)
	for _, prefix := range []string{"br", "docker", "veth", "vnet", "virbr", "fl", "wg", "tun", "tap"} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return true
}
