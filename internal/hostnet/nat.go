package hostnet

import (
	"fmt"
	"hash/fnv"

	"foxlab-cli/internal/lab"
)

func externalNATGatewayCIDR(l *lab.Lab, link lab.ExternalLink) (string, string) {
	octet := 1 + int(hash32(l.ID+"|"+link.ID)%250)
	gateway := fmt.Sprintf("10.250.%d.1", octet)
	return gateway, fmt.Sprintf("10.250.%d.0/24", octet)
}

func externalNATContainerAddress(l *lab.Lab, link lab.ExternalLink, ct lab.Container, index int) string {
	octet := 1 + int(hash32(l.ID+"|"+link.ID)%250)
	host := 20 + int(hash32(ct.ID+"|"+fmt.Sprintf("%d", index))%200)
	return fmt.Sprintf("10.250.%d.%d/24", octet, host)
}

func switchNATGatewayCIDR(l *lab.Lab, sw lab.Switch) (string, string) {
	octet := 16 + int(hash32(l.ID+"/"+sw.ID)%200)
	gateway := fmt.Sprintf("172.31.%d.1", octet)
	return gateway, fmt.Sprintf("172.31.%d.0/24", octet)
}

func switchNATContainerAddress(l *lab.Lab, sw lab.Switch, ct lab.Container, index int) string {
	octet := 16 + int(hash32(l.ID+"/"+sw.ID)%200)
	host := 20 + int(hash32(ct.ID+"|"+fmt.Sprintf("%d", index))%80)
	return fmt.Sprintf("172.31.%d.%d/24", octet, host)
}

func hash32(value string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return h.Sum32()
}
