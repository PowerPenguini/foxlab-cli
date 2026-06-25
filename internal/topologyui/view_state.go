package topologyui

const (
	FocusGraph = 0
	FocusTop   = 1
)

type ViewState struct {
	Selected            int
	Focus               int
	Message             string
	ContextMenu         bool
	ContextGroup        string
	ContextInSubmenu    bool
	ContextSelected     int
	ContextSubSelected  int
	ContextDeleteNIC    bool
	ContextEdit         bool
	ContextEditValue    string
	ContextEditCursor   int
	ContextAddDiskLayer bool
	ContextMergeDisk    bool
	ContextDetachDisk   bool
	ContextDeleteDisk   bool
	MoveMode            bool
	MoveNodeID          string
	MoveNodeType        string
	MoveStartX          int
	MoveStartY          int
	ConnectMode         bool
	ConnectNodeID       string
	ConnectNodeType     string
	ConnectNICIndex     string
	ConnectTargetMenu   bool
	ConnectTargetID     string
	ConnectTargetType   string
	ConnectTargetIndex  int
	TopMenuRootSelected int
	TopMenuOpen         bool
	TopMenuSelected     int
	MouseClickActive    bool
	MouseClickX         int
	MouseClickY         int
	MouseClickW         int
	MouseClickH         int
	DiskMenuItems       []string
	DiskMenuActions     []string
	DiskMenuKinds       []string
	CommandMode         bool
	Command             string
	Console             []string
}
