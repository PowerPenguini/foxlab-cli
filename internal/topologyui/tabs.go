package topologyui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"foxlab-cli/internal/workload"
	"golang.org/x/sys/unix"
)

const (
	tabBarHeight       = 1
	terminalScrollback = 10_000
)

type tabKind string

const (
	tabKindLab       tabKind = "lab"
	tabKindContainer tabKind = "container"
	tabKindVM        tabKind = "vm"
)

type tabStatus string

const (
	tabStatusStarting tabStatus = "starting"
	tabStatusRunning  tabStatus = "running"
	tabStatusExited   tabStatus = "exited"
)

type appTab struct {
	key           string
	kind          tabKind
	nodeID        string
	label         string
	status        tabStatus
	buffer        *terminalBuffer
	session       io.WriteCloser
	connectCancel context.CancelFunc
	scroll        int
	unread        bool
	exitError     error
	generation    uint64
	cols          int
	rows          int
}

type tabHit struct {
	index  int
	bounds rect
	closeX int
}

type tabManager struct {
	mu          sync.Mutex
	tabs        []*appTab
	active      int
	offset      int
	hits        []tabHit
	updates     chan struct{}
	gPrefix     bool
	closing     bool
	wakeMu      sync.RWMutex
	wakeReadFD  int
	wakeWriteFD int
}

func (a *App) ensureTabs() {
	if a.tabs != nil {
		return
	}
	name := "untitled"
	if a.Lab != nil && strings.TrimSpace(a.Lab.ID) != "" {
		name = a.Lab.ID
	} else if a.LabPath != "" {
		name = strings.TrimSuffix(filepath.Base(a.LabPath), filepath.Ext(a.LabPath))
	}
	a.tabs = &tabManager{
		tabs:        []*appTab{{key: "lab", kind: tabKindLab, label: "Lab: " + name, status: tabStatusRunning}},
		updates:     make(chan struct{}, 1),
		wakeReadFD:  -1,
		wakeWriteFD: -1,
	}
}

func (m *tabManager) openWakePipe() error {
	if m == nil {
		return nil
	}
	m.wakeMu.Lock()
	if m.wakeReadFD >= 0 && m.wakeWriteFD >= 0 {
		m.wakeMu.Unlock()
		return nil
	}
	pipe := []int{-1, -1}
	if err := unix.Pipe2(pipe, unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		m.wakeMu.Unlock()
		return err
	}
	m.wakeReadFD = pipe[0]
	m.wakeWriteFD = pipe[1]
	m.wakeMu.Unlock()
	if len(m.updates) > 0 {
		m.signalWake()
	}
	return nil
}

func (m *tabManager) closeWakePipe() {
	if m == nil {
		return
	}
	m.wakeMu.Lock()
	readFD, writeFD := m.wakeReadFD, m.wakeWriteFD
	m.wakeReadFD, m.wakeWriteFD = -1, -1
	if readFD >= 0 {
		_ = unix.Close(readFD)
	}
	if writeFD >= 0 {
		_ = unix.Close(writeFD)
	}
	m.wakeMu.Unlock()
}

func (m *tabManager) wakeReadDescriptor() int {
	if m == nil {
		return -1
	}
	m.wakeMu.RLock()
	defer m.wakeMu.RUnlock()
	return m.wakeReadFD
}

func (m *tabManager) signalWake() {
	if m == nil {
		return
	}
	m.wakeMu.RLock()
	defer m.wakeMu.RUnlock()
	if m.wakeWriteFD < 0 {
		return
	}
	_, _ = unix.Write(m.wakeWriteFD, []byte{1})
}

func (m *tabManager) drainWake() {
	if m == nil {
		return
	}
	m.wakeMu.RLock()
	defer m.wakeMu.RUnlock()
	if m.wakeReadFD < 0 {
		return
	}
	var buffer [64]byte
	for {
		_, err := unix.Read(m.wakeReadFD, buffer[:])
		if err == unix.EINTR {
			continue
		}
		return
	}
}

