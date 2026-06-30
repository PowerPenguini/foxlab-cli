package topologyui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestSystemdDaemonApplyDestroysPreviousLabBeforeRestart(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.lab")
	newPath := filepath.Join(dir, "new lab.lab")
	if err := lab.SaveFile(oldPath, &lab.Lab{ID: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := lab.SaveFile(newPath, &lab.Lab{ID: "new"}); err != nil {
		t.Fatal(err)
	}

	var commands []string
	var destroyed []string
	controller := &systemdDaemonController{
		configDir: dir,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			if name == "systemctl" && strings.Join(args, " ") == "show foxlabd.service -p ActiveState --value" {
				return []byte("active\n"), nil
			}
			return nil, nil
		},
		destroyLab: func(_ context.Context, _, _ string, l *lab.Lab) error {
			destroyed = append(destroyed, l.ID)
			return nil
		},
	}
	if err := controller.writeDropIn(context.Background(), oldPath); err != nil {
		t.Fatal(err)
	}

	err := controller.Apply(context.Background(), DaemonApplyRequest{LabPath: newPath})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(destroyed, []string{"old"}) {
		t.Fatalf("destroyed labs = %#v, want old", destroyed)
	}
	wantCommands := []string{
		"systemctl show foxlabd.service -p ActiveState --value",
		"systemctl --user disable --now foxlabd.service",
		"systemctl --user daemon-reload",
		"systemctl stop foxlabd.service",
		"systemctl cat foxlabd.service",
		"systemctl daemon-reload",
		"systemctl enable --now foxlabd.service",
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("commands = %#v, want %#v", commands, wantCommands)
	}
	configured, err := controller.configuredLabPath()
	if err != nil {
		t.Fatal(err)
	}
	if !sameLabPath(configured, newPath) {
		t.Fatalf("configured lab path = %q, want %q", configured, newPath)
	}
}

func TestSystemdDaemonApplyNoopsWhenOpenLabAlreadyActive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lab")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	var commands []string
	controller := &systemdDaemonController{
		configDir: dir,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			commands = append(commands, name+" "+strings.Join(args, " "))
			if name == "systemctl" && strings.Join(args, " ") == "show foxlabd.service -p ActiveState --value" {
				return []byte("active\n"), nil
			}
			return nil, nil
		},
		destroyLab: func(context.Context, string, string, *lab.Lab) error {
			t.Fatal("destroyLab called for already active lab")
			return nil
		},
	}
	if err := controller.writeDropIn(context.Background(), path); err != nil {
		t.Fatal(err)
	}

	if err := controller.Apply(context.Background(), DaemonApplyRequest{LabPath: path}); err != nil {
		t.Fatal(err)
	}
	want := []string{"systemctl show foxlabd.service -p ActiveState --value"}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestSystemdDaemonApplyIgnoresStopWhenUnitNotLoaded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lab")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	var commands []string
	controller := &systemdDaemonController{
		configDir: dir,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			commands = append(commands, command)
			switch command {
			case "systemctl show foxlabd.service -p ActiveState --value":
				return []byte("inactive\n"), nil
			case "systemctl stop foxlabd.service":
				return nil, fmt.Errorf("systemctl --user stop foxlabd.service: exit status 5: Failed to stop foxlabd.service: Unit foxlabd.service not loaded")
			default:
				return nil, nil
			}
		},
		destroyLab: func(context.Context, string, string, *lab.Lab) error {
			t.Fatal("destroyLab called without previous applied lab")
			return nil
		},
	}
	if err := controller.writeDropIn(context.Background(), path); err != nil {
		t.Fatal(err)
	}

	if err := controller.Apply(context.Background(), DaemonApplyRequest{LabPath: path}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"systemctl show foxlabd.service -p ActiveState --value",
		"systemctl --user disable --now foxlabd.service",
		"systemctl --user daemon-reload",
		"systemctl stop foxlabd.service",
		"systemctl cat foxlabd.service",
		"systemctl daemon-reload",
		"systemctl enable --now foxlabd.service",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestSystemdDaemonApplyWritesSystemUnitWhenServiceDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.lab")
	foxlabdPath := filepath.Join(dir, "bin", "foxlabd")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo"}); err != nil {
		t.Fatal(err)
	}
	var commands []string
	controller := &systemdDaemonController{
		configDir: dir,
		foxlabd:   foxlabdPath,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			command := name + " " + strings.Join(args, " ")
			commands = append(commands, command)
			switch command {
			case "systemctl show foxlabd.service -p ActiveState --value":
				return []byte("inactive\n"), nil
			case "systemctl stop foxlabd.service":
				return nil, fmt.Errorf("systemctl --user stop foxlabd.service: exit status 5: Failed to stop foxlabd.service: Unit foxlabd.service not loaded")
			case "systemctl cat foxlabd.service":
				return nil, fmt.Errorf("systemctl --user cat foxlabd.service: exit status 1: No files found for foxlabd.service")
			default:
				return nil, nil
			}
		},
		destroyLab: func(context.Context, string, string, *lab.Lab) error {
			t.Fatal("destroyLab called without previous applied lab")
			return nil
		},
	}
	if err := controller.writeDropIn(context.Background(), path); err != nil {
		t.Fatal(err)
	}

	if err := controller.Apply(context.Background(), DaemonApplyRequest{LabPath: path}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"systemctl show foxlabd.service -p ActiveState --value",
		"systemctl --user disable --now foxlabd.service",
		"systemctl --user daemon-reload",
		"systemctl stop foxlabd.service",
		"systemctl cat foxlabd.service",
		"systemctl daemon-reload",
		"systemctl enable --now foxlabd.service",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
	unitPath := filepath.Join(dir, "systemd", "system", "foxlabd.service")
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), "ExecStart="+foxlabdPath+" --lab ${FOXLAB_LAB} --status-socket ${FOXLAB_STATUS_SOCKET}") {
		t.Fatalf("unit file missing foxlabd ExecStart:\n%s", unit)
	}
	if !strings.Contains(string(unit), "After=containerd.service libvirtd.service") ||
		!strings.Contains(string(unit), "Wants=containerd.service libvirtd.service") {
		t.Fatalf("unit file missing runtime service dependencies:\n%s", unit)
	}
	if !strings.Contains(string(unit), "WantedBy=multi-user.target") {
		t.Fatalf("unit file missing system install target:\n%s", unit)
	}
}

func TestParseDropInLabPathKeepsSpaces(t *testing.T) {
	got := parseDropInLabPath("[Service]\nEnvironment=\"FOXLAB_LAB=/tmp/fox lab/demo.lab\"\n")
	if got != "/tmp/fox lab/demo.lab" {
		t.Fatalf("lab path = %q", got)
	}
}
