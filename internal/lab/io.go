package lab

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("lab file %q contains multiple YAML documents", path)
	} else if err != io.EOF {
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
	if l == nil {
		return fmt.Errorf("missing lab")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	copy := Clone(l)
	copy.path = abs
	copy.root = filepath.Dir(abs)
	copy.Normalize()
	if err := copy.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(copy)
	if err != nil {
		return err
	}
	return writeFileAtomic(abs, data, 0o644)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	var owner *syscall.Stat_t
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			owner = stat
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if owner != nil && os.Geteuid() == 0 {
		if err := tmp.Chown(int(owner.Uid), int(owner.Gid)); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	cleanup = false
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}
	return nil
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
