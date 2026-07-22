package topology

import (
	"fmt"
	"strings"

	"foxlab-cli/internal/lab"
)

type Service struct {
	session         *lab.Session
	States          map[string]string
	StatesConfirmed bool
	DiskCommands    DiskCommandRunner
}

func NewService(loadedLab *lab.Lab, path string) *Service {
	return NewServiceWithSession(lab.NewSession(loadedLab, path))
}

func NewServiceWithSession(session *lab.Session) *Service {
	if session == nil {
		session = lab.NewSession(nil, "")
	}
	return &Service{
		session:      session,
		DiskCommands: execDiskCommandRunner{},
	}
}

func (s *Service) CurrentLab() *lab.Lab {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.Current()
}

func (s *Service) LabPath() string {
	if s == nil || s.session == nil {
		return ""
	}
	return s.session.Path()
}

func (s *Service) ReplaceLab(current *lab.Lab) {
	if s == nil {
		return
	}
	s.ensureSession().Replace(current)
}

func (s *Service) SetLabPath(path string) {
	if s == nil {
		return
	}
	s.ensureSession().SetPath(path)
}

func (s *Service) ensureSession() *lab.Session {
	if s.session == nil {
		s.session = lab.NewSession(nil, "")
	}
	return s.session
}

func (s *Service) diskCommands() DiskCommandRunner {
	if s.DiskCommands == nil {
		s.DiskCommands = execDiskCommandRunner{}
	}
	return s.DiskCommands
}

func (s *Service) SaveAndRefresh() error {
	return s.ensureSession().SaveAndReload()
}

func (s *Service) savePath() string {
	return s.LabPath()
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
	if s.CurrentLab() == nil {
		return lab.Container{}, false
	}
	for _, ct := range s.CurrentLab().Containers {
		if ct.ID == id || ct.Name == id {
			return ct, true
		}
	}
	return lab.Container{}, false
}

func (s *Service) LabVM(id string) (lab.VM, bool) {
	if s.CurrentLab() == nil {
		return lab.VM{}, false
	}
	for _, vm := range s.CurrentLab().VMs {
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
	if s.CurrentLab() == nil {
		return lab.Switch{}, false
	}
	for _, sw := range s.CurrentLab().Switches {
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
	if s.CurrentLab() == nil {
		return lab.ExternalLink{}, false
	}
	for _, link := range s.CurrentLab().ExternalLinks {
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
	if s.CurrentLab() == nil || len(s.CurrentLab().ExternalLinks) == 0 {
		return ""
	}
	return s.CurrentLab().ExternalLinks[0].ID
}

func (s *Service) FirstSwitchID() string {
	if s.CurrentLab() == nil || len(s.CurrentLab().Switches) == 0 {
		return ""
	}
	return s.CurrentLab().Switches[0].ID
}

func (s *Service) SwitchForExternal(id string) string {
	if s.CurrentLab() == nil {
		return ""
	}
	for _, sw := range s.CurrentLab().Switches {
		if lab.SwitchHasExternalLink(sw, id) {
			return sw.ID
		}
	}
	return ""
}

func (s *Service) VMDesiredState(ref, state string) Result {
	if s.CurrentLab() == nil {
		return Failure("vm state needs a loaded .lab file")
	}
	id, ok := s.resolveVMID(ref)
	if !ok {
		return Failure("vm not found: " + ref)
	}
	state = lab.DesiredState(state)
	if state != lab.DesiredStateRunning && state != lab.DesiredStateStopped {
		return Failure("unsupported vm desired state: " + state)
	}
	for i := range s.CurrentLab().VMs {
		if s.CurrentLab().VMs[i].ID != id {
			continue
		}
		if err := s.mutateLab(func(current *lab.Lab) error {
			current.VMs[i].DesiredState = state
			return nil
		}); err != nil {
			return FailureWithCause("desired state failed: "+err.Error(), err)
		}
		return Success("desired " + s.workloadDisplayRef("vm", id) + " " + state)
	}
	return Failure("vm not found: " + id)
}

func (s *Service) ContainerDesiredState(ref, state string) Result {
	if s.CurrentLab() == nil {
		return Failure("container state needs a loaded .lab file")
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	state = lab.DesiredState(state)
	if state != lab.DesiredStateRunning && state != lab.DesiredStateStopped {
		return Failure("unsupported container desired state: " + state)
	}
	for i := range s.CurrentLab().Containers {
		if s.CurrentLab().Containers[i].ID != id {
			continue
		}
		if err := s.mutateLab(func(current *lab.Lab) error {
			current.Containers[i].DesiredState = state
			return nil
		}); err != nil {
			return FailureWithCause("desired state failed: "+err.Error(), err)
		}
		return Success("desired " + s.workloadDisplayRef("container", id) + " " + state)
	}
	return Failure("container not found: " + id)
}

func (s *Service) nodeCount() int {
	if s.CurrentLab() == nil {
		return 0
	}
	return len(s.CurrentLab().VMs) + len(s.CurrentLab().Containers) + len(s.CurrentLab().Switches) + len(s.CurrentLab().ExternalLinks)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Service) nodeDisplayName(kind, id string) string {
	switch kind {
	case "vm":
		if vm, ok := s.LabVM(id); ok {
			return firstNonEmpty(vm.Name, vm.ID)
		}
	case "container":
		if ct, ok := s.LabContainer(id); ok {
			return firstNonEmpty(ct.Name, ct.ID)
		}
	case "switch":
		if sw, ok := s.LabSwitch(id); ok {
			return firstNonEmpty(sw.Name, sw.ID)
		}
	case "uplink", "external":
		if link, ok := s.LabExternal(id); ok {
			return firstNonEmpty(link.Name, link.ID)
		}
	}
	return id
}

func (s *Service) workloadDisplayRef(kind, id string) string {
	return kind + ":" + s.nodeDisplayName(kind, id)
}
