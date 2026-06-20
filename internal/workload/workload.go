package workload

import (
	"context"

	"foxlab-cli/internal/lab"
)

const (
	TypeVM        = "vm"
	TypeContainer = "container"
)

type Ref struct {
	Type string
	ID   string
}

type Runtime interface {
	States(context.Context, *lab.Lab) (map[string]string, error)
	Start(context.Context, *lab.Lab, Ref) error
	Stop(context.Context, *lab.Lab, Ref) error
	Close() error
}

type VNCRuntime interface {
	VNCPorts(context.Context, *lab.Lab) (map[string]int, error)
}

func Key(ref Ref) string {
	return ref.Type + ":" + ref.ID
}
