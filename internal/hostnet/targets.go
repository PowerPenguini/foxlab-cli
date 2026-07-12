package hostnet

import (
	"context"
	"fmt"

	"foxlab-cli/internal/lab"
)

func (b *Bridge) vmNICBridge(ctx context.Context, l *lab.Lab, vm lab.VM, index int, nic lab.VMNetwork) (string, error) {
	if nic.Switch != "" {
		sw, ok := findSwitch(l, nic.Switch)
		if !ok {
			return "", fmt.Errorf("vm %q references missing switch %q", vm.ID, nic.Switch)
		}
		if err := b.EnsureSwitchBridge(ctx, l, sw); err != nil {
			return "", err
		}
		return l.ManagedSwitchBridgeName(sw), nil
	}
	if link, ok := findNetworkLinkForEndpoint(l, lab.NetworkEndpoint{Type: "vm", ID: vm.ID, NIC: index}); ok {
		bridge := l.ManagedNetworkLinkBridgeName(link)
		if err := b.ensureBridge(ctx, bridge); err != nil {
			return "", err
		}
		return bridge, nil
	}
	if nic.ExternalLink != "" {
		link, ok := findExternalLink(l, nic.ExternalLink)
		if !ok {
			return "", fmt.Errorf("vm %q references missing external link %q", vm.ID, nic.ExternalLink)
		}
		switch link.Mode {
		case lab.ExternalModeNAT:
			bridge := l.ManagedExternalBridgeName(link)
			gateway, cidr := externalNATGatewayCIDR(l, link)
			if err := b.ensureNATBridge(ctx, bridge, gateway, cidr, link.Interface); err != nil {
				return "", err
			}
			return bridge, nil
		case lab.ExternalModeMacNAT:
			bridge := l.ManagedExternalBridgeName(link)
			if err := b.ensureBridge(ctx, bridge); err != nil {
				return "", err
			}
			return bridge, nil
		default:
			return "", nil
		}
	}
	return "", nil
}

func (b *Bridge) containerNICAttachTarget(ctx context.Context, l *lab.Lab, ct lab.Container, index int, nic lab.ContainerNetwork) (containerNICAttachTarget, error) {
	if nic.Switch != "" {
		sw, ok := findSwitch(l, nic.Switch)
		if !ok {
			return containerNICAttachTarget{}, fmt.Errorf("container %q references missing switch %q", ct.ID, nic.Switch)
		}
		target := containerNICAttachTarget{Bridge: l.ManagedSwitchBridgeName(sw)}
		if sw.Mode == "nat" && !switchUsesMacNAT(l, sw) {
			if err := b.ensureNATSwitchBridge(ctx, l, sw); err != nil {
				return containerNICAttachTarget{}, err
			}
			gateway, _ := switchNATGatewayCIDR(l, sw)
			address, err := switchNATContainerAddress(l, sw, ct, index)
			if err != nil {
				return containerNICAttachTarget{}, err
			}
			target.Mode = lab.ExternalModeNAT
			target.Address = address
			target.Gateway = gateway
			return target, nil
		}
		if err := b.EnsureSwitchBridge(ctx, l, sw); err != nil {
			return containerNICAttachTarget{}, err
		}
		if switchUsesMacNAT(l, sw) {
			target.Mode = lab.ExternalModeMacNAT
		}
		return target, nil
	}
	if nic.ExternalLink != "" {
		link, ok := findExternalLink(l, nic.ExternalLink)
		if !ok {
			return containerNICAttachTarget{}, fmt.Errorf("container %q references missing external link %q", ct.ID, nic.ExternalLink)
		}
		switch link.Mode {
		case lab.ExternalModeNAT:
			bridge := l.ManagedExternalBridgeName(link)
			gateway, cidr := externalNATGatewayCIDR(l, link)
			if err := b.ensureNATBridge(ctx, bridge, gateway, cidr, link.Interface); err != nil {
				return containerNICAttachTarget{}, err
			}
			address, err := externalNATContainerAddress(l, link, ct, index)
			if err != nil {
				return containerNICAttachTarget{}, err
			}
			return containerNICAttachTarget{
				Bridge:  bridge,
				Mode:    lab.ExternalModeNAT,
				Address: address,
				Gateway: gateway,
			}, nil
		case lab.ExternalModeMacNAT:
			bridge := l.ManagedExternalBridgeName(link)
			if err := b.ensureBridge(ctx, bridge); err != nil {
				return containerNICAttachTarget{}, err
			}
			return containerNICAttachTarget{Bridge: bridge, Mode: lab.ExternalModeMacNAT}, nil
		}
		if isLinuxBridge(link.Interface) {
			return containerNICAttachTarget{Bridge: link.Interface, Mode: lab.ExternalModeDirect}, nil
		}
		return containerNICAttachTarget{Interface: link.Interface, Mode: lab.ExternalModeDirect}, nil
	}
	if link, ok := findNetworkLinkForEndpoint(l, lab.NetworkEndpoint{Type: "container", ID: ct.ID, NIC: index}); ok {
		bridge := l.ManagedNetworkLinkBridgeName(link)
		if err := b.ensureBridge(ctx, bridge); err != nil {
			return containerNICAttachTarget{}, err
		}
		return containerNICAttachTarget{Bridge: bridge}, nil
	}
	return containerNICAttachTarget{}, nil
}
