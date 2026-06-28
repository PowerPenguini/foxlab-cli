package workload

import (
	"context"
	"errors"
	"fmt"

	"foxlab-cli/internal/lab"
)

type Composite struct {
	VM        Runtime
	Container Runtime
}

func (c *Composite) States(ctx context.Context, l *lab.Lab) (map[string]string, error) {
	out := map[string]string{}
	var errs []error
	if l == nil {
		return out, nil
	}
	if len(l.VMs) > 0 && c.VM == nil {
		errs = append(errs, runtimeNotConfiguredError(TypeVM))
	}
	if c.VM != nil && len(l.VMs) > 0 {
		states, err := c.VM.States(ctx, l)
		if err != nil {
			errs = append(errs, err)
		}
		for key, state := range states {
			out[key] = state
		}
	}
	if len(l.Containers) > 0 && c.Container == nil {
		errs = append(errs, runtimeNotConfiguredError(TypeContainer))
	}
	if c.Container != nil && len(l.Containers) > 0 {
		states, err := c.Container.States(ctx, l)
		if err != nil {
			errs = append(errs, err)
		}
		for key, state := range states {
			out[key] = state
		}
	}
	return out, errors.Join(errs...)
}

func (c *Composite) VNCPorts(ctx context.Context, l *lab.Lab) (map[string]int, error) {
	if c.VM == nil || l == nil || len(l.VMs) == 0 {
		return map[string]int{}, nil
	}
	runtime, ok := c.VM.(VNCRuntime)
	if !ok {
		return map[string]int{}, nil
	}
	return runtime.VNCPorts(ctx, l)
}

func (c *Composite) Start(ctx context.Context, l *lab.Lab, ref Ref) error {
	runtime, err := c.runtimeFor(ref)
	if err != nil {
		return err
	}
	return runtime.Start(ctx, l, ref)
}

func (c *Composite) Stop(ctx context.Context, l *lab.Lab, ref Ref) error {
	runtime, err := c.runtimeFor(ref)
	if err != nil {
		return err
	}
	return runtime.Stop(ctx, l, ref)
}

func (c *Composite) Destroy(ctx context.Context, l *lab.Lab, ref Ref) error {
	runtime, err := c.runtimeFor(ref)
	if err != nil {
		return err
	}
	if destroyer, ok := runtime.(Destroyer); ok {
		return destroyer.Destroy(ctx, l, ref)
	}
	return runtime.Stop(ctx, l, ref)
}

func (c *Composite) Close() error {
	var errs []error
	for _, runtime := range []Runtime{c.VM, c.Container} {
		if runtime == nil {
			continue
		}
		if err := runtime.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c *Composite) runtimeFor(ref Ref) (Runtime, error) {
	switch ref.Type {
	case TypeVM:
		if c.VM != nil {
			return c.VM, nil
		}
	case TypeContainer:
		if c.Container != nil {
			return c.Container, nil
		}
	}
	return nil, runtimeNotConfiguredError(ref.Type)
}

func runtimeNotConfiguredError(typ string) error {
	return fmt.Errorf("runtime not configured for workload type %q", typ)
}
