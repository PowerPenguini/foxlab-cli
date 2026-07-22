package lab

import (
	"fmt"
	"strings"
)

// Session owns the currently loaded lab together with its persistence path.
// Revision changes whenever consumers must treat the current lab state as new.
type Session struct {
	current  *Lab
	path     string
	revision uint64
}

func NewSession(current *Lab, path string) *Session {
	return &Session{current: current, path: strings.TrimSpace(path)}
}

func (s *Session) Current() *Lab {
	if s == nil {
		return nil
	}
	return s.current
}

func (s *Session) Path() string {
	if s == nil {
		return ""
	}
	if path := strings.TrimSpace(s.path); path != "" {
		return path
	}
	if s.current == nil {
		return ""
	}
	return s.current.Path()
}

func (s *Session) Revision() uint64 {
	if s == nil {
		return 0
	}
	return s.revision
}

func (s *Session) Replace(current *Lab) {
	if s == nil {
		return
	}
	s.current = current
	s.revision++
}

func (s *Session) SetPath(path string) {
	path = strings.TrimSpace(path)
	if s == nil || s.path == path {
		return
	}
	s.path = path
	s.revision++
}

func (s *Session) SaveAndReload() error {
	if s == nil || s.current == nil {
		return fmt.Errorf("missing loaded lab")
	}
	path := s.Path()
	if path == "" {
		return fmt.Errorf("missing lab path")
	}
	if err := SaveFile(path, s.current); err != nil {
		s.reload(path)
		return err
	}
	loaded, err := LoadFile(path)
	if err != nil {
		return err
	}
	s.Replace(loaded)
	return nil
}

func (s *Session) reload(path string) {
	loaded, err := LoadFile(path)
	if err != nil {
		return
	}
	s.Replace(loaded)
}
