package topologyui

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

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
	VNCPorts          map[string]int
	VNCViewer         string
	CommandLog        []string
	HistoryIndex      int
	PendingShell      *shellCommand
	PendingVNC        *shellCommand
	In                *os.File
	Out               *os.File
	ViewWidth         int
	ViewHeight        int
	RouteCacheKey     string
	RouteCacheRoutes  []visibleEdge
	ReconcileInterval time.Duration
	VMConsole         func(context.Context, *lab.Lab, string) (io.ReadWriteCloser, string, error)
	pendingKeys       []string
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
	a.resetRouteCache()
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
	if a.WorkloadStates != nil {
		a.Service.States = a.WorkloadStates
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
		a.resetRouteCache()
		a.Model = ModelFromLab(a.Lab)
		a.applyWorkloadStates()
	}
}

func (a *App) resetRouteCache() {
	a.RouteCacheKey = ""
	a.RouteCacheRoutes = nil
}

type terminalStartFunc func(*App) (func(), error)
type keyReadFunc func(*App) (string, error)
type terminalSizeFunc func(*App) (int, int)

func (a *App) runInteractive(start terminalStartFunc, read keyReadFunc, size terminalSizeFunc) error {
	cleanup, err := start(a)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		if cleanup != nil {
			cleanup()
		}
	}()
	dirty := true
	lastWidth, lastHeight := 0, 0
	reconcileInterval := a.reconcileInterval()
	nextReconcile := time.Now().Add(reconcileInterval)
	reconcileActive := false
	reconcileUpdates := make(chan reconcileUpdate, 1)
	for {
		if a.drainReconcileUpdates(reconcileUpdates, &reconcileActive) {
			dirty = true
		}
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
			if err := a.render(a.Out, width, height, true); err != nil {
				return err
			}
			dirty = false
		}
		if !reconcileActive && a.Lab != nil && time.Now().After(nextReconcile) {
			reconcileActive = true
			nextReconcile = time.Now().Add(reconcileInterval)
			a.startReconcile(ctx, reconcileUpdates)
		}
		key, err := read(a)
		if err != nil {
			return err
		}
		if key == "" {
			continue
		}
		if strings.HasPrefix(key, "mouse:") && a.prepareMouseClickFeedback(key) {
			_, _ = io.WriteString(a.Out, ansiMoveHome)
			if err := a.render(a.Out, width, height, true); err != nil {
				return err
			}
			time.Sleep(45 * time.Millisecond)
			a.clearMouseClickFeedback()
		}
		quit := a.handleKey(key)
		if quit {
			return nil
		}
		if a.PendingShell != nil {
			command := *a.PendingShell
			a.PendingShell = nil
			cleanup()
			cleanup = nil
			if err := a.runShell(command); err != nil {
				a.State.Message = "shell failed: " + err.Error()
			} else {
				a.State.Message = "shell closed"
			}
			cleanup, err = start(a)
			if err != nil {
				return err
			}
			dirty = true
			continue
		}
		if a.PendingVNC != nil {
			command := *a.PendingVNC
			a.PendingVNC = nil
			cleanup()
			cleanup = nil
			if err := a.runShell(command); err != nil {
				a.State.Message = "vnc failed: " + err.Error()
			} else {
				a.State.Message = "vnc closed"
			}
			cleanup, err = start(a)
			if err != nil {
				return err
			}
			dirty = true
			continue
		}
		dirty = true
	}
}

type reconcileUpdate struct {
	states   map[string]string
	vncPorts map[string]int
	message  string
}

func (a *App) reconcileInterval() time.Duration {
	if a.ReconcileInterval > 0 {
		return a.ReconcileInterval
	}
	return time.Second
}

func (a *App) startReconcile(ctx context.Context, updates chan<- reconcileUpdate) {
	l := a.Lab
	go func() {
		runtime, closeRuntime, err := a.runtime()
		if err != nil {
			sendReconcileUpdate(ctx, updates, reconcileUpdate{message: "runtime connection failed: " + err.Error()})
			return
		}
		defer closeRuntime()
		result := (&workload.Reconciler{Runtime: runtime}).Step(ctx, l)
		update := reconcileUpdate{states: result.States}
		if ports, err := runtimeVNCPorts(ctx, runtime, l); err == nil {
			update.vncPorts = ports
		} else if len(result.Errors) == 0 {
			update.message = "vnc status failed: " + err.Error()
		}
		if len(result.Errors) > 0 {
			update.message = "reconcile failed: " + result.Errors[0].Error()
		} else if len(result.Actions) > 0 {
			update.message = strings.Join(result.Actions, ", ")
		}
		sendReconcileUpdate(ctx, updates, update)
	}()
}

