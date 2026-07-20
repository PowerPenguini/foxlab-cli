package workload

import (
	"context"
	"io"

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

type Destroyer interface {
	Destroy(context.Context, *lab.Lab, Ref) error
}

type OrphanCleaner interface {
	CleanupOrphans(context.Context, *lab.Lab) ([]string, error)
}

type VNCRuntime interface {
	VNCPorts(context.Context, *lab.Lab) (map[string]int, error)
}

type FileTransferer interface {
	PutFile(context.Context, *lab.Lab, Ref, string, string) error
	GetFile(context.Context, *lab.Lab, Ref, string, string) error
}

// TerminalSize describes the visible terminal area for an interactive
// workload session.
type TerminalSize struct {
	Columns int
	Rows    int
}

// TerminalSession is a runtime-neutral interactive console or shell. Wait
// reports the backend result after the stream ends or the session is closed.
type TerminalSession interface {
	io.ReadWriteCloser
	Resize(columns, rows int)
	Wait(context.Context) error
}

// SessionRuntime is an optional runtime capability for interactive workload
// consoles and shells.
type SessionRuntime interface {
	OpenTerminalSession(context.Context, *lab.Lab, Ref, TerminalSize) (TerminalSession, error)
}

type StartOutcome struct {
	Action string
}

type StartOutcomeRuntime interface {
	StartWithOutcome(context.Context, *lab.Lab, Ref) (StartOutcome, error)
}

func Key(ref Ref) string {
	return ref.Type + ":" + ref.ID
}
