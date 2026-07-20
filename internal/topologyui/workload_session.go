package topologyui

import (
	"context"

	"foxlab-cli/internal/workload"
)

func (a *App) openTerminalSession(ctx context.Context, ref workload.Ref, size workload.TerminalSize) (workload.OpenedTerminalSession, error) {
	return a.runtimeClient().openTerminalSession(ctx, a.Lab, ref, size)
}
