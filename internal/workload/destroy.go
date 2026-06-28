package workload

import (
	"context"
	"errors"
	"fmt"

	"foxlab-cli/internal/lab"
)

func DestroyLab(ctx context.Context, runtime Runtime, l *lab.Lab) error {
	if runtime == nil || l == nil {
		return nil
	}
	var errs []error
	for _, vm := range l.VMs {
		ref := Ref{Type: TypeVM, ID: vm.ID}
		if err := destroyWorkload(ctx, runtime, l, ref); err != nil {
			errs = append(errs, fmt.Errorf("destroy %s: %w", Key(ref), err))
		}
	}
	for _, ct := range l.Containers {
		ref := Ref{Type: TypeContainer, ID: ct.ID}
		if err := destroyWorkload(ctx, runtime, l, ref); err != nil {
			errs = append(errs, fmt.Errorf("destroy %s: %w", Key(ref), err))
		}
	}
	return errors.Join(errs...)
}

func destroyWorkload(ctx context.Context, runtime Runtime, l *lab.Lab, ref Ref) error {
	if destroyer, ok := runtime.(Destroyer); ok {
		return destroyer.Destroy(ctx, l, ref)
	}
	return runtime.Stop(ctx, l, ref)
}
