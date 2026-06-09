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
	path := firstNonEmpty(s.Path, s.Lab.Path())
	if path == "" {
		return fmt.Errorf("missing lab path")
	}
	if err := lab.SaveFile(path, s.Lab); err != nil {
		return err
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		return err
	}
	s.Lab = loaded
	return nil
}

func (s *Service) HasVM(id string) bool {
	return s.HasLabVM(id)
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
		if ct.ID == id {
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
		if vm.ID == id {
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
		if sw.ID == id {
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
		if link.ID == id {
			return link, true
		}
	}
	return lab.ExternalLink{}, false
}

func (s *Service) NextVMID() string {
	for i := s.nodeCount() + 1; ; i++ {
		id := fmt.Sprintf("vm%d", i)
		if !s.HasVM(id) {
			return id
		}
	}
}

func (s *Service) NextSwitchID() string {
	for i := s.nodeCount() + 1; ; i++ {
		id := fmt.Sprintf("sw%d", i)
		if !s.HasLabSwitch(id) {
			return id
		}
	}
}

func (s *Service) NextExternalID() string {
	for i := s.nodeCount() + 1; ; i++ {
		id := fmt.Sprintf("uplink%d", i)
		if !s.HasLabExternal(id) {
			return id
		}
	}
}

func (s *Service) NextContainerID() string {
	for i := s.nodeCount() + 1; ; i++ {
		id := fmt.Sprintf("ct%d", i)
		if !s.HasLabContainer(id) {
			return id
		}
	}
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
		if sw.ExternalLink == id {
			return sw.ID
		}
	}
	return ""
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
