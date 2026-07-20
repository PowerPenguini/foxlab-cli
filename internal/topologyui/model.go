package topologyui

import (
	"fmt"
	"sort"
	"strings"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/tui/graph"
)

const (
	NodeVM        = "vm"
	NodeContainer = "container"
	NodeSwitch    = "switch"
	NodeExternal  = "external"
)

type Model = graph.Model
type Node = graph.Node
type Edge = graph.Edge

func MockModel() Model {
	return Model{
		ID: "mock",
		Nodes: []Node{
			{ID: "router", Type: NodeVM, Badge: "VM", Label: "router", State: "running", DesiredState: lab.DesiredStateRunning, X: 4, Y: 3, Details: []string{"cpu=2", "mem=2048M", "vnc=true", "disk=labs/mock/disks/router.img", "nic0 → edge"}},
			{ID: "client01", Type: NodeVM, Badge: "VM", Label: "client01", State: "defined", X: 4, Y: 11, Details: []string{"cpu=2", "mem=2048M", "vnc=false", "disk=labs/mock/disks/client01.img", "nic0 → lan"}},
			{ID: "web", Type: NodeContainer, Badge: "CT", Label: "web", State: "missing", X: 4, Y: 19, Details: []string{"image=docker.io/library/nginx:latest", "nic0 → lan"}},
			{ID: "edge", Type: NodeSwitch, Badge: "SW", Label: "edge", State: "bridge", X: 28, Y: 5, Details: []string{"mode=macnat-bridge", "uplink=uplink0"}},
			{ID: "lan", Type: NodeSwitch, Badge: "SW", Label: "lan", State: "nat", X: 28, Y: 13, Details: []string{"mode=nat", "dhcp=on"}},
			{ID: "uplink0", Type: NodeExternal, Badge: "UP", Label: "wlp0s20f3", State: lab.ExternalModeNAT, X: 52, Y: 5, Details: []string{"interface=wlp0s20f3", "mode=nat"}},
			{ID: "hostnet", Type: NodeExternal, Badge: "UP", Label: "br0", State: lab.ExternalModeDirect, X: 52, Y: 13, Details: []string{"interface=br0", "mode=direct"}},
		},
		Edges: []Edge{
			{From: NodeKey(NodeVM, "router"), To: NodeKey(NodeSwitch, "edge")},
			{From: NodeKey(NodeVM, "client01"), To: NodeKey(NodeSwitch, "lan")},
			{From: NodeKey(NodeSwitch, "edge"), To: NodeKey(NodeExternal, "uplink0")},
			{From: NodeKey(NodeSwitch, "lan"), To: NodeKey(NodeExternal, "hostnet")},
			{From: NodeKey(NodeVM, "router"), To: NodeKey(NodeSwitch, "lan")},
			{From: NodeKey(NodeContainer, "web"), To: NodeKey(NodeSwitch, "lan")},
		},
	}
}

