package topologyui

import "strings"

func contextMenuAction(label string) string {
	switch strings.TrimSpace(label) {
	case "Apply lab", "apply lab":
		return "apply-lab"
	case "Configuration >", "Configuration", "config >":
		return "config-menu"
	case "NIC >", "NIC", "nic >":
		return "nic-menu"
	case "Permissions >", "Permissions", "permissions >":
		return "permissions-menu"
	case "Uplink >", "uplink >":
		return "uplink-menu"
	case "add >", "create >":
		return "create-menu"
	case "Disks", "disks":
		return "disk-explorer"
	case "Add SW":
		return "add sw"
	case "Attach Uplink":
		return "attach-uplink"
	case "Run":
		return "run"
	case "Stop":
		return "stop"
	case "Disk >":
		return "disk-menu"
	case "Add Disk":
		return "add-disk"
	case "Add NIC":
		return "add-nic"
	case "Connect":
		return "connect"
	case "Shell", "Console":
		return "shell"
	case "VNC":
		return "vnc"
	case "Delete", "delete":
		return "delete"
	case "Move", "move":
		return "move"
	case "Link", "link", "Uplink", "uplink", "Add Uplink":
		return "link"
	case "Exit", "exit":
		return "exit"
	}
	label = strings.TrimSpace(label)
	if capability, _, ok := permissionCapabilityState(label); ok {
		return "capability:" + capability
	}
	if key := contextItemKey(label); key == "disk" {
		return "disk"
	}
	switch {
	case isNICDetail(label):
		if index, ok := nicDetailIndex(label); ok {
			return "connect-nic:" + index
		}
		return label
	case isDiskMenuDetail(label):
		return "disk"
	case isDiskAttachMenuDetail(label):
		return "attach-disk"
	case strings.HasPrefix(label, "name="):
		return "rename"
	case strings.HasPrefix(label, "cpu="), strings.HasPrefix(label, "cpus="), strings.HasPrefix(label, "mem="), strings.HasPrefix(label, "memory="), strings.HasPrefix(label, "mode="), strings.HasPrefix(label, "image="), strings.HasPrefix(label, "command="), strings.HasPrefix(label, "switch="):
		return "edit"
	case strings.HasPrefix(label, "vnc="), label == "vnc":
		return "edit"
	case strings.HasPrefix(label, "disk="):
		return "disk"
	case strings.HasPrefix(label, "iso="):
		return "iso"
	case strings.HasPrefix(label, "interface="):
		return "interface"
	case strings.HasPrefix(label, "uplink="), strings.HasPrefix(label, "external="):
		return "edit"
	default:
		return label
	}
}

func nicDetailIndex(detail string) (string, bool) {
	detail = strings.TrimSpace(detail)
	if !strings.HasPrefix(detail, "nic") {
		return "", false
	}
	index := strings.Builder{}
	for _, r := range strings.TrimPrefix(detail, "nic") {
		if r < '0' || r > '9' {
			break
		}
		index.WriteRune(r)
	}
	if index.Len() == 0 {
		return "", false
	}
	return index.String(), true
}

func isContextGroup(action string) bool {
	return strings.HasSuffix(action, "-menu")
}

func activeRootContextGroup(items []string, selected int) string {
	if len(items) == 0 {
		return ""
	}
	action := contextMenuAction(items[normalizedMenuSelection(selected, len(items))])
	if isContextGroup(action) {
		return action
	}
	return ""
}

func contextGroupBelongsToRoot(rootGroup, group string) bool {
	if rootGroup == "" || group == "" {
		return false
	}
	return rootGroup == group
}
