package topology

type NetworkEndpointType string

const (
	NetworkEndpointAuto      NetworkEndpointType = ""
	NetworkEndpointSwitch    NetworkEndpointType = "switch"
	NetworkEndpointUplink    NetworkEndpointType = "uplink"
	NetworkEndpointVM        NetworkEndpointType = "vm"
	NetworkEndpointContainer NetworkEndpointType = "container"
)

type NetworkEndpointRef struct {
	Type NetworkEndpointType
	ID   string
	NIC  Field[int]
}

type SwitchCreateRequest struct {
	Name   string
	Mode   string
	Uplink string
}

type SwitchUpdate struct {
	Name         Field[string]
	Mode         Field[string]
	AttachUplink Field[string]
}

type ExternalCreateRequest struct {
	Name      string
	Interface string
	Mode      string
}

type ExternalUpdate struct {
	Name      Field[string]
	Interface Field[string]
	Mode      Field[string]
}

type NICAddRequest struct {
	MAC string
}

type NICConnectRequest struct {
	NIC      int
	Endpoint NetworkEndpointRef
	MAC      string
}
