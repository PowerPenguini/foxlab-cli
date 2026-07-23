package containerd

import (
	"fmt"

	"foxlab-cli/internal/hostnet"
	"foxlab-cli/internal/lab"
)

func prepareContainerForStart(l *lab.Lab, ct lab.Container) (lab.Container, error) {
	if !lab.IsDHCPContainer(ct) {
		return ct, nil
	}
	switchID, ok := lab.DHCPContainerSwitch(ct)
	if !ok {
		return lab.Container{}, fmt.Errorf("DHCP container %q must be connected to exactly one switch", ct.ID)
	}
	if err := lab.ValidateManagedDHCPContainer(ct); err != nil {
		return lab.Container{}, err
	}
	sw, ok := lab.FindSwitch(l, switchID)
	if !ok {
		return lab.Container{}, fmt.Errorf("DHCP container %q references missing switch %q", ct.ID, switchID)
	}
	config := hostnet.NATSwitchConfiguration(l, sw)
	ct.Command = []string{
		"/dnsmasq",
		"--no-daemon",
		"--leasefile-ro",
		"--port=0",
		"--no-resolv",
		"--no-hosts",
		"--bind-dynamic",
		"--interface=eth0",
		"--dhcp-authoritative",
		"--log-dhcp",
		fmt.Sprintf("--dhcp-range=%s,%s,%s,12h", config.DHCPRangeStart, config.DHCPRangeEnd, config.Netmask),
		"--dhcp-option=option:router," + config.Gateway,
		"--dhcp-option=option:dns-server,1.1.1.1,8.8.8.8",
	}
	if ct.Image == "" {
		ct.Image = lab.DefaultDHCPImage
	}
	ct.Capabilities = addContainerCapability(ct.Capabilities, "NET_ADMIN")
	return ct, nil
}

func addContainerCapability(current *lab.ContainerCapabilities, capability string) *lab.ContainerCapabilities {
	out := &lab.ContainerCapabilities{}
	if current != nil {
		out.Add = append(out.Add, current.Add...)
		out.Drop = append(out.Drop, current.Drop...)
	}
	capability = lab.NormalizeContainerCapability(capability)
	for _, existing := range out.Add {
		if lab.NormalizeContainerCapability(existing) == capability {
			return out
		}
	}
	out.Add = append(out.Add, capability)
	filteredDrop := out.Drop[:0]
	for _, dropped := range out.Drop {
		if lab.NormalizeContainerCapability(dropped) != capability {
			filteredDrop = append(filteredDrop, dropped)
		}
	}
	out.Drop = filteredDrop
	return out
}
