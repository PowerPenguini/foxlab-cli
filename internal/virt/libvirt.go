package virt

import (
	"context"
	"fmt"
	"strings"

	libvirt "github.com/libvirt/libvirt-go"

	"foxlab-cli/internal/hostnet"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

const DefaultURI = "qemu:///system"

type LibvirtRuntime struct {
	conn   *libvirt.Connect
	Bridge *hostnet.Bridge
}

func NewLibvirtRuntime(uri string) (*LibvirtRuntime, error) {
	if uri == "" {
		uri = DefaultURI
	}
	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		return nil, err
	}
	return &LibvirtRuntime{conn: conn, Bridge: hostnet.NewBridge()}, nil
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

func (r *LibvirtRuntime) States(ctx context.Context, l *lab.Lab) (map[string]string, error) {
	vmStates, err := r.VMStates(ctx, l)
	if err != nil {
		return nil, err
	}
	states := map[string]string{}
	for id, state := range vmStates {
		states[workload.Key(workload.Ref{Type: workload.TypeVM, ID: id})] = state
	}
	return states, nil
}

func (r *LibvirtRuntime) Start(ctx context.Context, l *lab.Lab, ref workload.Ref) error {
	if ref.Type != workload.TypeVM {
		return fmt.Errorf("libvirt cannot start workload type %q", ref.Type)
	}
	return r.StartVM(ctx, l, ref.ID)
}

func (r *LibvirtRuntime) Stop(ctx context.Context, l *lab.Lab, ref workload.Ref) error {
	if ref.Type != workload.TypeVM {
		return fmt.Errorf("libvirt cannot stop workload type %q", ref.Type)
	}
	return r.StopVM(ctx, l, ref.ID)
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
			if err := r.attachVMNICs(ctx, l, vm); err != nil {
				return err
			}
			defined, defineErr := r.defineVM(l, vm)
			if defineErr != nil {
				r.detachVMNICs(ctx, l, vm)
				return defineErr
			}
			dom = defined
			defer dom.Free()
		} else {
			return err
		}
	} else {
		state, _, stateErr := dom.GetState()
		_ = dom.Free()
		if stateErr == nil && state == libvirt.DOMAIN_RUNNING {
			return nil
		}
		if err := r.attachVMNICs(ctx, l, vm); err != nil {
			return err
		}
		defined, defineErr := r.defineVM(l, vm)
		if defineErr != nil {
			r.detachVMNICs(ctx, l, vm)
			return defineErr
		}
		dom = defined
		defer dom.Free()
	}
	state, _, err := dom.GetState()
	if err == nil && state == libvirt.DOMAIN_RUNNING {
		return nil
	}
	if err := dom.Create(); err != nil {
		r.detachVMNICs(ctx, l, vm)
		return fmt.Errorf("start domain %q: %w", id, err)
	}
	return nil
}

func (r *LibvirtRuntime) defineVM(l *lab.Lab, vm lab.VM) (*libvirt.Domain, error) {
	for _, nic := range vm.Networks {
		if nic.Switch == "" {
			continue
		}
		_, ok := findSwitch(l, nic.Switch)
		if !ok {
			return nil, fmt.Errorf("vm %q references missing switch %q", vm.ID, nic.Switch)
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
		r.detachVMNICs(ctx, l, vm)
		return nil
	}
	if err := dom.Shutdown(); err != nil && !isNotFound(err) {
		return fmt.Errorf("stop domain %q: %w", id, err)
	}
	r.detachVMNICs(ctx, l, vm)
	return nil
}

func (r *LibvirtRuntime) attachVMNICs(ctx context.Context, l *lab.Lab, vm lab.VM) error {
	if r.Bridge == nil {
		r.Bridge = hostnet.NewBridge()
	}
	return r.Bridge.AttachVMNICs(ctx, l, vm)
}

func (r *LibvirtRuntime) detachVMNICs(ctx context.Context, l *lab.Lab, vm lab.VM) {
	if r.Bridge == nil {
		r.Bridge = hostnet.NewBridge()
	}
	r.Bridge.DetachVMNICs(ctx, l, vm)
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
