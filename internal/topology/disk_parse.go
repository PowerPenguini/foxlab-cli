package topology

import (
	"path/filepath"
	"strconv"
	"strings"

	"foxlab-cli/internal/lab"
)

func parseDiskTarget(value string) (string, string, bool) {
	typ, id, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok || id == "" {
		return "", "", false
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	switch typ {
	case "vm", "container":
		return typ, strings.TrimSpace(id), true
	case "ct":
		return "container", strings.TrimSpace(id), true
	default:
		return "", "", false
	}
}

func parseDiskCreateTarget(args map[string]string) (string, string, bool, bool) {
	target := firstNonEmpty(args["to"], args["target"], args["attach"])
	if target == "" {
		return "", "", false, true
	}
	targetType, targetID, ok := parseDiskTarget(target)
	return targetType, targetID, true, ok
}

func diskKind(disk lab.Disk) string {
	if disk.Kind == "" {
		return "base"
	}
	return disk.Kind
}

func diskFormat(disk lab.Disk) string {
	if disk.Format != "" {
		return disk.Format
	}
	ext := strings.ToLower(filepath.Ext(disk.Path))
	if ext == ".img" || ext == ".raw" {
		return "raw"
	}
	return "qcow2"
}

func diskSizeGB(value string, fallback int) int {
	value = strings.TrimSpace(strings.ToUpper(value))
	value = strings.TrimSuffix(value, "GB")
	value = strings.TrimSuffix(value, "G")
	if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
		return parsed
	}
	return fallback
}