func ModelFromLab(l *lab.Lab) Model {
	if l == nil {
		return MockModel()
	}
	m := Model{ID: l.ID}
	nodeNames := labNodeNames(l)
	directNICDetails := map[string]string{}
	for _, link := range l.NetworkLinks {
		fromKey := networkEndpointKey(link.From)
		toKey := networkEndpointKey(link.To)
		directNICDetails[fromKey] = fmt.Sprintf("nic%d ↔ %s:nic%d", link.From.NIC, displayNodeRef(nodeNames, link.To.Type, link.To.ID), link.To.NIC)
		directNICDetails[toKey] = fmt.Sprintf("nic%d ↔ %s:nic%d", link.To.NIC, displayNodeRef(nodeNames, link.From.Type, link.From.ID), link.From.NIC)
		m.Edges = append(m.Edges, Edge{From: NodeKey(link.From.Type, link.From.ID), To: NodeKey(link.To.Type, link.To.ID)})
	}
	for i, vm := range l.VMs {
		details := []string{
			fmt.Sprintf("cpu=%d", vm.CPUs),
			fmt.Sprintf("mem=%dM", vm.MemoryMB),
			fmt.Sprintf("vnc=%t", vm.VNC),
		}
		if vm.Disk != "" {
			details = append(details, "disk="+vm.Disk)
		}
		if vm.ISO != "" {
			details = append(details, "iso="+vm.ISO)
		}
		for idx, nic := range vm.Networks {
			if detail := directNICDetails[networkEndpointKey(lab.NetworkEndpoint{Type: NodeVM, ID: vm.ID, NIC: idx})]; detail != "" {
				details = append(details, detail)
				continue
			}
			switch {
			case nic.Switch != "":
				details = append(details, fmt.Sprintf("nic%d → %s", idx, displayNodeRef(nodeNames, NodeSwitch, nic.Switch)))
			case nic.ExternalLink != "":
				details = append(details, fmt.Sprintf("nic%d → %s", idx, displayNodeRef(nodeNames, NodeExternal, nic.ExternalLink)))
			default:
				details = append(details, fmt.Sprintf("nic%d", idx))
			}
		}
		m.Nodes = append(m.Nodes, Node{
			ID:           vm.ID,
			Type:         NodeVM,
			Badge:        "VM",
			Label:        firstNonEmpty(vm.Name, vm.ID),
			State:        displayNodeWorkloadState(NodeVM, lab.DesiredState(vm.DesiredState), "defined", true, false),
			DesiredState: lab.DesiredState(vm.DesiredState),
			X:            layoutX(l, vm.ID, NodeVM, i),
			Y:            layoutY(l, vm.ID, i),
			Details:      details,
		})
		for _, nic := range vm.Networks {
			if nic.Switch != "" {
				m.Edges = append(m.Edges, Edge{From: NodeKey(NodeVM, vm.ID), To: NodeKey(NodeSwitch, nic.Switch)})
			}
			if nic.ExternalLink != "" {
				m.Edges = append(m.Edges, Edge{From: NodeKey(NodeVM, vm.ID), To: NodeKey(NodeExternal, nic.ExternalLink)})
			}
		}
	}
	for i, ct := range l.Containers {
		details := []string{
			"image=" + ct.Image,
			"capabilities=" + strings.Join(lab.EffectiveContainerCapabilities(ct), ","),
		}
		if ct.Disk != "" {
			details = append(details, "disk="+ct.Disk)
		}
		if len(ct.Command) > 0 {
			details = append(details, "command="+strings.Join(ct.Command, " "))
		}
		if ct.Shell != "" {
			details = append(details, "shell="+ct.Shell)
		}
		for key, value := range ct.Env {
			details = append(details, "env."+key+"="+value)
		}
		for idx, nic := range ct.Networks {
			if detail := directNICDetails[networkEndpointKey(lab.NetworkEndpoint{Type: NodeContainer, ID: ct.ID, NIC: idx})]; detail != "" {
				details = append(details, detail)
				continue
			}
			switch {
			case nic.Switch != "":
				details = append(details, fmt.Sprintf("nic%d → %s", idx, displayNodeRef(nodeNames, NodeSwitch, nic.Switch)))
				m.Edges = append(m.Edges, Edge{From: NodeKey(NodeContainer, ct.ID), To: NodeKey(NodeSwitch, nic.Switch)})
			case nic.ExternalLink != "":
				details = append(details, fmt.Sprintf("nic%d → %s", idx, displayNodeRef(nodeNames, NodeExternal, nic.ExternalLink)))
				m.Edges = append(m.Edges, Edge{From: NodeKey(NodeContainer, ct.ID), To: NodeKey(NodeExternal, nic.ExternalLink)})
			default:
				details = append(details, fmt.Sprintf("nic%d", idx))
			}
		}
		m.Nodes = append(m.Nodes, Node{
			ID:           ct.ID,
			Type:         NodeContainer,
			Badge:        "CT",
			Label:        firstNonEmpty(ct.Name, ct.ID),
			State:        displayNodeWorkloadState(NodeContainer, lab.DesiredState(ct.DesiredState), "missing", true, false),
			DesiredState: lab.DesiredState(ct.DesiredState),
			X:            layoutX(l, ct.ID, NodeContainer, i),
			Y:            layoutY(l, ct.ID, i),
			Details:      details,
		})
	}
	for i, sw := range l.Switches {
		details := []string{"mode=" + firstNonEmpty(sw.Mode, "bridge")}
		for _, externalID := range lab.SwitchExternalLinks(sw) {
			details = append(details, "uplink="+displayNodeRef(nodeNames, NodeExternal, externalID))
			m.Edges = append(m.Edges, Edge{From: NodeKey(NodeSwitch, sw.ID), To: NodeKey(NodeExternal, externalID)})
		}
		m.Nodes = append(m.Nodes, Node{
			ID:      sw.ID,
			Type:    NodeSwitch,
			Badge:   "SW",
			Label:   firstNonEmpty(sw.Name, sw.ID),
			State:   firstNonEmpty(sw.Mode, "bridge"),
			X:       layoutX(l, sw.ID, NodeSwitch, i),
			Y:       layoutY(l, sw.ID, i),
			Details: details,
		})
	}
	for i, link := range l.ExternalLinks {
		m.Nodes = append(m.Nodes, Node{
			ID:    link.ID,
			Type:  NodeExternal,
			Badge: "UP",
			Label: firstNonEmpty(link.Name, link.ID, link.Interface),
			State: firstNonEmpty(link.Mode, lab.ExternalModeDirect),
			X:     layoutX(l, link.ID, NodeExternal, i),
			Y:     layoutY(l, link.ID, i),
			Details: []string{
				"interface=" + firstNonEmpty(link.Interface, "-"),
				"mode=" + firstNonEmpty(link.Mode, lab.ExternalModeDirect),
			},
		})
	}
	sort.SliceStable(m.Nodes, func(i, j int) bool {
		return nodeSort(m.Nodes[i]) < nodeSort(m.Nodes[j])
	})
	return m
}

