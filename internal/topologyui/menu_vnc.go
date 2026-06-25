package topologyui

import (
	"strconv"
	"strings"
)

func withVNCDetailPort(details []string, port int) []string {
	out := make([]string, 0, len(details)+1)
	for _, detail := range details {
		if strings.HasPrefix(strings.TrimSpace(detail), "vnc-port=") {
			continue
		}
		out = append(out, detail)
	}
	if port > 0 {
		out = append(out, "vnc-port="+strconv.Itoa(port))
	}
	return out
}

func vncEnabled(detail string) bool {
	_, value, ok := strings.Cut(strings.TrimSpace(detail), "=")
	return ok && strings.EqualFold(strings.TrimSpace(value), "true")
}

func vncInfoItem(node Node) string {
	if port := vncPort(node); port > 0 {
		return "VNC: 127.0.0.1:" + strconv.Itoa(port)
	}
	if vncNeedsRestart(node.State) {
		return "VNC: restart needed"
	}
	return "VNC: start VM"
}

func vncPort(node Node) int {
	value := nodeDetailValue(node, "vnc-port", "")
	_, value, _ = strings.Cut(strings.TrimSpace(value), "=")
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port <= 0 {
		return 0
	}
	return port
}

func isContextInfoItem(item string) bool {
	item = strings.TrimSpace(item)
	return strings.HasPrefix(item, "VNC:") || item == noInterfacesItem
}

func vncNeedsRestart(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "running", "blocked", "paused":
		return true
	default:
		return false
	}
}
