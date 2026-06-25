package lab

import "path/filepath"

func (l *Lab) Path() string {
	return l.path
}

func (l *Lab) Root() string {
	if l.root != "" {
		return l.root
	}
	return "."
}

func (l *Lab) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(l.Root(), path))
}
