package topologyui

import (
	"fmt"
	"strconv"
	"strings"

	"foxlab-cli/internal/topology"
)

func switchCreateRequest(name string, args map[string]string) (topology.SwitchCreateRequest, error) {
	if invalid := unexpectedArgs(args, switchArgumentNames); len(invalid) > 0 {
		return topology.SwitchCreateRequest{}, fmt.Errorf("unsupported switch create argument: %s", invalid[0])
	}
	return topology.SwitchCreateRequest{
		Name:   firstNonEmpty(args["name"], name),
		Mode:   args["mode"],
		Uplink: firstNonEmpty(args["uplink"], args["external"], args["externallink"]),
	}, nil
}

func switchUpdateRequest(args map[string]string) (topology.SwitchUpdate, error) {
	if invalid := unexpectedArgs(args, switchArgumentNames); len(invalid) > 0 {
		return topology.SwitchUpdate{}, fmt.Errorf("unsupported switch set argument: %s", invalid[0])
	}
	var update topology.SwitchUpdate
	if value, ok := args["name"]; ok {
		update.Name = topology.SetField(value)
	}
	if value, ok := args["mode"]; ok {
		update.Mode = topology.SetField(value)
	}
	if _, uplinkSet := args["uplink"]; uplinkSet {
		update.AttachUplink = topology.SetField(firstNonEmpty(args["uplink"], args["external"], args["externallink"]))
	} else if _, externalSet := args["external"]; externalSet {
		update.AttachUplink = topology.SetField(firstNonEmpty(args["external"], args["externallink"]))
	} else if value, legacySet := args["externallink"]; legacySet {
		update.AttachUplink = topology.SetField(value)
	}
	return update, nil
}

func externalCreateRequest(name string, args map[string]string) (topology.ExternalCreateRequest, error) {
	if invalid := unexpectedArgs(args, externalArgumentNames); len(invalid) > 0 {
		return topology.ExternalCreateRequest{}, fmt.Errorf("unsupported uplink create argument: %s", invalid[0])
	}
	return topology.ExternalCreateRequest{
		Name:      firstNonEmpty(args["name"], name),
		Interface: args["interface"],
		Mode:      args["mode"],
	}, nil
}

func externalUpdateRequest(args map[string]string) (topology.ExternalUpdate, error) {
	if invalid := unexpectedArgs(args, externalArgumentNames); len(invalid) > 0 {
		return topology.ExternalUpdate{}, fmt.Errorf("unsupported uplink set argument: %s", invalid[0])
	}
	var update topology.ExternalUpdate
	if value, ok := args["name"]; ok {
		update.Name = topology.SetField(value)
	}
	if value, ok := args["interface"]; ok {
		update.Interface = topology.SetField(value)
	}
	if value, ok := args["mode"]; ok {
		update.Mode = topology.SetField(value)
	}
	return update, nil
}

func nicAddRequest(workloadType string, args map[string]string) (topology.NICAddRequest, error) {
	if invalid := unexpectedArgs(args, nicAddArgumentNames); len(invalid) > 0 {
		return topology.NICAddRequest{}, fmt.Errorf("unsupported %s nic add argument: %s", workloadType, invalid[0])
	}
	return topology.NICAddRequest{MAC: args["mac"]}, nil
}

func nicConnectRequest(workloadType, indexValue string, args map[string]string) (topology.NICConnectRequest, error) {
	if invalid := unexpectedArgs(args, nicConnectArgumentNames); len(invalid) > 0 {
		return topology.NICConnectRequest{}, fmt.Errorf("unsupported %s nic connect argument: %s", workloadType, invalid[0])
	}
	index, ok := parseNICIndex(indexValue)
	if !ok {
		return topology.NICConnectRequest{}, fmt.Errorf("usage: %s nic connect <id> <index> to=ID", workloadType)
	}
	target := firstNonEmpty(args["to"], args["target"])
	switchRef := args["switch"]
	uplinkRef := firstNonEmpty(args["uplink"], args["external"])
	if target != "" && (switchRef != "" || uplinkRef != "") {
		return topology.NICConnectRequest{}, fmt.Errorf("%s nic connect accepts to=ID or a compatibility alias, not both", workloadType)
	}
	request := topology.NICConnectRequest{NIC: index, MAC: args["mac"]}
	switch {
	case target != "":
		request.Endpoint = topology.NetworkEndpointRef{Type: topology.NetworkEndpointAuto, ID: target}
	case switchRef != "" && uplinkRef == "":
		request.Endpoint = topology.NetworkEndpointRef{Type: topology.NetworkEndpointSwitch, ID: switchRef}
	case switchRef == "" && uplinkRef != "":
		request.Endpoint = topology.NetworkEndpointRef{Type: topology.NetworkEndpointUplink, ID: uplinkRef}
	default:
		return topology.NICConnectRequest{}, fmt.Errorf("%s nic connect needs exactly one endpoint", workloadType)
	}
	return request, nil
}

func directNetworkEndpoint(typ, id, nic string) (topology.NetworkEndpointRef, bool) {
	endpointType, ok := directNetworkEndpointType(typ)
	if !ok || strings.TrimSpace(id) == "" {
		return topology.NetworkEndpointRef{}, false
	}
	endpoint := topology.NetworkEndpointRef{Type: endpointType, ID: id}
	if nic == "" {
		return endpoint, true
	}
	index, ok := parseNICIndex(nic)
	if !ok {
		return topology.NetworkEndpointRef{}, false
	}
	endpoint.NIC = topology.SetField(index)
	return endpoint, true
}

func directNetworkEndpointType(value string) (topology.NetworkEndpointType, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case NodeVM:
		return topology.NetworkEndpointVM, true
	case NodeContainer:
		return topology.NetworkEndpointContainer, true
	default:
		return topology.NetworkEndpointAuto, false
	}
}

func parseNICIndex(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, false
	}
	return parsed, true
}

var switchArgumentNames = map[string]struct{}{
	"name": {}, "mode": {}, "external": {}, "externallink": {}, "uplink": {},
}

var externalArgumentNames = map[string]struct{}{
	"name": {}, "interface": {}, "mode": {},
}

var nicAddArgumentNames = map[string]struct{}{
	"mac": {},
}

var nicConnectArgumentNames = map[string]struct{}{
	"to": {}, "target": {}, "switch": {}, "external": {}, "uplink": {}, "mac": {},
}
