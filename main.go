package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/topologyui"
	"foxlab-cli/internal/virt"
)

func main() {
	fs := flag.NewFlagSet("topology-tui", flag.ExitOnError)
	labPath := fs.String("lab", "", "optional .lab file to render")
	mock := fs.Bool("mock", false, "render built-in mock topology")
	noRaw := fs.Bool("no-raw", false, "render one frame without raw terminal mode")
	width := fs.Int("width", 100, "frame width for --no-raw")
	height := fs.Int("height", 30, "frame height for --no-raw")
	uri := fs.String("uri", virt.DefaultURI, "libvirt URI")
	containerdAddress := fs.String("containerd", "", "containerd socket path")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: topology-tui [--lab demo.lab] [--mock] [--no-raw] [demo.lab]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	if *labPath == "" && fs.NArg() > 0 {
		*labPath = fs.Arg(0)
	}
	if *labPath == "" && !*mock {
		path, ok, err := defaultLabPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if ok {
			*labPath = path
		}
	}
	model, loadedLab, err := loadModelAndLab(*labPath, *mock)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *noRaw {
		if err := topologyui.OneFrame(os.Stdout, model, *width, *height); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout)
		return
	}
	app := topologyui.App{
		Model:             model,
		Lab:               loadedLab,
		LabPath:           *labPath,
		Service:           topology.NewService(loadedLab, *labPath),
		LibvirtURI:        *uri,
		ContainerdAddress: *containerdAddress,
		State: topologyui.ViewState{
			Focus: topologyui.FocusGraph,
		},
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadModel(path string, mock bool) (topologyui.Model, error) {
	model, _, err := loadModelAndLab(path, mock)
	return model, err
}

func loadModelAndLab(path string, mock bool) (topologyui.Model, *lab.Lab, error) {
	if mock {
		return topologyui.MockModel(), nil, nil
	}
	if path == "" {
		return topologyui.Model{}, nil, fmt.Errorf("missing .lab file; pass a path, put one in ~/.foxlab, or use --mock")
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		return topologyui.Model{}, nil, err
	}
	return topologyui.ModelFromLab(loaded), loaded, nil
}

func defaultLabPath() (string, bool, error) {
	dir, err := lab.FoxlabHome()
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(dir, "default.lab")
	if path, ok := regularFile(path); ok {
		return path, true, nil
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.lab"))
	if err == nil && len(matches) == 1 {
		if path, ok := regularFile(matches[0]); ok {
			return path, true, nil
		}
	}
	if err := lab.SaveFile(path, &lab.Lab{ID: "default"}); err != nil {
		return "", false, fmt.Errorf("create default lab: %w", err)
	}
	return path, true, nil
}

func regularFile(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}
