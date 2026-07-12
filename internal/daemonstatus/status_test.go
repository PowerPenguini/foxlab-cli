package daemonstatus

import (
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestServeConnReturnsYAMLSnapshot(t *testing.T) {
	store := NewStore()
	store.Set(Snapshot{
		LabPath:   "/tmp/demo.lab",
		LabName:   "demo",
		UpdatedAt: time.Unix(100, 0).UTC(),
		States:    map[string]string{"container:web": "running"},
		VNCPorts:  map[string]int{"vm:router": 5903},
		Actions:   []string{"started container:web"},
	})
	server, client := net.Pipe()
	defer client.Close()
	go serveConn(server, store)

	if _, err := io.WriteString(client, CommandStatus+"\n"); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(client)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeSnapshot(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.LabPath != "/tmp/demo.lab" || got.LabName != "demo" {
		t.Fatalf("snapshot identity = %#v", got)
	}
	if got.States["container:web"] != "running" {
		t.Fatalf("state = %q, want running", got.States["container:web"])
	}
	if got.VNCPorts["vm:router"] != 5903 {
		t.Fatalf("vnc port = %d, want 5903", got.VNCPorts["vm:router"])
	}
	if len(got.Actions) != 1 || got.Actions[0] != "started container:web" {
		t.Fatalf("actions = %#v", got.Actions)
	}
}

func TestServeConnTimesOutIdleClient(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		serveConnWithTimeout(server, NewStore(), time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle status connection did not time out")
	}
}

func decodeSnapshot(data []byte) (Snapshot, error) {
	var snapshot Snapshot
	err := yaml.Unmarshal(data, &snapshot)
	return snapshot, err
}

func TestStoreCopiesSnapshots(t *testing.T) {
	store := NewStore()
	states := map[string]string{"vm:one": "running"}
	store.Set(Snapshot{States: states})
	states["vm:one"] = "shutoff"

	got := store.Get()
	got.States["vm:one"] = "missing"
	again := store.Get()
	if again.States["vm:one"] != "running" {
		t.Fatalf("stored state = %q, want immutable running", again.States["vm:one"])
	}
}

func TestStartRefusesRegularFileSocketPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foxlabd.sock")
	if err := os.WriteFile(path, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Start(context.Background(), path, NewStore())

	if err == nil || !strings.Contains(err.Error(), "exists and is not a socket") {
		t.Fatalf("Start error = %v, want non-socket refusal", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "keep me" {
		t.Fatalf("regular file was modified: %q", data)
	}
}

func TestStartReplacesStaleSocketAndRemovesSocketOnStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foxlabd.sock")
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: path}); err != nil {
		_ = syscall.Close(fd)
		t.Fatal(err)
	}
	if err := syscall.Close(fd); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("stale socket missing before Start: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startedPath, errs, err := Start(ctx, path, NewStore())
	if err != nil {
		t.Fatal(err)
	}
	if startedPath != path {
		t.Fatalf("started path = %q, want %q", startedPath, path)
	}
	cancel()
	select {
	case err := <-errs:
		if err != context.Canceled {
			t.Fatalf("listener error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("listener did not stop")
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket path after stop = %v, want removed", err)
	}
}

func TestStartPreparesSocketDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	path := filepath.Join(dir, "foxlabd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, errs, err := Start(ctx, path, NewStore())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		select {
		case <-errs:
		case <-time.After(time.Second):
			t.Fatal("listener did not stop")
		}
	}()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("socket dir mode = %o, want 755", got)
	}
}

func TestStartDoesNotChmodExistingSocketDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "foxlabd.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, errs, err := Start(ctx, path, NewStore())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		select {
		case <-errs:
		case <-time.After(time.Second):
			t.Fatal("listener did not stop")
		}
	}()

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("existing socket dir permissions = %o, want 700", got)
	}
}
