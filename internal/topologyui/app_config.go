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
// Mutable topology state is owned by the App session; runtime state remains on App.
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
	session := lab.NewSession(loadedLab, config.LabPath)
	app := &App{
		Model:                 model,
		State:                 ViewState{Focus: FocusGraph},
		Session:               session,
		LibvirtURI:            config.LibvirtURI,
		ContainerdAddress:     config.ContainerdAddress,
		VNCViewer:             config.VNCViewer,
		StatusRefreshInterval: config.StatusRefreshInterval,
		In:                    config.In,
		Out:                   config.Out,
		DaemonController:      deps.DaemonController,
		runtimeAccess:         newRuntimeAccess(deps.RuntimeFactory, config.StatusSocket, deps.StatusQuery),
	}
	app.Service = topology.NewServiceWithSession(session)
	return app
}