func (a *App) closeTabs() {
	if a.tabs == nil {
		return
	}
	a.tabs.mu.Lock()
	a.tabs.closing = true
	var sessions []io.WriteCloser
	var cancels []context.CancelFunc
	for _, tab := range a.tabs.tabs {
		tab.generation++
		if tab.connectCancel != nil {
			cancels = append(cancels, tab.connectCancel)
			tab.connectCancel = nil
		}
		if tab.buffer != nil {
			tab.buffer.setReplyWriter(nil)
		}
		if tab.session != nil {
			sessions = append(sessions, tab.session)
			tab.session = nil
		}
	}
	a.tabs.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	for _, session := range sessions {
		_ = session.Close()
	}
	for _, session := range sessions {
		waitForTabSession(session, 8*time.Second)
	}
}

func (a *App) drainTabUpdates() bool {
	if a.tabs == nil {
		return false
	}
	// Drain the descriptor before the channel. A notification racing with the
	// channel receive leaves a byte behind, causing at most one harmless wake;
	// it can never leave a queued update without a wake byte.
	a.tabs.drainWake()
	select {
	case <-a.tabs.updates:
		return true
	default:
		return false
	}
}

func (m *tabManager) notify() {
	select {
	case m.updates <- struct{}{}:
		m.signalWake()
	default:
	}
}

func (a *App) tabInputNeedsImmediateRender(key string, wasRunningShell bool) bool {
	if !wasRunningShell {
		return true
	}
	if key == "ctrl+]" || key == "shift-pageup" || key == "shift-pagedown" || strings.HasPrefix(key, "alt+") {
		return true
	}
	if event, ok := parseMouseEvent(key); ok && event.y == 0 {
		return true
	}
	return false
}

func (a *App) shellPaletteOpen() bool {
	return a != nil && a.State.PaletteOpen && a.tabs != nil && a.tabs.activeKind() != tabKindLab
}

func (m *tabManager) markActivity(tab *appTab) {
	m.mu.Lock()
	if m.active >= 0 && m.active < len(m.tabs) && m.tabs[m.active] != tab && tab.status == tabStatusRunning {
		tab.unread = true
	}
	m.mu.Unlock()
	m.notify()
}

func (m *tabManager) activeKind() tabKind {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active < 0 || m.active >= len(m.tabs) {
		return tabKindLab
	}
	return m.tabs[m.active].kind
}

func (m *tabManager) activeRunningShell() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active < 0 || m.active >= len(m.tabs) {
		return false
	}
	tab := m.tabs[m.active]
	return tab.kind != tabKindLab && tab.status == tabStatusRunning
}

func (a *App) openShellTab(node Node) {
	a.ensureTabs()
	key := node.Type + ":" + node.ID
	a.tabs.mu.Lock()
	for i, tab := range a.tabs.tabs {
		if tab.key == key {
			a.tabs.active = i
			tab.unread = false
			a.tabs.mu.Unlock()
			a.tabs.notify()
			return
		}
	}
	a.tabs.mu.Unlock()

	if err := a.ensureShellWorkloadRunning(node); err != nil {
		a.State.Message = "shell failed: " + err.Error()
		return
	}
	label := strings.ToUpper(node.Type)
	if node.Type == NodeContainer {
		label = "CT"
	}
	tab := &appTab{
		key:    key,
		kind:   tabKind(node.Type),
		nodeID: node.ID,
		label:  label + ": " + a.displayNodeName(node.Type, node.ID),
		status: tabStatusStarting,
	}
	cols, rows := a.initialShellSize()
	tab.cols, tab.rows = cols, rows
	tab.buffer = newTerminalBuffer(cols, rows, terminalScrollback, func() { a.tabs.markActivity(tab) })
	a.tabs.mu.Lock()
	a.tabs.tabs = append(a.tabs.tabs, tab)
	a.tabs.active = len(a.tabs.tabs) - 1
	a.tabs.mu.Unlock()
	a.tabs.notify()
	a.startTabSession(tab)
}

func (a *App) initialShellSize() (int, int) {
	cols := a.ViewWidth
	rows := a.contentHeight()
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	return cols, rows
}

