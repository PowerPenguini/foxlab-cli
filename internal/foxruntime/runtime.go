package foxruntime

import (
	"context"
	"fmt"
	"strings"

	containerdruntime "foxlab-cli/internal/containerd"
	"foxlab-cli/internal/hostnet"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/virt"
	"foxlab-cli/internal/workload"
)

func New(libvirtURI, containerdAddress string, l *lab.Lab) (*workload.Composite, error) {
	var vmRuntime workload.Runtime
	if l != nil && len(l.VMs) > 0 {
		runtime, err := virt.NewLibvirtRuntime(libvirtURI)
		if err != nil {
			vmRuntime = failingRuntime{kind: "libvirt", err: err}
		} else {
			vmRuntime = runtime
		}
	}
	var containerRuntime workload.Runtime
	if l != nil && len(l.Containers) > 0 {
		containerRuntime = containerdruntime.NewRuntime(firstNonEmpty(containerdAddress, ContainerdAddressFromLab(l)))
	}
	return &workload.Composite{
		VM:        vmRuntime,
		Container: containerRuntime,
	}, nil
}

func DestroyLab(ctx context.Context, libvirtURI, containerdAddress string, l *lab.Lab) error {
	runtime, err := New(libvirtURI, containerdAddress, l)
	if err != nil {
		return err
	}
	defer runtime.Close()
	if err := workload.DestroyLab(ctx, runtime, l); err != nil {
		return err
	}
	return hostnet.NewBridge().CleanupLab(ctx, l)
}

type failingRuntime struct {
	kind string
	err  error
}

func (r failingRuntime) States(context.Context, *lab.Lab) (map[string]string, error) {
	return nil, r.wrap("states")
}

func (r failingRuntime) Start(context.Context, *lab.Lab, workload.Ref) error {
	return r.wrap("start")
}

func (r failingRuntime) Stop(context.Context, *lab.Lab, workload.Ref) error {
	return r.wrap("stop")
}

func (r failingRuntime) Destroy(context.Context, *lab.Lab, workload.Ref) error {
	return r.wrap("destroy")
}

func (r failingRuntime) VNCPorts(context.Context, *lab.Lab) (map[string]int, error) {
	return nil, r.wrap("vnc ports")
}

func (r failingRuntime) Close() error { return nil }

func (r failingRuntime) wrap(action string) error {
	if r.err == nil {
		return fmt.Errorf("%s %s unavailable", r.kind, action)
	}
	return fmt.Errorf("%s %s unavailable: %w", r.kind, action, r.err)
}

func ContainerdAddressFromLab(l *lab.Lab) string {
	if l == nil || l.Meta == nil {
		return ""
	}
	return l.Meta["containerd.address"]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
