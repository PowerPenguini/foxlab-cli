package topologyui

import "strings"

func nodeCardLine(node Node, frame, width int) string {
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

func nodeCardExtra(node Node) string {
	switch node.Type {
	case NodeVM:
		parts := []string{}
		if cpu := nodeDetailRawValue(node, "cpu"); cpu != "" {
			parts = append(parts, cpu+"c")
		}
		if mem := nodeDetailRawValue(node, "mem"); mem != "" {
			parts = append(parts, shortMemory(mem))
		}
		return strings.Join(parts, " ")
	case NodeContainer:
		return shortImage(nodeDetailRawValue(node, "image"))
	case NodeSwitch:
		return nodeDetailRawValue(node, "uplink")
	case NodeExternal:
		value := nodeDetailRawValue(node, "interface")
		if value == node.Label {
			return ""
		}
		return value
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
