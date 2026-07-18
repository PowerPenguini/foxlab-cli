package daemoncontrol

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"foxlab-cli/internal/foxruntime"
	"foxlab-cli/internal/lab"
)

type Status struct {
	Active  bool
	LabPath string
}

type ApplyRequest struct {
	LabPath           string
	LibvirtURI        string
	ContainerdAddress string
}

type Controller interface {
	Status(context.Context) (Status, error)
	Apply(context.Context, ApplyRequest) error
}

type systemdDaemonController struct {
	run        commandRunner
	configDir  string
	foxlabd    string
	destroyLab func(context.Context, string, string, *lab.Lab) error
}

type commandRunner func(context.Context, string, ...string) ([]byte, error)

func NewSystemdController() Controller {
	return &systemdDaemonController{
		run:        runCommand,
		destroyLab: foxruntime.DestroyLab,
	}
}

func (c *systemdDaemonController) Status(ctx context.Context) (Status, error) {
	active, err := c.active(ctx)
	if err != nil {
		return Status{}, err
	}
	path, err := c.configuredLabPath()
	if err != nil {
		return Status{}, err
	}
	return Status{Active: active, LabPath: path}, nil
}

func (c *systemdDaemonController) Apply(ctx context.Context, req ApplyRequest) error {
	labPath, err := filepath.Abs(req.LabPath)
	if err != nil {
		return err
	}
	status, err := c.Status(ctx)
	if err != nil {
		return fmt.Errorf("read foxlabd status: %w", err)
	}
	if status.Active && SameLabPath(status.LabPath, labPath) {
		return nil
	}
	c.stopUserDaemon(ctx)
	if err := c.stopDaemon(ctx); err != nil {
		return fmt.Errorf("stop foxlabd: %w", err)
	}
	if strings.TrimSpace(status.LabPath) != "" && !SameLabPath(status.LabPath, labPath) {
		if err := c.destroyPreviousLab(ctx, req, status.LabPath); err != nil {
			return fmt.Errorf("destroy previous lab %q: %w", status.LabPath, err)
		}
	}
	if err := c.writeDropIn(ctx, labPath); err != nil {
		return err
	}
	if err := c.ensureSystemUnit(ctx); err != nil {
		return fmt.Errorf("install foxlabd unit: %w", err)
	}
	if err := c.systemctl(ctx, "daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd units: %w", err)
	}
	if err := c.systemctl(ctx, "enable", "--now", "foxlabd.service"); err != nil {
		return fmt.Errorf("start foxlabd: %w", err)
	}
	return nil
}

func (c *systemdDaemonController) active(ctx context.Context) (bool, error) {
	out, err := c.systemctlOutput(ctx, "show", "foxlabd.service", "-p", "ActiveState", "--value")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "active", nil
}

func (c *systemdDaemonController) stopUserDaemon(ctx context.Context) {
	_ = c.userSystemctl(ctx, "disable", "--now", "foxlabd.service")
	_ = c.userSystemctl(ctx, "daemon-reload")
}

func (c *systemdDaemonController) stopDaemon(ctx context.Context) error {
	if err := c.systemctl(ctx, "stop", "foxlabd.service"); err != nil {
		if isSystemdUnitMissing(err) {
			return nil
		}
		return err
	}
	return nil
}

func (c *systemdDaemonController) destroyPreviousLab(ctx context.Context, req ApplyRequest, path string) error {
	if c.destroyLab != nil {
		oldLab, err := lab.LoadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		return c.destroyLab(ctx, req.LibvirtURI, req.ContainerdAddress, oldLab)
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	binary, err := c.foxlabdBinary()
	if err != nil {
		return err
	}
	args := []string{"--lab", path, "--destroy"}
	if strings.TrimSpace(req.LibvirtURI) != "" {
		args = append(args, "--uri", req.LibvirtURI)
	}
	if strings.TrimSpace(req.ContainerdAddress) != "" {
		args = append(args, "--containerd", req.ContainerdAddress)
	}
	_, err = c.privilegedCommand(ctx, binary, args...)
	return err
}

func (c *systemdDaemonController) ensureSystemUnit(ctx context.Context) error {
	if _, err := c.systemctlOutput(ctx, "cat", "foxlabd.service"); err == nil {
		return nil
	} else if !isSystemdUnitMissing(err) {
		return err
	}
	path, err := c.unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	binary, err := c.foxlabdBinary()
	if err != nil {
		return err
	}
	data := []byte("[Unit]\n" +
		"Description=FoxLab reconciliator\n\n" +
		"After=containerd.service libvirtd.service\n" +
		"Wants=containerd.service libvirtd.service\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"Environment=FOXLAB_LAB=/root/.foxlab/default.lab\n" +
		"Environment=FOXLAB_STATUS_SOCKET=/run/foxlab/foxlabd.sock\n" +
		"ExecStart=" + systemdExecPath(binary) + " --lab ${FOXLAB_LAB} --status-socket ${FOXLAB_STATUS_SOCKET}\n" +
		"Restart=on-failure\n" +
		"RestartSec=2s\n\n" +
		"[Install]\n" +
		"WantedBy=multi-user.target\n")
	return c.writeRootFile(ctx, path, data, 0o644)
}

func isSystemdUnitMissing(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unit foxlabd.service not loaded") ||
		strings.Contains(text, "foxlabd.service not loaded") ||
		strings.Contains(text, "unit foxlabd.service could not be found") ||
		strings.Contains(text, "foxlabd.service could not be found") ||
		strings.Contains(text, "unit foxlabd.service does not exist") ||
		strings.Contains(text, "foxlabd.service does not exist") ||
		strings.Contains(text, "no files found for foxlabd.service")
}

func (c *systemdDaemonController) configuredLabPath() (string, error) {
	path, err := c.dropInPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if labPath := parseDropInLabPath(string(data)); labPath != "" {
			return filepath.Abs(labPath)
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return defaultLabPathNoCreate()
}

func (c *systemdDaemonController) writeDropIn(ctx context.Context, labPath string) error {
	path, err := c.dropInPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	statusSocket, err := UserStatusSocketPath()
	if err != nil {
		return err
	}
	env := []string{
		"FOXLAB_LAB=" + labPath,
		"FOXLAB_STATUS_SOCKET=" + statusSocket,
	}
	if home, userName := serviceUserHomeAndName(); home != "" {
		env = append(env, "HOME="+home)
		if userName != "" {
			env = append(env, "SUDO_USER="+userName)
		}
	}
	data := []byte("[Service]\n")
	for _, value := range env {
		data = append(data, []byte("Environment="+strconv.Quote(value)+"\n")...)
	}
	return c.writeRootFile(ctx, path, data, 0o644)
}

func (c *systemdDaemonController) dropInPath() (string, error) {
	dir, err := c.systemUnitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "foxlabd.service.d", "lab.conf"), nil
}

func (c *systemdDaemonController) unitPath() (string, error) {
	dir, err := c.systemUnitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "foxlabd.service"), nil
}

