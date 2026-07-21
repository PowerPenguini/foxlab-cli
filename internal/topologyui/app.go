package topologyui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
)

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
	CommandLog            []string
	HistoryIndex          int
	PendingShell          *shellCommand
	PendingVNC            *shellCommand
	vncViewers            map[string]*managedVNCViewer
	PendingStarts         map[string]bool
	In                    *os.File
	Out                   *os.File
	ViewWidth             int
	ViewHeight            int
	StatusRefreshInterval time.Duration
	DaemonController      DaemonController
	tabs                  *tabManager
	runtimeAccess         *runtimeAccess
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
		a.stopAllVNCViewers()
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
			if err := a.startVNCViewer(command); err != nil {
				a.State.Message = "vnc failed: " + err.Error()
			} else {
				a.State.Message = "vnc started: " + command.Display
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
	lab      *lab.Lab
	snapshot runtimeSnapshot
}

func (a *App) startStatusRefresh(ctx context.Context, updates chan<- statusUpdate) {
	l := a.Lab
	snapshot := lab.Clone(l)
	labPath := a.LabPath
	access := a.runtimeClient()
	go func() {
		statusCtx, cancel := context.WithTimeout(ctx, runtimeStatusTimeout)
		defer cancel()
		result := access.readStatus(statusCtx, snapshot, labPath)
		if result.source == runtimeSnapshotDirect && result.runtimeErr == nil && result.statesErr == nil {
			if status, err := a.daemonStatus(statusCtx); err == nil {
				result.applyStatus = &status
			}
		}
		sendStatusUpdate(ctx, updates, statusUpdate{lab: l, snapshot: result})
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
			changed = a.applyRuntimeSnapshot(update.lab, update.snapshot, runtimeSnapshotApplyOptions{
				updateMessage:      true,
				clearPending:       true,
				updateConfirmation: true,
			}) || changed
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

func (a *App) runtimeClient() *runtimeAccess {
	if a.runtimeAccess == nil {
		return &runtimeAccess{}
	}
	return a.runtimeAccess
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
	snapshot := a.runtimeClient().readStatus(ctx, a.Lab, a.LabPath)
	a.applyRuntimeSnapshot(a.Lab, snapshot, runtimeSnapshotApplyOptions{
		updateMessage:      true,
		clearPending:       snapshot.source == runtimeSnapshotDaemon,
		updateConfirmation: true,
	})
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
	snapshot := a.runtimeClient().readLiveStatus(ctx, a.Lab, liveStatusOptions{})
	if snapshot.runtimeErr != nil {
		a.State.Message = "runtime connection failed: " + snapshot.runtimeErr.Error()
		return false
	}
	if snapshot.statesErr != nil {
		a.State.Message = "runtime status failed: " + snapshot.statesErr.Error()
		return false
	}
	a.applyRuntimeSnapshot(a.Lab, snapshot, runtimeSnapshotApplyOptions{updateConfirmation: true})
	return true
}

type runtimeSnapshotApplyOptions struct {
	updateMessage      bool
	clearPending       bool
	updateConfirmation bool
}

func (a *App) applyRuntimeSnapshot(l *lab.Lab, snapshot runtimeSnapshot, options runtimeSnapshotApplyOptions) bool {
	changed := false
	message, clearMessage := runtimeSnapshotMessage(l, snapshot)
	if snapshot.applyStatus != nil {
		a.updateApplyLabState(*snapshot.applyStatus)
		changed = true
	}
	if snapshot.statesReceived {
		a.WorkloadStates = cloneRuntimeStateMap(snapshot.states)
		if a.Service != nil {
			a.Service.States = a.WorkloadStates
			if options.updateConfirmation {
				a.Service.StatesConfirmed = snapshot.statesConfirmed
			}
		}
		if options.clearPending {
			a.clearPendingStartsFromStates(a.WorkloadStates, message != "")
		}
		changed = true
	}
	if snapshot.vncReceived {
		a.VNCPorts = cloneRuntimePortMap(snapshot.vncPorts)
		changed = true
	}
	if snapshot.statesReceived || snapshot.vncReceived {
		a.applyWorkloadStates()
	}
	if options.updateMessage {
		if message != "" {
			a.State.Message = message
			changed = true
		} else if clearMessage && statusRefreshMessage(a.State.Message) {
			a.State.Message = ""
			changed = true
		}
	}
	return changed
}

func runtimeSnapshotMessage(l *lab.Lab, snapshot runtimeSnapshot) (string, bool) {
	if snapshot.source == runtimeSnapshotDaemon {
		if len(snapshot.errors) > 0 {
			return "foxlabd status: " + strings.Join(displayDaemonMessages(l, snapshot.errors), "; "), false
		}
		if len(snapshot.actions) > 0 {
			return "foxlabd: " + strings.Join(displayDaemonMessages(l, snapshot.actions), "; "), false
		}
		return "", true
	}
	if snapshot.runtimeErr != nil {
		return "runtime connection failed: " + snapshot.runtimeErr.Error(), false
	}
	if snapshot.statesErr != nil {
		return "runtime status failed: " + snapshot.statesErr.Error(), false
	}
	if snapshot.vncErr != nil {
		return "vnc status failed: " + snapshot.vncErr.Error(), false
	}
	return "", true
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

func (a *App) contextMenuRootItems(node Node, ok bool) []string {
	if !ok {
		return globalContextMenuItems("")
	}
	return contextMenuRootItemsForInspector(node, inspectorBounds(a.ViewWidth, a.contentHeight()).W > 0)
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
