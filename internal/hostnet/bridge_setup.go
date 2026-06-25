package hostnet

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"foxlab-cli/internal/lab"
)

func (b *Bridge) EnsureSwitchBridge(ctx context.Context, l *lab.Lab, sw lab.Switch) error {
	if b.Runner == nil {
		b.Runner = ExecRunner{}
	}
	return b.ensureBridge(ctx, l.ManagedSwitchBridgeName(sw))
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
