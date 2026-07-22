package topologyui

import (
	"os"
	"path/filepath"
	"testing"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/workload"
)

func TestTopologyMutationSkipsModelRefreshWithoutSessionChange(t *testing.T) {
	loaded := &lab.Lab{ID: "demo"}
	app := App{
		Model:      ModelFromLab(loaded),
		Session:    lab.NewSession(loaded, ""),
		State:      ViewState{Focus: FocusGraph},
		routeCache: routeCacheState{key: "keep"},
	}

	result := app.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.CreateVM(topology.VMCreateRequest{Name: "invalid id"})
	})

	if result.OK() || result.Changed {
		t.Fatalf("result = %#v, want unchanged failure", result)
	}
	if app.routeCache.key != "keep" {
		t.Fatalf("route cache was reset without a session change: %#v", app.routeCache)
	}
	if app.Session.Revision() != 0 {
		t.Fatalf("session revision = %d, want 0", app.Session.Revision())
	}
}

func TestTopologyMutationRefreshesModelAfterSessionChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{
		Model:      ModelFromLab(loaded),
		Session:    lab.NewSession(loaded, path),
		State:      ViewState{Focus: FocusGraph, Selected: 9},
		routeCache: routeCacheState{key: "stale"},
	}

	result := app.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.CreateVM(topology.VMCreateRequest{Name: "vm1"})
	})

	if !result.OK() || !result.Changed {
		t.Fatalf("result = %#v, want changed success", result)
	}
	if len(app.Model.Nodes) != 1 || app.Model.Nodes[0].ID != "vm1" {
		t.Fatalf("model after mutation = %#v", app.Model)
	}
	if app.routeCache.key != "" || len(app.routeCache.routes) != 0 {
		t.Fatalf("route cache after mutation = %#v, want reset", app.routeCache)
	}
	if app.State.Selected != 0 {
		t.Fatalf("selected = %d, want clamped 0", app.State.Selected)
	}
}

func TestTopologyMutationRefreshesRestoredStateAfterSaveFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocked")
	if err := writeTestFile(blocker); err != nil {
		t.Fatal(err)
	}
	loaded := &lab.Lab{ID: "demo", Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}}}
	app := App{
		Model:      ModelFromLab(loaded),
		Session:    lab.NewSession(loaded, filepath.Join(blocker, "demo.lab")),
		State:      ViewState{Focus: FocusGraph},
		routeCache: routeCacheState{key: "stale"},
	}

	result := app.runTopologyMutation(func(service *topology.Service) topology.Result {
		return service.CreateSwitch(topology.SwitchCreateRequest{Name: "other", Mode: "bridge"})
	})

	if result.OK() || result.Changed {
		t.Fatalf("result = %#v, want save failure", result)
	}
	if len(app.currentLab().Switches) != 1 || len(app.Model.Nodes) != 1 || app.Model.Nodes[0].ID != "lan" {
		t.Fatalf("restored state: lab=%#v model=%#v", app.currentLab(), app.Model)
	}
	if app.routeCache.key != "" || len(app.routeCache.routes) != 0 {
		t.Fatalf("route cache after rollback = %#v, want reset", app.routeCache)
	}
}

func TestFailedVNCMutationDoesNotRefreshRuntime(t *testing.T) {
	runtimeOpens := 0
	loaded := &lab.Lab{ID: "demo", VMs: []lab.VM{{ID: "vm1", MemoryMB: 512, CPUs: 1}}}
	app := App{
		Model:          ModelFromLab(loaded),
		Session:        lab.NewSession(loaded, ""),
		WorkloadStates: map[string]string{},
		runtimeAccess: newRuntimeAccess(func(*lab.Lab) (workload.Runtime, func(), error) {
			runtimeOpens++
			return &fakeVMRuntime{}, func() {}, nil
		}, "", nil),
	}

	app.vmSet("missing", topology.VMUpdate{VNC: topology.SetField(true)})

	if runtimeOpens != 0 {
		t.Fatalf("runtime opens = %d after failed mutation, want 0", runtimeOpens)
	}
}

func writeTestFile(path string) error {
	return os.WriteFile(path, nil, 0o600)
}
