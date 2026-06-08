package topologytui

import (
	"context"
	"fmt"
	"path/filepath"

	"foxlab-cli/internal/lab"
)

func (a *App) runGlobalMenuAction(action string) {
	switch action {
	case "create-vm":
		a.openCreateVMCommand(Node{})
	case "create-switch":
		a.openCreateSwitchCommand(Node{})
	case "create-external":
		a.openCreateExternalCommand()
	}
}

func (a *App) runMenuAction(node Node, action string) {
	switch action {
	case "edit":
		a.openConfigCommand(node)
	case "rename":
		switch node.Type {
		case NodeVM:
			if vm, ok := a.labVM(node.ID); ok {
				a.openCommand("vm set " + node.ID + " name=" + commandValue(firstNonEmpty(vm.Name, vm.ID)))
			} else {
				a.openCommand("vm set " + node.ID + " name=" + commandValue(node.Label))
			}
		case NodeExternal:
			a.openExternalNameCommand(node.ID)
		case NodeSwitch:
			a.openSwitchNameCommand(node.ID)
		default:
			a.openCommand("vm set " + node.ID + " name=" + commandValue(node.Label))
		}
	case "name":
		switch node.Type {
		case NodeSwitch:
			a.openSwitchNameCommand(node.ID)
		case NodeExternal:
			a.openExternalNameCommand(node.ID)
		}
	case "interface":
		if node.Type == NodeExternal {
			a.openExternalInterfaceCommand(node.ID)
		}
	case "disk":
		a.openDiskCommand(node.ID)
	case "iso":
		if vm, ok := a.labVM(node.ID); ok {
			a.openCommand("vm set " + node.ID + " iso=" + commandValue(vm.ISO))
		} else {
			a.openCommand("vm set " + node.ID + " iso=")
		}
	case "create-vm":
		a.openCreateVMCommand(node)
	case "create-switch":
		a.openCreateSwitchCommand(node)
	case "create-external":
		a.openCreateExternalCommand()
	case "delete":
		switch node.Type {
		case NodeVM:
			a.vmDelete(node.ID)
		case NodeSwitch:
			a.switchDelete(node.ID)
		case NodeExternal:
			a.externalDelete(node.ID)
		}
	case "move":
		a.startMove(node)
	case "run":
		a.runVM(node.ID)
	case "stop":
		a.stopVM(node.ID)
	}
}

func (a *App) runVM(id string) {
	if a.Lab == nil {
		a.State.Message = "run needs a loaded .lab file"
		return
	}
	runtime, closeRuntime, err := a.runtime()
	if err != nil {
		a.State.Message = "libvirt connection failed: " + err.Error()
		return
	}
	defer closeRuntime()
	if err := runtime.StartVM(context.Background(), a.Lab, id); err != nil {
		a.State.Message = "run failed: " + err.Error()
		return
	}
	a.State.Message = "running vm:" + id
	a.refreshVMStates()
}

func (a *App) stopVM(id string) {
	if a.Lab == nil {
		a.State.Message = "stop needs a loaded .lab file"
		return
	}
	runtime, closeRuntime, err := a.runtime()
	if err != nil {
		a.State.Message = "libvirt connection failed: " + err.Error()
		return
	}
	defer closeRuntime()
	if err := runtime.StopVM(context.Background(), a.Lab, id); err != nil {
		a.State.Message = "stop failed: " + err.Error()
		return
	}
	a.State.Message = "stopping vm:" + id
	a.refreshVMStates()
}

func (a *App) openCreateVMCommand(node Node) {
	a.openCommand("vm create " + a.nextVMID() + " cpus=2 memory=2048" + a.createVMHint(node))
}

func (a *App) openCreateSwitchCommand(node Node) {
	id := a.nextSwitchID()
	cmd := "switch create " + id + " mode=bridge name=" + id
	if node.Type == NodeExternal {
		cmd = "switch create " + id + " mode=macnat-bridge external=" + node.ID + " name=" + id
	}
	a.openCommand(cmd)
}

func (a *App) openCreateExternalCommand() {
	id := a.nextExternalID()
	a.openCommand("external create " + id + " interface= name=" + id)
}

