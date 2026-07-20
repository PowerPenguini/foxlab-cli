package topologyui

import (
	"context"
	"os"
	"time"

	"foxlab-cli/internal/daemonstatus"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/workload"
)

// AppConfig contains the stable process-level configuration used by App.
// Mutable topology and runtime state remain owned by App itself.
type AppConfig struct {
	LabPath               string
	LibvirtURI            string
	ContainerdAddress     string
	VNCViewer             string
	StatusSocket          string
	StatusRefreshInterval time.Duration
	In                    *os.File
	Out                   *os.File
}

// AppDeps contains replaceable process integrations. RuntimeFactory is
// required for every operation that inspects or controls workloads.
type AppDeps struct {
	RuntimeFactory   RuntimeFactory
	StatusQuery      func(context.Context, string) (daemonstatus.Snapshot, error)
	DaemonController DaemonController
}

// RuntimeFactory opens a workload runtime for the supplied lab. The returned
// close function must always be safe to call.
type RuntimeFactory func(*lab.Lab) (workload.Runtime, func(), error)

// NewApp is the composition root for the interactive topology application.
func NewApp(model Model, loadedLab *lab.Lab, config AppConfig, deps AppDeps) *App {
	app := &App{
		Model:                 model,
		State:                 ViewState{Focus: FocusGraph},
		Lab:                   loadedLab,
		LabPath:               config.LabPath,
		LibvirtURI:            config.LibvirtURI,
		ContainerdAddress:     config.ContainerdAddress,
		VNCViewer:             config.VNCViewer,
		StatusSocket:          config.StatusSocket,
		StatusRefreshInterval: config.StatusRefreshInterval,
		In:                    config.In,
		Out:                   config.Out,
		StatusQuery:           deps.StatusQuery,
		DaemonController:      deps.DaemonController,
		runtimeFactory:        deps.RuntimeFactory,
	}
	app.Service = topology.NewService(loadedLab, config.LabPath)
	return app
}