func (a *App) startTabSession(tab *appTab) {
	if tab == nil {
		return
	}
	a.tabs.mu.Lock()
	tab.generation++
	generation := tab.generation
	tab.status = tabStatusStarting
	tab.exitError = nil
	tab.scroll = 0
	tab.unread = false
	kind, id, output := tab.kind, tab.nodeID, tab.buffer
	cols, rows := tab.cols, tab.rows
	a.tabs.mu.Unlock()
	_, _ = io.WriteString(output, "\r\n[connecting]\r\n")

	if kind != tabKindContainer && kind != tabKindVM {
		return
	}
	runCtx, cancel := context.WithCancel(context.Background())
	a.tabs.mu.Lock()
	if a.tabs.closing || !a.tabPresentLocked(tab) || tab.generation != generation {
		a.tabs.mu.Unlock()
		cancel()
		return
	}
	tab.connectCancel = cancel
	a.tabs.mu.Unlock()
	go func() {
		ref := workload.Ref{Type: string(kind), ID: id}
		session, err := a.openTerminalSession(runCtx, ref, workload.TerminalSize{Columns: cols, Rows: rows})
		a.clearTabConnectCancel(tab, generation)
		if err != nil {
			cancel()
			a.finishTabSession(tab, generation, err)
			return
		}
		a.setTabSession(tab, generation, session, tabStatusRunning)
		_, copyErr := io.Copy(output, session)
		if errors.Is(copyErr, io.EOF) {
			copyErr = nil
		}
		waitErr := session.Wait(runCtx)
		err = errors.Join(copyErr, waitErr)
		if err != nil && kind == tabKindContainer && containerShellNeedsRestart(err.Error()) {
			err = fmt.Errorf("%w; stop and run the container to rebuild/restart its rootfs", err)
		}
		cancel()
		a.finishTabSession(tab, generation, err)
	}()
}

func (a *App) clearTabConnectCancel(tab *appTab, generation uint64) {
	a.tabs.mu.Lock()
	if a.tabPresentLocked(tab) && tab.generation == generation {
		tab.connectCancel = nil
	}
	a.tabs.mu.Unlock()
}

func (a *App) setTabSession(tab *appTab, generation uint64, session io.WriteCloser, status tabStatus) {
	a.tabs.mu.Lock()
	if !a.tabs.closing && a.tabPresentLocked(tab) && tab.generation == generation {
		tab.session = session
		tab.status = status
		if tab.buffer != nil {
			tab.buffer.setReplyWriter(session)
		}
		a.tabs.mu.Unlock()
		a.tabs.notify()
		return
	}
	a.tabs.mu.Unlock()
	if session != nil {
		_ = session.Close()
	}
	a.tabs.notify()
}

func (a *App) finishTabSession(tab *appTab, generation uint64, err error) {
	a.tabs.mu.Lock()
	if !a.tabPresentLocked(tab) || tab.generation != generation {
		a.tabs.mu.Unlock()
		return
	}
	session := tab.session
	tab.session = nil
	tab.status = tabStatusExited
	tab.exitError = err
	if a.tabs.tabs[a.tabs.active] != tab {
		tab.unread = true
	}
	a.tabs.mu.Unlock()
	if tab.buffer != nil {
		tab.buffer.setReplyWriter(nil)
	}
	if session != nil {
		_ = session.Close()
	}
	message := "[session exited]"
	if err != nil {
		message = "[session exited: " + err.Error() + "]"
	}
	_, _ = io.WriteString(tab.buffer, "\r\n"+message+"\r\n")
	a.tabs.notify()
}

func (a *App) tabPresentLocked(target *appTab) bool {
	for _, tab := range a.tabs.tabs {
		if tab == target {
			return true
		}
	}
	return false
}

type tabPipeSession struct {
	mu              sync.Mutex
	writer          *io.PipeWriter
	cancel          context.CancelFunc
	resize          chan workload.TerminalSize
	input           chan []byte
	done            chan struct{}
	runtimeDone     chan struct{}
	runtimeDoneOnce sync.Once
	closed          bool
}

func newTabPipeSession(writer *io.PipeWriter, cancel context.CancelFunc, resize chan workload.TerminalSize) *tabPipeSession {
	session := &tabPipeSession{
		writer:      writer,
		cancel:      cancel,
		resize:      resize,
		input:       make(chan []byte, 128),
		done:        make(chan struct{}),
		runtimeDone: make(chan struct{}),
	}
	go session.pumpInput()
	return session
}

func (s *tabPipeSession) markFinished() {
	s.runtimeDoneOnce.Do(func() { close(s.runtimeDone) })
}

