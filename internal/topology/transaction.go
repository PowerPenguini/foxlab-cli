package topology

import (
	"fmt"

	"foxlab-cli/internal/lab"
)

type labMutation struct {
	service  *Service
	snapshot *lab.Lab
}

func (s *Service) beginLabMutation() *labMutation {
	return &labMutation{service: s, snapshot: lab.Clone(s.CurrentLab())}
}

func (m *labMutation) Commit() error {
	if m == nil || m.service == nil {
		return fmt.Errorf("missing lab mutation")
	}
	if err := m.service.SaveAndRefresh(); err != nil {
		m.Rollback()
		return err
	}
	return nil
}

func (m *labMutation) Rollback() {
	if m == nil || m.service == nil || m.snapshot == nil {
		return
	}
	m.service.ReplaceLab(m.snapshot)
}

func (s *Service) mutateLab(apply func(*lab.Lab) error) error {
	if s.CurrentLab() == nil {
		return fmt.Errorf("missing loaded lab")
	}
	if apply == nil {
		return fmt.Errorf("missing lab mutation")
	}
	if err := s.requireSavePath(); err != nil {
		return err
	}
	mutation := s.beginLabMutation()
	if err := apply(s.CurrentLab()); err != nil {
		mutation.Rollback()
		return err
	}
	return mutation.Commit()
}
