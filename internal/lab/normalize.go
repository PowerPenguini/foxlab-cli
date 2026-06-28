package lab

import (
	"net"
	"strings"
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
		if l.Switches[i].ExternalLink != "" && l.Switches[i].Mode == "" {
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
