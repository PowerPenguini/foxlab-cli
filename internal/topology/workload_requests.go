package topology

type WorkloadNetworkInput struct {
	Switch string
	Uplink string
	MAC    string
}

type VMCreateRequest struct {
	Name     string
	CPUs     Field[int]
	MemoryMB Field[int]
	Disk     string
	Network  WorkloadNetworkInput
}

type VMUpdate struct {
	Name     Field[string]
	CPUs     Field[int]
	MemoryMB Field[int]
	Disk     Field[string]
	ISO      Field[string]
	VNC      Field[bool]
	Network  WorkloadNetworkInput
}

type ContainerCreateRequest struct {
	Name    string
	Image   string
	Disk    string
	Command []string
	Shell   string
	Env     map[string]string
	Network WorkloadNetworkInput
}

type ContainerUpdate struct {
	Name    Field[string]
	Image   Field[string]
	Disk    Field[string]
	Command Field[[]string]
	Shell   Field[string]
	Env     Field[map[string]string]
	Network WorkloadNetworkInput
}

type DHCPCreateRequest struct {
	Name   string
	Image  string
	Switch string
}
