package lab

import (
	"net"
	"strings"

	"github.com/google/uuid"
)

func (l *Lab) Normalize() {
	l.ID = strings.TrimSpace(l.ID)
	l.LegacyID = strings.TrimSpace(l.LegacyID)
	if l.ID == "" {
		l.ID = l.LegacyID
		l.LegacyID = ""
	}
	for i := range l.VMs {
		l.VMs[i].ID = strings.TrimSpace(l.VMs[i].ID)
		l.VMs[i].Name = strings.TrimSpace(l.VMs[i].Name)
		l.VMs[i].DesiredState = normalizeDesiredState(l.VMs[i].DesiredState)
		l.VMs[i].Disk = strings.TrimSpace(l.VMs[i].Disk)
		l.VMs[i].ISO = strings.TrimSpace(l.VMs[i].ISO)
		for j := range l.VMs[i].Networks {
			l.VMs[i].Networks[j].Switch = strings.TrimSpace(l.VMs[i].Networks[j].Switch)
			l.VMs[i].Networks[j].ExternalLink = strings.TrimSpace(l.VMs[i].Networks[j].ExternalLink)
			l.VMs[i].Networks[j].MAC = normalizeMAC(l.VMs[i].Networks[j].MAC)
		}
		if l.VMs[i].MemoryMB == 0 {
			l.VMs[i].MemoryMB = 2048
		}
		if l.VMs[i].CPUs == 0 {
			l.VMs[i].CPUs = 2
		}
	}
	for i := range l.Containers {
		l.Containers[i].ID = strings.TrimSpace(l.Containers[i].ID)
		l.Containers[i].Name = strings.TrimSpace(l.Containers[i].Name)
		l.Containers[i].DesiredState = normalizeDesiredState(l.Containers[i].DesiredState)
		l.Containers[i].Image = strings.TrimSpace(l.Containers[i].Image)
		l.Containers[i].Disk = strings.TrimSpace(l.Containers[i].Disk)
		for j := range l.Containers[i].Command {
			l.Containers[i].Command[j] = strings.TrimSpace(l.Containers[i].Command[j])
		}
		for j := range l.Containers[i].Networks {
			l.Containers[i].Networks[j].Switch = strings.TrimSpace(l.Containers[i].Networks[j].Switch)
			l.Containers[i].Networks[j].ExternalLink = strings.TrimSpace(l.Containers[i].Networks[j].ExternalLink)
			l.Containers[i].Networks[j].MAC = normalizeMAC(l.Containers[i].Networks[j].MAC)
		}
	}
	for i := range l.Switches {
		l.Switches[i].ID = strings.TrimSpace(l.Switches[i].ID)
		l.Switches[i].Name = strings.TrimSpace(l.Switches[i].Name)
		l.Switches[i].Mode = strings.TrimSpace(l.Switches[i].Mode)
		l.Switches[i].ExternalLink = strings.TrimSpace(l.Switches[i].ExternalLink)
		l.Switches[i].ExternalLinks = normalizeSwitchExternalLinks(l.Switches[i])
		l.Switches[i].ExternalLink = ""
		if len(l.Switches[i].ExternalLinks) > 0 && l.Switches[i].Mode == "" {
			l.Switches[i].Mode = "bridge"
		}
		if l.Switches[i].Mode == "" {
			l.Switches[i].Mode = "bridge"
		}
	}
	for i := range l.ExternalLinks {
		l.ExternalLinks[i].ID = strings.TrimSpace(l.ExternalLinks[i].ID)
		l.ExternalLinks[i].Name = strings.TrimSpace(l.ExternalLinks[i].Name)
		l.ExternalLinks[i].Interface = strings.TrimSpace(l.ExternalLinks[i].Interface)
		l.ExternalLinks[i].Mode = normalizeExternalMode(l.ExternalLinks[i].Mode)
	}
	for i := range l.NetworkLinks {
		l.NetworkLinks[i].From.Type = strings.ToLower(strings.TrimSpace(l.NetworkLinks[i].From.Type))
		l.NetworkLinks[i].From.ID = strings.TrimSpace(l.NetworkLinks[i].From.ID)
		l.NetworkLinks[i].To.Type = strings.ToLower(strings.TrimSpace(l.NetworkLinks[i].To.Type))
		l.NetworkLinks[i].To.ID = strings.TrimSpace(l.NetworkLinks[i].To.ID)
	}
	for i := range l.Layout.Links {
		l.Layout.Links[i].From.Type = strings.ToLower(strings.TrimSpace(l.Layout.Links[i].From.Type))
		l.Layout.Links[i].From.ID = strings.TrimSpace(l.Layout.Links[i].From.ID)
		l.Layout.Links[i].To.Type = strings.ToLower(strings.TrimSpace(l.Layout.Links[i].To.Type))
		l.Layout.Links[i].To.ID = strings.TrimSpace(l.Layout.Links[i].To.ID)
	}
	for i := range l.Disks {
		l.Disks[i].ID = strings.TrimSpace(l.Disks[i].ID)
		l.Disks[i].Path = strings.TrimSpace(l.Disks[i].Path)
		l.Disks[i].Format = strings.ToLower(strings.TrimSpace(l.Disks[i].Format))
		l.Disks[i].Kind = strings.ToLower(strings.TrimSpace(l.Disks[i].Kind))
		l.Disks[i].Base = strings.TrimSpace(l.Disks[i].Base)
		l.Disks[i].AttachedType = strings.ToLower(strings.TrimSpace(l.Disks[i].AttachedType))
		l.Disks[i].AttachedTo = strings.TrimSpace(l.Disks[i].AttachedTo)
		l.Disks[i].MountPath = strings.TrimSpace(l.Disks[i].MountPath)
	}
	l.migrateLegacyNodeIDs()
}

