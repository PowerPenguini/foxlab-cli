package topologyui

import (
	"testing"
	"time"

	"foxlab-cli/internal/lab"
)

func TestNewAppBuildsCompositionRoot(t *testing.T) {
	loaded := &lab.Lab{ID: "demo"}
	runtime := &fakeVMRuntime{}
	factoryCalls := 0
	app := NewApp(Model{}, loaded, AppConfig{
		LabPath:               "/tmp/demo.lab",
		LibvirtURI:            "qemu:///system",
		ContainerdAddress:     "/tmp/containerd.sock",
		VNCViewer:             "remote-viewer",
		StatusSocket:          "/tmp/foxlabd.sock",
		StatusRefreshInterval: 2 * time.Second,
	}, AppDeps{
		RuntimeFactory: func(got *lab.Lab) (WorkloadRuntime, func(), error) {
			factoryCalls++
			if got != loaded {
				t.Fatalf("runtime lab = %p, want %p", got, loaded)
			}
			return runtime, func() {}, nil
		},
	})

	if app.Lab != loaded || app.Service == nil || app.Service.Lab != loaded || app.Service.Path != "/tmp/demo.lab" {
		t.Fatalf("composition root did not wire lab service: %#v", app)
	}
	if app.State.Focus != FocusGraph || app.LibvirtURI != "qemu:///system" || app.ContainerdAddress != "/tmp/containerd.sock" {
		t.Fatalf("composition root config = %#v", app)
	}
	gotRuntime, closeRuntime, err := app.runtime()
	if err != nil {
		t.Fatal(err)
	}
	defer closeRuntime()
	if gotRuntime != runtime || factoryCalls != 1 {
		t.Fatalf("runtime = %p, calls = %d", gotRuntime, factoryCalls)
	}
}
