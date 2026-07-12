package lab

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
)

var containerRuntimeNamePattern = regexp.MustCompile(`^[A-Za-z0-9]+(?:[._-][A-Za-z0-9]+)*$`)

func (l *Lab) ManagedDomainName(vm VM) string {
	return managedName(l.ID, vm.ID)
}

func (l *Lab) ManagedNetworkName(sw Switch) string {
	return managedName(l.ID, sw.ID)
}

func (l *Lab) ManagedSwitchBridgeName(sw Switch) string {
	return bridgeName(l.ManagedNetworkName(sw))
}

func (l *Lab) ManagedExternalBridgeName(link ExternalLink) string {
	return bridgeName(managedName(l.ID, "uplink-"+link.ID))
}

func (l *Lab) ManagedContainerName(ct Container) string {
	name := managedName(l.ID, ct.ID)
	if len(name) <= 76 && containerRuntimeNamePattern.MatchString(name) {
		return name
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(l.ID + "\x00" + ct.ID))
	return fmt.Sprintf("foxlab-c-%016x", h.Sum64())
}

func (l *Lab) ManagedNetworkLinkBridgeName(link NetworkLink) string {
	from := networkEndpointKey(link.From)
	to := networkEndpointKey(link.To)
	if to < from {
		from, to = to, from
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(l.ID + "|" + from + "|" + to))
	return fmt.Sprintf("flp2p%08x", h.Sum32())
}

func (l *Lab) GeneratedNICMAC(typ, id string, index int) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(l.ID + "|" + typ + "|" + id + "|" + fmt.Sprintf("%d", index)))
	sum := h.Sum32()
	return fmt.Sprintf("02:00:%02x:%02x:%02x:%02x", byte(sum>>24), byte(sum>>16), byte(sum>>8), byte(sum))
}

func managedName(labID, resourceID string) string {
	return strings.ToLower(fmt.Sprintf("%s-%s-%s", ManagedPrefix, labID, resourceID))
}

func networkEndpointKey(endpoint NetworkEndpoint) string {
	return fmt.Sprintf("%s:%s:%d", endpoint.Type, endpoint.ID, endpoint.NIC)
}

func bridgeName(managedName string) string {
	const (
		maxLinuxIfName = 15
		prefix         = "fl"
		hashLen        = 6
	)
	clean := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(managedName))
	if clean == "" {
		clean = "foxlab"
	}
	maxBase := maxLinuxIfName - len(prefix)
	if len(clean) <= maxBase {
		return prefix + clean
	}
	hash := fmt.Sprintf("%06x", hash32(clean)&0xffffff)
	stemLen := maxLinuxIfName - len(prefix) - hashLen
	if stemLen < 1 {
		stemLen = 1
	}
	return prefix + clean[:stemLen] + hash
}

func hash32(value string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return h.Sum32()
}
