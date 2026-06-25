package lab

import (
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
	home, err := FoxlabHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "labs", l.ID), nil
}

func (l *Lab) DiskStoragePath(id, format string) (string, error) {
	root, err := l.StorageRoot()
	if err != nil {
		return "", err
	}
	ext := ".qcow2"
	if strings.EqualFold(format, "raw") {
		ext = ".img"
	}
	return filepath.Join(root, "disks", id+ext), nil
}

func (l *Lab) LayerStoragePath(workloadType, workloadID, diskID string) (string, error) {
	root, err := l.StorageRoot()
	if err != nil {
		return "", err
	}
	name := workloadType + "-" + workloadID + "-" + diskID + ".qcow2"
	return filepath.Join(root, "layers", name), nil
}
