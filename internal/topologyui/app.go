package topologyui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/workload"
)

type appRuntimeState struct {
	mu sync.Mutex
}

type appInputState struct {
	pendingKeys   []string
	pendingRaw    [][]byte
	currentRaw    []byte
	readBuffer    []byte
	readDeadline  time.Time
	pasteActive   bool
	pasteToUI     bool
	hostMouseMany bool
	hostAppKeypad bool
	mouse         mouseInteractionState
}

type mouseInteractionState struct {
	downNodeID   string
	downNodeType string
	downX        int
	downY        int
	dragStartX   int
	dragStartY   int
	dragMoved    bool
	panActive    bool
	panDownX     int
	panDownY     int
	panStartX    int
	panStartY    int
}

type appNotificationState struct {
	message         string
	setAt           time.Time
	messageRevision uint64
	nextRevision    uint64
}

type App struct {
	Model                 Model
	State                 ViewState
	Lab                   *lab.Lab
	LabPath               string
	Service               *topology.Service
	LibvirtURI            string
	ContainerdAddress     string
	WorkloadStates        map[string]string
	VNCPorts              map[string]int
	VNCViewer             string
	StatusSocket          string
	StatusQuery           func(context.Context, string) (daemonstatus.Snapshot, error)
	CommandLog            []string
	HistoryIndex          int
	PendingShell          *shellCommand
	PendingVNC            *shellCommand
	PendingStarts         map[string]bool
	In                    *os.File
	Out                   *os.File
	ViewWidth             int
	ViewHeight            int
	StatusRefreshInterval time.Duration
	DaemonController      DaemonController
	tabs                  *tabManager
	runtimeFactory        RuntimeFactory
	runtimeState          appRuntimeState
	inputState            appInputState
	notificationState     appNotificationState
	routeCache            routeCacheState
	quitRequested         bool
}

const runtimeStatusTimeout = 5 * time.Second
const daemonApplyTimeout = 90 * time.Second
const notificationMessageTTL = 10 * time.Second

func (a *App) Run() error {
	if a.In == nil {
		a.In = os.Stdin
	}
	if a.Out == nil {
		a.Out = os.Stdout
	}
	a.resetRouteCache()
	a.ensureDaemonController()
	a.ensureService()
	a.refreshWorkloadStates()
	a.refreshApplyLabState()
	a.ensureTabs()
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
	a.routeCache = routeCacheState{}
}

type terminalStartFunc func(*App) (func(), error)
type keyReadFunc func(*App) (string, error)
type terminalSizeFunc func(*App) (int, int)

