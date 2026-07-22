package topology

import (
	"path/filepath"
	"strings"

	"foxlab-cli/internal/lab"
)

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
