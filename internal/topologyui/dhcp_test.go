package topologyui

import (
	"path/filepath"
	"slices"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestCommandAddDHCPPersistsAndRendersDedicatedNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "demo.lab")
	if err := lab.SaveFile(path, &lab.Lab{ID: "demo", Switches: []lab.Switch{{ID: "lan", Mode: "nat"}}}); err != nil {
		t.Fatal(err)
	}
	loaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	app := App{Model: ModelFromLab(loaded), Session: lab.NewSession(loaded, path), State: ViewState{Focus: FocusGraph}}
	app.executeCommand("add dhcp dhcp1 switch=lan")
	reloaded, err := lab.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Containers) != 1 || !lab.IsDHCPContainer(reloaded.Containers[0]) {
		t.Fatalf("saved containers = %#v", reloaded.Containers)
	}
	node, ok := nodeByKey(app.Model, NodeKey(NodeContainer, "dhcp1"))
	if !ok || node.Badge != "SRV" || nodeDetailRawValue(node, "service") != lab.ContainerServiceDHCP {
		t.Fatalf("DHCP node = %#v, found=%t", node, ok)
	}
	if len(app.Model.Edges) != 1 || app.Model.Edges[0].To != NodeKey(NodeSwitch, "lan") {
		t.Fatalf("DHCP edges = %#v", app.Model.Edges)
	}
}

func TestPaletteOffersDHCPAddAction(t *testing.T) {
	actions := filteredAddPaletteActions("add dh")
	if len(actions) != 1 || actions[0].Action != "add dhcp" {
		t.Fatalf("DHCP palette actions = %#v", actions)
	}
}

func TestDHCPInspectorAndContextMenuExposeOnlyManagedServiceControls(t *testing.T) {
	l := &lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "nat"}},
		Containers: []lab.Container{{
			ID: "dhcp", Service: lab.ContainerServiceDHCP, Image: lab.DefaultDHCPImage,
			Networks: []lab.ContainerNetwork{{Switch: "lan"}},
		}},
	}
	l.Normalize()
	model := ModelFromLab(l)
	node, ok := nodeByKey(model, NodeKey(NodeContainer, "dhcp"))
	if !ok {
		t.Fatal("DHCP node missing from model")
	}
	fields := inspectorFields(node)
	got := make([]string, 0, len(fields))
	for _, field := range fields {
		got = append(got, field.id)
		if field.kind == inspectorFieldNIC && !field.managed {
			t.Fatalf("DHCP NIC must not be removable: %#v", field)
		}
	}
	want := []string{"desiredState", "name", "nic0", "moveAction", "deleteAction"}
	if !slices.Equal(got, want) {
		t.Fatalf("DHCP inspector fields = %#v, want %#v", got, want)
	}
	if got := inspectorFieldListY(fields); got != inspectorFieldListYPowerOnly {
		t.Fatalf("DHCP inspector list y = %d, want %d", got, inspectorFieldListYPowerOnly)
	}
	if got := containerContextMenuItems(node, ""); !slices.Equal(got, []string{"Configuration >", "NIC >", "Move"}) {
		t.Fatalf("DHCP root menu = %#v", got)
	}
	if got := containerContextMenuItems(node, "config-menu"); len(got) != 2 {
		t.Fatalf("DHCP config menu = %#v", got)
	}
}
