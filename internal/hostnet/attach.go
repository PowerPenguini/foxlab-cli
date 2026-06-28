package hostnet

import (
	"context"
	"fmt"

	"foxlab-cli/internal/lab"
)

func (b *Bridge) AttachVMNICs(ctx context.Context, l *lab.Lab, vm lab.VM) error {
	if b.Runner == nil {
		b.Runner = ExecRunner{}
	}
	for index, nic := range vm.Networks {
		bridge, err := b.vmNICBridge(ctx, l, vm, index, nic)
		if err != nil {
			return err
		}
		if bridge == "" {
			continue
		}
		_ = b.Runner.Run(ctx, "ip", "link", "delete", VMTapName(l, vm, index))
	}
	return b.configureMacNAT(ctx, l)
}

func (b *Bridge) DetachVMNICs(ctx context.Context, l *lab.Lab, vm lab.VM) {
	if b.Runner == nil {
		b.Runner = ExecRunner{}
	}
	for index, nic := range vm.Networks {
		if nic.Switch == "" && !endpointHasNetworkLink(l, lab.NetworkEndpoint{Type: "vm", ID: vm.ID, NIC: index}) && !vmExternalUsesManagedBridge(l, nic) {
			continue
		}
		_ = b.Runner.Run(ctx, "ip", "link", "delete", VMTapName(l, vm, index))
	}
}

func (b *Bridge) AttachContainer(ctx context.Context, l *lab.Lab, ct lab.Container, pid int) error {
	if b.Runner == nil {
		b.Runner = ExecRunner{}
	}
	var createdCleanups []func()
	cleanupAfterFailure := func() {
		for i := len(createdCleanups) - 1; i >= 0; i-- {
			createdCleanups[i]()
		}
	}
	fail := func(err error) error {
		cleanupAfterFailure()
		return err
	}
	for index, nic := range ct.Networks {
		target, err := b.containerNICAttachTarget(ctx, l, ct, index, nic)
		if err != nil {
			return fail(err)
		}
		if target.Bridge == "" && target.Interface == "" {
			continue
		}
		hostIf, guestIf := vethNames(l, ct, index)
		b.cleanupContainerNIC(ctx, pid, index, hostIf, guestIf)
		containerIf := guestIf
		markCreated := func() {
			createdCleanups = append(createdCleanups, func() {
				b.cleanupContainerNIC(ctx, pid, index, hostIf, guestIf)
			})
		}
		if target.Interface != "" {
			containerIf = hostIf
			if err := b.Runner.Run(ctx, "ip", "link", "add", "link", target.Interface, "name", hostIf, "type", "macvlan", "mode", "bridge"); err != nil {
				return fail(err)
			}
			markCreated()
		} else {
			if err := b.Runner.Run(ctx, "ip", "link", "add", hostIf, "type", "veth", "peer", "name", guestIf); err != nil {
				return fail(err)
			}
			markCreated()
			if err := b.Runner.Run(ctx, "ip", "link", "set", hostIf, "master", target.Bridge); err != nil {
				return fail(err)
			}
			if err := b.Runner.Run(ctx, "ip", "link", "set", hostIf, "up"); err != nil {
				return fail(err)
			}
		}
		if err := b.Runner.Run(ctx, "ip", "link", "set", containerIf, "netns", fmt.Sprintf("%d", pid)); err != nil {
			return fail(err)
		}
		if nic.MAC != "" {
			if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", containerIf, "address", nic.MAC); err != nil {
				return fail(err)
			}
		} else if target.Mode == lab.ExternalModeMacNAT {
			if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", containerIf, "address", l.GeneratedNICMAC("container", ct.ID, index)); err != nil {
				return fail(err)
			}
		}
		if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", containerIf, "name", fmt.Sprintf("eth%d", index)); err != nil {
			return fail(err)
		}
		if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", fmt.Sprintf("eth%d", index), "up"); err != nil {
			return fail(err)
		}
		if target.Address != "" {
			guest := fmt.Sprintf("eth%d", index)
			if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "addr", "replace", target.Address, "dev", guest); err != nil {
				return fail(err)
			}
			if target.Gateway != "" {
				if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "route", "replace", "default", "via", target.Gateway, "dev", guest); err != nil {
					return fail(err)
				}
			}
		}
	}
	if err := b.configureMacNAT(ctx, l); err != nil {
		return fail(err)
	}
	return nil
}

func (b *Bridge) cleanupContainerNIC(ctx context.Context, pid, index int, hostIf, guestIf string) {
	target := fmt.Sprintf("%d", pid)
	for _, name := range []string{fmt.Sprintf("eth%d", index), guestIf, hostIf} {
		_ = b.Runner.Run(ctx, "nsenter", "-t", target, "-n", "ip", "link", "delete", name)
	}
	_ = b.Runner.Run(ctx, "ip", "link", "delete", hostIf)
}

func (b *Bridge) DetachContainer(ctx context.Context, l *lab.Lab, ct lab.Container) {
	if b.Runner == nil {
		b.Runner = ExecRunner{}
	}
	for index := range ct.Networks {
		hostIf, _ := vethNames(l, ct, index)
		_ = b.Runner.Run(ctx, "ip", "link", "delete", hostIf)
	}
}
