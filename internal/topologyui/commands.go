package topologyui

import (
	"sort"
	"strings"
)

func (a *App) executeCommand(command string) bool {
	if command == "" {
		return false
	}
	fields, err := commandFields(command)
	if err != nil {
		a.State.Message = err.Error()
		return false
	}
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "q", "quit":
		if !a.requireExactCommandArgs(fields, 1, "usage: quit") {
			return false
		}
		return true
	case "help", "h":
		if len(fields) > 2 {
			a.State.Message = "usage: help [topic]"
			return false
		}
		a.State.Console = helpLines(commandArg(fields, 1))
		a.State.Message = ""
	case "add":
		a.executeAddCommand(fields)
	case "vm":
		a.executeVMCommand(fields)
	case "container", "ct":
		a.executeContainerCommand(fields)
	case "disk":
		a.executeDiskCommand(fields)
	case "shell":
		a.executeShellCommand(fields)
	case "tabnext", "tabprev", "tabclose", "tabrestart":
		a.ensureTabs()
		a.executeTabCommand(fields)
	case "switch", "sw":
		a.executeSwitchCommand(fields)
	case "uplink", "up", "external", "ext":
		a.executeExternalCommand(fields)
	case "link", "links":
		a.executeLinkCommand(fields)
	default:
		a.State.Message = "unknown command: " + fields[0]
	}
	return false
}

