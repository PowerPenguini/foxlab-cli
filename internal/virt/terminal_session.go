package virt

import (
	"context"
	"fmt"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

type consoleTerminalSession struct {
	*Console
}

func (r *LibvirtRuntime) OpenTerminalSession(ctx context.Context, l *lab.Lab, ref workload.Ref, _ workload.TerminalSize) (workload.TerminalSession, error) {
	if ref.Type != workload.TypeVM {
		return nil, fmt.Errorf("libvirt terminal sessions require a vm workload")
	}
	console, err := r.OpenConsole(ctx, l, ref.ID)
	if err != nil {
		return nil, err
	}
	return &consoleTerminalSession{Console: console}, nil
}

func (s *consoleTerminalSession) Resize(_, _ int) {}

func (s *consoleTerminalSession) Wait(context.Context) error { return nil }
