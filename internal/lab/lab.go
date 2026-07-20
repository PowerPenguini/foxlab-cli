package lab

const (
	ManagedPrefix = "foxlab"
	FileExtension = ".lab"

	DesiredStateRunning = "running"
	DesiredStateStopped = "stopped"

	ExternalModeNAT    = "nat"
	ExternalModeDirect = "direct"
	ExternalModeMacNAT = "macnat"
)

type Lab struct {
	ID            string            `json:"name" yaml:"name"`
	LegacyID      string            `json:"id,omitempty" yaml:"id,omitempty"`
	VMs           []VM              `json:"vms,omitempty" yaml:"vms,omitempty"`
	Containers    []Container       `json:"containers,omitempty" yaml:"containers,omitempty"`
	Switches      []Switch          `json:"switches,omitempty" yaml:"switches,omitempty"`
	ExternalLinks []ExternalLink    `json:"externalLinks,omitempty" yaml:"externalLinks,omitempty"`
	NetworkLinks  []NetworkLink     `json:"networkLinks,omitempty" yaml:"networkLinks,omitempty"`
	Disks         []Disk            `json:"disks,omitempty" yaml:"disks,omitempty"`
	Layout        Layout            `json:"layout,omitempty" yaml:"layout,omitempty"`
	Meta          map[string]string `json:"meta,omitempty" yaml:"meta,omitempty"`

	path string
	root string
}

type Disk struct {
	ID           string `json:"id" yaml:"id"`
	Path         string `json:"path" yaml:"path"`
	SizeGB       int    `json:"sizeGB,omitempty" yaml:"sizeGB,omitempty"`
	Format       string `json:"format,omitempty" yaml:"format,omitempty"`
	Kind         string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Base         string `json:"base,omitempty" yaml:"base,omitempty"`
	AttachedType string `json:"attachedType,omitempty" yaml:"attachedType,omitempty"`
	AttachedTo   string `json:"attachedTo,omitempty" yaml:"attachedTo,omitempty"`
	MountPath    string `json:"mountPath,omitempty" yaml:"mountPath,omitempty"`
}

type VM struct {
	ID           string      `json:"id" yaml:"id"`
	Name         string      `json:"name,omitempty" yaml:"name,omitempty"`
	DesiredState string      `json:"desiredState,omitempty" yaml:"desiredState,omitempty"`
	MemoryMB     int         `json:"memoryMB" yaml:"memoryMB"`
	CPUs         int         `json:"cpus" yaml:"cpus"`
	Disk         string      `json:"disk" yaml:"disk"`
	ISO          string      `json:"iso,omitempty" yaml:"iso,omitempty"`
	VNC          bool        `json:"vnc,omitempty" yaml:"vnc,omitempty"`
	Networks     []VMNetwork `json:"networks,omitempty" yaml:"networks,omitempty"`
}

type VMNetwork struct {
	Switch       string `json:"switch,omitempty" yaml:"switch,omitempty"`
	ExternalLink string `json:"externalLink,omitempty" yaml:"externalLink,omitempty"`
	MAC          string `json:"mac,omitempty" yaml:"mac,omitempty"`
}

type Container struct {
	ID           string                 `json:"id" yaml:"id"`
	Name         string                 `json:"name,omitempty" yaml:"name,omitempty"`
	DesiredState string                 `json:"desiredState,omitempty" yaml:"desiredState,omitempty"`
	Image        string                 `json:"image" yaml:"image"`
	Disk         string                 `json:"disk,omitempty" yaml:"disk,omitempty"`
	Command      []string               `json:"command,omitempty" yaml:"command,omitempty"`
	Shell        string                 `json:"shell,omitempty" yaml:"shell,omitempty"`
	Env          map[string]string      `json:"env,omitempty" yaml:"env,omitempty"`
	Capabilities *ContainerCapabilities `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Networks     []ContainerNetwork     `json:"networks,omitempty" yaml:"networks,omitempty"`
}

type ContainerCapabilities struct {
	Add  []string `json:"add,omitempty" yaml:"add,omitempty"`
	Drop []string `json:"drop,omitempty" yaml:"drop,omitempty"`
}

type ContainerNetwork struct {
	Switch       string `json:"switch,omitempty" yaml:"switch,omitempty"`
	ExternalLink string `json:"externalLink,omitempty" yaml:"externalLink,omitempty"`
	MAC          string `json:"mac,omitempty" yaml:"mac,omitempty"`
}

type Switch struct {
	ID            string   `json:"id" yaml:"id"`
	Name          string   `json:"name,omitempty" yaml:"name,omitempty"`
	Mode          string   `json:"mode" yaml:"mode"`
	ExternalLink  string   `json:"externalLink,omitempty" yaml:"externalLink,omitempty"`
	ExternalLinks []string `json:"externalLinks,omitempty" yaml:"externalLinks,omitempty"`
}

type ExternalLink struct {
	ID        string `json:"id" yaml:"id"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	Interface string `json:"interface" yaml:"interface"`
	Mode      string `json:"mode,omitempty" yaml:"mode,omitempty"`
}

type NetworkLink struct {
	From NetworkEndpoint `json:"from" yaml:"from"`
	To   NetworkEndpoint `json:"to" yaml:"to"`
}

type NetworkEndpoint struct {
	Type string `json:"type" yaml:"type"`
	ID   string `json:"id" yaml:"id"`
	NIC  int    `json:"nic" yaml:"nic"`
}

type Layout struct {
	Nodes map[string]Position `json:"nodes,omitempty" yaml:"nodes,omitempty"`
	Links []LayoutLink        `json:"links,omitempty" yaml:"links,omitempty"`
}

type LayoutLink struct {
	From LayoutEndpoint `json:"from" yaml:"from"`
	To   LayoutEndpoint `json:"to" yaml:"to"`
}

type LayoutEndpoint struct {
	Type string `json:"type" yaml:"type"`
	ID   string `json:"id" yaml:"id"`
}

type Position struct {
	X int `json:"x" yaml:"x"`
	Y int `json:"y" yaml:"y"`
}
