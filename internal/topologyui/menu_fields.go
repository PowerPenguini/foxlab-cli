package topologyui

import "strings"

const (
	contextEditPlaceholder = "empty"
	noInterfacesItem       = "No interfaces"
)

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
	value = strings.TrimSpace(value)
	if value == "" || value == "?" {
		value = contextEditPlaceholder
	} else if key == "mode" {
		value = modeDisplayLabel(value)
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
	case "image":
		return "Image"
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
		{"Image", "image"},
		{"Command", "command"},
		{"Switch", "switch"},
		{"Mode", "mode"},
		{"Uplink", "uplink"},
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

func contextEditText(value string, cursor int) string {
	runes := []rune(value)
	cursor = clamp(cursor, 0, len(runes))
	if len(runes) == 0 {
		return "|" + contextEditPlaceholder
	}
	return string(runes[:cursor]) + "|" + string(runes[cursor:])
}

func contextEditLabel(item, value string, cursor int) string {
	if item == "Add Disk" {
		return "Add Disk " + contextEditText(value, cursor)
	}
	if isDiskAttachMenuDetail(item) {
		diskID, _, _ := diskMenuParts(item)
		return diskID + " | " + contextEditText(value, cursor)
	}
	key, _, ok := strings.Cut(item, "=")
	if !ok {
		_, ok = contextDisplayValue(item)
		if !ok {
			return item
		}
		key = contextItemKey(item)
	}
	if contextDisplayKeyValue, ok := contextDisplayKey(item); ok && contextDisplayKeyValue == key {
		label := contextFieldLabel(key)
		return label + strings.Repeat(" ", max(1, 12-runeLen(label))) + contextEditText(value, cursor)
	}
	return key + "=" + contextEditText(value, cursor)
}

func contextDiskLayerEditLabel(item, value string, cursor int) string {
	return item + " " + contextEditText(value, cursor)
}
