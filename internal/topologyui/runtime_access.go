package topologyui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

type runtimeSnapshotSource uint8

const (
	runtimeSnapshotDirect runtimeSnapshotSource = iota
	runtimeSnapshotDaemon
)

type runtimeSnapshot struct {
	source          runtimeSnapshotSource
	states          map[string]string
	statesReceived  bool
	statesConfirmed bool
	vncPorts        map[string]int
	vncReceived     bool
	actions         []string
	errors          []string
	applyStatus     *DaemonStatus
	runtimeErr      error
	statesErr       error
	vncErr          error
}

type liveStatusOptions struct {
	includeVNC bool
}

type runtimeAccess struct {
	factory      RuntimeFactory
	statusSocket string
	statusQuery  func(context.Context, string) (daemonstatus.Snapshot, error)
	mu           sync.Mutex
}

func newRuntimeAccess(factory RuntimeFactory, statusSocket string, statusQuery func(context.Context, string) (daemonstatus.Snapshot, error)) *runtimeAccess {
	return &runtimeAccess{
		factory:      factory,
		statusSocket: statusSocket,
		statusQuery:  statusQuery,
	}
}

func (r *runtimeAccess) configured() bool {
	return r != nil && r.factory != nil
}

func (r *runtimeAccess) readStatus(ctx context.Context, l *lab.Lab, labPath string) runtimeSnapshot {
	if snapshot, ok := r.readDaemonSnapshot(ctx, labPath); ok {
		return runtimeSnapshot{
			source:          runtimeSnapshotDaemon,
			states:          cloneRuntimeStateMap(snapshot.States),
			statesReceived:  true,
			statesConfirmed: len(snapshot.Errors) == 0,
			vncPorts:        cloneRuntimePortMap(snapshot.VNCPorts),
			vncReceived:     true,
			actions:         append([]string(nil), snapshot.Actions...),
			errors:          append([]string(nil), snapshot.Errors...),
			applyStatus:     &DaemonStatus{Active: true, LabPath: snapshot.LabPath},
		}
	}
	return r.readLiveStatus(ctx, l, liveStatusOptions{includeVNC: true})
}

func (r *runtimeAccess) readLiveStatus(ctx context.Context, l *lab.Lab, options liveStatusOptions) runtimeSnapshot {
	snapshot := runtimeSnapshot{source: runtimeSnapshotDirect}
	runtime, closeRuntime, err := r.open(l)
	if err != nil {
		snapshot.runtimeErr = err
		return snapshot
	}
	defer closeRuntime()

	r.mu.Lock()
	defer r.mu.Unlock()

	states, err := runtime.States(ctx, l)
	if err != nil {
		snapshot.statesErr = err
	} else {
		snapshot.states = cloneRuntimeStateMap(states)
		snapshot.statesReceived = true
		snapshot.statesConfirmed = true
	}
	if !options.includeVNC {
		return snapshot
	}
	ports, err := runtimeVNCPorts(ctx, runtime, l)
	if err != nil {
		snapshot.vncErr = err
	} else {
		snapshot.vncPorts = cloneRuntimePortMap(ports)
		snapshot.vncReceived = true
	}
	return snapshot
}

func (r *runtimeAccess) openTerminalSession(ctx context.Context, l *lab.Lab, ref workload.Ref, size workload.TerminalSize) (workload.OpenedTerminalSession, error) {
	runtime, closeRuntime, err := r.open(l)
	if err != nil {
		return workload.OpenedTerminalSession{}, err
	}
	sessions, ok := runtime.(workload.SessionRuntime)
	if !ok {
		closeRuntime()
		return workload.OpenedTerminalSession{}, errors.New("runtime does not support terminal sessions")
	}
	opened, err := sessions.OpenTerminalSession(ctx, l, ref, size)
	if err != nil {
		closeRuntime()
		return workload.OpenedTerminalSession{}, err
	}
	if opened.Session == nil {
		closeRuntime()
		return workload.OpenedTerminalSession{}, errors.New("runtime returned an empty terminal session")
	}
	opened.Session = &managedTerminalSession{TerminalSession: opened.Session, closeRuntime: closeRuntime}
	return opened, nil
}

func (r *runtimeAccess) open(l *lab.Lab) (workload.Runtime, func(), error) {
	if r == nil || r.factory == nil {
		return nil, func() {}, errors.New("runtime factory is not configured")
	}
	runtime, closeRuntime, err := r.factory(l)
	if err != nil {
		safeRuntimeClose(closeRuntime)()
		return nil, func() {}, err
	}
	closeRuntime = safeRuntimeClose(closeRuntime)
	if runtime == nil {
		closeRuntime()
		return nil, func() {}, errors.New("runtime factory returned nil runtime")
	}
	return runtime, closeRuntime, nil
}

func safeRuntimeClose(closeRuntime func()) func() {
	if closeRuntime == nil {
		return func() {}
	}
	return closeRuntime
}

func runtimeVNCPorts(ctx context.Context, runtime workload.Runtime, l *lab.Lab) (map[string]int, error) {
	vncRuntime, ok := runtime.(workload.VNCRuntime)
	if !ok {
		return map[string]int{}, nil
	}
	return vncRuntime.VNCPorts(ctx, l)
}

func (r *runtimeAccess) readDaemonSnapshot(ctx context.Context, labPath string) (daemonstatus.Snapshot, bool) {
	if r == nil || strings.TrimSpace(labPath) == "" {
		return daemonstatus.Snapshot{}, false
	}
	current, err := filepath.Abs(labPath)
	if err != nil {
		return daemonstatus.Snapshot{}, false
	}
	query := r.statusQuery
	if query == nil {
		query = daemonstatus.Query
	}
	snapshot, err := query(ctx, r.statusSocketPath())
	if err != nil || !sameLabPath(snapshot.LabPath, current) {
		return daemonstatus.Snapshot{}, false
	}
	return snapshot, true
}

func (r *runtimeAccess) statusSocketPath() string {
	if r != nil && strings.TrimSpace(r.statusSocket) != "" {
		return r.statusSocket
	}
	path, err := userStatusSocketPath()
	if err != nil {
		return ""
	}
	return path
}

type managedTerminalSession struct {
	workload.TerminalSession
	closeRuntime func()
	closeOnce    sync.Once
	closeErr     error
}

func (s *managedTerminalSession) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.TerminalSession.Close()
		if s.closeRuntime != nil {
			s.closeRuntime()
		}
	})
	return s.closeErr
}

func (s *managedTerminalSession) wait(timeout time.Duration) bool {
	ctx := context.Background()
	cancel := func() {}
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	return !errors.Is(s.TerminalSession.Wait(ctx), context.DeadlineExceeded)
}
