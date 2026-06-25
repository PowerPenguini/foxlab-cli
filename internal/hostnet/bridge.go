package hostnet

import (
	"context"
	"fmt"
	"os/exec"

	"foxlab-cli/internal/macnat"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, string(output))
	}
	return nil
}

type Bridge struct {
	Runner CommandRunner
	MacNAT *macnat.Controller
}

type containerNICAttachTarget struct {
	Bridge    string
	Interface string
	Mode      string
	Address   string
	Gateway   string
}

func NewBridge() *Bridge {
	return &Bridge{Runner: ExecRunner{}}
}
