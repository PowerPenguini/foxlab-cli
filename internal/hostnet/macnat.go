package hostnet

import (
	"context"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/macnat"
)

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