func (a *App) executeDiskCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: disk <create|attach|detach|merge|resize|info|delete|layer> ..."
		return
	}
	switch fields[1] {
	case "create", "new":
		if len(fields) < 3 {
			a.State.Message = "usage: disk create <id> [size=N] [format=qcow2|raw]"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.diskCreate(fields[2], args)
	case "attach", "connect":
		if len(fields) < 4 {
			a.State.Message = "usage: disk attach <id> to=vm:<id>|container:<id>"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.diskAttach(fields[2], args)
	case "detach":
		if len(fields) < 3 {
			a.State.Message = "usage: disk detach <workload-id> [type=vm|container] [disk=ID]"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.diskDetach(fields[2], args)
	case "merge":
		if !a.requireExactCommandArgs(fields, 3, "usage: disk merge <id>") {
			return
		}
		a.diskMerge(fields[2])
	case "resize":
		if len(fields) < 4 {
			a.State.Message = "usage: disk resize <id> size=N [force=true]"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.diskResize(fields[2], args)
	case "info":
		if !a.requireExactCommandArgs(fields, 3, "usage: disk info <id>") {
			return
		}
		a.diskInfo(fields[2])
	case "rename", "mv":
		if !a.requireExactCommandArgs(fields, 4, "usage: disk rename <id> <new-id>") {
			return
		}
		a.diskRename(fields[2], fields[3])
	case "delete", "rm":
		if !a.requireExactCommandArgs(fields, 3, "usage: disk delete <id>") {
			return
		}
		a.diskDelete(fields[2])
	case "layer":
		if len(fields) == 5 && (fields[2] == "create" || fields[2] == "new") {
			a.diskLayerCreate(fields[3], fields[4])
			return
		}
		if len(fields) == 4 && (fields[2] == "delete" || fields[2] == "rm") {
			a.diskLayerDelete(fields[3])
			return
		}
		a.State.Message = "usage: disk layer create <base-id> <layer-id> | disk layer delete <id>"
	default:
		a.State.Message = "unknown disk command: " + fields[1]
	}
}

func (a *App) executeAddCommand(fields []string) {
	if len(fields) < 3 {
		a.State.Message = "usage: add <vm|sw|cont> <id> ..."
		return
	}
	args, err := parseArgs(fields[3:])
	if err != nil {
		a.State.Message = err.Error()
		return
	}
	switch fields[1] {
	case "vm":
		a.vmCreate(fields[2], args)
	case "sw", "switch":
		a.switchCreate(fields[2], args)
	case "cont", "container", "ct":
		a.containerCreate(fields[2], args)
	default:
		a.State.Message = "usage: add <vm|sw|cont> <id> ..."
	}
}

func (a *App) executeLinkCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: link <add|connect|delete> ..."
		return
	}
	switch fields[1] {
	case "add", "connect", "create":
		a.executeLinkAddCommand(fields)
	case "delete", "disconnect", "rm":
		a.executeLinkDeleteCommand(fields)
	default:
		a.State.Message = "unknown link command: " + fields[1]
	}
}

func (a *App) executeLinkAddCommand(fields []string) {
	if len(fields) < 4 {
		a.State.Message = "usage: link add <vm|container>:<id>:<nic> to=<vm|container>:<id>[:nic]"
		return
	}
	source, ok := parseLinkEndpoint(fields[2], true)
	if !ok {
		a.State.Message = "usage: link add <vm|container>:<id>:<nic> to=<vm|container>:<id>[:nic]"
		return
	}
	targetValue := ""
	if strings.HasPrefix(strings.ToLower(fields[3]), "to=") || strings.HasPrefix(strings.ToLower(fields[3]), "target=") {
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		var invalid []string
		for key := range args {
			if key != "to" && key != "target" {
				invalid = append(invalid, key)
			}
		}
		if len(invalid) > 0 {
			sort.Strings(invalid)
			a.State.Message = "unsupported link add argument: " + invalid[0]
			return
		}
		targetValue = firstNonEmpty(args["to"], args["target"])
	} else if len(fields) == 4 {
		targetValue = fields[3]
	} else {
		a.State.Message = "usage: link add <vm|container>:<id>:<nic> to=<vm|container>:<id>[:nic]"
		return
	}
	target, ok := parseLinkEndpoint(targetValue, false)
	if !ok {
		a.State.Message = "usage: link add <vm|container>:<id>:<nic> to=<vm|container>:<id>[:nic]"
		return
	}
	if target.NIC == "" {
		a.nicConnectDirect(source.Type, source.ID, source.NIC, target.Type, target.ID)
		return
	}
	a.nicConnectDirectTo(source.Type, source.ID, source.NIC, target.Type, target.ID, target.NIC)
}

func (a *App) executeLinkDeleteCommand(fields []string) {
	if len(fields) != 3 {
		a.State.Message = "usage: link delete <vm|container>:<id>:<nic>"
		return
	}
	endpoint, ok := parseLinkEndpoint(fields[2], true)
	if !ok {
		a.State.Message = "usage: link delete <vm|container>:<id>:<nic>"
		return
	}
	a.nicDisconnect(endpoint.Type, endpoint.ID, endpoint.NIC)
}

func (a *App) executeContainerCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: container <create|set|start|stop|nic|delete> ..."
		return
	}
	switch fields[1] {
	case "create", "new":
		if len(fields) < 3 {
			a.State.Message = "usage: container create <id> [image=REF] [command=CMD] [switch=ID]"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.containerCreate(fields[2], args)
	case "set", "config", "configure":
		if len(fields) < 4 {
			a.State.Message = "usage: container set <id> image=REF command=CMD switch=ID|uplink=ID"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.containerSet(fields[2], args)
	case "start", "run":
		if !a.requireExactCommandArgs(fields, 3, "usage: container start <id>") {
			return
		}
		a.runWorkload(NodeContainer, fields[2])
	case "stop":
		if !a.requireExactCommandArgs(fields, 3, "usage: container stop <id>") {
			return
		}
		a.stopWorkload(NodeContainer, fields[2])
	case "nic":
		if len(fields) < 3 {
			a.State.Message = "usage: container nic <add|connect|delete> ..."
			return
		}
		switch fields[2] {
		case "add":
			if len(fields) < 4 {
				a.State.Message = "usage: container nic add <id> [mac=MAC]"
				return
			}
			args, err := parseArgs(fields[4:])
			if err != nil {
				a.State.Message = err.Error()
				return
			}
			a.containerNICAdd(fields[3], args)
		case "connect":
			if len(fields) < 6 {
				a.State.Message = "usage: container nic connect <id> <index> to=ID [mac=MAC]"
				return
			}
			args, err := parseArgs(fields[5:])
			if err != nil {
				a.State.Message = err.Error()
				return
			}
			a.containerNICConnect(fields[3], fields[4], args)
		case "delete", "rm":
			if !a.requireExactCommandArgs(fields, 5, "usage: container nic delete <id> <index>") {
				return
			}
			a.containerNICDelete(fields[3], fields[4])
		default:
			a.State.Message = "unknown container nic command: " + fields[2]
		}
	case "delete", "rm":
		if !a.requireExactCommandArgs(fields, 3, "usage: container delete <id>") {
			return
		}
		a.containerDelete(fields[2])
	default:
		a.State.Message = "unknown container command: " + fields[1]
	}
}

func (a *App) executeSwitchCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: switch <create|set|delete> ..."
		return
	}
	switch fields[1] {
	case "create", "new":
		if len(fields) < 3 {
			a.State.Message = "usage: switch create <id> [mode=bridge|nat|macnat-bridge] [uplink=ID]"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.switchCreate(fields[2], args)
	case "set", "config", "configure":
		if len(fields) < 4 {
			a.State.Message = "usage: switch set <id> mode=bridge uplink=ID"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.switchSet(fields[2], args)
	case "delete", "rm":
		if !a.requireExactCommandArgs(fields, 3, "usage: switch delete <id>") {
			return
		}
		a.switchDelete(fields[2])
	default:
		a.State.Message = "unknown switch command: " + fields[1]
	}
}

func (a *App) executeExternalCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: uplink <create|set|delete> ..."
		return
	}
	switch fields[1] {
	case "create", "new":
		if len(fields) < 3 {
			a.State.Message = "usage: uplink create <id> interface=IFACE [mode=nat|direct|macnat]"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.externalCreate(fields[2], args)
	case "set", "config", "configure":
		if len(fields) < 4 {
			a.State.Message = "usage: uplink set <id> interface=IFACE name=NAME mode=nat|direct|macnat"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.externalSet(fields[2], args)
	case "delete", "rm":
		if !a.requireExactCommandArgs(fields, 3, "usage: uplink delete <id>") {
			return
		}
		a.externalDelete(fields[2])
	default:
		a.State.Message = "unknown uplink command: " + fields[1]
	}
}

func (a *App) executeVMCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: vm <create|set|start|stop|nic|delete> ..."
		return
	}
	switch fields[1] {
	case "create", "new":
		if len(fields) < 3 {
			a.State.Message = "usage: vm create <id> [cpus=N] [memory=N] [switch=ID|uplink=ID]"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.vmCreate(fields[2], args)
	case "set", "config", "configure":
		if len(fields) < 4 {
			a.State.Message = "usage: vm set <id> cpus=N memory=N name=NAME"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.vmSet(fields[2], args)
	case "start", "run":
		if !a.requireExactCommandArgs(fields, 3, "usage: vm start <id>") {
			return
		}
		a.runWorkload(NodeVM, fields[2])
	case "stop":
		if !a.requireExactCommandArgs(fields, 3, "usage: vm stop <id>") {
			return
		}
		a.stopWorkload(NodeVM, fields[2])
	case "nic":
		if len(fields) < 3 {
			a.State.Message = "usage: vm nic <add|connect|delete> ..."
			return
		}
		switch fields[2] {
		case "add":
			if len(fields) < 4 {
				a.State.Message = "usage: vm nic add <id> [mac=MAC]"
				return
			}
			args, err := parseArgs(fields[4:])
			if err != nil {
				a.State.Message = err.Error()
				return
			}
			a.vmNICAdd(fields[3], args)
		case "connect":
			if len(fields) < 6 {
				a.State.Message = "usage: vm nic connect <id> <index> to=ID [mac=MAC]"
				return
			}
			args, err := parseArgs(fields[5:])
			if err != nil {
				a.State.Message = err.Error()
				return
			}
			a.vmNICConnect(fields[3], fields[4], args)
		case "delete", "rm":
			if !a.requireExactCommandArgs(fields, 5, "usage: vm nic delete <id> <index>") {
				return
			}
			a.vmNICDelete(fields[3], fields[4])
		default:
			a.State.Message = "unknown vm nic command: " + fields[2]
		}
	case "delete", "rm":
		if !a.requireExactCommandArgs(fields, 3, "usage: vm delete <id>") {
			return
		}
		a.vmDelete(fields[2])
	default:
		a.State.Message = "unknown vm command: " + fields[1]
	}
}

func (a *App) requireCommandArgs(fields []string, min int, usage string) bool {
	if len(fields) >= min {
		return true
	}
	a.State.Message = usage
	return false
}

func (a *App) requireExactCommandArgs(fields []string, count int, usage string) bool {
	if len(fields) == count {
		return true
	}
	a.State.Message = usage
	return false
}
