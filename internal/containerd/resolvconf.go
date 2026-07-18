package containerd

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"foxlab-cli/internal/lab"
)

const hostResolvconfPath = "/etc/resolv.conf"

var publicDNSFallbacks = []string{"1.1.1.1", "8.8.8.8"}

func syncContainerResolvconf(l *lab.Lab, ct lab.Container) (string, error) {
	return syncContainerResolvconfFrom(l, ct, hostResolvconfPath)
}

func syncContainerResolvconfFrom(l *lab.Lab, ct lab.Container, sourcePath string) (string, error) {
	managedPath, err := containerResolvconfPath(l, ct)
	if err != nil {
		return "", err
	}
	hostConfig, err := os.ReadFile(sourcePath)
	if err != nil {
		if managedResolvconfValid(managedPath) {
			return managedPath, nil
		}
		return "", fmt.Errorf("read host resolv.conf: %w", err)
	}
	config := buildContainerResolvconf(hostConfig)
	if err := writeResolvconfInPlace(managedPath, config); err != nil {
		return "", fmt.Errorf("write managed resolv.conf: %w", err)
	}
	return managedPath, nil
}

func containerResolvconfPath(l *lab.Lab, ct lab.Container) (string, error) {
	if l == nil {
		return "", fmt.Errorf("missing lab")
	}
	home, err := lab.FoxlabHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "run", "resolv", l.ManagedContainerName(ct)+".conf"), nil
}

func buildContainerResolvconf(hostConfig []byte) []byte {
	directives := []string{}
	seenDirectives := map[string]bool{}
	hostNameserver := ""
	for _, line := range strings.Split(string(hostConfig), "\n") {
		line = strings.TrimSpace(line)
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") || strings.HasPrefix(fields[0], ";") {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "search", "domain", "options":
			if !seenDirectives[line] {
				directives = append(directives, line)
				seenDirectives[line] = true
			}
		case "nameserver":
			if hostNameserver == "" && len(fields) > 1 && usableContainerNameserver(fields[1]) {
				hostNameserver = fields[1]
			}
		}
	}

	nameservers := []string{}
	seenNameservers := map[string]bool{}
	addNameserver := func(address string) {
		if address == "" || seenNameservers[address] || len(nameservers) >= 3 {
			return
		}
		nameservers = append(nameservers, address)
		seenNameservers[address] = true
	}
	addNameserver(hostNameserver)
	for _, address := range publicDNSFallbacks {
		addNameserver(address)
	}

	lines := []string{"# Managed by FoxLab; generated from the host resolver."}
	lines = append(lines, directives...)
	for _, address := range nameservers {
		lines = append(lines, "nameserver "+address)
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func usableContainerNameserver(address string) bool {
	parseAddress := address
	if base, _, ok := strings.Cut(address, "%"); ok {
		parseAddress = base
	}
	ip := net.ParseIP(parseAddress)
	return ip != nil && !ip.IsLoopback()
}

func managedResolvconfValid(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && strings.EqualFold(fields[0], "nameserver") && usableContainerNameserver(fields[1]) {
			return true
		}
	}
	return false
}

func writeResolvconfInPlace(path string, data []byte) error {
	current, err := os.ReadFile(path)
	if err == nil && bytes.Equal(current, data) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	restore := func() {
		if current == nil {
			return
		}
		_ = file.Truncate(0)
		_, _ = file.WriteAt(current, 0)
	}
	if err := file.Truncate(0); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.WriteAt(data, 0); err != nil {
		restore()
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0o644); err != nil {
		restore()
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		restore()
		_ = file.Close()
		return err
	}
	return file.Close()
}