func (l *Lab) migrateLegacyNodeIDs() {
	if l == nil {
		return
	}
	switchIDs := map[string]string{}
	externalIDs := map[string]string{}
	vmIDs := map[string]string{}
	containerIDs := map[string]string{}
	for i := range l.Switches {
		old := l.Switches[i].ID
		next := legacyNodeUUID(l.ID, "switch", old)
		if next == old {
			continue
		}
		if l.Switches[i].Name == "" {
			l.Switches[i].Name = old
		}
		l.Switches[i].ID = next
		switchIDs[old] = next
	}
	for i := range l.ExternalLinks {
		old := l.ExternalLinks[i].ID
		next := legacyNodeUUID(l.ID, "external", old)
		if next == old {
			continue
		}
		if l.ExternalLinks[i].Name == "" {
			l.ExternalLinks[i].Name = old
		}
		l.ExternalLinks[i].ID = next
		externalIDs[old] = next
	}
	for i := range l.VMs {
		old := l.VMs[i].ID
		next := legacyNodeUUID(l.ID, "vm", old)
		if next == old {
			continue
		}
		if l.VMs[i].Name == "" {
			l.VMs[i].Name = old
		}
		l.VMs[i].ID = next
		vmIDs[old] = next
	}
	for i := range l.Containers {
		old := l.Containers[i].ID
		next := legacyNodeUUID(l.ID, "container", old)
		if next == old {
			continue
		}
		if l.Containers[i].Name == "" {
			l.Containers[i].Name = old
		}
		l.Containers[i].ID = next
		containerIDs[old] = next
	}
	for i := range l.Switches {
		for j := range l.Switches[i].ExternalLinks {
			if next, ok := externalIDs[l.Switches[i].ExternalLinks[j]]; ok {
				l.Switches[i].ExternalLinks[j] = next
			}
		}
		if next, ok := externalIDs[l.Switches[i].ExternalLink]; ok {
			l.Switches[i].ExternalLink = next
		}
	}
	for i := range l.VMs {
		for j := range l.VMs[i].Networks {
			if next, ok := switchIDs[l.VMs[i].Networks[j].Switch]; ok {
				l.VMs[i].Networks[j].Switch = next
			}
			if next, ok := externalIDs[l.VMs[i].Networks[j].ExternalLink]; ok {
				l.VMs[i].Networks[j].ExternalLink = next
			}
		}
	}
	for i := range l.Containers {
		for j := range l.Containers[i].Networks {
			if next, ok := switchIDs[l.Containers[i].Networks[j].Switch]; ok {
				l.Containers[i].Networks[j].Switch = next
			}
			if next, ok := externalIDs[l.Containers[i].Networks[j].ExternalLink]; ok {
				l.Containers[i].Networks[j].ExternalLink = next
			}
		}
	}
	for i := range l.NetworkLinks {
		migrateLegacyEndpointID(&l.NetworkLinks[i].From, vmIDs, containerIDs)
		migrateLegacyEndpointID(&l.NetworkLinks[i].To, vmIDs, containerIDs)
	}
	if l.Layout.Nodes != nil {
		nextNodes := make(map[string]Position, len(l.Layout.Nodes))
		for id, position := range l.Layout.Nodes {
			switch {
			case vmIDs[id] != "":
				nextNodes[vmIDs[id]] = position
			case switchIDs[id] != "":
				nextNodes[switchIDs[id]] = position
			case externalIDs[id] != "":
				nextNodes[externalIDs[id]] = position
			case containerIDs[id] != "":
				nextNodes[containerIDs[id]] = position
			default:
				nextNodes[id] = position
			}
		}
		l.Layout.Nodes = nextNodes
	}
	for i := range l.Layout.Links {
		migrateLegacyLayoutEndpointID(&l.Layout.Links[i].From, vmIDs, switchIDs, externalIDs, containerIDs)
		migrateLegacyLayoutEndpointID(&l.Layout.Links[i].To, vmIDs, switchIDs, externalIDs, containerIDs)
	}
	for i := range l.Disks {
		switch l.Disks[i].AttachedType {
		case "vm":
			if next, ok := vmIDs[l.Disks[i].AttachedTo]; ok {
				l.Disks[i].AttachedTo = next
			}
		case "container":
			if next, ok := containerIDs[l.Disks[i].AttachedTo]; ok {
				l.Disks[i].AttachedTo = next
			}
		}
	}
}

