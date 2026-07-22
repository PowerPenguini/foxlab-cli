package topology

import "foxlab-cli/internal/lab"

func (s *Service) ContainerCapabilitySet(ref, capability string, enabled bool) Result {
	if s.CurrentLab() == nil {
		return Failure("container capability needs a loaded .lab file")
	}
	capability = lab.NormalizeContainerCapability(capability)
	if !lab.IsSupportedContainerCapability(capability) {
		return Failure("unsupported container capability: " + capability)
	}
	id, ok := s.resolveContainerID(ref)
	if !ok {
		return Failure("container not found: " + ref)
	}
	for i := range s.CurrentLab().Containers {
		ct := &s.CurrentLab().Containers[i]
		if ct.ID != id {
			continue
		}
		if lab.ContainerCapabilityEnabled(*ct, capability) == enabled {
			return Info(containerCapabilityMessage(s.workloadDisplayRef("container", id), capability, enabled))
		}
		if err := s.requireSavePath(); err != nil {
			return FailureWithCause("container capability failed: "+err.Error(), err)
		}
		mutation := s.beginLabMutation()
		if ct.Capabilities == nil {
			ct.Capabilities = &lab.ContainerCapabilities{}
		}
		defaultEnabled := stringSliceContains(lab.DefaultContainerCapabilities(), capability)
		ct.Capabilities.Add = stringSliceWithout(ct.Capabilities.Add, capability)
		ct.Capabilities.Drop = stringSliceWithout(ct.Capabilities.Drop, capability)
		switch {
		case enabled && !defaultEnabled:
			ct.Capabilities.Add = append(ct.Capabilities.Add, capability)
		case !enabled && defaultEnabled:
			ct.Capabilities.Drop = append(ct.Capabilities.Drop, capability)
		}
		if len(ct.Capabilities.Add) == 0 && len(ct.Capabilities.Drop) == 0 {
			ct.Capabilities = nil
		}
		if err := mutation.Commit(); err != nil {
			return FailureWithCause("container capability failed: "+err.Error(), err)
		}
		return ChangedInfo(containerCapabilityMessage(s.workloadDisplayRef("container", id), capability, enabled) + "; runtime will be recreated")
	}
	return Failure("container not found: " + id)
}

func containerCapabilityMessage(ref, capability string, enabled bool) string {
	if enabled {
		return "enabled " + capability + " for " + ref
	}
	return "disabled " + capability + " for " + ref
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if lab.NormalizeContainerCapability(value) == want {
			return true
		}
	}
	return false
}

func stringSliceWithout(values []string, remove string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = lab.NormalizeContainerCapability(value)
		if value != "" && value != remove {
			out = append(out, value)
		}
	}
	return out
}