func (a *App) openSwitchNameCommand(id string) {
	name := id
	if sw, ok := a.labSwitch(id); ok {
		name = firstNonEmpty(sw.Name, sw.ID)
	}
	a.openCommand("switch set " + id + " name=" + commandValue(name))
}

func (a *App) openExternalNameCommand(id string) {
	name := id
	if link, ok := a.labExternal(id); ok {
		name = firstNonEmpty(link.Name, link.ID)
	}
	a.openCommand("external set " + id + " name=" + commandValue(name))
}

func (a *App) openExternalInterfaceCommand(id string) {
	if link, ok := a.labExternal(id); ok {
		a.openCommand("external set " + id + " interface=" + commandValue(link.Interface))
		return
	}
	a.openCommand("external set " + id + " interface=")
}

func (a *App) openExternalSwitchCommand(id string) {
	switchID := a.switchForExternal(id)
	if switchID == "" {
		switchID = a.firstSwitchID()
	}
	if switchID == "" {
		a.openCreateSwitchCommand(Node{ID: id, Type: NodeExternal})
		return
	}
	a.openCommand("switch set " + switchID + " mode=macnat-bridge external=" + id)
}

func (a *App) openDiskCommand(id string) {
	vm, ok := a.labVM(id)
	if !ok {
		a.openCommand("vm set " + id + " disk=")
		return
	}
	cmd := "vm set " + id + " disk=" + commandValue(vm.Disk)
	a.openCommand(cmd)
}

func (a *App) openConfigCommand(node Node) {
	switch node.Type {
	case NodeVM:
		if vm, ok := a.labVM(node.ID); ok {
			a.openCommand(fmt.Sprintf("vm set %s name=%s cpus=%d memory=%d vnc=%t", node.ID, commandValue(firstNonEmpty(vm.Name, vm.ID)), vm.CPUs, vm.MemoryMB, vm.VNC))
		} else {
			a.openCommand("vm set " + node.ID + " cpus=2 memory=2048")
		}
	case NodeSwitch:
		if sw, ok := a.labSwitch(node.ID); ok {
			cmd := fmt.Sprintf("switch set %s mode=%s", node.ID, firstNonEmpty(sw.Mode, "bridge"))
			if sw.ExternalLink != "" {
				cmd += " external=" + sw.ExternalLink
			}
			a.openCommand(cmd)
		} else {
			a.openCommand("switch set " + node.ID + " mode=bridge")
		}
	case NodeExternal:
		if link, ok := a.labExternal(node.ID); ok {
			a.openCommand(fmt.Sprintf("external set %s interface=%s name=%s", node.ID, commandValue(link.Interface), commandValue(firstNonEmpty(link.Name, node.ID))))
		} else {
			a.openCommand("external set " + node.ID + " interface=")
		}
	}
}

