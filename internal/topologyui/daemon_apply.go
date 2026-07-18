package topologyui

import "foxlab-cli/internal/daemoncontrol"

type DaemonStatus = daemoncontrol.Status

type DaemonApplyRequest = daemoncontrol.ApplyRequest

type DaemonController = daemoncontrol.Controller

func newSystemdDaemonController() DaemonController {
	return daemoncontrol.NewSystemdController()
}

func sameLabPath(left, right string) bool {
	return daemoncontrol.SameLabPath(left, right)
}

func userStatusSocketPath() (string, error) {
	return daemoncontrol.UserStatusSocketPath()
}
