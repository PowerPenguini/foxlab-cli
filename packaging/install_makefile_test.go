package packaging

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMakeInstallRestartsSystemDaemon(t *testing.T) {
	log := runMakeInstall(t, "")
	want := []string{
		"systemctl-user disable --now foxlabd.service",
		"systemctl daemon-reload",
		"systemctl enable foxlabd.service",
		"systemctl restart foxlabd.service",
	}
	for _, entry := range want {
		if !strings.Contains(log, entry) {
			t.Fatalf("install log missing %q:\n%s", entry, log)
		}
	}
	if strings.Contains(log, "systemctl enable --now foxlabd.service") {
		t.Fatalf("install still relies on enable --now instead of restart:\n%s", log)
	}
	if strings.Index(log, want[1]) > strings.Index(log, want[2]) || strings.Index(log, want[2]) > strings.Index(log, want[3]) {
		t.Fatalf("system daemon commands are out of order:\n%s", log)
	}
}

func TestStagedMakeInstallDoesNotControlDaemon(t *testing.T) {
	log := runMakeInstall(t, t.TempDir())
	if strings.Contains(log, "systemctl") {
		t.Fatalf("staged install controlled a live daemon:\n%s", log)
	}
}

func TestMakeInstallUsesDownloadableGoProxyByDefault(t *testing.T) {
	log := runMakeInstall(t, t.TempDir())
	const want = "go GOPROXY=https://proxy.golang.org,direct build"
	if !strings.Contains(log, want) {
		t.Fatalf("make install did not use the downloadable default Go proxy %q:\n%s", want, log)
	}
}

func runMakeInstall(t *testing.T, destDir string) string {
	t.Helper()
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "commands.log")
	tool := func(name string) string {
		path := filepath.Join(dir, name)
		script := "#!/bin/sh\n"
		if name == "go" {
			script += "printf '%s GOPROXY=%s %s\\n' 'go' \"$GOPROXY\" \"$*\" >> \"$FOXLAB_INSTALL_LOG\"\n"
		} else {
			script += "printf '%s %s\\n' '" + name + "' \"$*\" >> \"$FOXLAB_INSTALL_LOG\"\n"
		}
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	cmd := exec.Command(
		"make", "install",
		"GO="+tool("go"),
		"INSTALL_CMD="+tool("install"),
		"SYSTEMCTL="+tool("systemctl"),
		"SYSTEMCTL_USER="+tool("systemctl-user"),
		"BINDIR="+filepath.Join(dir, "bin"),
		"SYSTEMD_SYSTEM_UNIT_DIR="+filepath.Join(dir, "systemd"),
		"DESTDIR="+destDir,
	)
	cmd.Dir = repoRoot
	cmd.Env = make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GOPROXY=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "FOXLAB_INSTALL_LOG="+logPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make install failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
