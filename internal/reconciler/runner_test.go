package reconciler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/workload"
)

type fakeRuntime struct {
	states   map[string]string
	vncPorts map[string]int
	starts   []string
	closed   bool
	closeErr error
}

func (f *fakeRuntime) States(context.Context, *lab.Lab) (map[string]string, error) {
	return f.states, nil
}

func (f *fakeRuntime) VNCPorts(context.Context, *lab.Lab) (map[string]int, error) {
	return f.vncPorts, nil
}

func (f *fakeRuntime) Start(_ context.Context, _ *lab.Lab, ref workload.Ref) error {
	f.starts = append(f.starts, workload.Key(ref))
	f.states[workload.Key(ref)] = "running"
	return nil
}

func TestRunnerStepPublishesStatusSnapshot(t *testing.T) {
	path := t.TempDir() + "/demo.lab"
	if err := lab.SaveFile(path, &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest", DesiredState: lab.DesiredStateRunning}},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	containerID := loaded.Containers[0].ID
	store := daemonstatus.NewStore()
	runtime := &fakeRuntime{
		states:   map[string]string{workload.Key(workload.Ref{Type: workload.TypeContainer, ID: containerID}): "missing"},
		vncPorts: map[string]int{"vm:router": 5903},
	}
	runner := Runner{
		LabPath:     path,
		StatusStore: store,
		RuntimeFactory: func(*lab.Lab) (workload.Runtime, error) {
			return runtime, nil
		},
	}

	if err := runner.Step(context.Background()); err != nil {
		t.Fatalf("Step returned error: %v", err)
	}
	snapshot := store.Get()
	if snapshot.LabName != "demo" || snapshot.LabPath == "" {
		t.Fatalf("snapshot identity = %#v", snapshot)
	}
	key := workload.Key(workload.Ref{Type: workload.TypeContainer, ID: containerID})
	if snapshot.States[key] != "running" {
		t.Fatalf("snapshot state = %q, want running", snapshot.States[key])
	}
	if snapshot.VNCPorts["vm:router"] != 5903 {
		t.Fatalf("snapshot vnc port = %d, want 5903", snapshot.VNCPorts["vm:router"])
	}
	if len(snapshot.Actions) != 1 || snapshot.Actions[0] != "started "+key {
		t.Fatalf("snapshot actions = %#v", snapshot.Actions)
	}
}

func TestRunnerStepPublishesRuntimeFactoryError(t *testing.T) {
	path := t.TempDir() + "/demo.lab"
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	store := daemonstatus.NewStore()
	runner := Runner{
		LabPath:     path,
		StatusStore: store,
		RuntimeFactory: func(*lab.Lab) (workload.Runtime, error) {
			return nil, errors.New("runtime unavailable")
		},
	}

	err := runner.Step(context.Background())
	if err == nil {
		t.Fatal("Step returned nil error")
	}
	snapshot := store.Get()
	if len(snapshot.Errors) != 1 || snapshot.Errors[0] != "runtime unavailable" {
		t.Fatalf("snapshot errors = %#v", snapshot.Errors)
	}
	if snapshot.LabName != "demo" {
		t.Fatalf("snapshot lab name = %q, want demo", snapshot.LabName)
	}
}

func (f *fakeRuntime) Stop(context.Context, *lab.Lab, workload.Ref) error {
	return nil
}

func (f *fakeRuntime) Close() error {
	f.closed = true
	return f.closeErr
}

type stringLogger struct {
	lines []string
}

func (l *stringLogger) Printf(format string, args ...any) {
	l.lines = append(l.lines, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

func TestRunnerStepReconcilesLoadedLab(t *testing.T) {
	path := t.TempDir() + "/demo.lab"
	if err := lab.SaveFile(path, &lab.Lab{
		ID:  "demo",
		VMs: []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1, Disk: "vm1.qcow2", DesiredState: lab.DesiredStateRunning}},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	vmID := loaded.VMs[0].ID
	vmKey := workload.Key(workload.Ref{Type: workload.TypeVM, ID: vmID})
	runtime := &fakeRuntime{states: map[string]string{vmKey: "shutoff"}}
	logger := &stringLogger{}
	runner := Runner{
		LabPath: path,
		Logger:  logger,
		RuntimeFactory: func(*lab.Lab) (workload.Runtime, error) {
			return runtime, nil
		},
	}

	if err := runner.Step(context.Background()); err != nil {
		t.Fatalf("Step returned error: %v", err)
	}
	if len(runtime.starts) != 1 || runtime.starts[0] != vmKey {
		t.Fatalf("starts = %#v, want %s", runtime.starts, vmKey)
	}
	if !runtime.closed {
		t.Fatal("runtime was not closed")
	}
	if got := strings.Join(logger.lines, "\n"); !strings.Contains(got, "started "+vmKey) {
		t.Fatalf("log = %q, want start action", got)
	}
}

func TestRunnerStepRejectsNilRuntime(t *testing.T) {
	path := t.TempDir() + "/demo.lab"
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	runner := Runner{
		LabPath: path,
		RuntimeFactory: func(*lab.Lab) (workload.Runtime, error) {
			return nil, nil
		},
	}

	err := runner.Step(context.Background())
	if err == nil || !strings.Contains(err.Error(), "nil runtime") {
		t.Fatalf("Step error = %v, want nil runtime error", err)
	}
}

func TestRunnerStepReturnsRuntimeCloseError(t *testing.T) {
	path := t.TempDir() + "/demo.lab"
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("close failed")
	runtime := &fakeRuntime{closeErr: closeErr}
	logger := &stringLogger{}
	runner := Runner{
		LabPath: path,
		Logger:  logger,
		RuntimeFactory: func(*lab.Lab) (workload.Runtime, error) {
			return runtime, nil
		},
	}

	err := runner.Step(context.Background())
	if !errors.Is(err, closeErr) {
		t.Fatalf("Step error = %v, want close error", err)
	}
	if !runtime.closed {
		t.Fatal("runtime was not closed")
	}
	if got := strings.Join(logger.lines, "\n"); !strings.Contains(got, "runtime close failed: close failed") {
		t.Fatalf("log = %q, want close failure", got)
	}
}

func TestRunnerRunCanceledContextDoesNotOpenRuntime(t *testing.T) {
	path := t.TempDir() + "/demo.lab"
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	runner := Runner{
		LabPath: path,
		RuntimeFactory: func(*lab.Lab) (workload.Runtime, error) {
			called = true
			return &fakeRuntime{}, nil
		},
	}

	err := runner.Run(ctx, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run error = %v, want context canceled", err)
	}
	if called {
		t.Fatal("runtime factory was called after context cancellation")
	}
}

func TestRunnerRunReconcilesWhenLabFileChanges(t *testing.T) {
	path := t.TempDir() + "/demo.lab"
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	loadedCounts := make(chan int, 4)
	runner := Runner{
		LabPath:       path,
		Interval:      time.Hour,
		WatchInterval: 5 * time.Millisecond,
		RuntimeFactory: func(l *lab.Lab) (workload.Runtime, error) {
			loadedCounts <- len(l.Containers)
			return &fakeRuntime{states: map[string]string{}}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx, false)
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Run error = %v, want context canceled", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Run did not exit after cancellation")
		}
	}()

	if got := waitForLoadedCount(t, loadedCounts); got != 0 {
		t.Fatalf("initial container count = %d, want 0", got)
	}
	if err := lab.SaveFile(path, &lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "web", Image: "docker.io/library/nginx:latest"}},
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case got := <-loadedCounts:
			if got == 1 {
				return
			}
		case <-deadline:
			t.Fatal("runner did not reconcile after lab file change")
		}
	}
}

func waitForLoadedCount(t *testing.T, counts <-chan int) int {
	t.Helper()
	select {
	case count := <-counts:
		return count
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reconcile")
		return 0
	}
}

func TestRunnerStepCanceledContextDoesNotOpenRuntime(t *testing.T) {
	path := t.TempDir() + "/demo.lab"
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	runner := Runner{
		LabPath: path,
		RuntimeFactory: func(*lab.Lab) (workload.Runtime, error) {
			called = true
			return &fakeRuntime{}, nil
		},
	}

	err := runner.Step(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Step error = %v, want context canceled", err)
	}
	if called {
		t.Fatal("runtime factory was called after context cancellation")
	}
}
