package daemonstatus

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"foxlab-cli/internal/lab"
	"gopkg.in/yaml.v3"
)

const CommandStatus = "status"

type Snapshot struct {
	LabPath   string            `json:"labPath" yaml:"labPath"`
	LabName   string            `json:"labName" yaml:"labName"`
	UpdatedAt time.Time         `json:"updatedAt" yaml:"updatedAt"`
	States    map[string]string `json:"states,omitempty" yaml:"states,omitempty"`
	VNCPorts  map[string]int    `json:"vncPorts,omitempty" yaml:"vncPorts,omitempty"`
	Actions   []string          `json:"actions,omitempty" yaml:"actions,omitempty"`
	Errors    []string          `json:"errors,omitempty" yaml:"errors,omitempty"`
}

type Store struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) Set(snapshot Snapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = cloneSnapshot(snapshot)
}

func (s *Store) Get() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSnapshot(s.snapshot)
}

func Listen(ctx context.Context, path string, store *Store) error {
	_, errs, err := Start(ctx, path, store)
	if err != nil {
		return err
	}
	return <-errs
}

func Start(ctx context.Context, path string, store *Store) (string, <-chan error, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultSocketPath()
		if err != nil {
			return "", nil, err
		}
	}
	if err := prepareSocketDir(filepath.Dir(path)); err != nil {
		return "", nil, err
	}
	if err := removeSocketFile(path); err != nil {
		return "", nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return "", nil, err
	}
	_ = os.Chmod(path, 0o666)
	chownPathForSudoUser(path)
	errs := make(chan error, 1)
	go serveListener(ctx, path, listener, store, errs)
	return path, errs, nil
}

func serveListener(ctx context.Context, path string, listener net.Listener, store *Store, errs chan<- error) {
	defer listener.Close()
	defer removeSocketFile(path)
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				errs <- ctx.Err()
			} else {
				errs <- err
			}
			return
		}
		go serveConn(conn, store)
	}
}

func removeSocketFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("status socket path %q exists and is not a socket", path)
	}
	return os.Remove(path)
}

func prepareSocketDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		chownPathForSudoUser(dir)
	}
	return os.Chmod(dir, 0o755)
}

func chownPathForSudoUser(path string) {
	uid, gid, ok := sudoUserIDs()
	if !ok {
		return
	}
	_ = os.Chown(path, uid, gid)
}

func sudoUserIDs() (int, int, bool) {
	if os.Geteuid() != 0 {
		return 0, 0, false
	}
	name := strings.TrimSpace(os.Getenv("SUDO_USER"))
	if name == "" || name == "root" {
		return 0, 0, false
	}
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, false
	}
	uid, uidErr := strconv.Atoi(u.Uid)
	gid, gidErr := strconv.Atoi(u.Gid)
	if uidErr != nil || gidErr != nil {
		return 0, 0, false
	}
	return uid, gid, true
}

func Query(ctx context.Context, path string) (Snapshot, error) {
	if strings.TrimSpace(path) == "" {
		var err error
		path, err = DefaultSocketPath()
		if err != nil {
			return Snapshot{}, err
		}
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return Snapshot{}, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := io.WriteString(conn, CommandStatus+"\n"); err != nil {
		return Snapshot{}, err
	}
	data, err := io.ReadAll(conn)
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := yaml.Unmarshal(data, &snapshot); err != nil {
		return Snapshot{}, err
	}
	return cloneSnapshot(snapshot), nil
}

func DefaultSocketPath() (string, error) {
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
		return filepath.Join(runtimeDir, "foxlab", "foxlabd.sock"), nil
	}
	home, err := lab.FoxlabHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "run", "foxlabd.sock"), nil
}

func serveConn(conn net.Conn, store *Store) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && err != io.EOF {
		return
	}
	if strings.TrimSpace(line) != CommandStatus {
		_, _ = fmt.Fprintf(conn, "errors:\n  - unsupported command %q\n", strings.TrimSpace(line))
		return
	}
	data, err := yaml.Marshal(store.Get())
	if err != nil {
		_, _ = fmt.Fprintf(conn, "errors:\n  - %s\n", err)
		return
	}
	_, _ = conn.Write(data)
}

func cloneSnapshot(in Snapshot) Snapshot {
	out := in
	out.States = cloneStringMap(in.States)
	out.VNCPorts = cloneIntMap(in.VNCPorts)
	out.Actions = append([]string(nil), in.Actions...)
	out.Errors = append([]string(nil), in.Errors...)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneIntMap(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
