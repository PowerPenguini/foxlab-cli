package topologyui

import "strings"

func nodeCardLines(node Node, frame, width int) []string {
	if node.Type == NodeExternal {
		return externalNodeCardLines(node, frame, width)
	}
	return []string{nodeCardLine(node, frame, width)}
}

func nodeCardLine(node Node, frame, width int) string {
	if node.Type == NodeVM {
		return displayNodeState(node.State, frame)
	}
	if node.Type == NodeSwitch {
		return fit("Mode: "+displayNodeState(node.State, frame), width)
	}
	base := displayNodeState(node.State, frame)
	extra := nodeCardExtra(node)
	if extra == "" {
		return base
	}
	full := base + " " + extra
	if runeLen(full) <= width {
		return full
	}
	compact := compactDisplayNodeState(node.State, frame)
	full = compact + " " + extra
	if runeLen(full) <= width {
		return full
	}
	return base
}

func externalNodeCardLines(node Node, frame, width int) []string {
	mode := nodeDetailRawValue(node, "mode")
	if mode == "" {
		mode = node.State
	}
	if mode == "" {
		mode = "link"
	}
	iface := nodeDetailRawValue(node, "interface")
	lines := []string{"Mode: " + modeDisplayLabel(mode)}
	if iface != "" && iface != "-" {
		lines = append(lines, "Iface: "+iface)
	} else {
		lines[0] = displayNodeState(mode, frame)
	}
	for i := range lines {
		lines[i] = fit(lines[i], width)
	}
	return lines
}

func nodeCardExtra(node Node) string {
	switch node.Type {
	case NodeContainer:
		return shortImage(nodeDetailRawValue(node, "image"))
	default:
		return ""
	}
}

func nodeDetailRawValue(node Node, key string) string {
	for _, detail := range node.Details {
		left, right, ok := strings.Cut(detail, "=")
		if ok && (left == key || equivalentDetailKey(left, key)) {
			return strings.TrimSpace(right)
		}
	}
	return ""
}

func equivalentDetailKey(left, key string) bool {
	switch key {
	case "cpu":
		return left == "cpus"
	case "mem":
		return left == "memory"
	default:
		return false
	}
}

func compactDisplayNodeState(state string, frame int) string {
	if animatedState(state) {
		return spinner(frame) + " " + compactStateWord(state)
	}
	if glyph := stateGlyph(state); glyph != "" {
		return glyph + " " + compactStateWord(state)
	}
	return compactStateWord(state)
}

func compactStateWord(state string) string {
	switch state {
	case "running":
		return "run"
	case "defined":
		return "def"
	case "missing":
		return "miss"
	case "starting":
		return "start"
	case "stopping":
		return "stop"
	default:
		return state
	}
}

func shortDetailValue(key, value string) string {
	switch key {
	case "image":
		return shortImage(value)
	case "mem", "memory":
		return shortMemory(value)
	default:
		if key == "mode" {
			return modeDisplayLabel(value)
		}
		return value
	}
}

func modeDisplayLabel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "bridge":
		return "Bridge"
	case "nat":
		return "NAT"
	case "macnat", "macnat-bridge":
		return "MACNAT"
	case "direct":
		return "Direct"
	default:
		return value
	}
}

func modeValueForNode(nodeType, label string) string {
	value := strings.ToLower(strings.TrimSpace(label))
	switch nodeType {
	case NodeSwitch:
		switch value {
		case "bridge":
			return "bridge"
		case "nat":
			return "nat"
		case "macnat", "macnat-bridge":
			return "macnat-bridge"
		default:
			return value
		}
	case NodeExternal:
		switch value {
		case "nat":
			return "nat"
		case "direct":
			return "direct"
		case "macnat", "macnat-bridge":
			return "macnat"
		default:
			return value
		}
	default:
		return value
	}
}

func shortImage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimSuffix(value, ":latest")
	if i := strings.LastIndex(value, "/"); i >= 0 && i+1 < len(value) {
		value = value[i+1:]
	}
	return value
}

func shortMemory(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "M") {
		raw := strings.TrimSuffix(value, "M")
		if raw == "1024" {
			return "1G"
		}
		if raw == "2048" {
			return "2G"
		}
		if raw == "4096" {
			return "4G"
		}
	}
	return value
}
