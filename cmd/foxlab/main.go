package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	containerdruntime "foxlab-cli/internal/containerd"
	"foxlab-cli/internal/foxruntime"
	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
	"foxlab-cli/internal/topologyui"
	"foxlab-cli/internal/virt"
	"foxlab-cli/internal/workload"
)

type directAction struct {
	kind string
	name string
	src  string
	dst  string
}

func main() {
	fs := flag.NewFlagSet("foxlab", flag.ExitOnError)
	labPath := fs.String("lab", "", "optional .lab file to render")
	noRaw := fs.Bool("no-raw", false, "render one frame without raw terminal mode")
	width := fs.Int("width", 100, "frame width for --no-raw")
	height := fs.Int("height", 30, "frame height for --no-raw")
	uri := fs.String("uri", virt.DefaultURI, "libvirt URI")
	containerdAddress := fs.String("containerd", "", "containerd socket path")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: foxlab [--lab demo.lab] [--no-raw] [demo.lab] [sh NAME | vnc NAME | cp SRC DST]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	action, labArgs, err := resolveDirectAction(fs.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *noRaw && action.kind != "" {
		fmt.Fprintln(os.Stderr, "--no-raw cannot be combined with sh, vnc, or cp actions")
		os.Exit(2)
	}

	resolvedLabPath, err := resolveLabPath(*labPath, labArgs)
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
		case "cp":
			err = runFileCopy(loadedLab, *uri, *containerdAddress, action.src, action.dst)
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

func resolveDirectAction(args []string) (directAction, []string, error) {
	actionIndex := -1
	for i, arg := range args {
		if arg == "sh" || arg == "vnc" || arg == "cp" {
			actionIndex = i
			break
		}
	}
	if actionIndex < 0 {
		return directAction{}, args, nil
	}
	switch args[actionIndex] {
	case "sh":
		if len(args)-actionIndex != 2 {
			return directAction{}, nil, fmt.Errorf("usage: foxlab sh NAME")
		}
		return directAction{kind: "shell", name: args[actionIndex+1]}, args[:actionIndex], nil
	case "vnc":
		if len(args)-actionIndex != 2 {
			return directAction{}, nil, fmt.Errorf("usage: foxlab vnc NAME")
		}
		return directAction{kind: "vnc", name: args[actionIndex+1]}, args[:actionIndex], nil
	case "cp":
		if len(args)-actionIndex != 3 {
			return directAction{}, nil, fmt.Errorf("usage: foxlab cp SRC DST")
		}
		return directAction{kind: "cp", src: args[actionIndex+1], dst: args[actionIndex+2]}, args[:actionIndex], nil
	}
	return directAction{}, args, nil
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

type copyEndpoint struct {
	Workload string
	Path     string
	Remote   bool
}

type fileCopyRuntime interface {
	States(context.Context, *lab.Lab) (map[string]string, error)
	PutFile(context.Context, *lab.Lab, workload.Ref, string, string) error
	GetFile(context.Context, *lab.Lab, workload.Ref, string, string) error
}

func runFileCopy(loaded *lab.Lab, libvirtURI, containerdAddress, src, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runtime, err := foxruntime.New(libvirtURI, containerdAddress, loaded)
	if err != nil {
		return err
	}
	copyErr := copyWithRuntime(ctx, loaded, runtime, src, dst)
	closeErr := runtime.Close()
	return containerdruntime.WithAccessHint(errors.Join(copyErr, closeErr))
}

func copyWithRuntime(ctx context.Context, loaded *lab.Lab, runtime fileCopyRuntime, src, dst string) error {
	source := parseCopyEndpoint(src)
	target := parseCopyEndpoint(dst)
	if source.Remote == target.Remote {
		return fmt.Errorf("usage: foxlab cp SRC DST; exactly one side must be NAME:/absolute/path")
	}
	remote := source
	if target.Remote {
		remote = target
	}
	typ, id, err := resolveShellWorkload(loaded, remote.Workload)
	if err != nil {
		return err
	}
	ref := workload.Ref{Type: workloadType(typ), ID: id}
	states, err := runtime.States(ctx, loaded)
	if err != nil {
		return fmt.Errorf("runtime status unavailable: %w", err)
	}
	key := workload.Key(ref)
	state := strings.ToLower(strings.TrimSpace(firstNonEmpty(states[key], "missing")))
	if state != "running" {
		return fmt.Errorf("%s %s is %s; run it first", ref.Type, remote.Workload, state)
	}
	if source.Remote {
		return runtime.GetFile(ctx, loaded, ref, source.Path, dst)
	}
	return runtime.PutFile(ctx, loaded, ref, src, target.Path)
}

func parseCopyEndpoint(value string) copyEndpoint {
	name, guestPath, ok := strings.Cut(value, ":")
	if !ok || name == "" || !strings.HasPrefix(guestPath, "/") {
		return copyEndpoint{Path: value}
	}
	return copyEndpoint{Workload: name, Path: guestPath, Remote: true}
}

func workloadType(nodeType string) string {
	switch nodeType {
	case topologyui.NodeVM:
		return workload.TypeVM
	case topologyui.NodeContainer:
		return workload.TypeContainer
	default:
		return nodeType
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
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
