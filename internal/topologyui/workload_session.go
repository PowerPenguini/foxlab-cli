package topologyui

import (
	"context"
	"errors"
	"io"
	"sync"

	"foxlab-cli/internal/workload"
)

type managedTerminalSession struct {
	workload.TerminalSession
	closeRuntime func()
	closeOnce    sync.Once
	closeErr     error
}

func (a *App) openTerminalSession(ctx context.Context, ref workload.Ref, size workload.TerminalSize) (workload.TerminalSession, error) {
	if ref.Type == workload.TypeVM && a.VMConsole != nil {
		console, _, err := a.VMConsole(ctx, a.Lab, ref.ID)
		if err != nil {
			return nil, err
		}
		return &legacyTerminalSession{ReadWriteCloser: console}, nil
	}
	runtime, closeRuntime, err := a.runtime()
	if err != nil {
		return nil, err
	}
	sessions, ok := runtime.(workload.SessionRuntime)
	if !ok {
		closeRuntime()
		return nil, errors.New("runtime does not support terminal sessions")
	}
	session, err := sessions.OpenTerminalSession(ctx, a.Lab, ref, size)
	if err != nil {
		closeRuntime()
		return nil, err
	}
	return &managedTerminalSession{TerminalSession: session, closeRuntime: closeRuntime}, nil
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

type legacyTerminalSession struct {
	io.ReadWriteCloser
}

func (s *legacyTerminalSession) Resize(_, _ int) {}

func (s *legacyTerminalSession) Wait(context.Context) error { return nil }
