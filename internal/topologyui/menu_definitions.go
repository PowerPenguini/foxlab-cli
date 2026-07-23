package topologyui

import (
	"strings"

	"foxlab-cli/internal/lab"
)

func globalContextMenuItems(groups ...string) []string {
	group := contextGroupArg(groups)
	switch group {
	case "create-menu":
		return []string{"add vm", "add cont", "add dhcp", "add sw", "add disk", "add uplink"}
	default:
		return nil
	}
}

func contextMenuSubmenuItems(node Node, hasNode bool, group string) []string {
	if !hasNode {
		return globalContextMenuItems(group)
	}
	return contextMenuItems(node, group)
}

func contextMenuItems(node Node, groups ...string) []string {
	group := contextGroupArg(groups)
	switch node.Type {
	case NodeVM:
		return vmContextMenuItems(node, group)
	case NodeContainer:
		return containerContextMenuItems(node, group)
	case NodeSwitch:
		return switchContextMenuItems(node, group)
	case NodeExternal:
		return externalContextMenuItems(node, group)
	default:
		return []string{"Configuration >", "Move"}
	}
}

func containerContextMenuItems(node Node, group string) []string {
	switch group {
	case "config-menu":
		if nodeDetailRawValue(node, "service") == lab.ContainerServiceDHCP {
			return []string{
				contextPowerAction(node),
				contextFieldItem("name", node.Label),
			}
		}
		return configMenuItems([]string{
			contextPowerAction(node),
			contextFieldItem("name", node.Label),
			contextFieldItem("image", nodeDetailValue(node, "image", "image=?")),
			contextFieldItem("command", nodeDetailValue(node, "command", "command=?")),
		}, node.Details)
	case "nic-menu":
		if nodeDetailRawValue(node, "service") == lab.ContainerServiceDHCP {
			return nicDetails(node.Details)
		}
		return nicMenuItems(node.Details)
	case "disk-menu":
		return nil
	case "permissions-menu":
		return containerPermissionMenuItems(node)
	case "":
		if nodeDetailRawValue(node, "service") == lab.ContainerServiceDHCP {
			return []string{"Configuration >", "NIC >", "Move"}
		}
		return []string{"Configuration >", "Permissions >", "NIC >", "Disk >", "Move"}
	default:
		return nil
	}
}

func vmContextMenuItems(node Node, group string) []string {
	switch group {
	case "config-menu":
		vncDetail := nodeDetailValue(node, "vnc", "vnc=false")
		prefix := []string{
			contextPowerAction(node),
			contextFieldItem("name", node.Label),
			contextFieldItem("cpu", nodeDetailValue(node, "cpu", "cpus=?")),
			contextFieldItem("mem", nodeDetailValue(node, "mem", "memory=?")),
			contextCheckboxItem(vncDetail),
			contextFieldItem("iso", nodeDetailValue(node, "iso", "iso=")),
		}
		if vncEnabled(vncDetail) {
			prefix = append(prefix, vncInfoItem(node))
		}
		return configMenuItems(prefix, node.Details)
	case "nic-menu":
		return nicMenuItems(node.Details)
	case "disk-menu":
		return nil
	case "":
		return []string{"Configuration >", "NIC >", "Disk >", "Move", "VNC"}
	default:
		return nil
	}
}

func switchContextMenuItems(node Node, group string) []string {
	switch group {
	case "config-menu":
		return configMenuItems([]string{
			contextFieldItem("name", node.Label),
			contextFieldItem("mode", nodeDetailValue(node, "mode", "mode=bridge")),
		}, switchConfigurationDetails(node.Details))
	case "uplink-menu":
		return []string{"Attach Uplink"}
	case "mode-menu":
		return []string{"Bridge", "NAT", "MACNAT"}
	case "":
		return []string{"Configuration >", "Uplink >", "Add DHCP", "Move"}
	default:
		return nil
	}
}

func externalContextMenuItems(node Node, group string) []string {
	switch group {
	case "config-menu":
		return configMenuItems([]string{
			contextFieldItem("name", node.Label),
			contextFieldItem("interface", nodeDetailValue(node, "interface", "interface=?")),
			contextFieldItem("mode", nodeDetailValue(node, "mode", "mode=nat")),
		}, node.Details)
	case "interface-menu":
		return hostInterfaceMenuItems()
	case "mode-menu":
		return []string{"NAT", "Direct", "MACNAT"}
	case "":
		return []string{"Configuration >", "Connect", "Move"}
	default:
		return nil
	}
}

func contextMenuRootItemsForInspector(node Node, inspectorVisible bool) []string {
	items := contextMenuItems(node, "")
	if !inspectorVisible {
		return items
	}
	return nil
}

func hostInterfaceMenuItems() []string {
	items := hostInterfaceNames()
	if len(items) == 0 {
		return []string{noInterfacesItem}
	}
	return items
}

func configMenuItems(prefix []string, details []string) []string {
	return compactMenuItems(prefix, nonNICDetails(details), nil)
}

func nicMenuItems(details []string) []string {
	return compactMenuItems([]string{"Add NIC"}, nicDetails(details), nil)
}

func connectTargetNICMenuItems(node Node) []string {
	items := nicDetails(node.Details)
	return append(items, "New NIC")
}

func contextPowerAction(node Node) string {
	if lab.DesiredState(node.DesiredState) == lab.DesiredStateRunning {
		return "Stop"
	}
	return "Run"
}

func contextGroupArg(groups []string) string {
	if len(groups) == 0 {
		return ""
	}
	return groups[0]
}

func compactMenuItems(prefix []string, details []string, suffix []string) []string {
	out := append([]string{}, prefix...)
	for _, detail := range details {
		if len(out) >= 8 {
			break
		}
		if strings.TrimSpace(detail) == "hooks >" {
			continue
		}
		if contextItemKey(detail) != "" && containsContextItemKey(out, contextItemKey(detail)) {
			continue
		}
		out = append(out, detail)
	}
	return append(out, suffix...)
}
