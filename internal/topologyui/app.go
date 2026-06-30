package topologyui

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/foxruntime"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/workload"
)

type WorkloadRuntime interface {
	States(context.Context, *lab.Lab) (map[string]string, error)
	Start(context.Context, *lab.Lab, workload.Ref) error
	Stop(context.Context, *lab.Lab, workload.Ref) error
	Close() error
}

type App struct {
	Model                 Model
	State                 ViewState
	Lab                   *lab.Lab
	LabPath               string
	Service               *topology.Service
	LibvirtURI            string
	ContainerdAddress     string
	Runtime               WorkloadRuntime
	WorkloadStates        map[string]string
	VNCPorts              map[string]int
	VNCViewer             string
	StatusSocket          string
	StatusQuery           func(context.Context, string) (daemonstatus.Snapshot, error)
	CommandLog            []string
	HistoryIndex          int
	PendingShell          *shellCommand
	PendingVNC            *shellCommand
	In                    *os.File
	Out                   *os.File
	ViewWidth             int
	ViewHeight            int
	RouteCacheKey         string
	RouteCacheRoutes      []visibleEdge
	StatusRefreshInterval time.Duration
	VMConsole             func(context.Context, *lab.Lab, string) (io.ReadWriteCloser, string, error)
	DaemonController      DaemonController
	runtimeMu             sync.Mutex
	pendingKeys           []string
	mouseDownNodeID       string
	mouseDownNodeType     string
	mouseDownX            int
	mouseDownY            int
	mouseDragStartX       int
	mouseDragStartY       int
	mouseDragMoved        bool
	mousePanActive        bool
	mousePanDownX         int
	mousePanDownY         int
	mousePanStartX        int
	mousePanStartY        int
}