func (s *tabPipeSession) wait(timeout time.Duration) bool {
	if timeout <= 0 {
		<-s.runtimeDone
		return true
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-s.runtimeDone:
		return true
	case <-timer.C:
		return false
	}
}

func waitForTabSession(session io.WriteCloser, timeout time.Duration) bool {
	if waiter, ok := session.(interface{ wait(time.Duration) bool }); ok {
		return waiter.wait(timeout)
	}
	return true
}

func (s *tabPipeSession) pumpInput() {
	for {
		select {
		case input := <-s.input:
			if _, err := s.writer.Write(input); err != nil {
				_ = s.Close()
				return
			}
		case <-s.done:
			return
		}
	}
}

func (s *tabPipeSession) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.writer == nil {
		return 0, io.ErrClosedPipe
	}
	input := append([]byte(nil), p...)
	select {
	case s.input <- input:
		return len(p), nil
	default:
		return 0, errors.New("terminal input queue is full")
	}
}

func (s *tabPipeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.writer == nil {
		return nil
	}
	s.closed = true
	close(s.done)
	if s.cancel != nil {
		s.cancel()
	}
	return s.writer.Close()
}

func (s *tabPipeSession) Resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	size := workload.TerminalSize{Columns: cols, Rows: rows}
	select {
	case s.resize <- size:
		return
	default:
	}
	select {
	case <-s.resize:
	default:
	}
	select {
	case s.resize <- size:
	default:
	}
}

func (a *App) handleTabInput(key string, raw []byte) bool {
	if a.tabs == nil {
		return false
	}
	if key == "alt+:" {
		a.togglePalette()
		return true
	}
	paletteOwnsInput := a.State.PaletteOpen
	if a.inputState.pasteActive && key != "paste-start" && key != "paste-end" {
		a.tabs.mu.Lock()
		active := a.tabs.tabs[a.tabs.active]
		kind, status := active.kind, active.status
		a.tabs.mu.Unlock()
		if (kind == tabKindLab || paletteOwnsInput) && !a.inputState.pasteToUI {
			return true
		}
		if kind == tabKindLab || paletteOwnsInput {
			if strings.HasPrefix(key, "char:") {
				return false
			}
			if key == "enter" || key == "tab" {
				_ = a.handleKey("char: ")
			}
			return true
		}
		if kind != tabKindLab {
			if status == tabStatusRunning && active.buffer != nil && active.buffer.acceptsKeyboardInput() && len(raw) > 0 {
				if err := a.writeActiveTab(raw); err != nil {
					a.State.Message = "shell paste input failed: " + err.Error()
				}
			}
			return true
		}
	}
	if strings.HasPrefix(key, "alt+") {
		index, err := strconv.Atoi(strings.TrimPrefix(key, "alt+"))
		if err == nil && index >= 1 && index <= 9 {
			a.activateTab(index - 1)
			return true
		}
	}
	if isMouseKey(key) {
		if event, ok := parseMouseEvent(key); ok {
			if event.y == 0 {
				if event.kind == mousePress && event.button&64 == 0 && event.button&3 == 0 {
					return a.activateClickedTab(event.x)
				}
				return true
			}
			if paletteOwnsInput {
				return false
			}
			a.tabs.mu.Lock()
			active := a.tabs.tabs[a.tabs.active]
			kind, status, buffer := active.kind, active.status, active.buffer
			a.tabs.mu.Unlock()
			if kind == tabKindLab {
				return false
			}
			if status != tabStatusRunning || buffer == nil {
				return true
			}
			encoded, ok := buffer.encodeMouseInput(event)
			if !ok {
				return true
			}
			if err := a.writeActiveTab(encoded); err != nil {
				a.State.Message = "shell mouse input failed: " + err.Error()
			}
			return true
		}
		return true
	}
	if key == "paste-start" || key == "paste-end" {
		if key == "paste-end" {
			a.inputState.pasteActive = false
			a.inputState.pasteToUI = false
		}
		a.tabs.mu.Lock()
		active := a.tabs.tabs[a.tabs.active]
		kind, status, buffer := active.kind, active.status, active.buffer
		a.tabs.mu.Unlock()
		if kind == tabKindLab || paletteOwnsInput {
			if key == "paste-start" {
				a.inputState.pasteActive = true
				a.inputState.pasteToUI = paletteOwnsInput || a.State.ContextEdit || a.State.DiskExplorerEdit != ""
			}
			return true
		}
		if key == "paste-start" {
			a.inputState.pasteActive = true
			a.inputState.pasteToUI = false
		}
		if status == tabStatusRunning && buffer != nil && buffer.acceptsKeyboardInput() && buffer.acceptsBracketedPaste() {
			if err := a.writeActiveTab(raw); err != nil {
				a.State.Message = "shell paste input failed: " + err.Error()
			}
		}
		return true
	}
	if key == "focus-in" || key == "focus-out" {
		if paletteOwnsInput {
			return true
		}
		a.tabs.mu.Lock()
		active := a.tabs.tabs[a.tabs.active]
		kind, status, buffer := active.kind, active.status, active.buffer
		a.tabs.mu.Unlock()
		if kind != tabKindLab && status == tabStatusRunning && buffer != nil && buffer.acceptsFocusInput() {
			if err := a.writeActiveTab(raw); err != nil {
				a.State.Message = "shell focus input failed: " + err.Error()
			}
		}
		return true
	}
	if paletteOwnsInput {
		if key == "ctrl+]" {
			a.closePalette()
			a.activateTab(0)
			return true
		}
		return false
	}

	a.tabs.mu.Lock()
	active := a.tabs.tabs[a.tabs.active]
	status := active.status
	if active.kind == tabKindLab || active.status == tabStatusExited {
		if a.tabs.gPrefix {
			a.tabs.gPrefix = false
			a.tabs.mu.Unlock()
			switch key {
			case "char:t":
				a.nextTab(1)
				return true
			case "char:T":
				a.nextTab(-1)
				return true
			}
			return active.kind != tabKindLab
		}
		if key == "char:g" {
			a.tabs.gPrefix = true
			a.tabs.mu.Unlock()
			return true
		}
		a.tabs.mu.Unlock()
		if active.kind == tabKindLab {
			return false
		}
	} else {
		a.tabs.mu.Unlock()
	}
	if key == "ctrl+]" {
		a.activateTab(0)
		return true
	}
	if key == "shift-pageup" {
		a.scrollActiveTab(5)
		return true
	}
	if key == "shift-pagedown" {
		a.scrollActiveTab(-5)
		return true
	}
	if status == tabStatusExited {
		switch key {
		case "char:r":
			a.restartActiveTab()
		case "char:x":
			a.closeActiveTab()
		case "char::":
			return false
		default:
			return true
		}
		return true
	}
	if len(raw) == 0 {
		return false
	}
	encoded := raw
	if active.buffer != nil {
		encoded = active.buffer.encodeKeyInput(key, raw)
	}
	if len(encoded) == 0 {
		return true
	}
	if err := a.writeActiveTab(encoded); err != nil {
		a.State.Message = "shell input failed: " + err.Error()
	}
	return true
}

