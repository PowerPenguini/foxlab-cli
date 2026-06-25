package lab

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadFile(path string) (*Lab, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var l Lab
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&l); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	l.path = abs
	l.root = filepath.Dir(abs)
	l.Normalize()
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &l, nil
}

func SaveFile(path string, l *Lab) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	copy := *l
	copy.path = abs
	copy.root = filepath.Dir(abs)
	copy.Normalize()
	if err := copy.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(&copy)
	if err != nil {
		return err
	}
	return os.WriteFile(abs, data, 0o644)
}

func ListFiles(workspace string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(workspace, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != workspace {
				return filepath.SkipDir
			}
			return nil
		}
		if isLabFile(path) {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func isLabFile(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == FileExtension
}
