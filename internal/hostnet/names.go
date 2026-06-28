package hostnet

import (
	"fmt"
	"hash/fnv"

	"foxlab-cli/internal/lab"
)

func VMTapName(l *lab.Lab, vm lab.VM, index int) string {
	return ifName("t", l.ManagedDomainName(vm), index)
}

func vethNames(l *lab.Lab, ct lab.Container, index int) (string, string) {
	base := cleanPrefix(l.ManagedContainerName(ct))
	return ifName("v", base, index), ifName("p", base, index)
}

func ifName(prefix, base string, index int) string {
	const (
		maxLinuxIfName = 15
		hashLen        = 6
	)
	suffix := fmt.Sprintf("%d", index)
	maxSuffix := maxLinuxIfName - len(prefix) - 1
	if maxSuffix < 1 {
		maxSuffix = 1
	}
	if len(suffix) > maxSuffix {
		hash := fmt.Sprintf("%06x", ifNameHash(suffix)&0xffffff)
		stemLen := maxSuffix - hashLen
		if stemLen < 1 {
			stemLen = 1
			hash = hash[:maxSuffix-stemLen]
		}
		suffix = suffix[:stemLen] + hash
	}
	maxBase := maxLinuxIfName - len(prefix) - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	base = cleanPrefix(base)
	if len(base) <= maxBase {
		return prefix + base + suffix
	}
	hash := fmt.Sprintf("%06x", ifNameHash(base)&0xffffff)
	stemLen := maxBase - hashLen
	if stemLen < 1 {
		stemLen = 1
		hash = hash[:maxBase-stemLen]
	}
	return prefix + base[:stemLen] + hash + suffix
}

func cleanPrefix(base string) string {
	clean := ""
	for _, ch := range base {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			clean += string(ch)
		}
	}
	if clean == "" {
		return "foxlab"
	}
	return clean
}

func ifNameHash(value string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return h.Sum32()
}
