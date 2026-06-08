package topologytui

import (
	"fmt"
	"sort"
	"strings"

	"foxlab-cli/internal/lab"
)

const (
	NodeVM       = "vm"
	NodeSwitch   = "switch"
	NodeExternal = "external"
)

type Model struct {
	LabID string
	Nodes []Node
	Edges []Edge
}

type Node struct {
	ID      string
	Type    string
	Label   string
	State   string
	X       int
	Y       int
	Details []string
}

type Edge struct {
	From string
	To   string
}

func MockModel() Model {
	return Model{
		LabID: "mock",
		Nodes: []Node{
			{ID: "router", Type: NodeVM, Label: "router", State: "running", X: 4, Y: 3, Details: []string{"cpu=2", "mem=2048M", "vnc=true", "disk=labs/mock/disks/router.img", "nic0 → edge"}},
			{ID: "client01", Type: NodeVM, Label: "client01", State: "defined", X: 4, Y: 11, Details: []string{"cpu=2", "mem=2048M", "vnc=false", "disk=labs/mock/disks/client01.img", "nic0 → lan"}},
			{ID: "edge", Type: NodeSwitch, Label: "edge", State: "bridge", X: 28, Y: 5, Details: []string{"mode=macnat-bridge", "uplink=uplink0"}},
			{ID: "lan", Type: NodeSwitch, Label: "lan", State: "nat", X: 28, Y: 13, Details: []string{"mode=nat", "dhcp=on"}},
			{ID: "uplink0", Type: NodeExternal, Label: "wlp0s20f3", State: "link", X: 52, Y: 5, Details: []string{"interface=wlp0s20f3"}},
			{ID: "hostnet", Type: NodeExternal, Label: "br0", State: "link", X: 52, Y: 13, Details: []string{"interface=br0"}},
		},
		Edges: []Edge{
			{From: NodeKey(NodeVM, "router"), To: NodeKey(NodeSwitch, "edge")},
			{From: NodeKey(NodeVM, "client01"), To: NodeKey(NodeSwitch, "lan")},
			{From: NodeKey(NodeSwitch, "edge"), To: NodeKey(NodeExternal, "uplink0")},
			{From: NodeKey(NodeSwitch, "lan"), To: NodeKey(NodeExternal, "hostnet")},
			{From: NodeKey(NodeVM, "router"), To: NodeKey(NodeSwitch, "lan")},
		},
	}
}

func ModelFromLab(l *lab.Lab) Model {
	if l == nil {
		return MockModel()
	}
	m := Model{LabID: l.ID}
	for i, vm := range l.VMs {
		details := []string{
			fmt.Sprintf("cpu=%d", vm.CPUs),
			fmt.Sprintf("mem=%dM", vm.MemoryMB),
			fmt.Sprintf("vnc=%t", vm.VNC),
			"disk=" + vm.Disk,
		}
		if vm.ISO != "" {
			details = append(details, "iso="+vm.ISO)
		}
		for idx, nic := range vm.Networks {
			switch {
			case nic.Switch != "":
				details = append(details, fmt.Sprintf("nic%d → %s", idx, nic.Switch))
			case nic.ExternalLink != "":
				details = append(details, fmt.Sprintf("nic%d → %s", idx, nic.ExternalLink))
			}
		}
		m.Nodes = append(m.Nodes, Node{
			ID:      vm.ID,
			Type:    NodeVM,
			Label:   firstNonEmpty(vm.Name, vm.ID),
			State:   "defined",
			X:       layoutX(l, vm.ID, NodeVM, i),
			Y:       layoutY(l, vm.ID, i),
			Details: details,
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
	for i, sw := range l.Switches {
		details := []string{"mode=" + firstNonEmpty(sw.Mode, "bridge")}
		if sw.ExternalLink != "" {
			details = append(details, "uplink="+sw.ExternalLink)
			m.Edges = append(m.Edges, Edge{From: NodeKey(NodeSwitch, sw.ID), To: NodeKey(NodeExternal, sw.ExternalLink)})
		}
		m.Nodes = append(m.Nodes, Node{
			ID:      sw.ID,
			Type:    NodeSwitch,
			Label:   firstNonEmpty(sw.Name, sw.ID),
			State:   firstNonEmpty(sw.Mode, "bridge"),
			X:       layoutX(l, sw.ID, NodeSwitch, i),
			Y:       layoutY(l, sw.ID, i),
			Details: details,
		})
	}
	for i, link := range l.ExternalLinks {
		m.Nodes = append(m.Nodes, Node{
			ID:      link.ID,
			Type:    NodeExternal,
			Label:   firstNonEmpty(link.Name, link.Interface, link.ID),
			State:   "link",
			X:       layoutX(l, link.ID, NodeExternal, i),
			Y:       layoutY(l, link.ID, i),
			Details: []string{"interface=" + firstNonEmpty(link.Interface, "-")},
		})
	}
	sort.SliceStable(m.Nodes, func(i, j int) bool {
		return nodeSort(m.Nodes[i]) < nodeSort(m.Nodes[j])
	})
	return m
}

func NodeKey(typ, id string) string {
	return typ + ":" + id
}

func NodeKind(typ string) string {
	switch typ {
	case NodeVM:
		return "VM"
	case NodeExternal:
		return "IF"
	default:
		return "SW"
	}
}

func (n Node) Key() string {
	return NodeKey(n.Type, n.ID)
}

func (m Model) selected(index int) (Node, bool) {
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
	case NodeSwitch:
		return "1:" + n.ID
	case NodeExternal:
		return "2:" + n.ID
	default:
		return "9:" + n.ID
	}
}
