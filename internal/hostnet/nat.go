package hostnet

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"

	"foxlab-cli/internal/lab"
)

func externalNATGatewayCIDR(l *lab.Lab, link lab.ExternalLink) (string, string) {
	octet := 1 + int(hash32(l.ID+"|"+link.ID)%250)
	gateway := fmt.Sprintf("10.250.%d.1", octet)
	return gateway, fmt.Sprintf("10.250.%d.0/24", octet)
}

func externalNATContainerAddress(l *lab.Lab, link lab.ExternalLink, ct lab.Container, index int) (string, error) {
	octet := 1 + int(hash32(l.ID+"|"+link.ID)%250)
	host, err := natContainerHost(l, ct, index, 200, func(nic lab.ContainerNetwork) bool {
		return nic.ExternalLink == link.ID
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("10.250.%d.%d/24", octet, host), nil
}

func switchNATGatewayCIDR(l *lab.Lab, sw lab.Switch) (string, string) {
	octet := 16 + int(hash32(l.ID+"/"+sw.ID)%200)
	gateway := fmt.Sprintf("172.31.%d.1", octet)
	return gateway, fmt.Sprintf("172.31.%d.0/24", octet)
}

func switchNATContainerAddress(l *lab.Lab, sw lab.Switch, ct lab.Container, index int) (string, error) {
	octet := 16 + int(hash32(l.ID+"/"+sw.ID)%200)
	host, err := natContainerHost(l, ct, index, 80, func(nic lab.ContainerNetwork) bool {
		return nic.Switch == sw.ID
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("172.31.%d.%d/24", octet, host), nil
}

func natContainerHost(l *lab.Lab, target lab.Container, targetIndex, slots int, matches func(lab.ContainerNetwork) bool) (int, error) {
	targetKey := natContainerEndpointKey(target.ID, targetIndex)
	keys := []string{targetKey}
	seen := map[string]struct{}{targetKey: {}}
	if l != nil {
		for _, ct := range l.Containers {
			for index, nic := range ct.Networks {
				if !matches(nic) {
					continue
				}
				key := natContainerEndpointKey(ct.ID, index)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				keys = append(keys, key)
			}
		}
	}
	if len(keys) > slots {
		return 0, fmt.Errorf("NAT network supports at most %d container interfaces", slots)
	}
	sort.Strings(keys)
	used := make(map[int]struct{}, min(len(keys), slots))
	for _, key := range keys {
		preferred := int(hash32(key) % uint32(slots))
		assigned := preferred
		for probe := 0; probe < slots; probe++ {
			candidate := (preferred + probe) % slots
			if _, exists := used[candidate]; exists {
				continue
			}
			assigned = candidate
			used[candidate] = struct{}{}
			break
		}
		if key == targetKey {
			return 20 + assigned, nil
		}
	}
	return 20 + int(hash32(targetKey)%uint32(slots)), nil
}

func natContainerEndpointKey(id string, index int) string {
	return id + "|" + strconv.Itoa(index)
}

func hash32(value string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return h.Sum32()
}
