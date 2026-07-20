package topologyui

import (
	"strings"

	"foxlab-cli/internal/lab"
)

var priorityContainerCapabilities = []string{
	"NET_ADMIN",
	"NET_RAW",
	"NET_BIND_SERVICE",
	"SYS_ADMIN",
	"SYS_PTRACE",
	"BPF",
	"PERFMON",
}

func containerPermissionMenuItems(node Node) []string {
	enabled := map[string]bool{}
	for _, capability := range strings.Split(nodeDetailRawValue(node, "capabilities"), ",") {
		capability = lab.NormalizeContainerCapability(capability)
		if capability != "" {
			enabled[capability] = true
		}
	}
	capabilities := orderedContainerCapabilities()
	items := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		marker := "[ ]"
		if enabled[capability] {
			marker = "[X]"
		}
		items = append(items, marker+" "+capability)
	}
	return items
}

func orderedContainerCapabilities() []string {
	all := lab.SupportedContainerCapabilities()
	seen := map[string]bool{}
	out := make([]string, 0, len(all))
	for _, capability := range priorityContainerCapabilities {
		if lab.IsSupportedContainerCapability(capability) && !seen[capability] {
			seen[capability] = true
			out = append(out, capability)
		}
	}
	for _, capability := range all {
		if !seen[capability] {
			seen[capability] = true
			out = append(out, capability)
		}
	}
	return out
}

func permissionCapabilityState(item string) (string, bool, bool) {
	item = strings.TrimSpace(item)
	enabled := false
	switch {
	case strings.HasPrefix(item, "[X] "), strings.HasPrefix(item, "[x] "):
		enabled = true
		item = strings.TrimSpace(item[4:])
	case strings.HasPrefix(item, "[ ] "):
		item = strings.TrimSpace(item[4:])
	default:
		return "", false, false
	}
	capability := lab.NormalizeContainerCapability(item)
	if !lab.IsSupportedContainerCapability(capability) {
		return "", false, false
	}
	return capability, enabled, true
}
