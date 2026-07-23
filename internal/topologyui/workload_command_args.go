package topologyui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"foxlab-cli/internal/topology"
)

func vmCreateRequest(name string, args map[string]string) (topology.VMCreateRequest, error) {
	if invalid := unexpectedArgs(args, vmCreateArgumentNames); len(invalid) > 0 {
		return topology.VMCreateRequest{}, fmt.Errorf("unsupported vm create argument: %s", invalid[0])
	}
	request := topology.VMCreateRequest{
		Name: firstNonEmpty(args["name"], name),
		Disk: args["disk"],
		Network: topology.WorkloadNetworkInput{
			Switch: args["switch"],
			Uplink: firstNonEmpty(args["uplink"], args["external"]),
		},
	}
	if value, ok := args["cpus"]; ok {
		parsed, err := parsePositiveWorkloadInt("vm cpus", value)
		if err != nil {
			return topology.VMCreateRequest{}, err
		}
		request.CPUs = topology.SetField(parsed)
	}
	if value, ok := args["memory"]; ok {
		parsed, err := parsePositiveWorkloadInt("vm memory", value)
		if err != nil {
			return topology.VMCreateRequest{}, err
		}
		request.MemoryMB = topology.SetField(parsed)
	}
	if value, ok := args["mem"]; ok {
		parsed, err := parsePositiveWorkloadInt("vm memory", value)
		if err != nil {
			return topology.VMCreateRequest{}, err
		}
		request.MemoryMB = topology.SetField(parsed)
	}
	return request, nil
}

func vmUpdateRequest(args map[string]string) (topology.VMUpdate, error) {
	if invalid := unexpectedArgs(args, vmUpdateArgumentNames); len(invalid) > 0 {
		return topology.VMUpdate{}, fmt.Errorf("unsupported vm set argument: %s", invalid[0])
	}
	update := topology.VMUpdate{Network: topology.WorkloadNetworkInput{
		Switch: args["switch"],
		Uplink: firstNonEmpty(args["uplink"], args["external"]),
	}}
	if value, ok := args["name"]; ok {
		update.Name = topology.SetField(value)
	}
	if value, ok := args["disk"]; ok {
		update.Disk = topology.SetField(value)
	}
	if value, ok := args["iso"]; ok {
		update.ISO = topology.SetField(value)
	}
	if value, ok := args["vnc"]; ok {
		parsed, valid := parseCommandBool(value)
		if !valid {
			return topology.VMUpdate{}, fmt.Errorf("invalid vm vnc: %s", value)
		}
		update.VNC = topology.SetField(parsed)
	}
	if value, ok := args["cpus"]; ok {
		parsed, err := parsePositiveWorkloadInt("vm cpus", value)
		if err != nil {
			return topology.VMUpdate{}, err
		}
		update.CPUs = topology.SetField(parsed)
	}
	if value, ok := args["memory"]; ok {
		parsed, err := parsePositiveWorkloadInt("vm memory", value)
		if err != nil {
			return topology.VMUpdate{}, err
		}
		update.MemoryMB = topology.SetField(parsed)
	}
	if value, ok := args["mem"]; ok {
		parsed, err := parsePositiveWorkloadInt("vm memory", value)
		if err != nil {
			return topology.VMUpdate{}, err
		}
		update.MemoryMB = topology.SetField(parsed)
	}
	return update, nil
}

func containerCreateRequest(name string, args map[string]string) (topology.ContainerCreateRequest, error) {
	if invalid := unexpectedArgs(args, containerArgumentNames); len(invalid) > 0 {
		return topology.ContainerCreateRequest{}, fmt.Errorf("unsupported container create argument: %s", invalid[0])
	}
	return topology.ContainerCreateRequest{
		Name:    firstNonEmpty(args["name"], name),
		Image:   args["image"],
		Disk:    args["disk"],
		Command: splitWorkloadCommand(args["command"]),
		Shell:   args["shell"],
		Env:     parseWorkloadEnv(args["env"]),
		Network: topology.WorkloadNetworkInput{
			Switch: args["switch"],
			Uplink: firstNonEmpty(args["uplink"], args["external"]),
			MAC:    args["mac"],
		},
	}, nil
}

func containerUpdateRequest(args map[string]string) (topology.ContainerUpdate, error) {
	if invalid := unexpectedArgs(args, containerArgumentNames); len(invalid) > 0 {
		return topology.ContainerUpdate{}, fmt.Errorf("unsupported container set argument: %s", invalid[0])
	}
	update := topology.ContainerUpdate{Network: topology.WorkloadNetworkInput{
		Switch: args["switch"],
		Uplink: firstNonEmpty(args["uplink"], args["external"]),
		MAC:    args["mac"],
	}}
	if value, ok := args["name"]; ok {
		update.Name = topology.SetField(value)
	}
	if value, ok := args["image"]; ok {
		update.Image = topology.SetField(value)
	}
	if value, ok := args["disk"]; ok {
		update.Disk = topology.SetField(value)
	}
	if value, ok := args["command"]; ok {
		update.Command = topology.SetField(splitWorkloadCommand(value))
	}
	if value, ok := args["shell"]; ok {
		update.Shell = topology.SetField(value)
	}
	if value, ok := args["env"]; ok {
		update.Env = topology.SetField(parseWorkloadEnv(value))
	}
	return update, nil
}

func dhcpCreateRequest(name string, args map[string]string) (topology.DHCPCreateRequest, error) {
	if invalid := unexpectedArgs(args, dhcpArgumentNames); len(invalid) > 0 {
		return topology.DHCPCreateRequest{}, fmt.Errorf("unsupported DHCP create argument: %s", invalid[0])
	}
	return topology.DHCPCreateRequest{
		Name:   firstNonEmpty(args["name"], name),
		Switch: args["switch"],
	}, nil
}

func parsePositiveWorkloadInt(field, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s: %s", field, value)
	}
	return parsed, nil
}

func splitWorkloadCommand(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Fields(value)
}

func parseWorkloadEnv(value string) map[string]string {
	if value == "" {
		return nil
	}
	out := map[string]string{}
	for _, pair := range strings.Split(value, ",") {
		key, val, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && strings.TrimSpace(key) != "" {
			out[strings.TrimSpace(key)] = strings.TrimSpace(val)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func unexpectedArgs(args map[string]string, valid map[string]struct{}) []string {
	var invalid []string
	for key := range args {
		if _, ok := valid[key]; !ok {
			invalid = append(invalid, key)
		}
	}
	sort.Strings(invalid)
	return invalid
}

var vmCreateArgumentNames = map[string]struct{}{
	"name": {}, "cpus": {}, "memory": {}, "mem": {}, "disk": {}, "switch": {}, "external": {}, "uplink": {},
}

var vmUpdateArgumentNames = map[string]struct{}{
	"name": {}, "disk": {}, "iso": {}, "vnc": {}, "cpus": {}, "memory": {}, "mem": {}, "switch": {}, "external": {}, "uplink": {},
}

var containerArgumentNames = map[string]struct{}{
	"name": {}, "image": {}, "disk": {}, "command": {}, "shell": {}, "env": {}, "switch": {}, "external": {}, "uplink": {}, "mac": {},
}

var dhcpArgumentNames = map[string]struct{}{
	"name": {}, "switch": {},
}
