package hostnet

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/macnat"
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
	MacNAT *macnat.Controller
}

type containerNICAttachTarget struct {
	Bridge    string
	Interface string
	Mode      string
	Address   string
	Gateway   string
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
	for index, nic := range ct.Networks {
		target, err := b.containerNICAttachTarget(ctx, l, ct, index, nic)
		if err != nil {
			return err
		}
		if target.Bridge == "" && target.Interface == "" {
			continue
		}
		hostIf, guestIf := vethNames(l, ct, index)
		containerIf := guestIf
		if target.Interface != "" {
			containerIf = hostIf
			_ = b.Runner.Run(ctx, "ip", "link", "delete", hostIf)
			if err := b.Runner.Run(ctx, "ip", "link", "add", "link", target.Interface, "name", hostIf, "type", "macvlan", "mode", "bridge"); err != nil {
				return err
			}
		} else {
			_ = b.Runner.Run(ctx, "ip", "link", "delete", hostIf)
			if err := b.Runner.Run(ctx, "ip", "link", "add", hostIf, "type", "veth", "peer", "name", guestIf); err != nil {
				return err
			}
			if err := b.Runner.Run(ctx, "ip", "link", "set", hostIf, "master", target.Bridge); err != nil {
				return err
			}
			if err := b.Runner.Run(ctx, "ip", "link", "set", hostIf, "up"); err != nil {
				return err
			}
		}
		if err := b.Runner.Run(ctx, "ip", "link", "set", containerIf, "netns", fmt.Sprintf("%d", pid)); err != nil {
			return err
		}
		if nic.MAC != "" {
			if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", containerIf, "address", nic.MAC); err != nil {
				return err
			}
		} else if target.Mode == lab.ExternalModeMacNAT {
			if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", containerIf, "address", l.GeneratedNICMAC("container", ct.ID, index)); err != nil {
				return err
			}
		}
		if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", containerIf, "name", fmt.Sprintf("eth%d", index)); err != nil {
			return err
		}
		if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "link", "set", fmt.Sprintf("eth%d", index), "up"); err != nil {
			return err
		}
		if target.Address != "" {
			guest := fmt.Sprintf("eth%d", index)
			if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "addr", "replace", target.Address, "dev", guest); err != nil {
				return err
			}
			if target.Gateway != "" {
				if err := b.Runner.Run(ctx, "nsenter", "-t", fmt.Sprintf("%d", pid), "-n", "ip", "route", "replace", "default", "via", target.Gateway, "dev", guest); err != nil {
					return err
				}
			}
		}
	}
	if err := b.configureMacNAT(ctx, l); err != nil {
		return err
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
		if err := b.EnsureSwitchBridge(ctx, l, sw); err != nil {
			return containerNICAttachTarget{}, err
		}
		return containerNICAttachTarget{Bridge: l.ManagedSwitchBridgeName(sw)}, nil
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
			return containerNICAttachTarget{
				Bridge:  bridge,
				Mode:    lab.ExternalModeNAT,
				Address: externalNATContainerAddress(l, link, ct, index),
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

func (b *Bridge) ensureBridge(ctx context.Context, name string) error {
	if err := b.Runner.Run(ctx, "ip", "link", "show", name); err == nil {
		return b.Runner.Run(ctx, "ip", "link", "set", name, "up")
	}
	if err := b.Runner.Run(ctx, "ip", "link", "add", name, "type", "bridge"); err != nil {
		return err
	}
	return b.Runner.Run(ctx, "ip", "link", "set", name, "up")
}

func (b *Bridge) ensureNATBridge(ctx context.Context, bridge, gateway, cidr, uplink string) error {
	if err := b.ensureBridge(ctx, bridge); err != nil {
		return err
	}
	if err := b.Runner.Run(ctx, "ip", "addr", "replace", gateway+"/24", "dev", bridge); err != nil {
		return err
	}
	if err := b.Runner.Run(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return err
	}
	args := []string{"-t", "nat", "-C", "POSTROUTING", "-s", cidr}
	if uplink != "" {
		args = append(args, "-o", uplink)
	}
	args = append(args, "-j", "MASQUERADE")
	if err := b.Runner.Run(ctx, "iptables", args...); err == nil {
		return nil
	}
	args[2] = "-A"
	return b.Runner.Run(ctx, "iptables", args...)
}

func (b *Bridge) configureMacNAT(ctx context.Context, l *lab.Lab) error {
	sessions, err := b.macNATSessions(ctx, l)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return nil
	}
	controller := macnat.NewController("")
	if b.MacNAT != nil {
		controller = *b.MacNAT
	}
	return controller.Configure(ctx, sessions)
}

func (b *Bridge) macNATSessions(ctx context.Context, l *lab.Lab) ([]macnat.Session, error) {
	if l == nil {
		return nil, nil
	}
	var sessions []macnat.Session
	for _, link := range l.ExternalLinks {
		if link.Mode != lab.ExternalModeMacNAT {
			continue
		}
		bridge := l.ManagedExternalBridgeName(link)
		if err := b.ensureBridge(ctx, bridge); err != nil {
			return nil, err
		}
		macs := directExternalMACs(l, link.ID)
		if len(macs) == 0 {
			continue
		}
		sessions = append(sessions, macnat.Session{
			LabID:    l.ID,
			SwitchID: "external-" + link.ID,
			Bridge:   bridge,
			Uplink:   link.Interface,
			MACs:     macs,
		})
	}
	for _, sw := range l.Switches {
		link, ok := findExternalLink(l, sw.ExternalLink)
		if !ok {
			continue
		}
		if sw.Mode != "macnat-bridge" && link.Mode != lab.ExternalModeMacNAT {
			continue
		}
		if err := b.EnsureSwitchBridge(ctx, l, sw); err != nil {
			return nil, err
		}
		macs := switchMACs(l, sw.ID)
		if len(macs) == 0 {
			continue
		}
		sessions = append(sessions, macnat.Session{
			LabID:    l.ID,
			SwitchID: sw.ID,
			Bridge:   l.ManagedSwitchBridgeName(sw),
			Uplink:   link.Interface,
			MACs:     macs,
		})
	}
	return sessions, nil
}

func directExternalMACs(l *lab.Lab, externalID string) []string {
	var macs []string
	for _, vm := range l.VMs {
		for index, nic := range vm.Networks {
			if nic.ExternalLink == externalID {
				macs = append(macs, firstNonEmpty(nic.MAC, l.GeneratedNICMAC("vm", vm.ID, index)))
			}
		}
	}
	for _, ct := range l.Containers {
		for index, nic := range ct.Networks {
			if nic.ExternalLink == externalID {
				macs = append(macs, firstNonEmpty(nic.MAC, l.GeneratedNICMAC("container", ct.ID, index)))
			}
		}
	}
	return macs
}

func switchMACs(l *lab.Lab, switchID string) []string {
	var macs []string
	for _, vm := range l.VMs {
		for index, nic := range vm.Networks {
			if nic.Switch == switchID {
				macs = append(macs, firstNonEmpty(nic.MAC, l.GeneratedNICMAC("vm", vm.ID, index)))
			}
		}
	}
	for _, ct := range l.Containers {
		for index, nic := range ct.Networks {
			if nic.Switch == switchID {
				macs = append(macs, firstNonEmpty(nic.MAC, l.GeneratedNICMAC("container", ct.ID, index)))
			}
		}
	}
	return macs
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func externalNATGatewayCIDR(l *lab.Lab, link lab.ExternalLink) (string, string) {
	octet := 1 + int(hash32(l.ID+"|"+link.ID)%250)
	gateway := fmt.Sprintf("10.250.%d.1", octet)
	return gateway, fmt.Sprintf("10.250.%d.0/24", octet)
}

func externalNATContainerAddress(l *lab.Lab, link lab.ExternalLink, ct lab.Container, index int) string {
	octet := 1 + int(hash32(l.ID+"|"+link.ID)%250)
	host := 20 + int(hash32(ct.ID+"|"+fmt.Sprintf("%d", index))%200)
	return fmt.Sprintf("10.250.%d.%d/24", octet, host)
}

func hash32(value string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return h.Sum32()
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

func findExternalLink(l *lab.Lab, id string) (lab.ExternalLink, bool) {
	if l == nil {
		return lab.ExternalLink{}, false
	}
	for _, link := range l.ExternalLinks {
		if link.ID == id {
			return link, true
		}
	}
	return lab.ExternalLink{}, false
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

func vmExternalUsesManagedBridge(l *lab.Lab, nic lab.VMNetwork) bool {
	if nic.ExternalLink == "" {
		return false
	}
	link, ok := findExternalLink(l, nic.ExternalLink)
	if !ok {
		return false
	}
	return link.Mode == lab.ExternalModeNAT || link.Mode == lab.ExternalModeMacNAT
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

func isLinuxBridge(name string) bool {
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "br") || strings.HasPrefix(lower, "virbr") || lower == "docker0" {
		return true
	}
	_, err := os.Stat(filepath.Join("/sys/class/net", name, "bridge"))
	return err == nil
}
