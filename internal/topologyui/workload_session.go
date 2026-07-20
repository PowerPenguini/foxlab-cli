package topologyui

import (
	"context"
	"errors"
	"sync"
	"time"

	"foxlab-cli/internal/workload"
)

type managedTerminalSession struct {
	workload.TerminalSession
	closeRuntime func()
	closeOnce    sync.Once
	closeErr     error
}

func (a *App) openTerminalSession(ctx context.Context, ref workload.Ref, size workload.TerminalSize) (workload.OpenedTerminalSession, error) {
	runtime, closeRuntime, err := a.runtime()
	if err != nil {
		return workload.OpenedTerminalSession{}, err
	}
	sessions, ok := runtime.(workload.SessionRuntime)
	if !ok {
		closeRuntime()
		return workload.OpenedTerminalSession{}, errors.New("runtime does not support terminal sessions")
	}
	opened, err := sessions.OpenTerminalSession(ctx, a.Lab, ref, size)
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
