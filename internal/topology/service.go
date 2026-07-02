package topology

import (
	"fmt"
	"strings"

	"foxlab-cli/internal/lab"
)

type Service struct {
	Lab    *lab.Lab
	Path   string
	States map[string]string
}

func NewService(loadedLab *lab.Lab, path string) *Service {
	return &Service{
		Lab:  loadedLab,
		Path: path,
	}
}

func (s *Service) SaveAndRefresh() error {
	if s.Lab == nil {
		return fmt.Errorf("missing loaded lab")
	}
	path := s.savePath()
	if path == "" {
		return fmt.Errorf("missing lab path")
	}
	if err := lab.SaveFile(path, s.Lab); err != nil {
		s.reloadAfterFailedSave(path)
		return err
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		return err
	}
	s.Lab = loaded
	return nil
}

func (s *Service) saveAndRefreshWithRollback(snapshot *lab.Lab) error {
	if err := s.SaveAndRefresh(); err != nil {
		if snapshot != nil {
			s.Lab = snapshot
		}
		return err
	}
	return nil
}

func (s *Service) reloadAfterFailedSave(path string) {
	loaded, err := lab.LoadFile(path)
	if err != nil {
		return
	}
	s.Lab = loaded
}

func (s *Service) savePath() string {
	if s.Lab == nil {
		return ""
	}
	return firstNonEmpty(s.Path, s.Lab.Path())
}

func (s *Service) requireSavePath() error {
	if s.savePath() == "" {
		return fmt.Errorf("missing lab path")
	}
	return nil
}

func (s *Service) HasVM(id string) bool {
	return s.HasLabVM(id)
}

func (s *Service) existingNodeKind(id string) string {
	switch {
	case s.HasLabSwitch(id):
		return "switch"
	case s.HasLabExternal(id):
		return "uplink"
	case s.HasLabVM(id):
		return "vm"
	case s.HasLabContainer(id):
		return "container"
	default:
		return ""
	}
}

func (s *Service) HasLabVM(id string) bool {
	_, ok := s.LabVM(id)
	return ok
}

func (s *Service) HasLabContainer(id string) bool {
	_, ok := s.LabContainer(id)
	return ok
}

func (s *Service) LabContainer(id string) (lab.Container, bool) {
	if s.Lab == nil {
		return lab.Container{}, false
	}
	for _, ct := range s.Lab.Containers {
		if ct.ID == id || ct.Name == id {
			return ct, true
		}
	}
	return lab.Container{}, false
}

func (s *Service) LabVM(id string) (lab.VM, bool) {
	if s.Lab == nil {
		return lab.VM{}, false
	}
	for _, vm := range s.Lab.VMs {
		if vm.ID == id || vm.Name == id {
			return vm, true
		}
	}
	return lab.VM{}, false
}

func (s *Service) HasLabSwitch(id string) bool {
	_, ok := s.LabSwitch(id)
	return ok
}

func (s *Service) LabSwitch(id string) (lab.Switch, bool) {
	if s.Lab == nil {
		return lab.Switch{}, false
	}
	for _, sw := range s.Lab.Switches {
		if sw.ID == id || sw.Name == id {
			return sw, true
		}
	}
	return lab.Switch{}, false
}

func (s *Service) HasLabExternal(id string) bool {
	_, ok := s.LabExternal(id)
	return ok
}

func (s *Service) LabExternal(id string) (lab.ExternalLink, bool) {
	if s.Lab == nil {
		return lab.ExternalLink{}, false
	}
	for _, link := range s.Lab.ExternalLinks {
		if link.ID == id || link.Name == id {
			return link, true
		}
	}
	return lab.ExternalLink{}, false
}

func (s *Service) NextVMID() string {
	return s.nextNodeName("vm")
}

func (s *Service) NextSwitchID() string {
	return s.nextNodeName("switch")
}

func (s *Service) NextExternalID() string {
	return s.nextNodeName("uplink")
}

func (s *Service) NextContainerID() string {
	return s.nextNodeName("container")
}

func (s *Service) FirstExternalID() string {
	if s.Lab == nil || len(s.Lab.ExternalLinks) == 0 {
		return ""
	}
	return s.Lab.ExternalLinks[0].ID
}

func (s *Service) FirstSwitchID() string {
	if s.Lab == nil || len(s.Lab.Switches) == 0 {
		return ""
	}
	return s.Lab.Switches[0].ID
}

func (s *Service) SwitchForExternal(id string) string {
	if s.Lab == nil {
		return ""
	}
	for _, sw := range s.Lab.Switches {
		if lab.SwitchHasExternalLink(sw, id) {
			return sw.ID
		}
	}
	return ""
}

func (s *Service) VMDesiredState(ref, state string) string {
	if s.Lab == nil {
		return "vm state needs a loaded .lab file"
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return "vm not found: " + ref
	}
	state = lab.DesiredState(state)
	if state != lab.DesiredStateRunning && state != lab.DesiredStateStopped {
		return "unsupported vm desired state: " + state
	}
	for i := range s.Lab.VMs {
		if s.Lab.VMs[i].ID != id {
			continue
		}
		if err := s.requireSavePath(); err != nil {
			return "desired state failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		s.Lab.VMs[i].DesiredState = state
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "desired state failed: " + err.Error()
		}
		return "desired vm:" + id + " " + state
	}
	return "vm not found: " + id
}

func (s *Service) ContainerDesiredState(ref, state string) string {
	if s.Lab == nil {
		return "container state needs a loaded .lab file"
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return "container not found: " + ref
	}
	state = lab.DesiredState(state)
	if state != lab.DesiredStateRunning && state != lab.DesiredStateStopped {
		return "unsupported container desired state: " + state
	}
	for i := range s.Lab.Containers {
		if s.Lab.Containers[i].ID != id {
			continue
		}
		if err := s.requireSavePath(); err != nil {
			return "desired state failed: " + err.Error()
		}
		snapshot := lab.Clone(s.Lab)
		s.Lab.Containers[i].DesiredState = state
		if err := s.saveAndRefreshWithRollback(snapshot); err != nil {
			return "desired state failed: " + err.Error()
		}
		return "desired container:" + id + " " + state
	}
	return "container not found: " + id
}

func (s *Service) nodeCount() int {
	if s.Lab == nil {
		return 0
	}
	return len(s.Lab.VMs) + len(s.Lab.Containers) + len(s.Lab.Switches) + len(s.Lab.ExternalLinks)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
