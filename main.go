package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topologytui"
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
		if path, ok := defaultLabPath(); ok {
			*labPath = path
		}
	}
	model, loadedLab, err := loadModelAndLab(*labPath, *mock)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *noRaw {
		if err := topologytui.OneFrame(os.Stdout, model, *width, *height); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stdout)
		return
	}
	app := topologytui.App{
		Model:      model,
		Lab:        loadedLab,
		LabPath:    *labPath,
		LibvirtURI: *uri,
		State: topologytui.ViewState{
			Focus: topologytui.FocusGraph,
		},
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func loadModel(path string, mock bool) (topologytui.Model, error) {
	model, _, err := loadModelAndLab(path, mock)
	return model, err
}

func loadModelAndLab(path string, mock bool) (topologytui.Model, *lab.Lab, error) {
	if mock {
		return topologytui.MockModel(), nil, nil
	}
	if path == "" {
		return topologytui.Model{}, nil, fmt.Errorf("missing .lab file; pass a path or use --mock")
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		return topologytui.Model{}, nil, err
	}
	return topologytui.ModelFromLab(loaded), loaded, nil
}

func defaultLabPath() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if path, ok := regularFile(filepath.Join(dir, "topology-tui.lab")); ok {
			return path, true
		}
		matches, err := filepath.Glob(filepath.Join(dir, "*.lab"))
		if err == nil && len(matches) == 1 {
			if path, ok := regularFile(matches[0]); ok {
				return path, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func regularFile(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}
