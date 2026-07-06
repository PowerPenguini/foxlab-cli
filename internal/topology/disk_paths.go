package topology

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var runDiskCommand = func(name string, args ...string) error {
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

var runDiskCommandOutput = func(name string, args ...string) ([]byte, error) {
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
	for _, disk := range s.Lab.Disks {
		if disk.Base == baseID && diskKind(disk) == "layer" {
			return true
		}
	}
	return false
}

func (s *Service) layerStoragePath(layerID string) (string, error) {
	root, err := s.Lab.StorageRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "layers", layerID+".qcow2"), nil
}
