package lab

import (
	"fmt"
	"path/filepath"
)

func DefaultPath() (string, bool, error) {
	dir, err := FoxlabHome()
	if err != nil {
		return "", false, err
	}
	path := filepath.Join(dir, "default.lab")
	if path, ok := regularFile(path); ok {
		return path, true, nil
	}
	if existingNonRegularFile(path) {
		return "", false, fmt.Errorf("default lab path %q is not a regular file", path)
	}
	if err := SaveFile(path, &Lab{ID: "default"}); err != nil {
		return "", false, fmt.Errorf("create default lab: %w", err)
	}
	return path, true, nil
}
