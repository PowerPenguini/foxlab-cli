package topologyui

const (
	FocusGraph     = 0
	FocusTop       = 1
	FocusInspector = 2
)

type ViewState struct {
	Selected               int
	Focus                  int
	PanX                   int
	PanY                   int
	Message                string
	Notification           Notification
	ContextMenu            bool
	ContextGroup           string
	ContextInSubmenu       bool
	ContextSelected        int
	ContextSubSelected     int
	ContextSelectGroup     string
	ContextSelectSelected  int
	ContextDeleteNIC       bool
	ContextDeleteUplink    bool
	ContextEdit            bool
	ContextEditValue       string
	ContextEditCursor      int
	ContextAddDiskLayer    bool
	ContextMergeDisk       bool
	ContextDetachDisk      bool
	ContextDeleteDisk      bool
	MoveMode               bool
	MoveNodeID             string
	MoveNodeType           string
	MoveStartX             int
	MoveStartY             int
	ConnectMode            bool
	ConnectNodeID          string
	ConnectNodeType        string
	ConnectNICIndex        string
	ConnectTargetMenu      bool
	ConnectTargetID        string
	ConnectTargetType      string
	ConnectTargetIndex     int
	TopMenuRootSelected    int
	TopMenuOpen            bool
	TopMenuSelected        int
	PaletteOpen            bool
	PaletteQuery           string
	PaletteSelected        int
	DiskExplorerOpen       bool
	DiskExplorerSelected   int
	DiskExplorerScroll     int
	DiskExplorerEdit       string
	DiskExplorerEditValue  string
	DiskExplorerEditCursor int
	DiskExplorerRows       []string
	DiskExplorerKinds      []string
	DiskExplorerRowViews   []DiskExplorerRowView
	ApplyLabDisabled       bool
	StatusRefreshing       bool
	AnimationFrame         int
	MouseClickActive       bool
	MouseClickX            int
	MouseClickY            int
	MouseClickW            int
	MouseClickH            int
	DiskMenuItems          []string
	DiskMenuActions        []string
	DiskMenuKinds          []string
	InspectorSelected      int
	InspectorEditing       bool
	InspectorEditValue     string
	InspectorEditCursor    int
	InspectorCapOpen       bool
	InspectorCapQuery      string
	InspectorCapSelected   int
	CommandMode            bool
	Command                string
	Console                []string
}

type DiskExplorerRowView struct {
	ID       string
	Kind     string
	Size     string
	Format   string
	Relation string
	Path     string
	Depth    int
	Missing  bool
}
