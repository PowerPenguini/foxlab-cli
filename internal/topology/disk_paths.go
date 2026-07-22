package topology

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func moveDiskFile(source, destination string) error {
	if err := os.Link(source, destination); err == nil {
		if err := os.Remove(source); err != nil {
			_ = os.Remove(destination)
			return err
		}
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	removeDestination := true
	defer func() {
		_ = output.Close()
		if removeDestination {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	if err := os.Remove(source); err != nil {
		return err
	}
	removeDestination = false
	return nil
}

type DiskCommandRunner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

type execDiskCommandRunner struct{}

func (execDiskCommandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, text)
	}
	return nil
}

func (execDiskCommandRunner) Output(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return out, err
		}
		return out, fmt.Errorf("%w: %s", err, text)
	}
	return out, nil
}

type DiskCommandFuncs struct {
	RunFunc    func(name string, args ...string) error
	OutputFunc func(name string, args ...string) ([]byte, error)
}

func (f DiskCommandFuncs) Run(name string, args ...string) error {
	if f.RunFunc == nil {
		return execDiskCommandRunner{}.Run(name, args...)
	}
	return f.RunFunc(name, args...)
}

func (f DiskCommandFuncs) Output(name string, args ...string) ([]byte, error) {
	if f.OutputFunc == nil {
		return execDiskCommandRunner{}.Output(name, args...)
	}
	return f.OutputFunc(name, args...)
}

func ensureDiskDirectoryWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".foxlab-write-check-*")
	if err != nil {
		return fmt.Errorf("disk storage directory is not writable: %s: %w", dir, err)
	}
	name := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return fmt.Errorf("disk storage directory write check failed: %s: %w", dir, closeErr)
	}
	if removeErr != nil {
		return fmt.Errorf("disk storage directory cleanup failed: %s: %w", dir, removeErr)
	}
	return nil
}

func reserveDiskPath(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("disk path already exists: %s", path)
		}
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func (s *Service) nextLayerID(baseID string) string {
	base := baseID + "-layer"
	if _, exists := s.diskByID(base); !exists {
		return base
	}
	for i := 2; ; i++ {
		id := base + "-" + strconv.Itoa(i)
		if _, exists := s.diskByID(id); !exists {
			return id
		}
	}
}

func (s *Service) diskHasLayers(baseID string) bool {
	for _, disk := range s.CurrentLab().Disks {
		if disk.Base == baseID && diskKind(disk) == "layer" {
			return true
		}
	}
	return false
}

func (s *Service) layerStoragePath(layerID string) (string, error) {
	root, err := s.CurrentLab().StorageRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "layers", layerID+".qcow2"), nil
}
