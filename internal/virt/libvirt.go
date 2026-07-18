package virt

import (
	"context"
	"errors"
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

func (r *LibvirtRuntime) VNCPorts(ctx context.Context, l *lab.Lab) (map[string]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ports := map[string]int{}
	for _, vm := range l.VMs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !vm.VNC {
			continue
		}
		dom, err := r.conn.LookupDomainByName(l.ManagedDomainName(vm))
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return nil, err
		}
		xmlText, xmlErr := dom.GetXMLDesc(0)
		_ = dom.Free()
		if xmlErr != nil {
			return nil, fmt.Errorf("read domain XML %q: %w", vm.ID, xmlErr)
		}
		if port := parseVNCPort(xmlText); port > 0 {
			ports[workload.Key(workload.Ref{Type: workload.TypeVM, ID: vm.ID})] = port
		}
	}
	return ports, nil
}

func (r *LibvirtRuntime) Start(ctx context.Context, l *lab.Lab, ref workload.Ref) error {
	_, err := r.StartWithOutcome(ctx, l, ref)
	return err
}

func (r *LibvirtRuntime) StartWithOutcome(ctx context.Context, l *lab.Lab, ref workload.Ref) (workload.StartOutcome, error) {
	if ref.Type != workload.TypeVM {
		return workload.StartOutcome{}, fmt.Errorf("libvirt cannot start workload type %q", ref.Type)
	}
	return r.startVM(ctx, l, ref.ID)
}

func (r *LibvirtRuntime) Stop(ctx context.Context, l *lab.Lab, ref workload.Ref) error {
	if ref.Type != workload.TypeVM {
		return fmt.Errorf("libvirt cannot stop workload type %q", ref.Type)
	}
	return r.StopVM(ctx, l, ref.ID)
}

func (r *LibvirtRuntime) Destroy(ctx context.Context, l *lab.Lab, ref workload.Ref) error {
	if ref.Type != workload.TypeVM {
		return fmt.Errorf("libvirt cannot destroy workload type %q", ref.Type)
	}
	return r.StopVM(ctx, l, ref.ID)
}

func (r *LibvirtRuntime) StartVM(ctx context.Context, l *lab.Lab, id string) error {
	_, err := r.startVM(ctx, l, id)
	return err
}

func (r *LibvirtRuntime) startVM(ctx context.Context, l *lab.Lab, id string) (workload.StartOutcome, error) {
	if err := ctx.Err(); err != nil {
		return workload.StartOutcome{}, err
	}
	vm, ok := labVM(l, id)
	if !ok {
		return workload.StartOutcome{}, fmt.Errorf("vm not found: %s", id)
	}
	dom, err := r.conn.LookupDomainByName(l.ManagedDomainName(vm))
	if err != nil {
		if isNotFound(err) {
			if err := r.attachVMNICs(ctx, l, vm); err != nil {
				return workload.StartOutcome{}, err
			}
			defined, defineErr := r.defineVM(l, vm)
			if defineErr != nil {
				r.detachVMNICs(ctx, l, vm)
				return workload.StartOutcome{}, defineErr
			}
			dom = defined
			defer dom.Free()
		} else {
			return workload.StartOutcome{}, err
		}
	} else {
		state, _, stateErr := dom.GetState()
		if stateErr != nil {
			_ = dom.Free()
			return workload.StartOutcome{}, stateErr
		}
		xmlText, xmlErr := dom.GetXMLDesc(0)
		if xmlErr != nil {
			_ = dom.Free()
			return workload.StartOutcome{}, fmt.Errorf("read domain XML %q: %w", id, xmlErr)
		}
		matches, matchErr := domainConfigMatches(l, vm, xmlText)
		if matchErr != nil {
			_ = dom.Free()
			return workload.StartOutcome{}, fmt.Errorf("compare domain configuration %q: %w", id, matchErr)
		}
		if state == libvirt.DOMAIN_RUNNING && matches {
			_ = dom.Free()
			return workload.StartOutcome{}, nil
		}
		outcome := workload.StartOutcome{}
		active, activeErr := dom.IsActive()
		if activeErr != nil {
			_ = dom.Free()
			return workload.StartOutcome{}, activeErr
		}
		if active {
			if err := dom.Destroy(); err != nil && !isNotFound(err) && !isDomainNotRunning(err) {
				_ = dom.Free()
				return workload.StartOutcome{}, fmt.Errorf("restart domain %q: %w", id, err)
			}
			r.detachVMNICs(ctx, l, vm)
			if state == libvirt.DOMAIN_RUNNING && !matches {
				outcome.Action = "restarted " + workload.Key(workload.Ref{Type: workload.TypeVM, ID: id}) + " for configuration change"
			} else {
				outcome.Action = "restarted " + workload.Key(workload.Ref{Type: workload.TypeVM, ID: id}) + " from " + domainStateName(state)
			}
		}
		defined, defineErr := r.redefineInactiveVM(l, vm, dom)
		_ = dom.Free()
		if defineErr != nil {
			return workload.StartOutcome{}, defineErr
		}
		dom = defined
		defer dom.Free()
		if err := r.attachVMNICs(ctx, l, vm); err != nil {
			return workload.StartOutcome{}, err
		}
		state, _, err = dom.GetState()
		if err == nil && state == libvirt.DOMAIN_RUNNING {
			return outcome, nil
		}
		if err := dom.Create(); err != nil {
			r.detachVMNICs(ctx, l, vm)
			return workload.StartOutcome{}, fmt.Errorf("start domain %q: %w", id, err)
		}
		return outcome, nil
	}
	state, _, err := dom.GetState()
	if err == nil && state == libvirt.DOMAIN_RUNNING {
		return workload.StartOutcome{}, nil
	}
	if err := dom.Create(); err != nil {
		r.detachVMNICs(ctx, l, vm)
		return workload.StartOutcome{}, fmt.Errorf("start domain %q: %w", id, err)
	}
	return workload.StartOutcome{}, nil
}