func (a *App) vmCreate(id string, args map[string]string) {
	if a.Lab == nil {
		a.State.Message = "vm create needs a loaded .lab file"
		return
	}
	if a.hasLabVM(id) {
		a.State.Message = "vm already exists: " + id
		return
	}
	if invalid := unexpectedVMCreateArgs(args); len(invalid) > 0 {
		a.State.Message = "unsupported vm create argument: " + invalid[0]
		return
	}
	cpus := intArg(args, "cpus", 2)
	memory := intArg(args, "memory", 2048)
	if value, ok := positiveInt(args["mem"]); ok {
		memory = value
	}
	diskPath := firstNonEmpty(args["disk"], filepath.ToSlash(filepath.Join("labs", a.Lab.ID, "disks", id+".qcow2")))
	vm := lab.VM{
		ID:       id,
		Name:     firstNonEmpty(args["name"], id),
		MemoryMB: memory,
		CPUs:     cpus,
		Disk:     diskPath,
	}
	switchRef := args["switch"]
	externalRef := args["external"]
	if switchRef == "" && externalRef == "" && len(a.Lab.Switches) > 0 {
		switchRef = a.Lab.Switches[0].ID
	}
	if switchRef != "" {
		vm.Networks = append(vm.Networks, lab.VMNetwork{Switch: switchRef})
	}
	if externalRef != "" {
		vm.Networks = append(vm.Networks, lab.VMNetwork{ExternalLink: externalRef})
	}
	a.Lab.VMs = append(a.Lab.VMs, vm)
	if a.Lab.Layout.Nodes == nil {
		a.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	a.Lab.Layout.Nodes[id] = lab.Position{X: 80, Y: 80 + len(a.Lab.VMs)*96}
	if err := a.saveAndRefresh(); err != nil {
		a.State.Message = "create failed: " + err.Error()
		return
	}
	a.State.Message = "created vm:" + id
}

func (a *App) vmSet(id string, args map[string]string) {
	if a.Lab == nil {
		a.State.Message = "vm set needs a loaded .lab file"
		return
	}
	for i := range a.Lab.VMs {
		if a.Lab.VMs[i].ID != id {
			continue
		}
		if invalid := unexpectedVMSetArgs(args); len(invalid) > 0 {
			a.State.Message = "unsupported vm set argument: " + invalid[0]
			return
		}
		if value := args["name"]; value != "" {
			a.Lab.VMs[i].Name = value
		}
		if value := args["disk"]; value != "" {
			a.Lab.VMs[i].Disk = value
		}
		if value := args["iso"]; value != "" {
			a.Lab.VMs[i].ISO = value
		}
		if value := args["vnc"]; value != "" {
			a.Lab.VMs[i].VNC = boolArg(value, a.Lab.VMs[i].VNC)
		}
		if value, ok := positiveInt(args["cpus"]); ok {
			a.Lab.VMs[i].CPUs = value
		}
		if value, ok := positiveInt(firstNonEmpty(args["memory"], args["mem"])); ok {
			a.Lab.VMs[i].MemoryMB = value
		}
		if value := args["switch"]; value != "" {
			a.Lab.VMs[i].Networks = []lab.VMNetwork{{Switch: value}}
		}
		if value := args["external"]; value != "" {
			a.Lab.VMs[i].Networks = []lab.VMNetwork{{ExternalLink: value}}
		}
		if err := a.saveAndRefresh(); err != nil {
			a.State.Message = "config failed: " + err.Error()
			return
		}
		a.State.Message = "configured vm:" + id
		return
	}
	a.State.Message = "vm not found: " + id
}

func (a *App) vmDelete(id string) {
	if a.Lab == nil {
		a.State.Message = "vm delete needs a loaded .lab file"
		return
	}
	if id == "" {
		a.State.Message = "usage: vm delete <id>"
		return
	}
	found := false
	filtered := a.Lab.VMs[:0]
	for _, vm := range a.Lab.VMs {
		if vm.ID == id {
			found = true
			continue
		}
		filtered = append(filtered, vm)
	}
	if !found {
		a.State.Message = "vm not found: " + id
		return
	}
	a.Lab.VMs = filtered
	delete(a.Lab.Layout.Nodes, id)
	if err := a.saveAndRefresh(); err != nil {
		a.State.Message = "delete failed: " + err.Error()
		return
	}
	a.State.Message = "deleted vm:" + id
}

func (a *App) switchCreate(id string, args map[string]string) {
	if a.Lab == nil {
		a.State.Message = "switch create needs a loaded .lab file"
		return
	}
	if a.hasLabSwitch(id) {
		a.State.Message = "switch already exists: " + id
		return
	}
	a.Lab.Switches = append(a.Lab.Switches, lab.Switch{
		ID:           id,
		Name:         args["name"],
		Mode:         firstNonEmpty(args["mode"], "bridge"),
		ExternalLink: firstNonEmpty(args["external"], args["externallink"]),
	})
	if a.Lab.Layout.Nodes == nil {
		a.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	a.Lab.Layout.Nodes[id] = lab.Position{X: 448, Y: 80 + len(a.Lab.Switches)*96}
	if err := a.saveAndRefresh(); err != nil {
		a.State.Message = "switch create failed: " + err.Error()
		return
	}
	a.State.Message = "created switch:" + id
}

func (a *App) switchSet(id string, args map[string]string) {
	if a.Lab == nil {
		a.State.Message = "switch set needs a loaded .lab file"
		return
	}
	for i := range a.Lab.Switches {
		if a.Lab.Switches[i].ID != id {
			continue
		}
		if value := args["name"]; value != "" {
			a.Lab.Switches[i].Name = value
		}
		if value := args["mode"]; value != "" {
			a.Lab.Switches[i].Mode = value
		}
		if value := firstNonEmpty(args["external"], args["externallink"]); value != "" {
			a.Lab.Switches[i].ExternalLink = value
		}
		if err := a.saveAndRefresh(); err != nil {
			a.State.Message = "switch config failed: " + err.Error()
			return
		}
		a.State.Message = "configured switch:" + id
		return
	}
	a.State.Message = "switch not found: " + id
}

func (a *App) switchDelete(id string) {
	if a.Lab == nil {
		a.State.Message = "switch delete needs a loaded .lab file"
		return
	}
	if id == "" {
		a.State.Message = "usage: switch delete <id>"
		return
	}
	found := false
	switches := a.Lab.Switches[:0]
	for _, sw := range a.Lab.Switches {
		if sw.ID == id {
			found = true
			continue
		}
		switches = append(switches, sw)
	}
	if !found {
		a.State.Message = "switch not found: " + id
		return
	}
	a.Lab.Switches = switches
	for i := range a.Lab.VMs {
		networks := a.Lab.VMs[i].Networks[:0]
		for _, nic := range a.Lab.VMs[i].Networks {
			if nic.Switch != id {
				networks = append(networks, nic)
			}
		}
		a.Lab.VMs[i].Networks = networks
	}
	delete(a.Lab.Layout.Nodes, id)
	if err := a.saveAndRefresh(); err != nil {
		a.State.Message = "switch delete failed: " + err.Error()
		return
	}
	a.State.Message = "deleted switch:" + id
}

func (a *App) externalCreate(id string, args map[string]string) {
	if a.Lab == nil {
		a.State.Message = "external create needs a loaded .lab file"
		return
	}
	if a.hasLabExternal(id) {
		a.State.Message = "external already exists: " + id
		return
	}
	a.Lab.ExternalLinks = append(a.Lab.ExternalLinks, lab.ExternalLink{
		ID:        id,
		Name:      args["name"],
		Interface: args["interface"],
	})
	if a.Lab.Layout.Nodes == nil {
		a.Lab.Layout.Nodes = map[string]lab.Position{}
	}
	a.Lab.Layout.Nodes[id] = lab.Position{X: 832, Y: 80 + len(a.Lab.ExternalLinks)*96}
	if err := a.saveAndRefresh(); err != nil {
		a.State.Message = "external create failed: " + err.Error()
		return
	}
	a.State.Message = "created external:" + id
}

func (a *App) externalSet(id string, args map[string]string) {
	if a.Lab == nil {
		a.State.Message = "external set needs a loaded .lab file"
		return
	}
	for i := range a.Lab.ExternalLinks {
		if a.Lab.ExternalLinks[i].ID != id {
			continue
		}
		if value := args["name"]; value != "" {
			a.Lab.ExternalLinks[i].Name = value
		}
		if value := args["interface"]; value != "" {
			a.Lab.ExternalLinks[i].Interface = value
		}
		if err := a.saveAndRefresh(); err != nil {
			a.State.Message = "external config failed: " + err.Error()
			return
		}
		a.State.Message = "configured external:" + id
		return
	}
	a.State.Message = "external not found: " + id
}

func (a *App) externalDelete(id string) {
	if a.Lab == nil {
		a.State.Message = "external delete needs a loaded .lab file"
		return
	}
	if id == "" {
		a.State.Message = "usage: external delete <id>"
		return
	}
	found := false
	links := a.Lab.ExternalLinks[:0]
	for _, link := range a.Lab.ExternalLinks {
		if link.ID == id {
			found = true
			continue
		}
		links = append(links, link)
	}
	if !found {
		a.State.Message = "external not found: " + id
		return
	}
	a.Lab.ExternalLinks = links
	for i := range a.Lab.Switches {
		if a.Lab.Switches[i].ExternalLink == id {
			a.Lab.Switches[i].ExternalLink = ""
			if a.Lab.Switches[i].Mode == "macnat-bridge" {
				a.Lab.Switches[i].Mode = "bridge"
			}
		}
	}
	for i := range a.Lab.VMs {
		networks := a.Lab.VMs[i].Networks[:0]
		for _, nic := range a.Lab.VMs[i].Networks {
			if nic.ExternalLink != id {
				networks = append(networks, nic)
			}
		}
		a.Lab.VMs[i].Networks = networks
	}
	delete(a.Lab.Layout.Nodes, id)
	if err := a.saveAndRefresh(); err != nil {
		a.State.Message = "external delete failed: " + err.Error()
		return
	}
	a.State.Message = "deleted external:" + id
}

func (a *App) saveAndRefresh() error {
	path := firstNonEmpty(a.LabPath, a.Lab.Path())
	if path == "" {
		return fmt.Errorf("missing lab path")
	}
	if err := lab.SaveFile(path, a.Lab); err != nil {
		return err
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		return err
	}
	a.Lab = loaded
	a.Model = ModelFromLab(loaded)
	if a.State.Selected >= len(a.Model.Nodes) {
		a.State.Selected = max(0, len(a.Model.Nodes)-1)
	}
	return nil
}

func (a *App) hasVM(id string) bool {
	if a.hasLabVM(id) {
		return true
	}
	for _, node := range a.Model.Nodes {
		if node.Type == NodeVM && node.ID == id {
			return true
		}
	}
	return false
}

func (a *App) hasLabVM(id string) bool {
	_, ok := a.labVM(id)
	return ok
}

func (a *App) labVM(id string) (lab.VM, bool) {
	if a.Lab == nil {
		return lab.VM{}, false
	}
	for _, vm := range a.Lab.VMs {
		if vm.ID == id {
			return vm, true
		}
	}
	return lab.VM{}, false
}

func (a *App) hasLabSwitch(id string) bool {
	_, ok := a.labSwitch(id)
	return ok
}

func (a *App) labSwitch(id string) (lab.Switch, bool) {
	if a.Lab == nil {
		return lab.Switch{}, false
	}
	for _, sw := range a.Lab.Switches {
		if sw.ID == id {
			return sw, true
		}
	}
	return lab.Switch{}, false
}

func (a *App) hasLabExternal(id string) bool {
	_, ok := a.labExternal(id)
	return ok
}

func (a *App) labExternal(id string) (lab.ExternalLink, bool) {
	if a.Lab == nil {
		return lab.ExternalLink{}, false
	}
	for _, link := range a.Lab.ExternalLinks {
		if link.ID == id {
			return link, true
		}
	}
	return lab.ExternalLink{}, false
}

func (a *App) nextVMID() string {
	for i := len(a.Model.Nodes) + 1; ; i++ {
		id := fmt.Sprintf("vm%d", i)
		if !a.hasVM(id) {
			return id
		}
	}
}

func (a *App) nextSwitchID() string {
	for i := len(a.Model.Nodes) + 1; ; i++ {
		id := fmt.Sprintf("sw%d", i)
		if !a.hasLabSwitch(id) {
			return id
		}
	}
}

func (a *App) nextExternalID() string {
	for i := len(a.Model.Nodes) + 1; ; i++ {
		id := fmt.Sprintf("uplink%d", i)
		if !a.hasLabExternal(id) {
			return id
		}
	}
}

func (a *App) firstExternalID() string {
	if a.Lab == nil || len(a.Lab.ExternalLinks) == 0 {
		return ""
	}
	return a.Lab.ExternalLinks[0].ID
}

func (a *App) firstSwitchID() string {
	if a.Lab == nil || len(a.Lab.Switches) == 0 {
		return ""
	}
	return a.Lab.Switches[0].ID
}

func (a *App) switchForExternal(id string) string {
	if a.Lab == nil {
		return ""
	}
	for _, sw := range a.Lab.Switches {
		if sw.ExternalLink == id {
			return sw.ID
		}
	}
	return ""
}

func (a *App) createVMHint(node Node) string {
	switch node.Type {
	case NodeSwitch:
		return " switch=" + node.ID
	case NodeExternal:
		return " external=" + node.ID
	default:
		if a.Lab != nil && len(a.Lab.Switches) > 0 {
			return " switch=" + a.Lab.Switches[0].ID
		}
		return ""
	}
}
