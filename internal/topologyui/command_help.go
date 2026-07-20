package topologyui

import "strings"

func helpLines(topic string) []string {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "", "general", "all":
		return []string{
			"palette/menu: : opens actions from Lab; Alt+: opens them over a shell; Escape closes",
			"navigation: arrows or hjkl move selection; mouse click selects nodes and buttons",
			"tabs: gt/gT from Lab or Disks; Alt+g then t/T or Alt+1..9 works globally",
			"closing: q or quit closes one card; qa silently aliases quit all",
			"shell: Ctrl-] returns to Lab; Shift+PageUp/PageDown scrolls session history",
			"editing: choose Configuration fields and type inline",
			"links: use NIC menu and connect mode to create direct links",
			"disks: open the Disks card from the palette for global disk management",
		}
	case "add":
		return []string{
			"add: open the global add menu, then click VM, container, switch, disk, or uplink",
			"defaults: new nodes get generated ids and can be edited from Configuration",
			"node add: click add actions from a switch/uplink to preconnect where supported",
		}
	case "vm", "vms":
		return []string{
			"vm: Configuration edits name, CPU, memory, VNC, ISO, and power state",
			"vm nic: NIC menu adds, deletes, or connects NICs",
			"vm disk: Disk menu creates, attaches, detaches, deletes, and manages layers",
			"vm actions: Console, VNC, Move, and Delete are root menu actions",
		}
	case "container", "containers", "ct":
		return []string{
			"container: Configuration edits name, image, command, and power state",
			"container permissions: Permissions toggles Linux capabilities; NET_ADMIN enables advanced network tools",
			"container nic: NIC menu adds, deletes, or connects NICs",
			"container disk: Disk menu creates, attaches, detaches, and deletes rootfs layers",
			"container actions: Shell, Move, and Delete are root menu actions",
		}
	case "disk", "disks":
		return []string{
			"disk: open the Disks card from the palette or use Disk menu from a workload",
			"disk buttons: L creates a layer, M merges, D detaches, X deletes",
			"disk explorer: N creates base, L creates layer, E renames, R resizes, M merges, X deletes",
			"disk commands: create, attach, detach, merge, rename, resize, info, layer create/delete, delete",
		}
	case "link", "links":
		return []string{
			"link: use NIC menu on a VM/container and choose a NIC to enter connect mode",
			"link target: click a switch/uplink/workload, then choose or create target NIC",
			"link delete: click X on a NIC row",
		}
	case "tab", "tabs", "shell", "console":
		return []string{
			"tabs: Lab, Disks, and each workload session are closable cards",
			"switch: gt/gT from Lab, Disks, or ended sessions; Alt+g then t/T works globally",
			"running shell: Alt+: opens FoxLab actions; Ctrl-] returns to Lab; click x to close it",
			"ended shell: r restarts, x closes; scrollback remains available",
			"commands: tabnext, tabprev, tabclose [index|label], tabrestart [index|label]",
			"quit: q or quit closes the active card; quit all closes the application",
		}
	case "switch", "switches":
		return []string{
			"switch: Configuration edits name and mode",
			"switch actions: Uplink menu attaches/detaches uplinks; Move and Delete are menu actions",
		}
	case "uplink", "external":
		return []string{
			"uplink: Configuration edits name, interface, and mode",
			"uplink actions: Connect, Add Switch, Move, and Delete are menu actions",
		}
	default:
		return []string{
			"unknown help topic: " + topic,
			"topics: add, vm, container, disk, link, switch, uplink, tabs",
		}
	}
}
