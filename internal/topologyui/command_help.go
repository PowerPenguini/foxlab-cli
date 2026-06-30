package topologyui

import "strings"

func helpLines(topic string) []string {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "", "general", "all":
		return []string{
			"menu: Space or mouse click opens actions; Enter/click selects; Escape closes",
			"navigation: arrows or hjkl move selection; mouse click selects nodes and buttons",
			"editing: choose Configuration fields and type inline",
			"links: use NIC menu and connect mode to create direct links",
		}
	case "add":
		return []string{
			"add: open the global add menu, then click VM, container, switch, disk, or external",
			"defaults: new nodes get generated ids and can be edited from Configuration",
			"node add: click add actions from a switch/external to preconnect where supported",
		}
	case "vm", "vms":
		return []string{
			"vm: Configuration edits name, CPU, memory, VNC, ISO, and power state",
			"vm nic: NIC menu adds, deletes, or connects NICs",
			"vm disk: Disk menu creates, attaches, detaches, deletes, and manages layers",
			"vm actions: Shell, VNC, Move, and Delete are root menu actions",
		}
	case "container", "containers", "ct":
		return []string{
			"container: Configuration edits name, image, command, and power state",
			"container nic: NIC menu adds, deletes, or connects NICs",
			"container disk: Disk menu creates, attaches, detaches, and deletes rootfs layers",
			"container actions: Shell, Move, and Delete are root menu actions",
		}
	case "disk", "disks":
		return []string{
			"disk: use Disk menu from a VM or container",
			"disk buttons: L creates a layer, M merges, D detaches, X deletes",
			"disk create: Add Disk opens inline naming; Enter/click attaches existing disks",
		}
	case "link", "links":
		return []string{
			"link: use NIC menu on a VM/container and choose a NIC to enter connect mode",
			"link target: click a switch/external/workload, then choose or create target NIC",
			"link delete: click X on a NIC row",
		}
	case "switch", "switches":
		return []string{
			"switch: Configuration edits name, mode, and external link",
			"switch actions: Add VM, Add Container, Move, and Delete are menu actions",
		}
	case "external":
		return []string{
			"external: Configuration edits name and interface",
			"external actions: Add Switch, Move, and Delete are menu actions",
		}
	default:
		return []string{
			"unknown help topic: " + topic,
			"topics: add, vm, container, disk, link, switch, external",
		}
	}
}
