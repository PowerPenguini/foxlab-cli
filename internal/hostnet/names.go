package hostnet

import (
	"fmt"

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
	const maxLinuxIfName = 15
	suffix := fmt.Sprintf("%d", index)
	maxBase := maxLinuxIfName - len(prefix) - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	base = cleanPrefix(base)
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return prefix + base + suffix
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