func sendReconcileUpdate(ctx context.Context, updates chan<- reconcileUpdate, update reconcileUpdate) {
	select {
	case updates <- update:
	case <-ctx.Done():
	}
}

func (a *App) drainReconcileUpdates(updates <-chan reconcileUpdate, active *bool) bool {
	changed := false
	for {
		select {
		case update := <-updates:
			*active = false
			if update.states != nil && !sameStringMap(a.WorkloadStates, update.states) {
				a.WorkloadStates = update.states
				if a.Service != nil {
					a.Service.States = update.states
				}
				a.applyWorkloadStates()
				changed = true
			}
			if update.vncPorts != nil && !sameIntMap(a.VNCPorts, update.vncPorts) {
				a.VNCPorts = update.vncPorts
				a.applyWorkloadStates()
				changed = true
			}
			if update.message != "" && a.State.Message != update.message {
				a.State.Message = update.message
				changed = true
			}
		default:
			return changed
		}
	}
}

func (a *App) render(w io.Writer, width, height int, ansi bool) error {
	key := renderRouteCacheKey(a.Model, width, height)
	reuseMovingRoutes := a.State.MoveMode && a.RouteCacheKey != "" && len(a.RouteCacheRoutes) > 0
	if key != a.RouteCacheKey && !reuseMovingRoutes {
		_, routes := planRenderRoutes(a.Model, width, height)
		a.RouteCacheKey = key
		a.RouteCacheRoutes = routes
	}
	_, err := io.WriteString(w, renderGridWithRoutes(a.Model, a.State, width, height, a.RouteCacheRoutes).String(ansi))
	return err
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	states, err := runtime.States(ctx, a.Lab)
	if err != nil {
		a.State.Message = "runtime status failed: " + err.Error()
		_ = a.refreshVNCPortsWithRuntime(ctx, runtime)
		return
	}
	a.WorkloadStates = states
	a.Service.States = states
	if portErr := a.refreshVNCPortsWithRuntime(ctx, runtime); portErr != nil {
		a.State.Message = "vnc status failed: " + portErr.Error()
	}
	a.applyWorkloadStates()
}

func (a *App) refreshVNCPortsWithRuntime(ctx context.Context, runtime WorkloadRuntime) error {
	ports, err := runtimeVNCPorts(ctx, runtime, a.Lab)
	if err != nil {
		return err
	}
	a.VNCPorts = ports
	a.applyWorkloadStates()
	return nil
}

func (a *App) applyWorkloadStates() {
	for i := range a.Model.Nodes {
		node := &a.Model.Nodes[i]
		key := NodeKey(node.Type, node.ID)
		if state, ok := a.WorkloadStates[key]; ok {
			node.State = displayWorkloadState(node.DesiredState, state)
		}
		if node.Type == NodeVM {
			node.Details = withVNCDetailPort(node.Details, a.VNCPorts[key])
		}
	}
}

func runtimeVNCPorts(ctx context.Context, runtime WorkloadRuntime, l *lab.Lab) (map[string]int, error) {
	vncRuntime, ok := runtime.(workload.VNCRuntime)
	if !ok {
		return map[string]int{}, nil
	}
	return vncRuntime.VNCPorts(ctx, l)
}

func sameStringMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func sameIntMap(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
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
	if a.State.ContextGroup == "disk-menu" {
		return a.State.DiskMenuItems
	}
	return contextMenuItems(node, a.State.ContextGroup)
}

func (a *App) openCommand(command string) {
	a.State.DiskMenuItems = nil
	a.State.DiskMenuActions = nil
	a.State.DiskMenuKinds = nil
	a.State.ContextDeleteNIC = false
	a.State.ContextEdit = false
	a.State.ContextEditValue = ""
	a.State.ContextEditCursor = 0
	a.State.CommandMode = false
	a.State.Command = ""
	a.State.Message = "command bar removed; use the menu"
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
