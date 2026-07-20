package daemoncontrol

import "strconv"

func systemdUnitData(binary string) []byte {
	return []byte("[Unit]\n" +
		"Description=FoxLab reconciliator\n\n" +
		"After=containerd.service libvirtd.service\n" +
		"Wants=containerd.service libvirtd.service\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"Environment=FOXLAB_LAB=/root/.foxlab/default.lab\n" +
		"Environment=FOXLAB_STATUS_SOCKET=/run/foxlab/foxlabd.sock\n" +
		"ExecStart=" + systemdExecPath(binary) + " --lab ${FOXLAB_LAB} --status-socket ${FOXLAB_STATUS_SOCKET}\n" +
		"Restart=on-failure\n" +
		"RestartSec=2s\n\n" +
		"[Install]\n" +
		"WantedBy=multi-user.target\n")
}

func systemdDropInData(labPath, statusSocket, home, userName string) []byte {
	env := []string{
		"FOXLAB_LAB=" + labPath,
		"FOXLAB_STATUS_SOCKET=" + statusSocket,
	}
	if home != "" {
		env = append(env, "HOME="+home)
		if userName != "" {
			env = append(env, "SUDO_USER="+userName)
		}
	}
	data := []byte("[Service]\n")
	for _, value := range env {
		data = append(data, []byte("Environment="+strconv.Quote(value)+"\n")...)
	}
	return data
}
