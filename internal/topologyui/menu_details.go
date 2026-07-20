package topologyui

import "strings"

func nonNICDetails(details []string) []string {
	out := make([]string, 0, len(details))
	for _, detail := range details {
		if !isNICDetail(detail) && !isRuntimeDetail(detail) && !isDiskDetail(detail) && !isPermissionDetail(detail) {
			out = append(out, detail)
		}
	}
	return out
}

func switchConfigurationDetails(details []string) []string {
	out := make([]string, 0, len(details))
	for _, detail := range nonNICDetails(details) {
		key, _, ok := strings.Cut(strings.TrimSpace(detail), "=")
		if ok {
			switch strings.ToLower(key) {
			case "uplink", "external", "externallink":
				continue
			}
		}
		out = append(out, detail)
	}
	return out
}

func nicDetails(details []string) []string {
	out := make([]string, 0, len(details))
	for _, detail := range details {
		if isNICDetail(detail) {
			out = append(out, detail)
		}
	}
	return out
}

func isNICDetail(detail string) bool {
	return strings.HasPrefix(strings.TrimSpace(detail), "nic")
}

func isRuntimeDetail(detail string) bool {
	return strings.HasPrefix(strings.TrimSpace(detail), "vnc-port=")
}

func isDiskDetail(detail string) bool {
	return strings.HasPrefix(strings.TrimSpace(detail), "disk=")
}

func isPermissionDetail(detail string) bool {
	return strings.HasPrefix(strings.TrimSpace(detail), "capabilities=")
}

func isDiskMenuDetail(detail string) bool {
	_, marker, ok := diskMenuParts(detail)
	return ok && marker != "base"
}

func isDiskAttachMenuDetail(detail string) bool {
	_, marker, ok := diskMenuParts(detail)
	return ok && marker == "base"
}

func diskMenuParts(detail string) (string, string, bool) {
	detail = strings.TrimLeft(strings.TrimRight(detail, " \t"), " \t")
	if strings.HasPrefix(detail, strings.TrimSpace(diskMenuLayerTreePrefix)+" ") {
		detail = strings.TrimSpace(strings.TrimPrefix(detail, strings.TrimSpace(diskMenuLayerTreePrefix)+" "))
		if !strings.Contains(detail, "|") {
			return detail, detail, detail != ""
		}
	}
	left, right, ok := strings.Cut(detail, "|")
	if !ok {
		return "", "", false
	}
	left = strings.TrimSpace(left)
	fields := strings.Fields(strings.TrimSpace(right))
	if left == "" || len(fields) == 0 {
		return "", "", false
	}
	return left, fields[0], true
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
