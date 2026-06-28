package lab

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

var userHomeDir = os.UserHomeDir
var effectiveUserID = os.Geteuid
var lookupUserHome = func(name string) (string, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return "", err
	}
	return u.HomeDir, nil
}

func FoxlabHome() (string, error) {
	home, err := foxlabUserHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".foxlab"), nil
}

func foxlabUserHome() (string, error) {
	if effectiveUserID() == 0 {
		sudoUser := strings.TrimSpace(os.Getenv("SUDO_USER"))
		if sudoUser != "" && sudoUser != "root" {
			if home, err := lookupUserHome(sudoUser); err == nil && home != "" {
				return home, nil
			}
		}
	}
	return userHomeDir()
}

func (l *Lab) StorageRoot() (string, error) {
	if l == nil {
		return "", fmt.Errorf("missing lab")
	}
	if !validID(l.ID) {
		return "", fmt.Errorf("lab name %q is invalid", l.ID)
	}
	home, err := FoxlabHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "labs", l.ID), nil
}

func (l *Lab) DiskStoragePath(id, format string) (string, error) {
	if !validID(id) {
		return "", fmt.Errorf("disk id %q is invalid", id)
	}
	root, err := l.StorageRoot()
	if err != nil {
		return "", err
	}
	ext := ".qcow2"
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "qcow2":
	case "raw":
		ext = ".img"
	default:
		return "", fmt.Errorf("disk format %q is invalid", format)
	}
	return filepath.Join(root, "disks", id+ext), nil
}

func (l *Lab) LayerStoragePath(workloadType, workloadID, diskID string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(workloadType)) {
	case "vm", "container":
	default:
		return "", fmt.Errorf("workload type %q is invalid", workloadType)
	}
	if !validID(workloadID) {
		return "", fmt.Errorf("workload id %q is invalid", workloadID)
	}
	if !validID(diskID) {
		return "", fmt.Errorf("disk id %q is invalid", diskID)
	}
	root, err := l.StorageRoot()
	if err != nil {
		return "", err
	}
	name := strings.ToLower(strings.TrimSpace(workloadType)) + "-" + workloadID + "-" + diskID + ".qcow2"
	return filepath.Join(root, "layers", name), nil
}