func (a *App) writeActiveTab(raw []byte) error {
	a.tabs.mu.Lock()
	tab := a.tabs.tabs[a.tabs.active]
	session := tab.session
	tab.scroll = 0
	a.tabs.mu.Unlock()
	if session == nil {
		return errors.New("session is not connected")
	}
	_, err := session.Write(raw)
	return err
}

func (a *App) activateTab(index int) {
	a.tabs.mu.Lock()
	defer a.tabs.mu.Unlock()
	if index < 0 || index >= len(a.tabs.tabs) {
		return
	}
	a.tabs.active = index
	a.tabs.tabs[index].unread = false
	a.tabs.gPrefix = false
	a.tabs.notify()
}

func (a *App) nextTab(delta int) {
	a.tabs.mu.Lock()
	count := len(a.tabs.tabs)
	if count > 0 {
		a.tabs.active = (a.tabs.active + delta%count + count) % count
		a.tabs.tabs[a.tabs.active].unread = false
	}
	a.tabs.mu.Unlock()
	a.tabs.notify()
}

func (a *App) closeActiveTab() {
	a.tabs.mu.Lock()
	index := a.tabs.active
	a.tabs.mu.Unlock()
	a.closeTab(index)
}

func (a *App) closeTab(index int) {
	a.tabs.mu.Lock()
	if index <= 0 || index >= len(a.tabs.tabs) {
		a.tabs.mu.Unlock()
		return
	}
	tab := a.tabs.tabs[index]
	session := tab.session
	connectCancel := tab.connectCancel
	tab.session = nil
	tab.connectCancel = nil
	if tab.buffer != nil {
		tab.buffer.setReplyWriter(nil)
	}
	a.tabs.tabs = append(a.tabs.tabs[:index], a.tabs.tabs[index+1:]...)
	if a.tabs.active >= len(a.tabs.tabs) {
		a.tabs.active = len(a.tabs.tabs) - 1
	} else if a.tabs.active > index {
		a.tabs.active--
	}
	a.tabs.mu.Unlock()
	if connectCancel != nil {
		connectCancel()
	}
	if session != nil {
		_ = session.Close()
		waitForTabSession(session, 8*time.Second)
	}
	a.tabs.notify()
}

