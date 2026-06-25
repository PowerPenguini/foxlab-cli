package topologyui

import "strings"

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
		return true
	case "help", "h":
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
	case "switch", "sw":
		a.executeSwitchCommand(fields)
	case "external", "ext":
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
		a.State.Message = "usage: disk <create|attach|detach|merge|delete|layer> ..."
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
		a.diskMerge(commandArg(fields, 2))
	case "delete", "rm":
		a.diskDelete(commandArg(fields, 2))
	case "layer":
		if len(fields) >= 4 && (fields[2] == "delete" || fields[2] == "rm") {
			a.diskLayerDelete(fields[3])
			return
		}
		a.State.Message = "usage: disk layer delete <id>"
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
		for key := range args {
			if key != "to" && key != "target" {
				a.State.Message = "unsupported link add argument: " + key
				return
			}
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
	if len(fields) < 3 {
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
			a.State.Message = "usage: container set <id> image=REF command=CMD switch=ID|external=ID"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.containerSet(fields[2], args)
	case "start", "run":
		a.runWorkload(NodeContainer, commandArg(fields, 2))
	case "stop":
		a.stopWorkload(NodeContainer, commandArg(fields, 2))
	case "nic":
		if len(fields) < 3 {
			a.State.Message = "usage: container nic <add|connect> ..."
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
		default:
			a.State.Message = "unknown container nic command: " + fields[2]
		}
	case "delete", "rm":
		a.containerDelete(commandArg(fields, 2))
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
			a.State.Message = "usage: switch create <id> [mode=bridge|nat|macnat-bridge] [external=ID]"
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
			a.State.Message = "usage: switch set <id> mode=bridge external=ID"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.switchSet(fields[2], args)
	case "delete", "rm":
		a.switchDelete(commandArg(fields, 2))
	default:
		a.State.Message = "unknown switch command: " + fields[1]
	}
}

func (a *App) executeExternalCommand(fields []string) {
	if len(fields) < 2 {
		a.State.Message = "usage: external <create|set|delete> ..."
		return
	}
	switch fields[1] {
	case "create", "new":
		if len(fields) < 3 {
			a.State.Message = "usage: external create <id> interface=IFACE [mode=nat|direct|macnat]"
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
			a.State.Message = "usage: external set <id> interface=IFACE name=NAME mode=nat|direct|macnat"
			return
		}
		args, err := parseArgs(fields[3:])
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.externalSet(fields[2], args)
	case "delete", "rm":
		a.externalDelete(commandArg(fields, 2))
	default:
		a.State.Message = "unknown external command: " + fields[1]
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
			a.State.Message = "usage: vm create <id> [cpus=N] [memory=N] [switch=ID|external=ID]"
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
		a.runWorkload(NodeVM, commandArg(fields, 2))
	case "stop":
		a.stopWorkload(NodeVM, commandArg(fields, 2))
	case "nic":
		if len(fields) < 3 {
			a.State.Message = "usage: vm nic <add|connect> ..."
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
		default:
			a.State.Message = "unknown vm nic command: " + fields[2]
		}
	case "delete", "rm":
		a.vmDelete(commandArg(fields, 2))
	default:
		a.State.Message = "unknown vm command: " + fields[1]
	}
}
