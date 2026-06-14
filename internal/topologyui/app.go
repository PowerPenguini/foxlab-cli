package topologyui

import (
	"context"
	"io"
	"os"

	containerdruntime "foxlab-cli/internal/containerd"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/virt"
	"foxlab-cli/internal/workload"
)

type WorkloadRuntime interface {
	States(context.Context, *lab.Lab) (map[string]string, error)
	Start(context.Context, *lab.Lab, workload.Ref) error
	Stop(context.Context, *lab.Lab, workload.Ref) error
	Close() error
}

type App struct {
	Model             Model
	State             ViewState
	Lab               *lab.Lab
	LabPath           string
	Service           *topology.Service
	LibvirtURI        string
	ContainerdAddress string
	Runtime           WorkloadRuntime
	WorkloadStates    map[string]string
	CommandLog        []string
	HistoryIndex      int
	In                *os.File
	Out               *os.File
	ViewWidth         int
	ViewHeight        int
}

func (a *App) Run() error {
	if a.In == nil {
		a.In = os.Stdin
	}
	if a.Out == nil {
		a.Out = os.Stdout
	}
	if len(a.Model.Nodes) == 0 && a.Lab == nil {
		a.Model = MockModel()
	}
	a.ensureService()
	a.refreshWorkloadStates()
	return a.runInteractive(startAppTerminalSession, readAppKey, appTerminalSize)
}

func (a *App) ensureService() *topology.Service {
	if a.Service == nil {
		a.Service = topology.NewService(a.Lab, a.LabPath)
	}
	a.Service.Lab = a.Lab
	if a.LabPath != "" {
		a.Service.Path = a.LabPath
	}
	return a.Service
}

func (a *App) syncFromService() {
	if a.Service == nil {
		return
	}
	a.Lab = a.Service.Lab
	a.LabPath = a.Service.Path
	if a.Lab != nil {
		a.Model = ModelFromLab(a.Lab)
		a.applyWorkloadStates()
	}
}

type terminalStartFunc func(*App) (func(), error)
type keyReadFunc func(*App) (string, error)
type terminalSizeFunc func(*App) (int, int)

func (a *App) runInteractive(start terminalStartFunc, read keyReadFunc, size terminalSizeFunc) error {
	cleanup, err := start(a)
	if err != nil {
		return err
	}
	defer cleanup()
	dirty := true
	lastWidth, lastHeight := 0, 0
	for {
		width, height := size(a)
		a.ViewWidth = width
		a.ViewHeight = height
		if width != lastWidth || height != lastHeight {
			dirty = true
			lastWidth = width
			lastHeight = height
		}
		if dirty {
			_, _ = io.WriteString(a.Out, ansiMoveHome)
			if err := Render(a.Out, a.Model, a.State, width, height, true); err != nil {
				return err
			}
			dirty = false
		}
		key, err := read(a)
		if err != nil {
			return err
		}
		if key == "" {
			continue
		}
		if a.handleKey(key) {
			return nil
		}
		dirty = true
	}
}

func (a *App) runtime() (WorkloadRuntime, func(), error) {
	if a.Runtime != nil {
		return a.Runtime, func() {}, nil
	}
	vmRuntime, err := virt.NewLibvirtRuntime(a.LibvirtURI)
	if err != nil {
		return nil, func() {}, err
	}
	runtime := &workload.Composite{
		VM:        vmRuntime,
		Container: containerdruntime.NewRuntime(firstNonEmpty(a.ContainerdAddress, a.containerdAddressFromLab())),
	}
	return runtime, func() { _ = runtime.Close() }, nil
}

func (a *App) refreshWorkloadStates() {
	a.ensureService()
	if a.Lab == nil {
		return
	}
	runtime, closeRuntime, err := a.runtime()
	if err != nil {
		a.State.Message = "runtime connection failed: " + err.Error()
		return
	}
	defer closeRuntime()
	states, err := runtime.States(context.Background(), a.Lab)
	if err != nil {
		a.State.Message = "runtime status failed: " + err.Error()
		return
	}
	a.WorkloadStates = states
	a.Service.States = states
	a.applyWorkloadStates()
}

func (a *App) applyWorkloadStates() {
	for i := range a.Model.Nodes {
		if state, ok := a.WorkloadStates[NodeKey(a.Model.Nodes[i].Type, a.Model.Nodes[i].ID)]; ok {
			a.Model.Nodes[i].State = state
		}
	}
}

func (a *App) containerdAddressFromLab() string {
	if a.Lab == nil || a.Lab.Meta == nil {
		return ""
	}
	return a.Lab.Meta["containerd.address"]
}

func (a *App) contextMenuRootItems(node Node, ok bool) []string {
	if !ok {
		return globalContextMenuItems("")
	}
	return contextMenuItems(node, "")
}

func (a *App) contextMenuSubmenuItems(node Node, ok bool) []string {
	if !ok {
		return globalContextMenuItems(a.State.ContextGroup)
	}
	return contextMenuItems(node, a.State.ContextGroup)
}

func (a *App) openCommand(command string) {
	a.State.ContextMenu = false
	a.State.ContextGroup = ""
	a.State.ContextInSubmenu = false
	a.State.ContextSubSelected = 0
	a.State.ContextDeleteNIC = false
	a.State.ContextEdit = false
	a.State.ContextEditValue = ""
	a.State.ContextEditCursor = 0
	a.State.CommandMode = true
	a.State.Command = command
	a.HistoryIndex = len(a.CommandLog)
}

func (a *App) rememberCommand(command string) {
	if command == "" {
		a.HistoryIndex = len(a.CommandLog)
		return
	}
	if len(a.CommandLog) == 0 || a.CommandLog[len(a.CommandLog)-1] != command {
		a.CommandLog = append(a.CommandLog, command)
	}
	a.HistoryIndex = len(a.CommandLog)
}

func (a *App) recallCommand(delta int) {
	if len(a.CommandLog) == 0 {
		return
	}
	a.HistoryIndex = clamp(a.HistoryIndex+delta, 0, len(a.CommandLog))
	if a.HistoryIndex == len(a.CommandLog) {
		a.State.Command = ""
		return
	}
	a.State.Command = a.CommandLog[a.HistoryIndex]
}

func OneFrame(w io.Writer, m Model, width, height int) error {
	return Render(w, m, ViewState{Focus: FocusGraph}, width, height, false)
}
