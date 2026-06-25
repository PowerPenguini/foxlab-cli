package lab

import (
	"fmt"
	"hash/fnv"
	"strings"
)

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
	return managedName(l.ID, ct.ID)
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
	const maxLinuxIfName = 15
	clean := strings.NewReplacer("_", "", "-", "").Replace(managedName)
	if len(clean) > maxLinuxIfName-2 {
		clean = clean[:maxLinuxIfName-2]
	}
	return "fl" + clean
}
