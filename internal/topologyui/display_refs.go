package topologyui

import (
	"strconv"
	"strings"

	"foxlab-cli/internal/lab"
)

func (a *App) displayNodeName(typ, id string) string {
	if node, ok := nodeByKey(a.Model, NodeKey(typ, id)); ok {
		return firstNonEmpty(node.Label, node.ID)
	}
	return displayNodeNameFromLab(a.currentLab(), typ, id)
}

func (a *App) displayNodeKey(typ, id string) string {
	return typ + ":" + a.displayNodeName(typ, id)
}

func displayNodeNameFromLab(l *lab.Lab, typ, id string) string {
	if l == nil {
		return id
	}
	switch typ {
	case NodeVM:
		for _, vm := range l.VMs {
			if vm.ID == id {
				return firstNonEmpty(vm.Name, vm.ID)
			}
		}
	case NodeContainer:
		for _, ct := range l.Containers {
			if ct.ID == id {
				return firstNonEmpty(ct.Name, ct.ID)
			}
		}
	case NodeSwitch:
		for _, sw := range l.Switches {
			if sw.ID == id {
				return firstNonEmpty(sw.Name, sw.ID)
			}
		}
	case NodeExternal:
		for _, link := range l.ExternalLinks {
			if link.ID == id {
				return firstNonEmpty(link.Name, link.ID, link.Interface)
			}
		}
	}
	return id
}

func displayDaemonMessages(l *lab.Lab, messages []string) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, displayDaemonMessage(l, message))
	}
	return out
}

func displayDaemonMessage(l *lab.Lab, message string) string {
	if l == nil || message == "" {
		return message
	}
	replacements := daemonMessageReplacements(l)
	for _, replacement := range replacements {
		if replacement.id == "" || replacement.name == "" || replacement.id == replacement.name {
			continue
		}
		message = strings.ReplaceAll(message, replacement.typ+":"+replacement.id, replacement.typ+":"+replacement.name)
		message = strings.ReplaceAll(message, strconv.Quote(replacement.id), strconv.Quote(replacement.name))
		if lab.ValidNodeID(replacement.id) {
			message = strings.ReplaceAll(message, replacement.id, replacement.name)
		}
	}
	return message
}

type daemonMessageReplacement struct {
	typ  string
	id   string
	name string
}

func daemonMessageReplacements(l *lab.Lab) []daemonMessageReplacement {
	out := []daemonMessageReplacement{}
	for _, vm := range l.VMs {
		out = append(out, daemonMessageReplacement{typ: NodeVM, id: vm.ID, name: firstNonEmpty(vm.Name, vm.ID)})
	}
	for _, ct := range l.Containers {
		out = append(out, daemonMessageReplacement{typ: NodeContainer, id: ct.ID, name: firstNonEmpty(ct.Name, ct.ID)})
	}
	for _, sw := range l.Switches {
		out = append(out, daemonMessageReplacement{typ: NodeSwitch, id: sw.ID, name: firstNonEmpty(sw.Name, sw.ID)})
	}
	for _, link := range l.ExternalLinks {
		out = append(out, daemonMessageReplacement{typ: NodeExternal, id: link.ID, name: firstNonEmpty(link.Name, link.ID, link.Interface)})
	}
	return out
}