const runtimeStatusTimeout = 5 * time.Second
const daemonApplyTimeout = 90 * time.Second

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
	a.ensureDaemonController()
	a.ensureService()
	a.refreshWorkloadStates()
	a.refreshApplyLabState()
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
	lastAnimation := time.Now()
	statusRefreshInterval := a.statusRefreshInterval()
	nextStatusRefresh := time.Now().Add(statusRefreshInterval)
	statusRefreshActive := false
	statusUpdates := make(chan statusUpdate, 1)
	for {
		if a.drainStatusUpdates(statusUpdates, &statusRefreshActive) {
			dirty = true
		}
		a.State.StatusRefreshing = statusRefreshActive
		if !statusRefreshActive && a.Lab != nil && time.Now().After(nextStatusRefresh) {
			nextStatusRefresh = time.Now().Add(statusRefreshInterval)
			statusRefreshActive = true
			a.State.StatusRefreshing = true
			a.startStatusRefresh(ctx, statusUpdates)
			dirty = true
		}
		if a.animationActive() && time.Since(lastAnimation) >= spinnerInterval {
			a.State.AnimationFrame++
			lastAnimation = time.Now()
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

func (a *App) animationActive() bool {
	if a.State.StatusRefreshing {
		return true
	}
	if animatedStateFromMessage(a.State.Message) {
		return true
	}
	for _, node := range a.Model.Nodes {
		if animatedState(node.State) {
			return true
		}
	}
	return false
}

func animatedStateFromMessage(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	return strings.HasPrefix(message, "applying ") ||
		strings.HasPrefix(message, "loading ") ||
		strings.HasPrefix(message, "pulling ") ||
		strings.HasPrefix(message, "refreshing ")
}

func (a *App) statusRefreshInterval() time.Duration {
	if a.StatusRefreshInterval > 0 {
		return a.StatusRefreshInterval
	}
	return time.Second
}

type statusUpdate struct {
	lab                 *lab.Lab
	states              map[string]string
	vncPorts            map[string]int
	message             string
	clearStatusMessage  bool
	applyStatus         DaemonStatus
	applyStatusReceived bool
}

func (a *App) startStatusRefresh(ctx context.Context, updates chan<- statusUpdate) {
	l := a.Lab
	snapshot := lab.Clone(l)
	go func() {
		statusCtx, cancel := context.WithTimeout(ctx, runtimeStatusTimeout)
		defer cancel()
		if daemonSnapshot, ok := a.queryDaemonSnapshot(statusCtx); ok {
			update := a.statusUpdateFromDaemonSnapshot(l, daemonSnapshot)
			sendStatusUpdate(ctx, updates, update)
			return
		}
		runtime, closeRuntime, err := a.runtimeForLab(snapshot)
		if err != nil {
			sendStatusUpdate(ctx, updates, statusUpdate{lab: l, message: "runtime connection failed: " + err.Error()})
			return
		}
		defer closeRuntime()
		a.runtimeMu.Lock()
		states, err := runtime.States(statusCtx, snapshot)
		if err != nil {
			update := statusUpdate{lab: l, message: "runtime status failed: " + err.Error()}
			if ports, portErr := runtimeVNCPorts(statusCtx, runtime, snapshot); portErr == nil {
				update.vncPorts = cloneRuntimePortMap(ports)
			}
			a.runtimeMu.Unlock()
			sendStatusUpdate(ctx, updates, update)
			return
		}
		update := statusUpdate{lab: l, states: cloneRuntimeStateMap(states), clearStatusMessage: true}
		if ports, err := runtimeVNCPorts(statusCtx, runtime, snapshot); err == nil {
			update.vncPorts = cloneRuntimePortMap(ports)
		} else {
			update.message = "vnc status failed: " + err.Error()
			update.clearStatusMessage = false
		}
		if status, err := a.daemonStatus(statusCtx); err == nil {
			update.applyStatus = status
			update.applyStatusReceived = true
		}
		a.runtimeMu.Unlock()
		sendStatusUpdate(ctx, updates, update)
	}()
}

func sendStatusUpdate(ctx context.Context, updates chan<- statusUpdate, update statusUpdate) {
	select {
	case updates <- update:
	case <-ctx.Done():
	}
}

func (a *App) drainStatusUpdates(updates <-chan statusUpdate, active *bool) bool {
	changed := false
	for {
		select {
		case update := <-updates:
			*active = false
			if update.lab != nil && update.lab != a.Lab {
				continue
			}
			if update.states != nil {
				a.WorkloadStates = cloneRuntimeStateMap(update.states)
				if a.Service != nil {
					a.Service.States = a.WorkloadStates
				}
				a.applyWorkloadStates()
				changed = true
			}
			if update.vncPorts != nil {
				a.VNCPorts = cloneRuntimePortMap(update.vncPorts)
				a.applyWorkloadStates()
				changed = true
			}
			if update.message != "" {
				a.State.Message = update.message
				changed = true
			} else if update.clearStatusMessage && statusRefreshMessage(a.State.Message) {
				a.State.Message = ""
				changed = true
			}
			if update.applyStatusReceived {
				a.updateApplyLabState(update.applyStatus)
				changed = true
			}
		default:
			return changed
		}
	}
}

func (a *App) render(w io.Writer, width, height int, ansi bool) error {
	key := renderRouteCacheKey(a.Model, width, height, a.State.PanX, a.State.PanY)
	reuseMovingRoutes := a.State.MoveMode && a.RouteCacheKey != "" && len(a.RouteCacheRoutes) > 0
	if key != a.RouteCacheKey && !reuseMovingRoutes {
		_, routes := planRenderRoutes(a.Model, a.State, width, height)
		a.RouteCacheKey = key
		a.RouteCacheRoutes = routes
	}
	_, err := io.WriteString(w, renderGridWithRoutes(a.Model, a.State, width, height, a.RouteCacheRoutes).String(ansi))
	return err
}

func (a *App) runtime() (WorkloadRuntime, func(), error) {
	return a.runtimeForLab(a.Lab)
}

func (a *App) runtimeForLab(l *lab.Lab) (WorkloadRuntime, func(), error) {
	if a.Runtime != nil {
		return a.Runtime, func() {}, nil
	}
	runtime, err := foxruntime.New(a.LibvirtURI, a.ContainerdAddress, l)
	if err != nil {
		return nil, func() {}, err
	}
	return runtime, func() { _ = runtime.Close() }, nil
}

func (a *App) ensureDaemonController() {
	if a.DaemonController == nil {
		a.DaemonController = newSystemdDaemonController()
	}
}

func (a *App) daemonStatus(ctx context.Context) (DaemonStatus, error) {
	if a.DaemonController == nil {
		return DaemonStatus{}, nil
	}
	return a.DaemonController.Status(ctx)
}

func (a *App) currentLabAbsPath() (string, error) {
	if strings.TrimSpace(a.LabPath) == "" {
		return "", os.ErrNotExist
	}
	return filepath.Abs(a.LabPath)
}

func (a *App) refreshApplyLabState() bool {
	if _, err := a.currentLabAbsPath(); err != nil {
		a.State.ApplyLabDisabled = true
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer cancel()
	status, err := a.daemonStatus(ctx)
	if err != nil {
		a.State.ApplyLabDisabled = false
		return false
	}
	a.updateApplyLabState(status)
	return true
}

func (a *App) updateApplyLabState(status DaemonStatus) {
	current, err := a.currentLabAbsPath()
	if err != nil {
		a.State.ApplyLabDisabled = true
		return
	}
	a.State.ApplyLabDisabled = status.Active && sameLabPath(status.LabPath, current)
}

func (a *App) applyOpenLab() {
	current, err := a.currentLabAbsPath()
	if err != nil {
		a.State.Message = "apply lab failed: no open lab file"
		a.State.ApplyLabDisabled = true
		return
	}
	a.ensureDaemonController()
	ctx, cancel := context.WithTimeout(context.Background(), daemonApplyTimeout)
	defer cancel()
	if status, err := a.daemonStatus(ctx); err == nil && status.Active && sameLabPath(status.LabPath, current) {
		a.State.Message = "lab already applied " + filepath.Base(current)
		a.State.ApplyLabDisabled = true
		return
	}
	if err := a.DaemonController.Apply(ctx, DaemonApplyRequest{
		LabPath:           current,
		LibvirtURI:        a.LibvirtURI,
		ContainerdAddress: a.ContainerdAddress,
	}); err != nil {
		a.State.Message = "apply lab failed: " + err.Error()
		a.refreshApplyLabState()
		return
	}
	a.State.Message = "applied lab " + filepath.Base(current)
	a.State.ApplyLabDisabled = true
	a.refreshWorkloadStates()
}

func (a *App) refreshWorkloadStates() bool {
	a.ensureService()
	if a.Lab == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer cancel()
	if snapshot, ok := a.queryDaemonSnapshot(ctx); ok {
		a.applyDaemonSnapshot(snapshot)
		return true
	}
	runtime, closeRuntime, err := a.runtime()
	if err != nil {
		a.State.Message = "runtime connection failed: " + err.Error()
		return true
	}
	defer closeRuntime()
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	states, err := runtime.States(ctx, a.Lab)
	if err != nil {
		a.State.Message = "runtime status failed: " + err.Error()
		_ = a.refreshVNCPortsWithRuntime(ctx, runtime)
		return true
	}
	a.WorkloadStates = cloneRuntimeStateMap(states)
	a.Service.States = a.WorkloadStates
	if portErr := a.refreshVNCPortsWithRuntime(ctx, runtime); portErr != nil {
		a.State.Message = "vnc status failed: " + portErr.Error()
	} else if statusRefreshMessage(a.State.Message) {
		a.State.Message = ""
	}
	a.applyWorkloadStates()
	return true
}

func (a *App) queryDaemonSnapshot(ctx context.Context) (daemonstatus.Snapshot, bool) {
	current, err := a.currentLabAbsPath()
	if err != nil {
		return daemonstatus.Snapshot{}, false
	}
	query := daemonstatus.Query
	if a.StatusQuery != nil {
		query = a.StatusQuery
	}
	snapshot, err := query(ctx, a.StatusSocket)
	if err != nil {
		return daemonstatus.Snapshot{}, false
	}
	if !sameLabPath(snapshot.LabPath, current) {
		return daemonstatus.Snapshot{}, false
	}
	return snapshot, true
}

func (a *App) statusUpdateFromDaemonSnapshot(l *lab.Lab, snapshot daemonstatus.Snapshot) statusUpdate {
	update := statusUpdate{
		lab:                 l,
		states:              cloneRuntimeStateMap(snapshot.States),
		vncPorts:            cloneRuntimePortMap(snapshot.VNCPorts),
		applyStatus:         DaemonStatus{Active: true, LabPath: snapshot.LabPath},
		applyStatusReceived: true,
	}
	if len(snapshot.Errors) > 0 {
		update.message = "foxlabd status: " + strings.Join(snapshot.Errors, "; ")
	} else {
		update.clearStatusMessage = true
	}
	return update
}

func (a *App) applyDaemonSnapshot(snapshot daemonstatus.Snapshot) {
	a.WorkloadStates = cloneRuntimeStateMap(snapshot.States)
	a.Service.States = a.WorkloadStates
	a.VNCPorts = cloneRuntimePortMap(snapshot.VNCPorts)
	a.updateApplyLabState(DaemonStatus{Active: true, LabPath: snapshot.LabPath})
	if len(snapshot.Errors) > 0 {
		a.State.Message = "foxlabd status: " + strings.Join(snapshot.Errors, "; ")
	} else if statusRefreshMessage(a.State.Message) {
		a.State.Message = ""
	}
	a.applyWorkloadStates()
}

func statusRefreshMessage(message string) bool {
	message = strings.TrimSpace(message)
	for _, prefix := range []string{
		"foxlabd status:",
		"runtime connection failed:",
		"runtime status failed:",
		"vnc status failed:",
	} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

func (a *App) refreshVNCPortsWithRuntime(ctx context.Context, runtime WorkloadRuntime) error {
	ports, err := runtimeVNCPorts(ctx, runtime, a.Lab)
	if err != nil {
		return err
	}
	a.VNCPorts = cloneRuntimePortMap(ports)
	a.applyWorkloadStates()
	return nil
}

func cloneRuntimeStateMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = normalizeRuntimeState(value)
	}
	return out
}

func normalizeRuntimeState(state string) string {
	return strings.ToLower(strings.TrimSpace(state))
}

func cloneRuntimePortMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
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
	if a.State.ContextGroup == "uplink-menu" && node.Type == NodeSwitch {
		return switchUplinkMenuItems(node)
	}
	return contextMenuItems(node, a.State.ContextGroup)
}

func (a *App) openCommand(command string) {
	a.State.DiskMenuItems = nil
	a.State.DiskMenuActions = nil
	a.State.DiskMenuKinds = nil
	a.State.ContextDeleteNIC = false
	a.State.ContextDeleteUplink = false
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
