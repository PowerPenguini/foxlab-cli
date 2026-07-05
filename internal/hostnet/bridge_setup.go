package hostnet

import (
	"context"
	"fmt"
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

func (b *Bridge) ensureNATSwitchBridge(ctx context.Context, l *lab.Lab, sw lab.Switch) error {
	bridge := l.ManagedSwitchBridgeName(sw)
	gateway, cidr := switchNATGatewayCIDR(l, sw)
	linkIDs := lab.SwitchExternalLinks(sw)
	if len(linkIDs) == 0 {
		return b.ensureNATBridge(ctx, bridge, gateway, cidr, "")
	}
	for _, linkID := range linkIDs {
		link, ok := findExternalLink(l, linkID)
		if !ok {
			return fmt.Errorf("switch %q references missing external link %q", sw.ID, linkID)
		}
		if link.Mode == lab.ExternalModeMacNAT {
			continue
		}
		if err := b.ensureNATBridge(ctx, bridge, gateway, cidr, link.Interface); err != nil {
			return err
		}
	}
	return nil
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
	forwardArgs := []string{"FORWARD", "-i", bridge}
	if uplink != "" {
		forwardArgs = append(forwardArgs, "-o", uplink)
	}
	forwardArgs = append(forwardArgs, "-j", "ACCEPT")
	if err := b.ensureIPTablesRule(ctx, "", forwardArgs); err != nil {
		return err
	}
	returnArgs := []string{"FORWARD"}
	if uplink != "" {
		returnArgs = append(returnArgs, "-i", uplink)
	}
	returnArgs = append(returnArgs, "-o", bridge, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")
	if err := b.ensureIPTablesRule(ctx, "", returnArgs); err != nil {
		return err
	}
	natArgs := []string{"POSTROUTING", "-s", cidr}
	if uplink != "" {
		natArgs = append(natArgs, "-o", uplink)
	}
	natArgs = append(natArgs, "-j", "MASQUERADE")
	return b.ensureIPTablesRule(ctx, "nat", natArgs)
}

func (b *Bridge) ensureIPTablesRule(ctx context.Context, table string, rule []string) error {
	checkArgs := []string{}
	if table != "" {
		checkArgs = append(checkArgs, "-t", table)
	}
	checkArgs = append(checkArgs, "-C")
	checkArgs = append(checkArgs, rule...)
	if err := b.Runner.Run(ctx, "iptables", checkArgs...); err == nil {
		return nil
	}
	addArgs := []string{}
	if table != "" {
		addArgs = append(addArgs, "-t", table)
	}
	addArgs = append(addArgs, "-A")
	addArgs = append(addArgs, rule...)
	return b.Runner.Run(ctx, "iptables", addArgs...)
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