func labNodeNames(l *lab.Lab) map[string]string {
	names := map[string]string{}
	for _, vm := range l.VMs {
		names[NodeKey(NodeVM, vm.ID)] = firstNonEmpty(vm.Name, vm.ID)
	}
	for _, ct := range l.Containers {
		names[NodeKey(NodeContainer, ct.ID)] = firstNonEmpty(ct.Name, ct.ID)
	}
	for _, sw := range l.Switches {
		names[NodeKey(NodeSwitch, sw.ID)] = firstNonEmpty(sw.Name, sw.ID)
	}
	for _, link := range l.ExternalLinks {
		names[NodeKey(NodeExternal, link.ID)] = firstNonEmpty(link.Name, link.ID, link.Interface)
	}
	return names
}

func displayNodeRef(names map[string]string, typ, id string) string {
	if name := names[NodeKey(typ, id)]; name != "" {
		return name
	}
	return id
}

func displayWorkloadState(desired, actual string) string {
	return displayWorkloadStateWithTransitions(desired, actual, true)
}

func displayNodeWorkloadState(typ, desired, actual string, transitions, pendingStart bool) string {
	actual = normalizeRuntimeState(actual)
	if pendingStart && desired == lab.DesiredStateRunning && actual == "missing" {
		return "starting"
	}
	if typ == NodeVM && desired == lab.DesiredStateStopped && actual == "missing" {
		return "defined"
	}
	return displayWorkloadStateWithTransitions(desired, actual, transitions)
}

func displayWorkloadStateWithTransitions(desired, actual string, transitions bool) string {
	actual = normalizeRuntimeState(actual)
	if !transitions {
		return actual
	}
	if desired == lab.DesiredStateRunning {
		switch actual {
		case "", "created", "defined", "stopped", "shutoff":
			return "starting"
		}
	}
	if desired == lab.DesiredStateStopped {
		switch actual {
		case "running", "starting", "loading", "pulling", "creating":
			return "stopping"
		}
	}
	return actual
}

func NodeKey(typ, id string) string { return graph.Key(typ, id) }

func nodeByKey(m Model, key string) (Node, bool) {
	for _, node := range m.Nodes {
		if node.Key() == key {
			return node, true
		}
	}
	typ, id, ok := strings.Cut(key, ":")
	if !ok {
		return Node{}, false
	}
	for _, node := range m.Nodes {
		if node.Type == typ && node.Label == id {
			return node, true
		}
	}
	return Node{}, false
}

func NodeKind(typ string) string {
	switch typ {
	case NodeVM:
		return "VM"
	case NodeContainer:
		return "CT"
	case NodeExternal:
		return "UP"
	default:
		return "SW"
	}
}

func selectedNode(m Model, index int) (Node, bool) {
	if len(m.Nodes) == 0 {
		return Node{}, false
	}
	if index < 0 {
		index = 0
	}
	if index >= len(m.Nodes) {
		index = len(m.Nodes) - 1
	}
	return m.Nodes[index], true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func layoutX(l *lab.Lab, id, typ string, index int) int {
	if pos, ok := l.Layout.Nodes[id]; ok {
		return pos.X / 16
	}
	switch typ {
	case NodeVM:
		return 4
	case NodeContainer:
		return 4
	case NodeExternal:
		return 52
	default:
		return 28
	}
}

func layoutY(l *lab.Lab, id string, index int) int {
	if pos, ok := l.Layout.Nodes[id]; ok {
		return pos.Y / 24
	}
	return 3 + index*8
}

func nodeSort(n Node) string {
	switch n.Type {
	case NodeVM:
		return "0:" + n.ID
	case NodeContainer:
		return "1:" + n.ID
	case NodeSwitch:
		return "2:" + n.ID
	case NodeExternal:
		return "3:" + n.ID
	default:
		return "9:" + n.ID
	}
}

func networkEndpointKey(endpoint lab.NetworkEndpoint) string {
	return fmt.Sprintf("%s:%s:%d", endpoint.Type, endpoint.ID, endpoint.NIC)
}
