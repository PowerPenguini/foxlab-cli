package reconciler

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

type RuntimeFactory func(*lab.Lab) (workload.Runtime, error)

type Logger interface {
	Printf(string, ...any)
}

type Runner struct {
	LabPath        string
	Interval       time.Duration
	RuntimeFactory RuntimeFactory
	Logger         Logger
	StatusStore    *daemonstatus.Store
	WatchInterval  time.Duration
}

func (r Runner) Run(ctx context.Context, once bool) error {
	if once {
		return r.Step(ctx)
	}
	interval := r.Interval
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	watchInterval := r.watchInterval(interval)
	watcher := time.NewTicker(watchInterval)
	defer watcher.Stop()

	lastSignature := labFileSignature{}
	if signature, err := fileSignature(r.LabPath); err == nil {
		lastSignature = signature
	}
	runStep := func() {
		if err := r.Step(ctx); err != nil {
			r.logf("reconcile failed: %v", err)
		}
		if signature, err := fileSignature(r.LabPath); err == nil {
			lastSignature = signature
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runStep()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			runStep()
		case <-watcher.C:
			signature, err := fileSignature(r.LabPath)
			if err != nil {
				continue
			}
			if !signature.equal(lastSignature) {
				runStep()
			}
		}
	}
}

func (r Runner) watchInterval(interval time.Duration) time.Duration {
	if r.WatchInterval > 0 {
		return r.WatchInterval
	}
	if interval > 0 && interval < 250*time.Millisecond {
		return interval
	}
	return 250 * time.Millisecond
}

func (r Runner) Step(ctx context.Context) (retErr error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.LabPath == "" {
		err := errors.New("missing .lab path")
		r.publishError("", "", err)
		return err
	}
	if r.RuntimeFactory == nil {
		err := errors.New("missing runtime factory")
		r.publishError(r.LabPath, "", err)
		return err
	}
	loaded, err := lab.LoadFile(r.LabPath)
	if err != nil {
		r.publishError(r.LabPath, "", err)
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime, err := r.RuntimeFactory(loaded)
	if err != nil {
		r.publishError(r.LabPath, loaded.ID, err)
		return err
	}
	if runtime == nil {
		err := errors.New("runtime factory returned nil runtime")
		r.publishError(r.LabPath, loaded.ID, err)
		return err
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			r.logf("runtime close failed: %v", err)
			retErr = errors.Join(retErr, err)
		}
	}()
	result := (&workload.Reconciler{Runtime: runtime}).Step(ctx, loaded)
	vncPorts := map[string]int{}
	if vncRuntime, ok := runtime.(workload.VNCRuntime); ok {
		ports, err := vncRuntime.VNCPorts(ctx, loaded)
		if err != nil {
			result.Errors = append(result.Errors, err)
		} else {
			vncPorts = cloneIntMap(ports)
		}
	}
	r.publishStatus(loaded, result, vncPorts)
	for _, action := range result.Actions {
		r.logf("%s", action)
	}
	for _, err := range result.Errors {
		r.logf("%v", err)
	}
	return errors.Join(result.Errors...)
}

func (r Runner) publishStatus(l *lab.Lab, result workload.ReconcileResult, vncPorts map[string]int) {
	if r.StatusStore == nil || l == nil {
		return
	}
	labPath := l.Path()
	if abs, err := filepath.Abs(firstNonEmpty(labPath, r.LabPath)); err == nil {
		labPath = abs
	}
	r.StatusStore.Set(daemonstatus.Snapshot{
		LabPath:   labPath,
		LabName:   l.ID,
		UpdatedAt: time.Now(),
		States:    cloneStringMap(result.States),
		VNCPorts:  cloneIntMap(vncPorts),
		Actions:   append([]string(nil), result.Actions...),
		Errors:    errorStrings(result.Errors),
	})
}

func (r Runner) publishError(labPath, labName string, err error) {
	if r.StatusStore == nil || err == nil {
		return
	}
	if abs, absErr := filepath.Abs(labPath); absErr == nil {
		labPath = abs
	}
	r.StatusStore.Set(daemonstatus.Snapshot{
		LabPath:   labPath,
		LabName:   labName,
		UpdatedAt: time.Now(),
		Errors:    []string{err.Error()},
	})
}

func (r Runner) logf(format string, args ...any) {
	logger := r.Logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf(format, args...)
}

func errorStrings(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, err := range errs {
		if err != nil {
			out = append(out, err.Error())
		}
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type labFileSignature struct {
	modTime time.Time
	size    int64
}

func fileSignature(path string) (labFileSignature, error) {
	info, err := os.Stat(path)
	if err != nil {
		return labFileSignature{}, err
	}
	return labFileSignature{modTime: info.ModTime(), size: info.Size()}, nil
}

func (s labFileSignature) equal(other labFileSignature) bool {
	return s.modTime.Equal(other.modTime) && s.size == other.size
}