func (a *App) runInteractive(start terminalStartFunc, read keyReadFunc, size terminalSizeFunc) error {
	a.ensureTabs()
	if err := a.tabs.openWakePipe(); err != nil {
		return fmt.Errorf("terminal update wake pipe: %w", err)
	}
	defer a.tabs.closeWakePipe()
	cleanup, err := start(a)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		a.closeTabs()
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
		now := time.Now()
		if a.drainTabUpdates() {
			dirty = true
		}
		if a.drainStatusUpdates(statusUpdates, &statusRefreshActive) {
			dirty = true
		}
		a.State.StatusRefreshing = statusRefreshActive
		a.syncHostTerminalModes()
		if a.updateMessageLifetime(now) {
			dirty = true
		}
		if !statusRefreshActive && a.Lab != nil && now.After(nextStatusRefresh) {
			nextStatusRefresh = now.Add(statusRefreshInterval)
			statusRefreshActive = true
			a.State.StatusRefreshing = true
			a.startStatusRefresh(ctx, statusUpdates)
			dirty = true
		}
		if a.animationActive() && now.Sub(lastAnimation) >= spinnerInterval {
			a.State.AnimationFrame++
			lastAnimation = now
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
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if key == "" {
			continue
		}
		wasRunningShell := a.tabs.activeRunningShell()
		if a.handleTabInput(key, a.inputState.currentRaw) {
			if a.quitRequested {
				return nil
			}
			dirty = a.tabInputNeedsImmediateRender(key, wasRunningShell)
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
		if quit || a.quitRequested {
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

func (a *App) updateMessageLifetime(now time.Time) bool {
	notification, ok := notificationFromState(a.State)
	revision := uint64(0)
	if ok {
		revision = notification.Revision
	}
	if a.State.Message != a.notificationState.message || revision != a.notificationState.messageRevision {
		a.notificationState.message = a.State.Message
		a.notificationState.messageRevision = revision
		if a.State.Message == "" {
			a.notificationState.setAt = time.Time{}
		} else {
			a.notificationState.setAt = now
		}
		return false
	}
	if a.State.Message == "" || a.notificationState.setAt.IsZero() {
		return false
	}
	if now.Sub(a.notificationState.setAt) < notificationMessageTTL {
		return false
	}
	a.dismissNotification()
	return true
}

func (a *App) dismissNotification() {
	a.State.Message = ""
	a.State.Notification = Notification{}
	a.notificationState.message = ""
	a.notificationState.setAt = time.Time{}
	a.notificationState.messageRevision = 0
}

func (a *App) animationActive() bool {
	if a.State.StatusRefreshing {
		return true
	}
	if notification, ok := notificationFromState(a.State); ok && (notification.Busy || animatedStateFromMessage(notification.Text)) {
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
	statesConfirmed     bool
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
		a.runtimeState.mu.Lock()
		states, err := runtime.States(statusCtx, snapshot)
		if err != nil {
			update := statusUpdate{lab: l, message: "runtime status failed: " + err.Error()}
			if ports, portErr := runtimeVNCPorts(statusCtx, runtime, snapshot); portErr == nil {
				update.vncPorts = cloneRuntimePortMap(ports)
			}
			a.runtimeState.mu.Unlock()
			sendStatusUpdate(ctx, updates, update)
			return
		}
		update := statusUpdate{lab: l, states: cloneRuntimeStateMap(states), statesConfirmed: true, clearStatusMessage: true}
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
		a.runtimeState.mu.Unlock()
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
			if update.applyStatusReceived {
				a.updateApplyLabState(update.applyStatus)
				changed = true
			}
			if update.states != nil {
				a.WorkloadStates = cloneRuntimeStateMap(update.states)
				if a.Service != nil {
					a.Service.States = a.WorkloadStates
					a.Service.StatesConfirmed = update.statesConfirmed
				}
				a.clearPendingStartsFromStates(a.WorkloadStates, update.message != "")
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
		default:
			return changed
		}
	}
}

func (a *App) render(w io.Writer, width, height int, ansi bool) error {
	a.ensureTabs()
	switch a.tabs.activeKind() {
	case tabKindDisks:
		_, err := io.WriteString(w, a.renderDiskExplorerTab(width, height).String(ansi))
		return err
	case tabKindContainer, tabKindVM:
		_, err := io.WriteString(w, a.renderShellTabs(width, height).String(ansi))
		return err
	}
	contentHeight := max(0, height-tabBarHeight)
	renderState := a.diskExplorerRenderState()
	key := renderRouteCacheKey(a.Model, width, contentHeight, a.State.PanX, a.State.PanY)
	stableKey := renderRouteCacheStableKey(a.Model, width, contentHeight)
	reuseMovingRoutes := a.State.MoveMode &&
		a.routeCache.key != "" &&
		len(a.routeCache.routes) > 0 &&
		a.routeCache.width == width &&
		a.routeCache.height == height &&
		a.routeCache.panX == a.State.PanX &&
		a.routeCache.panY == a.State.PanY
	reusePanningRoutes := a.inputState.mouse.panActive && stableKey == a.routeCache.stableKey && len(a.routeCache.routes) > 0
	if key != a.routeCache.key && !reuseMovingRoutes && !reusePanningRoutes {
		_, routes := planRenderRoutes(a.Model, renderState, width, contentHeight)
		a.routeCache = routeCacheState{
			key: key, stableKey: stableKey,
			width: width, height: height,
			panX: a.State.PanX, panY: a.State.PanY,
			routes: routes,
		}
	}
	routes := a.routeCache.routes
	if key != a.routeCache.key && reusePanningRoutes {
		routes = translateVisibleEdges(routes, a.State.PanX-a.routeCache.panX, a.State.PanY-a.routeCache.panY)
	}
	content := renderGridWithRoutes(a.Model, renderState, width, contentHeight, routes)
	g := newGrid(width, height)
	copyCanvas(g, content, 0, tabBarHeight)
	a.drawTabBar(g)
	applyTerminalBackground(g)
	_, err := io.WriteString(w, g.String(ansi))
	return err
}

func (a *App) runtime() (workload.Runtime, func(), error) {
	return a.runtimeForLab(a.Lab)
}

func (a *App) runtimeForLab(l *lab.Lab) (workload.Runtime, func(), error) {
	if a.runtimeFactory == nil {
		return nil, func() {}, errors.New("runtime factory is not configured")
	}
	return a.runtimeFactory(l)
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

func (a *App) ensureAppliedAfterDesiredState(message string) {
	current, err := a.currentLabAbsPath()
	if err != nil {
		a.State.Message = message + "; apply lab failed: no open lab file"
		a.State.ApplyLabDisabled = true
		return
	}
	a.ensureDaemonController()
	ctx, cancel := context.WithTimeout(context.Background(), daemonApplyTimeout)
	defer cancel()
	if status, err := a.daemonStatus(ctx); err == nil && status.Active && sameLabPath(status.LabPath, current) {
		a.State.Message = ""
		a.State.ApplyLabDisabled = true
		a.refreshWorkloadStates()
		return
	}
	if err := a.DaemonController.Apply(ctx, DaemonApplyRequest{
		LabPath:           current,
		LibvirtURI:        a.LibvirtURI,
		ContainerdAddress: a.ContainerdAddress,
	}); err != nil {
		a.State.Message = message + "; apply lab failed: " + err.Error()
		a.refreshApplyLabState()
		return
	}
	a.State.Message = ""
	a.State.ApplyLabDisabled = true
	a.refreshWorkloadStates()
}

func (a *App) refreshWorkloadStates() bool {
	service := a.ensureService()
	service.StatesConfirmed = false
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
	a.runtimeState.mu.Lock()
	defer a.runtimeState.mu.Unlock()
	states, err := runtime.States(ctx, a.Lab)
	if err != nil {
		a.State.Message = "runtime status failed: " + err.Error()
		_ = a.refreshVNCPortsWithRuntime(ctx, runtime)
		return true
	}
	a.WorkloadStates = cloneRuntimeStateMap(states)
	a.Service.States = a.WorkloadStates
	a.Service.StatesConfirmed = true
	if portErr := a.refreshVNCPortsWithRuntime(ctx, runtime); portErr != nil {
		a.State.Message = "vnc status failed: " + portErr.Error()
	} else if statusRefreshMessage(a.State.Message) {
		a.State.Message = ""
	}
	a.applyWorkloadStates()
	return true
}

func (a *App) refreshDiskOperationStates() bool {
	service := a.ensureService()
	service.StatesConfirmed = false
	if a.Lab == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer cancel()
	runtime, closeRuntime, err := a.runtime()
	if err != nil {
		a.State.Message = "runtime connection failed: " + err.Error()
		return false
	}
	defer closeRuntime()
	a.runtimeState.mu.Lock()
	defer a.runtimeState.mu.Unlock()
	states, err := runtime.States(ctx, a.Lab)
	if err != nil {
		a.State.Message = "runtime status failed: " + err.Error()
		return false
	}
	a.WorkloadStates = cloneRuntimeStateMap(states)
	service.States = a.WorkloadStates
	service.StatesConfirmed = true
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
	snapshot, err := query(ctx, a.statusSocketPath())
	if err != nil {
		return daemonstatus.Snapshot{}, false
	}
	if !sameLabPath(snapshot.LabPath, current) {
		return daemonstatus.Snapshot{}, false
	}
	return snapshot, true
}

func (a *App) statusSocketPath() string {
	if strings.TrimSpace(a.StatusSocket) != "" {
		return a.StatusSocket
	}
	path, err := userStatusSocketPath()
	if err != nil {
		return ""
	}
	return path
}

func (a *App) statusUpdateFromDaemonSnapshot(l *lab.Lab, snapshot daemonstatus.Snapshot) statusUpdate {
	update := statusUpdate{
		lab:                 l,
		states:              cloneRuntimeStateMap(snapshot.States),
		statesConfirmed:     len(snapshot.Errors) == 0,
		vncPorts:            cloneRuntimePortMap(snapshot.VNCPorts),
		applyStatus:         DaemonStatus{Active: true, LabPath: snapshot.LabPath},
		applyStatusReceived: true,
	}
	if len(snapshot.Errors) > 0 {
		update.message = "foxlabd status: " + strings.Join(displayDaemonMessages(l, snapshot.Errors), "; ")
	} else if len(snapshot.Actions) > 0 {
		update.message = "foxlabd: " + strings.Join(displayDaemonMessages(l, snapshot.Actions), "; ")
	} else {
		update.clearStatusMessage = true
	}
	return update
}

func (a *App) applyDaemonSnapshot(snapshot daemonstatus.Snapshot) {
	a.WorkloadStates = cloneRuntimeStateMap(snapshot.States)
	a.Service.States = a.WorkloadStates
	a.Service.StatesConfirmed = len(snapshot.Errors) == 0
	a.VNCPorts = cloneRuntimePortMap(snapshot.VNCPorts)
	a.clearPendingStartsFromStates(a.WorkloadStates, len(snapshot.Errors) > 0)
	a.updateApplyLabState(DaemonStatus{Active: true, LabPath: snapshot.LabPath})
	if len(snapshot.Errors) > 0 {
		a.State.Message = "foxlabd status: " + strings.Join(displayDaemonMessages(a.Lab, snapshot.Errors), "; ")
	} else if len(snapshot.Actions) > 0 {
		a.State.Message = "foxlabd: " + strings.Join(displayDaemonMessages(a.Lab, snapshot.Actions), "; ")
	} else if statusRefreshMessage(a.State.Message) {
		a.State.Message = ""
	}
	a.applyWorkloadStates()
}

func statusRefreshMessage(message string) bool {
	message = strings.TrimSpace(message)
	for _, prefix := range []string{
		"foxlabd status:",
		"foxlabd:",
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

func (a *App) refreshVNCPortsWithRuntime(ctx context.Context, runtime workload.Runtime) error {
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
	transitions := a.reconcileTransitionsActive()
	for i := range a.Model.Nodes {
		node := &a.Model.Nodes[i]
		key := NodeKey(node.Type, node.ID)
		state, ok := a.WorkloadStates[key]
		if !ok && node.Label != "" {
			state, ok = a.WorkloadStates[NodeKey(node.Type, node.Label)]
		}
		if ok {
			pendingStart := a.PendingStarts[key]
			if !pendingStart && node.Label != "" {
				pendingStart = a.PendingStarts[NodeKey(node.Type, node.Label)]
			}
			node.State = displayNodeWorkloadState(node.Type, node.DesiredState, state, transitions, pendingStart)
		}
		if node.Type == NodeVM {
			port := a.VNCPorts[key]
			if port == 0 && node.Label != "" {
				port = a.VNCPorts[NodeKey(node.Type, node.Label)]
			}
			node.Details = withVNCDetailPort(node.Details, port)
		}
	}
}

func (a *App) clearPendingStartsFromStates(states map[string]string, clearMissing bool) {
	if len(a.PendingStarts) == 0 {
		return
	}
	for key, state := range states {
		if clearMissing || normalizeRuntimeState(state) != "missing" {
			delete(a.PendingStarts, key)
		}
	}
	if len(a.PendingStarts) == 0 {
		a.PendingStarts = nil
	}
}

func (a *App) reconcileTransitionsActive() bool {
	if _, err := a.currentLabAbsPath(); err != nil {
		return false
	}
	return a.State.ApplyLabDisabled
}

func runtimeVNCPorts(ctx context.Context, runtime workload.Runtime, l *lab.Lab) (map[string]int, error) {
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
		return switchUplinkMenuActions(node)
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
	return OneFrameForLab(w, m, nil, width, height)
}

func OneFrameForLab(w io.Writer, m Model, loadedLab *lab.Lab, width, height int) error {
	app := App{
		Model:      m,
		Lab:        loadedLab,
		State:      ViewState{Focus: FocusGraph},
		ViewWidth:  width,
		ViewHeight: height,
	}
	return app.render(w, width, height, false)
}
