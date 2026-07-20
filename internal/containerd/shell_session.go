package containerd

import (
	"context"
	"errors"
	"io"
	"sync"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

type terminalSession struct {
	input     *io.PipeWriter
	output    *io.PipeReader
	resize    chan ShellSize
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
	errMu     sync.Mutex
	err       error
}

func (r *Runtime) OpenTerminalSession(ctx context.Context, l *lab.Lab, ref workload.Ref, size workload.TerminalSize) (workload.OpenedTerminalSession, error) {
	if ref.Type != workload.TypeContainer {
		return workload.OpenedTerminalSession{}, errors.New("containerd terminal sessions require a container workload")
	}
	ct, ok := findContainer(l, ref.ID)
	if !ok {
		return workload.OpenedTerminalSession{}, errors.New("container not found: " + ref.ID)
	}
	runCtx, cancel := context.WithCancel(ctx)
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	resize := make(chan ShellSize, 1)
	if size.Columns > 0 && size.Rows > 0 {
		resize <- ShellSize{Columns: size.Columns, Rows: size.Rows}
	}
	session := &terminalSession{
		input:  inputWriter,
		output: outputReader,
		resize: resize,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() {
		err := r.ExecShellWithResize(runCtx, l, ref.ID, inputReader, outputWriter, resize)
		err = WithAccessHint(err)
		_ = inputReader.Close()
		_ = outputWriter.CloseWithError(err)
		session.errMu.Lock()
		session.err = err
		session.errMu.Unlock()
		close(session.done)
	}()
	return workload.OpenedTerminalSession{Session: session, Endpoint: l.ManagedContainerName(ct)}, nil
}

func (s *terminalSession) Read(p []byte) (int, error) {
	return s.output.Read(p)
}

func (s *terminalSession) Write(p []byte) (int, error) {
	return s.input.Write(p)
}

func (s *terminalSession) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.cancel()
		err = errors.Join(s.input.Close(), s.output.Close())
	})
	return err
}

func (s *terminalSession) Resize(columns, rows int) {
	if columns <= 0 || rows <= 0 {
		return
	}
	size := ShellSize{Columns: columns, Rows: rows}
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

func (s *terminalSession) Wait(ctx context.Context) error {
	select {
	case <-s.done:
		s.errMu.Lock()
		defer s.errMu.Unlock()
		return s.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