func (r *LibvirtRuntime) CleanupOrphans(ctx context.Context, l *lab.Lab) ([]string, error) {
	if l == nil {
		return nil, nil
	}
	domains, err := r.conn.ListAllDomains(0)
	if err != nil {
		return nil, err
	}
	desired := map[string]bool{}
	for _, vm := range l.VMs {
		desired[vm.ID] = true
	}
	var actions []string
	var errs []error
	for i := range domains {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		dom := &domains[i]
		xmlText, xmlErr := dom.GetXMLDesc(0)
		if xmlErr != nil {
			errs = append(errs, fmt.Errorf("read domain XML for orphan cleanup: %w", xmlErr))
			_ = dom.Free()
			continue
		}
		id, orphan := orphanManagedDomainID(l, desired, xmlText)
		if !orphan {
			_ = dom.Free()
			continue
		}
		active, activeErr := dom.IsActive()
		if activeErr != nil {
			errs = append(errs, fmt.Errorf("check orphan domain %s: %w", id, activeErr))
			_ = dom.Free()
			continue
		}
		if active {
			if destroyErr := dom.Destroy(); destroyErr != nil && !isNotFound(destroyErr) && !isDomainNotRunning(destroyErr) {
				errs = append(errs, fmt.Errorf("destroy orphan domain %s: %w", id, destroyErr))
				_ = dom.Free()
				continue
			}
		}
		if undefineErr := dom.Undefine(); undefineErr != nil && !isNotFound(undefineErr) {
			errs = append(errs, fmt.Errorf("undefine orphan domain %s: %w", id, undefineErr))
			_ = dom.Free()
			continue
		}
		actions = append(actions, "deleted orphan vm:"+id)
		_ = dom.Free()
	}
	return actions, errors.Join(errs...)
}

func orphanManagedDomainID(l *lab.Lab, desired map[string]bool, xmlText string) (string, bool) {
	if l == nil {
		return "", false
	}
	labID, id, _, managed := managedDomainMetadata(xmlText)
	return id, managed && labID == l.ID && !desired[id]
}

func (r *LibvirtRuntime) redefineInactiveVM(l *lab.Lab, vm lab.VM, existing *libvirt.Domain) (*libvirt.Domain, error) {
	if existing == nil {
		return nil, fmt.Errorf("redefine domain %q: missing existing domain", vm.ID)
	}
	uuid, err := existing.GetUUIDString()
	if err != nil {
		return nil, fmt.Errorf("read domain UUID %q: %w", vm.ID, err)
	}
	xmlText, err := domainXMLWithUUID(l, vm, uuid)
	if err != nil {
		return nil, err
	}
	defined, err := r.conn.DomainDefineXML(xmlText)
	if err != nil {
		return nil, fmt.Errorf("define domain %q: %w", vm.ID, err)
	}
	return defined, nil
}

func (r *LibvirtRuntime) defineVM(l *lab.Lab, vm lab.VM) (*libvirt.Domain, error) {
	for _, nic := range vm.Networks {
		if nic.Switch == "" {
			continue
		}
		_, ok := lab.FindSwitch(l, nic.Switch)
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
			return r.stopMissingVM(ctx, l, vm)
		}
		return err
	}
	defer dom.Free()
	state, _, err := dom.GetState()
	if err == nil && state == libvirt.DOMAIN_SHUTOFF {
		r.detachVMNICs(ctx, l, vm)
		return r.undefineVM(vm, dom)
	}
	if err := dom.Destroy(); err != nil && !isNotFound(err) && !isDomainNotRunning(err) {
		return fmt.Errorf("destroy domain %q: %w", id, err)
	}
	r.detachVMNICs(ctx, l, vm)
	return r.undefineVM(vm, dom)
}

func (r *LibvirtRuntime) stopMissingVM(ctx context.Context, l *lab.Lab, vm lab.VM) error {
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

func (r *LibvirtRuntime) undefineVM(vm lab.VM, dom *libvirt.Domain) error {
	if err := dom.Undefine(); err != nil && !isNotFound(err) {
		return fmt.Errorf("undefine domain %q: %w", vm.ID, err)
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

func isDomainNotRunning(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "domain is not running") ||
		strings.Contains(text, "domain not running") ||
		strings.Contains(text, "not active")
}
