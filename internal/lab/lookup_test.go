package lab

import (
	"errors"
	"testing"
)

func TestResolveNodePrefersExactIDBeforeName(t *testing.T) {
	l := &Lab{
		VMs:        []VM{{ID: "router", Name: "edge"}},
		Containers: []Container{{ID: "edge", Name: "web"}},
	}
	resolved, err := ResolveNode(l, "edge", NodeKindVM, NodeKindContainer)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != (NodeRef{Kind: NodeKindContainer, ID: "edge"}) {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestResolveNodeFiltersKindsAndPreservesCase(t *testing.T) {
	l := &Lab{VMs: []VM{{ID: "Router", Name: "Edge"}}}
	if _, err := ResolveNode(l, "Router", NodeKindContainer); err == nil {
		t.Fatal("expected kind-filtered lookup to fail")
	}
	if _, err := ResolveNode(l, "router", NodeKindVM); err == nil {
		t.Fatal("expected case-sensitive lookup to fail")
	}
}

func TestResolveNodeReportsAmbiguousName(t *testing.T) {
	l := &Lab{VMs: []VM{{ID: "vm-1", Name: "node"}}, Containers: []Container{{ID: "ct-1", Name: "node"}}}
	_, err := ResolveNode(l, "node", NodeKindVM, NodeKindContainer)
	var resolveErr *ResolveNodeError
	if !errors.As(err, &resolveErr) || resolveErr.Match != ResolveMatchName || resolveErr.Count != 2 {
		t.Fatalf("error = %#v", err)
	}
}
