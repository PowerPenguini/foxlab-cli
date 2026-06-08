package topologyui

import "strings"

func (a *App) handleContextEditKey(key string, node Node, ok bool, subItems []string) bool {
	selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
	item := ""
	if len(subItems) > 0 {
		item = subItems[selected]
	}
	switch {
	case key == "enter":
		if ok && item != "" {
			a.applyContextEdit(node, item, a.State.ContextEditValue)
		}
		a.State.ContextEdit = false
		a.State.ContextEditValue = ""
		a.State.ContextEditCursor = 0
	case key == "escape":
		a.State.ContextEdit = false
		a.State.ContextEditValue = ""
		a.State.ContextEditCursor = 0
	case key == "left":
		a.State.ContextEditCursor = clamp(a.State.ContextEditCursor-1, 0, runeLen(a.State.ContextEditValue))
	case key == "right":
		a.State.ContextEditCursor = clamp(a.State.ContextEditCursor+1, 0, runeLen(a.State.ContextEditValue))
	case key == "home":
		a.State.ContextEditCursor = 0
	case key == "end":
		a.State.ContextEditCursor = runeLen(a.State.ContextEditValue)
	case key == "delete":
		a.State.ContextEditValue = deleteRuneAt(a.State.ContextEditValue, a.State.ContextEditCursor)
	case key == "backspace":
		if a.State.ContextEditCursor > 0 {
			a.State.ContextEditValue = deleteRuneAt(a.State.ContextEditValue, a.State.ContextEditCursor-1)
			a.State.ContextEditCursor--
		}
	case key == "space":
		if isBoolContextItem(item) {
			a.State.ContextEditValue = toggledBoolValue(a.State.ContextEditValue)
			a.State.ContextEditCursor = runeLen(a.State.ContextEditValue)
		} else {
			a.insertContextEditText(" ")
		}
	case strings.HasPrefix(key, "char:"):
		a.insertContextEditText(strings.TrimPrefix(key, "char:"))
	}
	return false
}

func (a *App) insertContextEditText(value string) {
	runes := []rune(a.State.ContextEditValue)
	cursor := clamp(a.State.ContextEditCursor, 0, len(runes))
	insert := []rune(value)
	runes = append(runes[:cursor], append(insert, runes[cursor:]...)...)
	a.State.ContextEditValue = string(runes)
	a.State.ContextEditCursor = cursor + len(insert)
}

func isEditableContextItem(item string) bool {
	key := contextItemKey(item)
	if key == "" {
		return false
	}
	switch key {
	case "name", "cpu", "cpus", "mem", "memory", "vnc", "disk", "iso", "mode", "external", "uplink", "interface":
		return true
	default:
		return false
	}
}

func isBoolContextItem(item string) bool {
	return contextItemKey(item) == "vnc"
}

func contextItemValue(item string) string {
	_, value, ok := strings.Cut(strings.TrimSpace(item), "=")
	if !ok {
		value, ok = contextDisplayValue(item)
		if !ok {
			return ""
		}
	}
	switch strings.TrimSpace(value) {
	case "[x]", "[X]", "x", "X":
		return "true"
	case "[ ]", "[]":
		return "false"
	}
	return strings.TrimSuffix(value, "M")
}

func toggledBoolValue(value string) string {
	switch strings.TrimSpace(value) {
	case "[x]", "[X]":
		return "false"
	case "[ ]":
		return "true"
	}
	if strings.EqualFold(strings.TrimSpace(value), "true") {
		return "false"
	}
	return "true"
}

func deleteRuneAt(value string, index int) string {
	runes := []rune(value)
	if index < 0 || index >= len(runes) {
		return value
	}
	return string(append(runes[:index], runes[index+1:]...))
}

func (a *App) applyContextEdit(node Node, item, value string) {
	key := contextItemKey(item)
	if key == "" {
		return
	}
	args := map[string]string{}
	switch key {
	case "cpu", "cpus":
		args["cpus"] = value
	case "mem", "memory":
		args["memory"] = strings.TrimSuffix(value, "M")
	case "uplink":
		args["external"] = value
	case "vnc":
		args["vnc"] = contextItemValue("vnc=" + value)
	default:
		args[key] = value
	}
	switch node.Type {
	case NodeVM:
		a.vmSet(node.ID, args)
	case NodeSwitch:
		a.switchSet(node.ID, args)
	case NodeExternal:
		a.externalSet(node.ID, args)
	}
}
