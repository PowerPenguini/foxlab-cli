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

type directAction struct {
	kind string
	name string
}

func main() {
	fs := flag.NewFlagSet("foxlab", flag.ExitOnError)
	labPath := fs.String("lab", "", "optional .lab file to render")
	noRaw := fs.Bool("no-raw", false, "render one frame without raw terminal mode")
	width := fs.Int("width", 100, "frame width for --no-raw")
	height := fs.Int("height", 30, "frame height for --no-raw")
	uri := fs.String("uri", virt.DefaultURI, "libvirt URI")
	containerdAddress := fs.String("containerd", "", "containerd socket path")
	shellTarget := fs.String("sh", "", "enter a VM or container shell by id or name")
	vncTarget := fs.String("vnc", "", "open VNC for a VM id or name")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: foxlab [--lab demo.lab] [--no-raw] [--sh NAME | --vnc NAME] [demo.lab]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	action, err := resolveDirectAction(*shellTarget, *vncTarget)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *noRaw && action.kind != "" {
		fmt.Fprintln(os.Stderr, "--no-raw cannot be combined with direct shell or vnc flags")
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
	if action.kind != "" {
		switch action.kind {
		case "shell":
			var typ, id string
			typ, id, err = resolveShellWorkload(loadedLab, action.name)
			if err == nil {
				err = app.RunShell(typ, id)
			}
		case "vnc":
			var id string
			id, err = resolveVMName(loadedLab, action.name)
			if err == nil {
				err = app.RunVNC(id)
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resolveDirectAction(shellTarget, vncTarget string) (directAction, error) {
	set := 0
	for _, value := range []string{shellTarget, vncTarget} {
		if value != "" {
			set++
		}
	}
	if set > 1 {
		return directAction{}, fmt.Errorf("choose only one of --sh or --vnc")
	}
	if shellTarget != "" {
		return directAction{kind: "shell", name: shellTarget}, nil
	}
	if vncTarget != "" {
		return directAction{kind: "vnc", name: vncTarget}, nil
	}
	return directAction{}, nil
}

func resolveShellWorkload(loaded *lab.Lab, name string) (string, string, error) {
	type workloadMatch struct {
		typ string
		id  string
	}
	exactMatches := []workloadMatch{}
	nameMatches := []workloadMatch{}
	for _, vm := range loaded.VMs {
		match := workloadMatch{typ: topologyui.NodeVM, id: vm.ID}
		if vm.ID == name {
			exactMatches = append(exactMatches, match)
		} else if vm.Name == name {
			nameMatches = append(nameMatches, match)
		}
	}
	for _, ct := range loaded.Containers {
		match := workloadMatch{typ: topologyui.NodeContainer, id: ct.ID}
		if ct.ID == name {
			exactMatches = append(exactMatches, match)
		} else if ct.Name == name {
			nameMatches = append(nameMatches, match)
		}
	}
	if len(exactMatches) == 1 {
		return exactMatches[0].typ, exactMatches[0].id, nil
	}
	if len(exactMatches) > 1 {
		return "", "", fmt.Errorf("workload id is ambiguous: %s", name)
	}
	matches := nameMatches
	if len(matches) == 0 {
		return "", "", fmt.Errorf("workload not found: %s", name)
	}
	if len(matches) > 1 {
		return "", "", fmt.Errorf("workload name is ambiguous: %s", name)
	}
	return matches[0].typ, matches[0].id, nil
}

func resolveVMName(loaded *lab.Lab, name string) (string, error) {
	exactMatches := []string{}
	nameMatches := []string{}
	for _, vm := range loaded.VMs {
		if vm.ID == name {
			exactMatches = append(exactMatches, vm.ID)
		} else if vm.Name == name {
			nameMatches = append(nameMatches, vm.ID)
		}
	}
	if len(exactMatches) == 1 {
		return exactMatches[0], nil
	}
	if len(exactMatches) > 1 {
		return "", fmt.Errorf("vm id is ambiguous: %s", name)
	}
	matches := nameMatches
	if len(matches) == 0 {
		return "", fmt.Errorf("vm not found: %s", name)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("vm name is ambiguous: %s", name)
	}
	return matches[0], nil
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
