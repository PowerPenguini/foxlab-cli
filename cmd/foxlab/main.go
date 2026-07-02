package main

import (
	"flag"
	"fmt"
	"os"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/topologyui"
	"foxlab-cli/internal/virt"
)

func main() {
	fs := flag.NewFlagSet("foxlab", flag.ExitOnError)
	labPath := fs.String("lab", "", "optional .lab file to render")
	noRaw := fs.Bool("no-raw", false, "render one frame without raw terminal mode")
	width := fs.Int("width", 100, "frame width for --no-raw")
	height := fs.Int("height", 30, "frame height for --no-raw")
	uri := fs.String("uri", virt.DefaultURI, "libvirt URI")
	containerdAddress := fs.String("containerd", "", "containerd socket path")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: foxlab [--lab demo.lab] [--no-raw] [demo.lab]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	resolvedLabPath, err := resolveLabPath(*labPath, fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	*labPath = resolvedLabPath
	if *labPath == "" {
		path, ok, err := lab.DefaultPath()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if ok {
			*labPath = path
		}
	}
	model, loadedLab, err := loadModelAndLab(*labPath)
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

func resolveLabPath(flagPath string, args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("unexpected argument %q", args[1])
	}
	if flagPath != "" && len(args) > 0 {
		return "", fmt.Errorf("unexpected argument %q; --lab is already set", args[0])
	}
	if flagPath == "" && len(args) == 1 {
		return args[0], nil
	}
	return flagPath, nil
}

func loadModel(path string) (topologyui.Model, error) {
	model, _, err := loadModelAndLab(path)
	return model, err
}

func loadModelAndLab(path string) (topologyui.Model, *lab.Lab, error) {
	if path == "" {
		return topologyui.Model{}, nil, fmt.Errorf("missing .lab file; pass a path or put one in ~/.foxlab")
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		return topologyui.Model{}, nil, err
	}
	return topologyui.ModelFromLab(loaded), loaded, nil
}