func legacyNodeUUID(labID, kind, id string) string {
	id = strings.TrimSpace(id)
	if id == "" || validNodeID(id) {
		return id
	}
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(labID+"|"+kind+"|"+id)).String()
}

func migrateLegacyEndpointID(endpoint *NetworkEndpoint, vmIDs, containerIDs map[string]string) {
	switch endpoint.Type {
	case "vm":
		if next, ok := vmIDs[endpoint.ID]; ok {
			endpoint.ID = next
		}
	case "container":
		if next, ok := containerIDs[endpoint.ID]; ok {
			endpoint.ID = next
		}
	}
}

func migrateLegacyLayoutEndpointID(endpoint *LayoutEndpoint, vmIDs, switchIDs, externalIDs, containerIDs map[string]string) {
	switch endpoint.Type {
	case "vm":
		if next, ok := vmIDs[endpoint.ID]; ok {
			endpoint.ID = next
		}
	case "switch":
		if next, ok := switchIDs[endpoint.ID]; ok {
			endpoint.ID = next
		}
	case "external":
		if next, ok := externalIDs[endpoint.ID]; ok {
			endpoint.ID = next
		}
	case "container":
		if next, ok := containerIDs[endpoint.ID]; ok {
			endpoint.ID = next
		}
	}
}

func normalizeSwitchExternalLinks(sw Switch) []string {
	out := make([]string, 0, len(sw.ExternalLinks)+1)
	seen := map[string]struct{}{}
	for _, id := range sw.ExternalLinks {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if id := strings.TrimSpace(sw.ExternalLink); id != "" {
		if _, exists := seen[id]; !exists {
			out = append(out, id)
		}
	}
	return out
}

func DesiredState(value string) string {
	value = normalizeDesiredState(value)
	if value == "" {
		return DesiredStateStopped
	}
	return value
}

func normalizeDesiredState(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeExternalMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ExternalModeDirect
	}
	return value
}

func normalizeMAC(value string) string {
	value = strings.TrimSpace(value)
	if mac, ok := parseNICMAC(value); ok {
		return mac.String()
	}
	return value
}

func parseNICMAC(value string) (net.HardwareAddr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, false
	}
	mac, err := net.ParseMAC(value)
	return mac, err == nil && len(mac) == 6
}
