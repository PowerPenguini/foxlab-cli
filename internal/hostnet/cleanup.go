package hostnet

import (
	"context"

	"foxlab-cli/internal/lab"
)

func (b *Bridge) CleanupLab(ctx context.Context, l *lab.Lab) error {
	if b.Runner == nil {
		b.Runner = ExecRunner{}
	}
	if l == nil {
		return nil
	}
	for _, link := range l.ExternalLinks {
		if link.Mode == "nat" {
			gateway, cidr := externalNATGatewayCIDR(l, link)
			b.deleteNATRules(ctx, l.ManagedExternalBridgeName(link), gateway, cidr, link.Interface)
		}
	}
	for _, name := range managedBridgeNames(l) {
		_ = b.Runner.Run(ctx, "ip", "link", "delete", name)
	}
	return nil
}

func (b *Bridge) deleteNATRules(ctx context.Context, bridge, _, cidr, uplink string) {
	forwardArgs := []string{"FORWARD", "-i", bridge}
	if uplink != "" {
		forwardArgs = append(forwardArgs, "-o", uplink)
	}
	forwardArgs = append(forwardArgs, "-j", "ACCEPT")
	b.deleteIPTablesRule(ctx, "", forwardArgs)

	returnArgs := []string{"FORWARD"}
	if uplink != "" {
		returnArgs = append(returnArgs, "-i", uplink)
	}
	returnArgs = append(returnArgs, "-o", bridge, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")
	b.deleteIPTablesRule(ctx, "", returnArgs)

	natArgs := []string{"POSTROUTING", "-s", cidr}
	if uplink != "" {
		natArgs = append(natArgs, "-o", uplink)
	}
	natArgs = append(natArgs, "-j", "MASQUERADE")
	b.deleteIPTablesRule(ctx, "nat", natArgs)
}

func (b *Bridge) deleteIPTablesRule(ctx context.Context, table string, rule []string) {
	args := []string{}
	if table != "" {
		args = append(args, "-t", table)
	}
	args = append(args, "-D")
	args = append(args, rule...)
	_ = b.Runner.Run(ctx, "iptables", args...)
}

func managedBridgeNames(l *lab.Lab) []string {
	seen := map[string]struct{}{}
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, sw := range l.Switches {
		add(l.ManagedSwitchBridgeName(sw))
	}
	for _, link := range l.ExternalLinks {
		add(l.ManagedExternalBridgeName(link))
	}
	for _, link := range l.NetworkLinks {
		add(l.ManagedNetworkLinkBridgeName(link))
	}
	return names
}
