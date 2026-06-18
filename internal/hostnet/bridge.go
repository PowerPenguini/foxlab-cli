package hostnet

import (
	"context"
	"fmt"
	"os/exec"

	"foxlab-cli/internal/lab"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) error
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, string(output))
	}
	return nil
}

type Bridge struct {
	Runner CommandRunner
}

func NewBridge() *Bridge {
	return &Bridge{Runner: ExecRunner{}}
}

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
	return nil
}

func (b *Bridge) DetachVMNICs(ctx context.Context, l *lab.Lab, vm lab.VM) {
	if b.Runner == nil {
		b.Runner = ExecRunner{}
	}
	for index, nic := range vm.Networks {
		if nic.Switch == "" && !endpointHasNetworkLink(l, lab.NetworkEndpoint{Type: "vm", ID: vm.ID, NIC: index}) {
			continue
		}
		_ = b.Runner.Run(ctx, "ip", "link", "delete", VMTapName(l, vm, index))
	}
}

func (b *Bridge) AttachContainer(ctx context.Context, l *lab.Lab, ct lab.Container, pid int) error {
	if b.Runner == nil {
		b.Runner = ExecRunner{}
	}
	for index, nic := range ct.Networks {
		bridge, err := b.containerNICBridge(ctx, l, ct, index, nic)
		if err != nil {
			return err
		}
		if bridge == "" {
			continue
		}
		hostIf, guestIf := vethNames(l, ct, index)
		_ = b.Runner.Run(ctx, "ip", "link", "delete", hostIf)
		if err := b.Runner.Run(ctx, "ip", "link", "add", hostIf, "type", "veth", "peer", "name", guestIf); err != nil {
			return err
		}
		if err := b.Runner.Run(ctx, "ip", "link", "set", hostIf, "master", bridge); err != nil {
			return err
		}
		if err := b.Runner.Run(ctx, "ip", "link", "set", hostIf, "up"); err != nil {
			return err
		}
		if err := b.Runner.Run(ctx, "ip", "link", "set", guestIf, "netns", fmt.Sprintf("%d", pid)); err != nil {
			return err
		}
		if nic.MAC != "" {
			if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", guestIf, "address", nic.MAC); err != nil {
				return err
			}
		}
		if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", guestIf, "name", fmt.Sprintf("eth%d", index)); err != nil {
			return err
		}
		if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", fmt.Sprintf("eth%d", index), "up"); err != nil {
			return err
		}
	}
	return nil
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

func (b *Bridge) EnsureSwitchBridge(ctx context.Context, l *lab.Lab, sw lab.Switch) error {
	if b.Runner == nil {
		b.Runner = ExecRunner{}
	}
	return b.ensureBridge(ctx, l.ManagedSwitchBridgeName(sw))
}

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
	return "", nil
}

func (b *Bridge) containerNICBridge(ctx context.Context, l *lab.Lab, ct lab.Container, index int, nic lab.ContainerNetwork) (string, error) {
	if nic.Switch != "" {
		sw, ok := findSwitch(l, nic.Switch)
		if !ok {
			return "", fmt.Errorf("container %q references missing switch %q", ct.ID, nic.Switch)
		}
		if err := b.EnsureSwitchBridge(ctx, l, sw); err != nil {
			return "", err
		}
		return l.ManagedSwitchBridgeName(sw), nil
	}
	if link, ok := findNetworkLinkForEndpoint(l, lab.NetworkEndpoint{Type: "container", ID: ct.ID, NIC: index}); ok {
		bridge := l.ManagedNetworkLinkBridgeName(link)
		if err := b.ensureBridge(ctx, bridge); err != nil {
			return "", err
		}
		return bridge, nil
	}
	return "", nil
}

func (b *Bridge) ensureBridge(ctx context.Context, name string) error {
	if err := b.Runner.Run(ctx, "ip", "link", "show", name); err == nil {
		return b.Runner.Run(ctx, "ip", "link", "set", name, "up")
	}
	if err := b.Runner.Run(ctx, "ip", "link", "add", name, "type", "bridge"); err != nil {
		return err
	}
	return b.Runner.Run(ctx, "ip", "link", "set", name, "up")
}

func findSwitch(l *lab.Lab, id string) (lab.Switch, bool) {
	if l == nil {
		return lab.Switch{}, false
	}
	for _, sw := range l.Switches {
		if sw.ID == id {
			return sw, true
		}
	}
	return lab.Switch{}, false
}

func findNetworkLinkForEndpoint(l *lab.Lab, endpoint lab.NetworkEndpoint) (lab.NetworkLink, bool) {
	if l == nil {
		return lab.NetworkLink{}, false
	}
	for _, link := range l.NetworkLinks {
		if sameNetworkEndpoint(link.From, endpoint) || sameNetworkEndpoint(link.To, endpoint) {
			return link, true
		}
	}
	return lab.NetworkLink{}, false
}

func endpointHasNetworkLink(l *lab.Lab, endpoint lab.NetworkEndpoint) bool {
	_, ok := findNetworkLinkForEndpoint(l, endpoint)
	return ok
}

func sameNetworkEndpoint(a, b lab.NetworkEndpoint) bool {
	return a.Type == b.Type && a.ID == b.ID && a.NIC == b.NIC
}

func VMTapName(l *lab.Lab, vm lab.VM, index int) string {
	return ifName("t", l.ManagedDomainName(vm), index)
}

func vethNames(l *lab.Lab, ct lab.Container, index int) (string, string) {
	base := cleanPrefix(l.ManagedContainerName(ct))
	return ifName("v", base, index), ifName("p", base, index)
}

func ifName(prefix, base string, index int) string {
	const maxLinuxIfName = 15
	suffix := fmt.Sprintf("%d", index)
	maxBase := maxLinuxIfName - len(prefix) - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	base = cleanPrefix(base)
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return prefix + base + suffix
}

func cleanPrefix(base string) string {
	clean := ""
	for _, ch := range base {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			clean += string(ch)
		}
	}
	if clean == "" {
		return "foxlab"
	}
	return clean
}
