package virt

import (
	"context"
	"fmt"
	"strings"

	libvirt "github.com/libvirt/libvirt-go"

	"foxlab-cli/internal/lab"
)

const DefaultURI = "qemu:///system"

type LibvirtRuntime struct {
	conn *libvirt.Connect
}

func NewLibvirtRuntime(uri string) (*LibvirtRuntime, error) {
	if uri == "" {
		uri = DefaultURI
	}
	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		return nil, err
	}
	return &LibvirtRuntime{conn: conn}, nil
}

func (r *LibvirtRuntime) Close() error {
	if r.conn == nil {
		return nil
	}
	_, err := r.conn.Close()
	return err
}

func (r *LibvirtRuntime) VMStates(ctx context.Context, l *lab.Lab) (map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	states := map[string]string{}
	for _, vm := range l.VMs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		states[vm.ID] = "missing"
		dom, err := r.conn.LookupDomainByName(l.ManagedDomainName(vm))
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return nil, err
		}
		state, _, stateErr := dom.GetState()
		_ = dom.Free()
		if stateErr != nil {
			states[vm.ID] = "unknown"
			continue
		}
		states[vm.ID] = domainStateName(state)
	}
	return states, nil
}

func (r *LibvirtRuntime) StartVM(ctx context.Context, l *lab.Lab, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	vm, ok := labVM(l, id)
	if !ok {
		return fmt.Errorf("vm not found: %s", id)
	}
	dom, err := r.conn.LookupDomainByName(l.ManagedDomainName(vm))
	if err != nil {
		if isNotFound(err) {
			defined, defineErr := r.defineVM(l, vm)
			if defineErr != nil {
				return defineErr
			}
			dom = defined
			defer dom.Free()
		} else {
			return err
		}
	} else {
		defer dom.Free()
	}
	state, _, err := dom.GetState()
	if err == nil && state == libvirt.DOMAIN_RUNNING {
		return nil
	}
	if err := dom.Create(); err != nil {
		return fmt.Errorf("start domain %q: %w", id, err)
	}
	return nil
}

func (r *LibvirtRuntime) defineVM(l *lab.Lab, vm lab.VM) (*libvirt.Domain, error) {
	for _, nic := range vm.Networks {
		if nic.Switch == "" {
			continue
		}
		sw, ok := findSwitch(l, nic.Switch)
		if !ok {
			return nil, fmt.Errorf("vm %q references missing switch %q", vm.ID, nic.Switch)
		}
		if err := r.ensureNetwork(l, sw); err != nil {
			return nil, err
		}
	}
	xmlText, err := domainXML(l, vm)
	if err != nil {
		return nil, err
	}
	dom, err := r.conn.DomainDefineXML(xmlText)
	if err != nil {
		return nil, fmt.Errorf("define domain %q: %w", vm.ID, err)
	}
	return dom, nil
}

func (r *LibvirtRuntime) ensureNetwork(l *lab.Lab, sw lab.Switch) error {
	name := l.ManagedNetworkName(sw)
	net, err := r.conn.LookupNetworkByName(name)
	if err == nil {
		defer net.Free()
		active, activeErr := net.IsActive()
		if activeErr != nil {
			return activeErr
		}
		if active {
			return nil
		}
		if startErr := net.Create(); startErr != nil {
			return fmt.Errorf("start network %q: %w", sw.ID, startErr)
		}
		return nil
	}
	if !isNotFound(err) {
		return err
	}
	xmlText, err := networkXML(l, sw)
	if err != nil {
		return err
	}
	net, err = r.conn.NetworkDefineXML(xmlText)
	if err != nil {
		return fmt.Errorf("define network %q: %w", sw.ID, err)
	}
	defer net.Free()
	if err := net.Create(); err != nil {
		_ = net.Undefine()
		return fmt.Errorf("start network %q: %w", sw.ID, err)
	}
	return nil
}

func (r *LibvirtRuntime) StopVM(ctx context.Context, l *lab.Lab, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	vm, ok := labVM(l, id)
	if !ok {
		return fmt.Errorf("vm not found: %s", id)
	}
	dom, err := r.conn.LookupDomainByName(l.ManagedDomainName(vm))
	if err != nil {
		if isNotFound(err) {
			return fmt.Errorf("libvirt domain not found: %s", l.ManagedDomainName(vm))
		}
		return err
	}
	defer dom.Free()
	state, _, err := dom.GetState()
	if err == nil && state == libvirt.DOMAIN_SHUTOFF {
		return nil
	}
	if err := dom.Shutdown(); err != nil && !isNotFound(err) {
		return fmt.Errorf("stop domain %q: %w", id, err)
	}
	return nil
}

func labVM(l *lab.Lab, id string) (lab.VM, bool) {
	if l == nil {
		return lab.VM{}, false
	}
	for _, vm := range l.VMs {
		if vm.ID == id {
			return vm, true
		}
	}
	return lab.VM{}, false
}

func domainStateName(state libvirt.DomainState) string {
	switch state {
	case libvirt.DOMAIN_RUNNING:
		return "running"
	case libvirt.DOMAIN_BLOCKED:
		return "blocked"
	case libvirt.DOMAIN_PAUSED:
		return "paused"
	case libvirt.DOMAIN_SHUTDOWN:
		return "shutdown"
	case libvirt.DOMAIN_SHUTOFF:
		return "shutoff"
	case libvirt.DOMAIN_CRASHED:
		return "crashed"
	case libvirt.DOMAIN_PMSUSPENDED:
		return "suspended"
	default:
		return "unknown"
	}
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not found") ||
		strings.Contains(text, "no domain") ||
		strings.Contains(text, "domain not found")
}
