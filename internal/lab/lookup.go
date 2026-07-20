package lab

import "fmt"

type NodeKind string

const (
	NodeKindVM        NodeKind = "vm"
	NodeKindContainer NodeKind = "container"
	NodeKindSwitch    NodeKind = "switch"
	NodeKindExternal  NodeKind = "external"
)

type NodeRef struct {
	Kind NodeKind
	ID   string
}

type ResolveMatch string

const (
	ResolveMatchID   ResolveMatch = "id"
	ResolveMatchName ResolveMatch = "name"
)

type ResolveNodeError struct {
	Input string
	Match ResolveMatch
	Count int
}

func (e *ResolveNodeError) Error() string {
	if e.Count > 1 {
		return fmt.Sprintf("node %s is ambiguous: %s", e.Match, e.Input)
	}
	return "node not found: " + e.Input
}

func ResolveNode(l *Lab, input string, kinds ...NodeKind) (NodeRef, error) {
	allowed := map[NodeKind]bool{}
	for _, kind := range kinds {
		allowed[kind] = true
	}
	accept := func(kind NodeKind) bool { return len(allowed) == 0 || allowed[kind] }
	type candidate struct {
		ref  NodeRef
		name string
	}
	candidates := []candidate{}
	if l != nil {
		for _, vm := range l.VMs {
			if accept(NodeKindVM) {
				candidates = append(candidates, candidate{ref: NodeRef{Kind: NodeKindVM, ID: vm.ID}, name: vm.Name})
			}
		}
		for _, ct := range l.Containers {
			if accept(NodeKindContainer) {
				candidates = append(candidates, candidate{ref: NodeRef{Kind: NodeKindContainer, ID: ct.ID}, name: ct.Name})
			}
		}
		for _, sw := range l.Switches {
			if accept(NodeKindSwitch) {
				candidates = append(candidates, candidate{ref: NodeRef{Kind: NodeKindSwitch, ID: sw.ID}, name: sw.Name})
			}
		}
		for _, external := range l.ExternalLinks {
			if accept(NodeKindExternal) {
				candidates = append(candidates, candidate{ref: NodeRef{Kind: NodeKindExternal, ID: external.ID}, name: external.Name})
			}
		}
	}
	for _, match := range []ResolveMatch{ResolveMatchID, ResolveMatchName} {
		matches := []NodeRef{}
		for _, candidate := range candidates {
			value := candidate.ref.ID
			if match == ResolveMatchName {
				value = candidate.name
			}
			if value == input {
				matches = append(matches, candidate.ref)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return NodeRef{}, &ResolveNodeError{Input: input, Match: match, Count: len(matches)}
		}
	}
	return NodeRef{}, &ResolveNodeError{Input: input}
}

func FindSwitch(l *Lab, id string) (Switch, bool) {
	if l == nil {
		return Switch{}, false
	}
	for _, sw := range l.Switches {
		if sw.ID == id {
			return sw, true
		}
	}
	return Switch{}, false
}

func FindExternalLink(l *Lab, id string) (ExternalLink, bool) {
	if l == nil {
		return ExternalLink{}, false
	}
	for _, link := range l.ExternalLinks {
		if link.ID == id {
			return link, true
		}
	}
	return ExternalLink{}, false
}

func FindNetworkLinkForEndpoint(l *Lab, endpoint NetworkEndpoint) (NetworkLink, bool) {
	if l == nil {
		return NetworkLink{}, false
	}
	for _, link := range l.NetworkLinks {
		if SameNetworkEndpoint(link.From, endpoint) || SameNetworkEndpoint(link.To, endpoint) {
			return link, true
		}
	}
	return NetworkLink{}, false
}

func SameNetworkEndpoint(a, b NetworkEndpoint) bool {
	return a.Type == b.Type && a.ID == b.ID && a.NIC == b.NIC
}