func (a *App) restartActiveTab() {
	a.tabs.mu.Lock()
	if a.tabs.active <= 0 || a.tabs.active >= len(a.tabs.tabs) {
		a.tabs.mu.Unlock()
		return
	}
	tab := a.tabs.tabs[a.tabs.active]
	session := tab.session
	connectCancel := tab.connectCancel
	tab.session = nil
	tab.connectCancel = nil
	// Invalidate callbacks from the previous backend before closing it. The
	// replacement session receives its own generation in startTabSession.
	tab.generation++
	if tab.buffer != nil {
		tab.buffer.setReplyWriter(nil)
	}
	tab.buffer = newTerminalBuffer(max(1, tab.cols), max(1, tab.rows), terminalScrollback, func() { a.tabs.markActivity(tab) })
	buffer := tab.buffer
	a.tabs.mu.Unlock()
	if connectCancel != nil {
		connectCancel()
	}
	if session != nil {
		_ = session.Close()
		waitForTabSession(session, 8*time.Second)
	}
	_, _ = io.WriteString(buffer, "[restarting session]\r\n")
	a.startTabSession(tab)
}

func (a *App) scrollActiveTab(delta int) {
	a.tabs.mu.Lock()
	tab := a.tabs.tabs[a.tabs.active]
	if tab.kind != tabKindLab {
		tab.scroll = max(0, tab.scroll+delta)
	}
	a.tabs.mu.Unlock()
	a.tabs.notify()
}

func (a *App) activateClickedTab(x int) bool {
	a.tabs.mu.Lock()
	hits := append([]tabHit(nil), a.tabs.hits...)
	a.tabs.mu.Unlock()
	for _, hit := range hits {
		if x < hit.bounds.X || x >= hit.bounds.X+hit.bounds.W {
			continue
		}
		if hit.closeX == x {
			a.closeTab(hit.index)
		} else {
			a.activateTab(hit.index)
		}
		return true
	}
	return true
}

func (a *App) tabBySelector(selector string) (int, bool) {
	a.tabs.mu.Lock()
	defer a.tabs.mu.Unlock()
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return a.tabs.active, true
	}
	if value, err := strconv.Atoi(selector); err == nil {
		value--
		return value, value >= 0 && value < len(a.tabs.tabs)
	}
	for index, tab := range a.tabs.tabs {
		if strings.EqualFold(tab.label, selector) {
			return index, true
		}
	}
	return 0, false
}

func (a *App) executeTabCommand(fields []string) {
	name := fields[0]
	switch name {
	case "tabnext":
		if len(fields) == 1 {
			a.nextTab(1)
			return
		}
	case "tabprev":
		if len(fields) == 1 {
			a.nextTab(-1)
			return
		}
	case "tabclose", "tabrestart":
		if len(fields) >= 1 {
			selector := ""
			if len(fields) > 1 {
				selector = strings.Join(fields[1:], " ")
			}
			index, ok := a.tabBySelector(selector)
			if !ok {
				a.State.Message = "tab not found: " + selector
				return
			}
			if name == "tabclose" {
				a.closeTab(index)
			} else {
				a.activateTab(index)
				a.restartActiveTab()
			}
			return
		}
	}
	a.State.Message = "usage: " + name + " [index|label]"
}

func (a *App) tabStatusForTest(index int) (tabStatus, bool) {
	a.tabs.mu.Lock()
	defer a.tabs.mu.Unlock()
	if index < 0 || index >= len(a.tabs.tabs) {
		return "", false
	}
	return a.tabs.tabs[index].status, true
}
