package lab

import (
	"fmt"
	"sort"
	"strings"
)

var supportedContainerCapabilities = []string{
	"AUDIT_CONTROL",
	"AUDIT_READ",
	"AUDIT_WRITE",
	"BLOCK_SUSPEND",
	"BPF",
	"CHECKPOINT_RESTORE",
	"CHOWN",
	"DAC_OVERRIDE",
	"DAC_READ_SEARCH",
	"FOWNER",
	"FSETID",
	"IPC_LOCK",
	"IPC_OWNER",
	"KILL",
	"LEASE",
	"LINUX_IMMUTABLE",
	"MAC_ADMIN",
	"MAC_OVERRIDE",
	"MKNOD",
	"NET_ADMIN",
	"NET_BIND_SERVICE",
	"NET_BROADCAST",
	"NET_RAW",
	"PERFMON",
	"SETFCAP",
	"SETGID",
	"SETPCAP",
	"SETUID",
	"SYS_ADMIN",
	"SYS_BOOT",
	"SYS_CHROOT",
	"SYS_MODULE",
	"SYS_NICE",
	"SYS_PACCT",
	"SYS_PTRACE",
	"SYS_RAWIO",
	"SYS_RESOURCE",
	"SYS_TIME",
	"SYS_TTY_CONFIG",
	"SYSLOG",
	"WAKE_ALARM",
}

var defaultContainerCapabilities = []string{
	"CHOWN",
	"DAC_OVERRIDE",
	"FSETID",
	"FOWNER",
	"MKNOD",
	"NET_RAW",
	"SETGID",
	"SETUID",
	"SETFCAP",
	"SETPCAP",
	"NET_BIND_SERVICE",
	"SYS_CHROOT",
	"KILL",
	"AUDIT_WRITE",
}

var supportedContainerCapabilitySet = func() map[string]struct{} {
	out := make(map[string]struct{}, len(supportedContainerCapabilities))
	for _, capability := range supportedContainerCapabilities {
		out[capability] = struct{}{}
	}
	return out
}()

func SupportedContainerCapabilities() []string {
	return append([]string(nil), supportedContainerCapabilities...)
}

func DefaultContainerCapabilities() []string {
	return append([]string(nil), defaultContainerCapabilities...)
}

func NormalizeContainerCapability(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	return strings.TrimPrefix(value, "CAP_")
}

func IsSupportedContainerCapability(value string) bool {
	_, ok := supportedContainerCapabilitySet[NormalizeContainerCapability(value)]
	return ok
}

func EffectiveContainerCapabilities(ct Container) []string {
	enabled := make(map[string]bool, len(defaultContainerCapabilities))
	for _, capability := range defaultContainerCapabilities {
		enabled[capability] = true
	}
	if ct.Capabilities != nil {
		for _, capability := range ct.Capabilities.Add {
			enabled[NormalizeContainerCapability(capability)] = true
		}
		for _, capability := range ct.Capabilities.Drop {
			delete(enabled, NormalizeContainerCapability(capability))
		}
	}
	out := make([]string, 0, len(enabled))
	for capability := range enabled {
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
}

func ContainerCapabilityEnabled(ct Container, capability string) bool {
	capability = NormalizeContainerCapability(capability)
	for _, enabled := range EffectiveContainerCapabilities(ct) {
		if enabled == capability {
			return true
		}
	}
	return false
}

func normalizeContainerCapabilities(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		capability := NormalizeContainerCapability(value)
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
}

func validateContainerCapabilities(container string, capabilities *ContainerCapabilities) []string {
	if capabilities == nil {
		return nil
	}
	var problems []string
	add := map[string]struct{}{}
	for _, capability := range capabilities.Add {
		capability = NormalizeContainerCapability(capability)
		if !IsSupportedContainerCapability(capability) {
			problems = append(problems, fmt.Sprintf("container %q adds unsupported capability %q", container, capability))
			continue
		}
		add[capability] = struct{}{}
	}
	for _, capability := range capabilities.Drop {
		capability = NormalizeContainerCapability(capability)
		if !IsSupportedContainerCapability(capability) {
			problems = append(problems, fmt.Sprintf("container %q drops unsupported capability %q", container, capability))
			continue
		}
		if _, ok := add[capability]; ok {
			problems = append(problems, fmt.Sprintf("container %q cannot both add and drop capability %q", container, capability))
		}
	}
	return problems
}