func (c *systemdDaemonController) systemUnitDir() (string, error) {
	configDir := c.configDir
	if configDir == "" {
		return "/etc/systemd/system", nil
	}
	return filepath.Join(configDir, "systemd", "system"), nil
}

func (c *systemdDaemonController) systemctl(ctx context.Context, args ...string) error {
	_, err := c.systemctlOutput(ctx, args...)
	return err
}

func (c *systemdDaemonController) systemctlOutput(ctx context.Context, args ...string) ([]byte, error) {
	return c.privilegedCommand(ctx, "systemctl", args...)
}

func (c *systemdDaemonController) userSystemctl(ctx context.Context, args ...string) error {
	_, err := c.command(ctx, "systemctl", append([]string{"--user"}, args...)...)
	return err
}

func (c *systemdDaemonController) privilegedCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.run != nil || os.Geteuid() == 0 {
		return c.command(ctx, name, args...)
	}
	return c.command(ctx, "sudo", append([]string{name}, args...)...)
}

func (c *systemdDaemonController) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	run := c.run
	if run == nil {
		run = runCommand
	}
	return run(ctx, name, args...)
}

func parseDropInLabPath(data string) string {
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Environment=") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "Environment="))
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		if labPath, ok := strings.CutPrefix(value, "FOXLAB_LAB="); ok {
			return strings.TrimSpace(labPath)
		}
		for _, field := range strings.Fields(value) {
			if labPath, ok := strings.CutPrefix(field, "FOXLAB_LAB="); ok {
				return strings.TrimSpace(labPath)
			}
		}
	}
	return ""
}

func defaultLabPathNoCreate() (string, error) {
	home, err := lab.FoxlabHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "default.lab"), nil
}

func UserStatusSocketPath() (string, error) {
	home, err := lab.FoxlabHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "run", "foxlabd.sock"), nil
}

func serviceUserHomeAndName() (string, string) {
	home, err := lab.FoxlabHome()
	if err != nil {
		return "", ""
	}
	userHome := filepath.Dir(home)
	userName := strings.TrimSpace(os.Getenv("SUDO_USER"))
	if userName == "" || userName == "root" {
		userName = strings.TrimSpace(os.Getenv("USER"))
	}
	if userName == "root" {
		userName = ""
	}
	return userHome, userName
}

func foxlabUserConfigDir() (string, error) {
	if configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); configHome != "" {
		return configHome, nil
	}
	foxlabHome, err := lab.FoxlabHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(foxlabHome), ".config"), nil
}

func (c *systemdDaemonController) writeRootFile(ctx context.Context, path string, data []byte, mode os.FileMode) error {
	if c.run == nil && os.Geteuid() != 0 {
		tmp, err := os.CreateTemp("", "foxlabd-root-file-*")
		if err != nil {
			return err
		}
		tmpPath := tmp.Name()
		_, writeErr := tmp.Write(data)
		closeErr := tmp.Close()
		if writeErr != nil {
			_ = os.Remove(tmpPath)
			return writeErr
		}
		if closeErr != nil {
			_ = os.Remove(tmpPath)
			return closeErr
		}
		defer os.Remove(tmpPath)
		_, err = c.privilegedCommand(ctx, "install", "-D", "-m", fmt.Sprintf("%04o", mode.Perm()), tmpPath, path)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

func SameLabPath(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == "" || right == "" {
		return false
	}
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr == nil {
		left = leftAbs
	}
	if rightErr == nil {
		right = rightAbs
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func (c *systemdDaemonController) foxlabdBinary() (string, error) {
	if strings.TrimSpace(c.foxlabd) != "" {
		return filepath.Abs(c.foxlabd)
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		sibling := filepath.Join(filepath.Dir(exe), "foxlabd")
		if isExecutableFile(sibling) {
			return sibling, nil
		}
	}
	if path, err := exec.LookPath("foxlabd"); err == nil {
		return filepath.Abs(path)
	}
	return "", fmt.Errorf("foxlabd binary not found next to foxlab or in PATH")
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

func systemdExecPath(path string) string {
	path = filepath.Clean(path)
	if strings.ContainsAny(path, " \t") {
		return strconv.Quote(path)
	}
	return path
}

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail != "" {
			return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, detail)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}
