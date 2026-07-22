package topologyui

import (
	"fmt"
	"strconv"
	"strings"

	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/workload"
)

const defaultDiskCreateSizeGB = 10

func diskCreateRequest(id string, args map[string]string) (topology.DiskCreateRequest, error) {
	if invalid := unexpectedArgs(args, diskCreateArgumentNames); len(invalid) > 0 {
		return topology.DiskCreateRequest{}, fmt.Errorf("unsupported disk create argument: %s", invalid[0])
	}
	request := topology.DiskCreateRequest{ID: id}
	if target := firstNonEmpty(args["to"], args["target"], args["attach"]); target != "" {
		ref, ok := parseDiskWorkloadRef(target)
		if !ok {
			return topology.DiskCreateRequest{}, fmt.Errorf("usage: disk create <id> [size=N] [format=qcow2|raw] [to=vm:<id>|container:<id>]")
		}
		request.AttachTo = topology.SetField(ref)
	}
	format := strings.ToLower(firstNonEmpty(args["format"], string(topology.DiskFormatQCOW2)))
	switch topology.DiskFormat(format) {
	case topology.DiskFormatQCOW2, topology.DiskFormatRaw:
		request.Format = topology.DiskFormat(format)
	default:
		return topology.DiskCreateRequest{}, fmt.Errorf("unsupported disk format: %s", format)
	}
	if value, present := args["size"]; present {
		sizeGB, ok := parseDiskCreateSizeGB(value)
		if !ok {
			return topology.DiskCreateRequest{}, fmt.Errorf("invalid disk size: %s", value)
		}
		request.SizeGB = topology.SetField(sizeGB)
	}
	return request, nil
}

func diskAttachRequest(id string, args map[string]string) (topology.DiskAttachRequest, error) {
	if invalid := unexpectedArgs(args, diskAttachArgumentNames); len(invalid) > 0 {
		return topology.DiskAttachRequest{}, fmt.Errorf("unsupported disk attach argument: %s", invalid[0])
	}
	ref, ok := parseDiskWorkloadRef(firstNonEmpty(args["to"], args["target"]))
	if !ok {
		return topology.DiskAttachRequest{}, fmt.Errorf("usage: disk attach <id> to=vm:<id>|container:<id>")
	}
	return topology.DiskAttachRequest{DiskID: id, Target: ref}, nil
}

func diskDetachRequest(target string, args map[string]string) (topology.DiskDetachRequest, error) {
	if invalid := unexpectedArgs(args, diskDetachArgumentNames); len(invalid) > 0 {
		return topology.DiskDetachRequest{}, fmt.Errorf("unsupported disk detach argument: %s", invalid[0])
	}
	targetType := strings.ToLower(strings.TrimSpace(args["type"]))
	if targetType != "" && targetType != workload.TypeVM && targetType != workload.TypeContainer {
		return topology.DiskDetachRequest{}, fmt.Errorf("disk target must be vm or container")
	}
	ref := workload.Ref{Type: targetType, ID: strings.TrimSpace(target)}
	if parsed, ok := parseDiskWorkloadRef(firstNonEmpty(args["from"], args["target"], target)); ok {
		ref = parsed
	}
	return topology.DiskDetachRequest{Target: ref, DiskID: args["disk"]}, nil
}

func diskResizeRequest(id string, args map[string]string) (topology.DiskResizeRequest, error) {
	if invalid := unexpectedArgs(args, diskResizeArgumentNames); len(invalid) > 0 {
		return topology.DiskResizeRequest{}, fmt.Errorf("unsupported disk resize argument: %s", invalid[0])
	}
	sizeValue, present := args["size"]
	if !present {
		return topology.DiskResizeRequest{}, fmt.Errorf("usage: disk resize <id> size=N [force=true]")
	}
	sizeGB, err := strconv.Atoi(sizeValue)
	if err != nil || sizeGB <= 0 {
		return topology.DiskResizeRequest{}, fmt.Errorf("usage: disk resize <id> size=N [force=true]")
	}
	request := topology.DiskResizeRequest{DiskID: id, SizeGB: sizeGB}
	if value, present := args["force"]; present {
		force, ok := parseCommandBool(value)
		if !ok {
			return topology.DiskResizeRequest{}, fmt.Errorf("invalid disk resize force: %s", value)
		}
		request.Force = force
	}
	return request, nil
}

func parseDiskWorkloadRef(value string) (workload.Ref, bool) {
	typ, id, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok {
		return workload.Ref{}, false
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "ct" {
		typ = workload.TypeContainer
	}
	id = strings.TrimSpace(id)
	if id == "" || (typ != workload.TypeVM && typ != workload.TypeContainer) {
		return workload.Ref{}, false
	}
	return workload.Ref{Type: typ, ID: id}, true
}

func parseDiskCreateSizeGB(value string) (int, bool) {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return defaultDiskCreateSizeGB, true
	}
	value = strings.TrimSuffix(value, "GB")
	value = strings.TrimSuffix(value, "G")
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil && parsed > 0
}

var diskCreateArgumentNames = map[string]struct{}{
	"size": {}, "format": {}, "to": {}, "target": {}, "attach": {},
}

var diskAttachArgumentNames = map[string]struct{}{
	"to": {}, "target": {},
}

var diskDetachArgumentNames = map[string]struct{}{
	"type": {}, "from": {}, "target": {}, "disk": {},
}

var diskResizeArgumentNames = map[string]struct{}{
	"size": {}, "force": {},
}
