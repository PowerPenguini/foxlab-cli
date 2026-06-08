package topologyui

import "strings"

func globalContextMenuItems(groups ...string) []string {
	group := contextGroupArg(groups)
	switch group {
	case "create-menu":
		return []string{"create-vm", "create-switch", "create-external"}
	default:
		return []string{"create >"}
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
	case NodeSwitch:
		return switchContextMenuItems(node, group)
	case NodeExternal:
		return externalContextMenuItems(node, group)
	default:
		return []string{"Configuration >", "Move", "Delete"}
	}
}

func vmContextMenuItems(node Node, group string) []string {
	switch group {
	case "config-menu":
		return configMenuItems([]string{
			contextPowerAction(node),
			contextFieldItem("name", node.Label),
			contextFieldItem("cpu", nodeDetailValue(node, "cpu", "cpus=?")),
			contextFieldItem("mem", nodeDetailValue(node, "mem", "memory=?")),
			contextCheckboxItem(nodeDetailValue(node, "vnc", "vnc=false")),
			contextFieldItem("disk", nodeDetailValue(node, "disk", "disk=?")),
			contextFieldItem("iso", nodeDetailValue(node, "iso", "iso=?")),
		}, node.Details)
	case "":
		return []string{"Configuration >", "Move", "Delete"}
	default:
		return nil
	}
}

func switchContextMenuItems(node Node, group string) []string {
	switch group {
	case "config-menu":
		return configMenuItems([]string{
			contextPowerAction(node),
			contextFieldItem("name", node.ID),
			contextFieldItem("mode", nodeDetailValue(node, "mode", "mode=bridge")),
			contextFieldItem("external", nodeDetailValue(node, "external", "external=?")),
		}, node.Details)
	case "":
		return []string{"Configuration >", "Move", "Delete"}
	default:
		return nil
	}
}

func externalContextMenuItems(node Node, group string) []string {
	switch group {
	case "config-menu":
		return configMenuItems([]string{
			contextPowerAction(node),
			contextFieldItem("name", node.Label),
			contextFieldItem("interface", nodeDetailValue(node, "interface", "interface=?")),
		}, node.Details)
	case "":
		return []string{"Configuration >", "Move", "Delete"}
	default:
		return nil
	}
}

func configMenuItems(prefix []string, details []string) []string {
	return compactMenuItems(prefix, details, nil)
}

func contextPowerAction(node Node) string {
	if node.State == "running" || node.State == "link" {
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

func containsContextItemKey(items []string, key string) bool {
	for _, item := range items {
		if contextItemKey(item) == key {
			return true
		}
	}
	return false
}

func contextItemKey(item string) string {
	item = strings.TrimSpace(item)
	if key, _, ok := strings.Cut(item, "="); ok {
		return key
	}
	key, ok := contextDisplayKey(item)
	if !ok {
		return ""
	}
	return key
}

func contextCheckboxItem(item string) string {
	key, value, ok := strings.Cut(strings.TrimSpace(item), "=")
	if !ok {
		return item
	}
	label := contextFieldLabel(key)
	prefix := label + strings.Repeat(" ", max(1, 12-runeLen(label)))
	if strings.EqualFold(strings.TrimSpace(value), "true") {
		return prefix + "[X]"
	}
	return prefix + "[ ]"
}

func contextFieldItem(key, value string) string {
	if _, parsedValue, ok := strings.Cut(strings.TrimSpace(value), "="); ok {
		value = parsedValue
	}
	return contextFieldLabel(key) + strings.Repeat(" ", max(1, 12-runeLen(contextFieldLabel(key)))) + value
}

func contextFieldLabel(key string) string {
	switch key {
	case "cpu", "cpus":
		return "CPU"
	case "mem", "memory":
		return "Memory"
	case "vnc":
		return "VNC"
	case "iso":
		return "ISO"
	default:
		return strings.ToUpper(key[:1]) + key[1:]
	}
}

func contextDisplayKey(item string) (string, bool) {
	for _, field := range []struct {
		label string
		key   string
	}{
		{"Name", "name"},
		{"CPU", "cpu"},
		{"Memory", "mem"},
		{"VNC", "vnc"},
		{"Disk", "disk"},
		{"ISO", "iso"},
		{"Mode", "mode"},
		{"External", "external"},
		{"Interface", "interface"},
	} {
		if item == field.label || strings.HasPrefix(item, field.label+" ") {
			return field.key, true
		}
	}
	return "", false
}

func contextDisplayValue(item string) (string, bool) {
	item = strings.TrimSpace(item)
	key, ok := contextDisplayKey(item)
	if !ok {
		return "", false
	}
	label := contextFieldLabel(key)
	if !strings.HasPrefix(item, label) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(item, label)), true
}

func nodeDetailValue(node Node, key, fallback string) string {
	prefix := key + "="
	for _, detail := range node.Details {
		if strings.HasPrefix(detail, prefix) {
			return detail
		}
	}
	return fallback
}

func contextMenuAction(label string) string {
	switch strings.TrimSpace(label) {
	case "Configuration >", "Configuration", "config >":
		return "config-menu"
	case "create >":
		return "create-menu"
	case "Run":
		return "run"
	case "Stop":
		return "stop"
	case "Delete", "delete":
		return "delete"
	case "Move", "move":
		return "move"
	}
	label = strings.TrimSpace(label)
	switch {
	case strings.HasPrefix(label, "name="):
		return "rename"
	case strings.HasPrefix(label, "cpu="), strings.HasPrefix(label, "cpus="), strings.HasPrefix(label, "mem="), strings.HasPrefix(label, "memory="), strings.HasPrefix(label, "mode="):
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

func contextMenuWidth(items []string) int {
	w := 0
	for _, item := range items {
		w = max(w, runeLen(item)+3)
	}
	return max(w, 10)
}

func contextEditLabel(item, value string, cursor int) string {
	key, _, ok := strings.Cut(item, "=")
	if !ok {
		var displayValue string
		displayValue, ok = contextDisplayValue(item)
		if !ok {
			return item
		}
		key = contextItemKey(item)
		if value == "" {
			value = displayValue
		}
	}
	runes := []rune(value)
	cursor = clamp(cursor, 0, len(runes))
	if contextDisplayKeyValue, ok := contextDisplayKey(item); ok && contextDisplayKeyValue == key {
		label := contextFieldLabel(key)
		return label + strings.Repeat(" ", max(1, 12-runeLen(label))) + string(runes[:cursor]) + "|" + string(runes[cursor:])
	}
	return key + "=" + string(runes[:cursor]) + "|" + string(runes[cursor:])
}

func contextMenuStart(active, itemCount, visibleCount int) int {
	if itemCount <= visibleCount {
		return 0
	}
	half := visibleCount / 2
	return clamp(active-half, 0, itemCount-visibleCount)
}
