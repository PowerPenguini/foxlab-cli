package containerd

import (
	"io"
	"reflect"
	"strings"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"

	"foxlab-cli/internal/lab"
)

func TestShellExitReaderPassesInput(t *testing.T) {
	exitC := make(chan struct{})
	reader := newShellExitReader(strings.NewReader("id\n"), exitC)
	buf := make([]byte, 8)

	n, err := reader.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "id\n" {
		t.Fatalf("read = %q, want id newline", got)
	}
	select {
	case <-exitC:
		t.Fatal("exit channel closed without ctrl-]")
	default:
	}
}

func TestShellExitReaderStopsAtCtrlBracket(t *testing.T) {
	exitC := make(chan struct{})
	reader := newShellExitReader(strings.NewReader("echo ok\x1dignored"), exitC)
	buf := make([]byte, 32)

	n, err := reader.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "echo ok" {
		t.Fatalf("read = %q, want echo ok", got)
	}
	select {
	case <-exitC:
	default:
		t.Fatal("exit channel did not close")
	}
}

func TestShellExitReaderCtrlBracketOnlyReturnsEOF(t *testing.T) {
	exitC := make(chan struct{})
	reader := newShellExitReader(strings.NewReader("\x1d"), exitC)
	buf := make([]byte, 8)

	n, err := reader.Read(buf)
	if n != 0 || err != io.EOF {
		t.Fatalf("read = %d, %v; want 0, EOF", n, err)
	}
	select {
	case <-exitC:
	default:
		t.Fatal("exit channel did not close")
	}
}

func TestContainerShellArgsStartInteractiveShell(t *testing.T) {
	got := containerShellArgs(lab.Container{Shell: "/usr/bin/bash"})
	want := []string{"/usr/bin/bash", "-i"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("containerShellArgs = %#v, want %#v", got, want)
	}
}

func TestContainerShellProcessKeepsImageEnvironment(t *testing.T) {
	process := containerShellProcess(lab.Container{Shell: "/usr/bin/bash"}, &specs.Process{
		Cwd: "/work",
		Env: []string{"PATH=/image/bin", "HOME=/home/kali", "TERM=xterm-kitty"},
	})
	if process.Cwd != "/work" {
		t.Fatalf("cwd = %q, want /work", process.Cwd)
	}
	if !reflect.DeepEqual(process.Args, []string{"/usr/bin/bash", "-i"}) {
		t.Fatalf("args = %#v", process.Args)
	}
	got := strings.Join(process.Env, "\n")
	for _, want := range []string{"PATH=/image/bin", "HOME=/home/kali", "TERM=xterm-256color", "SHELL=/usr/bin/bash"} {
		if !strings.Contains(got, want) {
			t.Fatalf("env missing %q in %#v", want, process.Env)
		}
	}
	if strings.Contains(got, "TERM=xterm-kitty") {
		t.Fatalf("shell inherited incompatible TERM: %#v", process.Env)
	}
	if strings.Contains(got, "USER=root") || strings.Contains(got, "LOGNAME=root") {
		t.Fatalf("env forced root identity: %#v", process.Env)
	}
}

func TestShellProcessReceivesInitialConsoleSize(t *testing.T) {
	process := containerShellProcess(lab.Container{}, nil)
	setShellProcessSize(process, ShellSize{Columns: 132, Rows: 40})
	if process.ConsoleSize == nil || process.ConsoleSize.Width != 132 || process.ConsoleSize.Height != 40 {
		t.Fatalf("console size = %#v", process.ConsoleSize)
	}
}
