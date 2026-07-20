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

func (r *LibvirtRuntime) OpenTerminalSession(ctx context.Context, l *lab.Lab, ref workload.Ref, _ workload.TerminalSize) (workload.OpenedTerminalSession, error) {
	if ref.Type != workload.TypeVM {
		return workload.OpenedTerminalSession{}, fmt.Errorf("libvirt terminal sessions require a vm workload")
	}
	console, err := r.OpenConsole(ctx, l, ref.ID)
	if err != nil {
		return workload.OpenedTerminalSession{}, err
	}
	endpoint := terminalSessionEndpoint(ref.ID, console.Path())
	return workload.OpenedTerminalSession{Session: &consoleTerminalSession{Console: console}, Endpoint: endpoint}, nil
}

func terminalSessionEndpoint(id, path string) string {
	if path != "" {
		return path
	}
	return id
}

func (s *consoleTerminalSession) Resize(_, _ int) {}

func (s *consoleTerminalSession) Wait(context.Context) error { return nil }
