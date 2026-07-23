package lab

import "strings"

func IsDHCPContainer(ct Container) bool {
	return strings.EqualFold(strings.TrimSpace(ct.Service), ContainerServiceDHCP)
}

func DHCPContainerSwitch(ct Container) (string, bool) {
	if !IsDHCPContainer(ct) || len(ct.Networks) != 1 {
		return "", false
	}
	switchID := strings.TrimSpace(ct.Networks[0].Switch)
	return switchID, switchID != ""
}
