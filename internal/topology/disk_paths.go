package topology

import (
	"fmt"
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
