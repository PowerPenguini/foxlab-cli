package topologytui

import (
	"strings"
	"testing"

	"foxlab-cli/internal/lab"
)

func TestRenderMockFrame(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Focus: FocusGraph}, 100, 30, false)
	for _, want := range []string{
		"[VM] router",
		"[SW] edge",
		"[IF] wlp0s20f3",
		"┌",
		"─",
		"│",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{
		"foxlab://topology-tui",
		"lab=mock",
		" graph ",
		"[graph]>",
		"inspector",
		"nic0 → edge",
		"lifecycle >",
		"qemu",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("render contains removed content %q:\n%s", notWant, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("non-ANSI render contains escape sequences:\n%q", out)
	}
}

func TestRenderEdgesDoNotCorruptNodeBoxes(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Focus: FocusGraph}, 56, 35, false)
	for _, notWant := range []string{
		"defined────",
		"defined────┐",
		"defined────┤",
		"├defined",
		"┼defined",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("render has edge merged into node content %q:\n%s", notWant, out)
		}
	}
}

func TestDrawEdgeUsesVerticalPortsForStackedNodes(t *testing.T) {
	g := newGrid(40, 20)
	from := rect{x: 10, y: 2, w: nodeWidth, h: nodeHeight}
	to := rect{x: 10, y: 10, w: nodeWidth, h: nodeHeight}

	route, ok := drawEdge(g, from, to, rect{x: 0, y: 0, w: 40, h: 20})
	if !ok {
		t.Fatal("stacked edge was not routed")
	}
	if !route.vertical {
		t.Fatal("stacked edge used horizontal route")
	}

	if got := g.cells[6*g.width+18].ch; got != lineVertical {
		t.Fatalf("edge below source = %q, want vertical line", got)
	}
	if got := g.cells[9*g.width+18].ch; got != lineVertical {
		t.Fatalf("edge above target = %q, want vertical line", got)
	}
	if got := g.cells[4*g.width+26].ch; got != ' ' {
		t.Fatalf("edge used source side port = %q, want blank", got)
	}
	if got := g.cells[12*g.width+9].ch; got != ' ' {
		t.Fatalf("edge used target side port = %q, want blank", got)
	}
}

func TestDrawEdgePortsAttachToNodeBorders(t *testing.T) {
	g := newGrid(40, 20)
	from := rect{x: 10, y: 2, w: nodeWidth, h: nodeHeight}
	to := rect{x: 10, y: 10, w: nodeWidth, h: nodeHeight}
	drawBox(g, from, "", "")
	drawBox(g, to, "", "")

	route, ok := planEdgeRoute(from, to, rect{x: 0, y: 0, w: 40, h: 20})
	if !ok {
		t.Fatal("stacked edge was not routed")
	}
	drawEdgePorts(g, from, to, route)

	if got := g.cells[(from.y+from.h-1)*g.width+from.x+from.w/2].ch; got != '┬' {
		t.Fatalf("source bottom port = %q, want ┬", got)
	}
	if got := g.cells[to.y*g.width+to.x+to.w/2].ch; got != '┴' {
		t.Fatalf("target top port = %q, want ┴", got)
	}
}

func TestDrawEdgeTreatsNearbyColumnsAsVertical(t *testing.T) {
	from := rect{x: 10, y: 2, w: nodeWidth, h: nodeHeight}
	to := rect{x: 22, y: 10, w: nodeWidth, h: nodeHeight}
	route, ok := planEdgeRoute(from, to, rect{x: 0, y: 0, w: 50, h: 20})
	if !ok {
		t.Fatal("nearby stacked edge was not routed")
	}
	if !route.vertical {
		t.Fatal("nearby stacked nodes should use vertical edge routing")
	}
}

func TestDrawEdgeSkipsWhenNoFreeLaneExists(t *testing.T) {
	g := newGrid(60, 20)
	from := rect{x: 30, y: 8, w: nodeWidth, h: nodeHeight}
	to := rect{x: 20, y: 10, w: nodeWidth, h: nodeHeight}

	if _, ok := drawEdge(g, from, to, rect{x: 0, y: 0, w: 60, h: 20}); ok {
		t.Fatal("overlapping nearby nodes should not route through a box")
	}
	if strings.Contains(g.String(false), "─") || strings.Contains(g.String(false), "│") {
		t.Fatalf("edge left partial lines without a valid route:\n%s", g.String(false))
	}
}

func TestDrawEdgeRendersAdjacentVerticalTeeAndCorner(t *testing.T) {
	g := newGrid(90, 24)
	from := rect{x: 64, y: 10, w: nodeWidth, h: nodeHeight}
	to := rect{x: 53, y: 14, w: nodeWidth, h: nodeHeight}
	drawBox(g, from, "", "")
	drawBox(g, to, "", "")

	route, ok := drawEdge(g, from, to, rect{x: 0, y: 0, w: 90, h: 24})
	if !ok {
		t.Fatal("adjacent vertical edge was not routed")
	}
	if !route.overlay {
		t.Fatal("adjacent vertical edge did not use overlay route")
	}
	drawEdgeRoute(g, route)
	drawEdgePorts(g, from, to, route)

	if got := g.cells[(from.y+from.h-1)*g.width+from.x+from.w/2].ch; got != '┬' {
		t.Fatalf("source adjacent port = %q, want ┬", got)
	}
	if got := g.cells[to.y*g.width+from.x+from.w/2].ch; got != '┘' {
		t.Fatalf("adjacent corner = %q, want ┘", got)
	}
	if got := g.cells[to.y*g.width+to.x+to.w/2].ch; got != lineHorizontal {
		t.Fatalf("target adjacent top border = %q, want horizontal line", got)
	}
}

func TestDrawEdgeMergesAdjacentRouteWithNodeTopBorder(t *testing.T) {
	g := newGrid(90, 24)
	from := rect{x: 64, y: 10, w: nodeWidth, h: nodeHeight}
	to := rect{x: 53, y: 14, w: nodeWidth, h: nodeHeight}
	drawBox(g, from, "", "")
	drawBox(g, to, "", "")

	route, ok := drawEdge(g, from, to, rect{x: 0, y: 0, w: 90, h: 24})
	if !ok {
		t.Fatal("adjacent vertical edge was not routed")
	}
	drawEdgeRoute(g, route)
	drawEdgePorts(g, from, to, route)

	if got := g.cells[to.y*g.width+to.x+to.w/2].ch; got != lineHorizontal {
		t.Fatalf("target top border = %q, want horizontal line", got)
	}
	if got := g.cells[to.y*g.width+to.x+to.w-1].ch; got != '┬' {
		t.Fatalf("target top-right intersection = %q, want ┬", got)
	}
	if got := g.cells[to.y*g.width+from.x+from.w/2].ch; got != '┘' {
		t.Fatalf("outer adjacent corner = %q, want ┘", got)
	}
}

func TestDrawEdgeMergesCrossingRouteAboveNodePort(t *testing.T) {
	g := newGrid(90, 24)
	uplink := rect{x: 40, y: 7, w: nodeWidth, h: nodeHeight}
	vm := rect{x: 64, y: 9, w: nodeWidth, h: nodeHeight}
	sw := rect{x: 53, y: 14, w: nodeWidth, h: nodeHeight}

	uplinkRoute, ok := drawEdge(g, uplink, sw, rect{x: 0, y: 0, w: 90, h: 24})
	if !ok {
		t.Fatal("uplink edge was not routed")
	}
	if uplinkRoute.overlay {
		t.Fatal("uplink edge unexpectedly used overlay route")
	}
	drawBox(g, uplink, "", "")
	drawBox(g, vm, "", "")
	drawBox(g, sw, "", "")
	drawEdgePorts(g, uplink, sw, uplinkRoute)

	vmRoute, ok := drawEdge(g, vm, sw, rect{x: 0, y: 0, w: 90, h: 24})
	if !ok {
		t.Fatal("vm edge was not routed")
	}
	drawEdgeRoute(g, vmRoute)
	drawEdgePorts(g, vm, sw, vmRoute)

	if got := g.cells[(sw.y-1)*g.width+sw.x+sw.w/2].ch; got != '├' {
		t.Fatalf("crossing above target port = %q, want ├", got)
	}
	if got := g.cells[(vm.y+vm.h-1)*g.width+vm.x+vm.w/2].ch; got != '┬' {
		t.Fatalf("source bottom tee = %q, want ┬", got)
	}
}

func TestDrawEdgeMergesOverlayRouteWithNodeTopBorder(t *testing.T) {
	g := newGrid(80, 20)
	sw := rect{x: 12, y: 2, w: nodeWidth, h: nodeHeight}
	vm := rect{x: 24, y: 6, w: nodeWidth, h: nodeHeight}
	drawBox(g, sw, "", "")
	drawBox(g, vm, "", "")

	route, ok := drawEdge(g, sw, vm, rect{x: 0, y: 0, w: 80, h: 20})
	if !ok {
		t.Fatal("overlay edge was not routed")
	}
	if !route.overlay {
		t.Fatal("nearby edge did not use overlay route")
	}
	drawEdgeRoute(g, route)
	drawEdgePorts(g, sw, vm, route)

	if got := g.cells[vm.y*g.width+vm.x+vm.w/2].ch; got != lineHorizontal {
		t.Fatalf("target top segment = %q, want horizontal line", got)
	}
	if got := g.cells[vm.y*g.width+sw.x+sw.w/2].ch; got != '└' {
		t.Fatalf("overlay corner = %q, want └", got)
	}
}

func TestRenderSelectedOverlayRouteMergesWithTopBorder(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "macnat-bridge", X: 12, Y: 2},
			{ID: "hello", Type: NodeVM, Label: "hello", State: "missing", X: 24, Y: 6},
		},
		Edges: []Edge{{From: NodeKey(NodeSwitch, "sw1"), To: NodeKey(NodeVM, "hello")}},
	}

	out := RenderString(m, ViewState{Selected: 1, Focus: FocusGraph}, 60, 16, false)
	if !strings.Contains(out, "└───┬──────────────┐") {
		t.Fatalf("selected top border did not merge overlay route:\n%s", out)
	}
	if strings.Contains(out, "────│") {
		t.Fatalf("overlay route left a disconnected vertical tick:\n%s", out)
	}
	if strings.Contains(out, "───────┴──────") {
		t.Fatalf("overlay route left a top-border tick:\n%s", out)
	}

	ansiOut := RenderString(m, ViewState{Selected: 1, Focus: FocusGraph}, 60, 16, true)
	if !strings.Contains(ansiOut, ansiBold+ansiBrightCyan+"┬──────────────┐") {
		t.Fatalf("selected top border was not kept cyan/bold after overlay route:\n%q", ansiOut)
	}
	if strings.Contains(ansiOut, ansiDim+"┬──────────────┐") {
		t.Fatalf("selected top border was overwritten by edge style:\n%q", ansiOut)
	}
	if strings.Contains(ansiOut, ansiBold+ansiBrightCyan+"───┬──────────────┐") {
		t.Fatalf("selected border style leaked onto connector before box:\n%q", ansiOut)
	}
}

func TestRenderNodeBordersKeepUniformStyleAfterEdges(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "macnat-bridge", X: 12, Y: 2},
			{ID: "hello", Type: NodeVM, Label: "hello", State: "missing", X: 24, Y: 6},
		},
		Edges: []Edge{{From: NodeKey(NodeSwitch, "sw1"), To: NodeKey(NodeVM, "hello")}},
	}

	g := renderGrid(m, ViewState{Selected: 1, Focus: FocusGraph}, 60, 16)
	rects := layoutNodeRects(m, rect{x: 0, y: 0, w: 60, h: 16})
	assertBorderStyle(t, g, rects[NodeKey(NodeVM, "hello")], ansiBold+ansiBrightCyan)
	assertBorderStyle(t, g, rects[NodeKey(NodeSwitch, "sw1")], "")
	if got := g.cells[rects[NodeKey(NodeVM, "hello")].y*g.width+rects[NodeKey(NodeVM, "hello")].x-1].style; got == ansiBold+ansiBrightCyan {
		t.Fatalf("selected border style leaked to connector before box")
	}
}

func assertBorderStyle(t *testing.T, g *grid, r rect, want string) {
	t.Helper()
	for x := r.x; x < r.x+r.w; x++ {
		for _, y := range []int{r.y, r.y + r.h - 1} {
			if got := g.cells[y*g.width+x].style; got != want {
				t.Fatalf("border style at (%d,%d) = %q, want %q", x, y, got, want)
			}
		}
	}
	for y := r.y + 1; y < r.y+r.h-1; y++ {
		for _, x := range []int{r.x, r.x + r.w - 1} {
			if got := g.cells[y*g.width+x].style; got != want {
				t.Fatalf("border style at (%d,%d) = %q, want %q", x, y, got, want)
			}
		}
	}
}

func TestRenderSelectedOverlayRouteMergesWithBottomBorder(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "hello", Type: NodeVM, Label: "hello", State: "missing", X: 24, Y: 2},
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "macnat-bridge", X: 12, Y: 6},
		},
		Edges: []Edge{{From: NodeKey(NodeVM, "hello"), To: NodeKey(NodeSwitch, "sw1")}},
	}

	out := RenderString(m, ViewState{Selected: 0, Focus: FocusGraph}, 60, 16, false)
	if !strings.Contains(out, "└───────┬──────┘") {
		t.Fatalf("selected bottom border did not merge overlay route:\n%s", out)
	}
}

func TestRenderSelectedNodeANSIHighlight(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph}, 100, 30, true)
	if !strings.Contains(out, ansiBold+ansiBrightCyan) {
		t.Fatalf("ANSI render missing selected-node border style:\n%q", out)
	}
	for _, notWant := range []string{
		ansiInverse + "[VM] client01",
		ansiInverse + "defined",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("ANSI render has unwanted selected-node text highlight %q:\n%q", notWant, out)
		}
	}
}

func TestLayoutNodeRectsUseFixedPlane(t *testing.T) {
	base := Model{Nodes: []Node{
		{ID: "top", Type: NodeVM, X: 4, Y: 2},
		{ID: "bottom", Type: NodeVM, X: 4, Y: 18},
	}}
	moved := Model{Nodes: []Node{
		{ID: "top", Type: NodeVM, X: 4, Y: 0},
		{ID: "bottom", Type: NodeVM, X: 4, Y: 18},
	}}
	bounds := rect{x: 0, y: 0, w: 90, h: 24}

	baseRects := layoutNodeRects(base, bounds)
	movedRects := layoutNodeRects(moved, bounds)

	if baseRects[NodeKey(NodeVM, "bottom")] != movedRects[NodeKey(NodeVM, "bottom")] {
		t.Fatalf("bottom node moved when only top node changed: before=%#v after=%#v", baseRects[NodeKey(NodeVM, "bottom")], movedRects[NodeKey(NodeVM, "bottom")])
	}
}

func TestLayoutNodeRectsBottomNodeCanMoveVertically(t *testing.T) {
	bounds := rect{x: 0, y: 0, w: 90, h: 35}
	up := layoutNodeRects(Model{Nodes: []Node{{ID: "bottom", Type: NodeVM, X: 4, Y: 28}}}, bounds)[NodeKey(NodeVM, "bottom")]
	base := layoutNodeRects(Model{Nodes: []Node{{ID: "bottom", Type: NodeVM, X: 4, Y: 29}}}, bounds)[NodeKey(NodeVM, "bottom")]
	down := layoutNodeRects(Model{Nodes: []Node{{ID: "bottom", Type: NodeVM, X: 4, Y: 30}}}, bounds)[NodeKey(NodeVM, "bottom")]

	if !(up.y < base.y && base.y < down.y) {
		t.Fatalf("bottom vertical movement is not visible: up=%#v base=%#v down=%#v", up, base, down)
	}
}

func TestLayoutNodeRectsDoNotClampOffscreenNodes(t *testing.T) {
	bounds := rect{x: 0, y: 0, w: 40, h: 20}
	rects := layoutNodeRects(Model{Nodes: []Node{{ID: "right", Type: NodeSwitch, X: 80, Y: 2}}}, bounds)
	got := rects[NodeKey(NodeSwitch, "right")]
	if got.x != 80 {
		t.Fatalf("offscreen node x = %d, want unclamped x=80", got.x)
	}
}

func TestRenderSkipsEdgesWithOffscreenEndpoints(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "visible", Type: NodeVM, Label: "visible", X: 10, Y: 10, State: "defined"},
			{ID: "offscreen", Type: NodeSwitch, X: 80, Y: 2, State: "bridge"},
		},
		Edges: []Edge{{From: NodeKey(NodeVM, "visible"), To: NodeKey(NodeSwitch, "offscreen")}},
	}
	out := RenderString(m, ViewState{Focus: FocusGraph}, 40, 20, false)
	if strings.Contains(out, "offscreen") {
		t.Fatalf("render drew offscreen node:\n%s", out)
	}
	if strings.Contains(out, "─") || strings.Contains(out, "│") {
		clean := strings.ReplaceAll(out, "│[VM] visible  │", "")
		clean = strings.ReplaceAll(clean, "│defined       │", "")
		clean = strings.ReplaceAll(clean, "┌──────────────┐", "")
		clean = strings.ReplaceAll(clean, "└──────────────┘", "")
		if strings.Contains(clean, "─") || strings.Contains(clean, "│") {
			t.Fatalf("render drew edge to offscreen endpoint:\n%s", out)
		}
	}
}

func TestRenderSkipsPartiallyVisibleNodes(t *testing.T) {
	m := Model{Nodes: []Node{{ID: "partial", Type: NodeSwitch, X: 50, Y: 5, Label: "partial", State: "bridge"}}}
	out := RenderString(m, ViewState{Focus: FocusGraph}, 56, 20, false)
	if strings.Contains(out, "[SW]") || strings.Contains(out, "bridge") {
		t.Fatalf("render drew partially visible node:\n%s", out)
	}
}

func TestRenderContextMenuForSelectedNode(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true}, 100, 30, false)
	for _, want := range []string{
		" Configuration >",
		" Move",
		" Delete",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing context menu item %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " lifecycle >") {
		t.Fatalf("render contains removed context item:\n%s", out)
	}
	if strings.Contains(out, " help") {
		t.Fatalf("render contains removed context help item:\n%s", out)
	}
	if strings.Contains(out, " create >") {
		t.Fatalf("render contains removed node create item:\n%s", out)
	}
}

func TestRenderContextSubmenuForSelectedNode(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:         1,
		Focus:            FocusGraph,
		ContextMenu:      true,
		ContextGroup:     "config-menu",
		ContextInSubmenu: true,
	}, 100, 30, false)
	for _, want := range []string{
		" Configuration >",
		" Run",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing config submenu item %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " status") {
		t.Fatalf("render contains removed status item:\n%s", out)
	}
	if strings.Contains(out, " back") {
		t.Fatalf("render contains removed back item:\n%s", out)
	}
	if strings.Contains(out, " create >") {
		t.Fatalf("render contains removed node create item:\n%s", out)
	}
}

func TestRenderContextSubmenuHasNoGap(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:         1,
		Focus:            FocusGraph,
		ContextMenu:      true,
		ContextSelected:  0,
		ContextGroup:     "config-menu",
		ContextInSubmenu: true,
	}, 100, 30, false)
	if !strings.Contains(out, "Configuration >   Run") {
		t.Fatalf("expected submenu to sit directly next to root menu:\n%s", out)
	}
	if strings.Contains(out, "Configuration >    Run") {
		t.Fatalf("found extra margin between context menus:\n%s", out)
	}
}

func TestRenderContextSubmenuHidesForRootAction(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:        1,
		Focus:           FocusGraph,
		ContextMenu:     true,
		ContextSelected: 2,
	}, 100, 30, false)
	if !strings.Contains(out, " Delete") {
		t.Fatalf("expected delete root item:\n%s", out)
	}
	if strings.Contains(out, "create-vm") {
		t.Fatalf("render shows removed node create submenu:\n%s", out)
	}
	if strings.Contains(out, "Run") || strings.Contains(out, "Stop") {
		t.Fatalf("render kept config submenu for delete root item:\n%s", out)
	}
	if strings.Contains(out, "Delete     Configuration >") {
		t.Fatalf("render duplicated root menu next to delete action:\n%s", out)
	}
}

func TestRenderContextSubmenuIgnoresStaleGroupForRootAction(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:         1,
		Focus:            FocusGraph,
		ContextMenu:      true,
		ContextSelected:  2,
		ContextGroup:     "config-menu",
		ContextInSubmenu: true,
	}, 100, 30, false)
	if strings.Contains(out, "Run") || strings.Contains(out, "Stop") {
		t.Fatalf("render used stale submenu group for delete root item:\n%s", out)
	}
}

func TestRenderContextFormSubmenuForSelectedNode(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu"}, 100, 30, false)
	for _, want := range []string{
		" Run",
		" Name        client01",
		" CPU         2",
		" Memory      2048M",
		" VNC         [ ]",
		" Disk        labs/mock/disks/client01.img",
		" ISO         ?",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing config form submenu item %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "backend=state") {
		t.Fatalf("render contains removed backend action %q", out)
	}
	if strings.Contains(out, "State") {
		t.Fatalf("render contains removed state field:\n%s", out)
	}
}

func TestRenderContextPowerActionUsesStopForRunningNode(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 0, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu"}, 100, 30, false)
	if !strings.Contains(out, "Stop") {
		t.Fatalf("render missing stop action for running node:\n%s", out)
	}
	if strings.Contains(out, "State") {
		t.Fatalf("render contains removed state field:\n%s", out)
	}
}

func TestRenderContextCheckboxUsesUppercaseX(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 0, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu"}, 100, 30, false)
	if !strings.Contains(out, "VNC         [X]") {
		t.Fatalf("render missing checked checkbox with uppercase X:\n%s", out)
	}
	if strings.Contains(out, "[x]") {
		t.Fatalf("render contains lowercase checkbox marker:\n%s", out)
	}
}

func TestRenderContextInlineEditValue(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:           1,
		Focus:              FocusGraph,
		ContextMenu:        true,
		ContextGroup:       "config-menu",
		ContextInSubmenu:   true,
		ContextSubSelected: 1,
		ContextEdit:        true,
		ContextEditValue:   "renamed",
		ContextEditCursor:  7,
	}, 100, 30, false)
	if !strings.Contains(out, "Name        renamed|") {
		t.Fatalf("render missing inline edit value:\n%s", out)
	}
	if strings.Contains(out, ":vm set") || strings.Contains(out, "vm set") {
		t.Fatalf("render routed inline edit through command bar:\n%s", out)
	}
}

func TestRenderNoHooksMenuItem(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu"}, 100, 30, false)
	if strings.Contains(out, "hooks >") {
		t.Fatalf("render contains removed hooks menu item: %q", out)
	}
}

func TestRenderContextMenuANSIStyle(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true}, 100, 30, true)
	for _, want := range []string{
		ansiBgGray + ansiWhite,
		ansiBgGray + ansiWhite + ansiBold,
		ansiBgCyan,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ANSI render missing context menu style %q:\n%q", want, out)
		}
	}
}

func TestContextMenuStartCalculatesVisibleWindow(t *testing.T) {
	if got := contextMenuStart(20, 21, 14); got != 7 {
		t.Fatalf("contextMenuStart = %d, want 7", got)
	}
	if got := contextMenuStart(0, 21, 14); got != 0 {
		t.Fatalf("contextMenuStart at top = %d, want 0", got)
	}
}

func TestRenderCommandConsole(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Focus:       FocusGraph,
		CommandMode: true,
		Command:     "help",
		Console:     []string{"help: :help :quit"},
		Message:     "old message",
	}, 100, 30, false)
	for _, want := range []string{":help"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing console text %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{"help: :help :quit", "old message"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("command render contains non-input line %q:\n%s", notWant, out)
		}
	}
	if count := strings.Count(out, ":help"); count != 1 {
		t.Fatalf("command render contains %d input lines, want 1:\n%s", count, out)
	}
}

func TestRenderStatusBarAlwaysVisible(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Focus: FocusGraph}, 100, 30, true)
	if !strings.Contains(out, ansiBgGray+ansiWhite) {
		t.Fatalf("render missing always-visible bottom bar:\n%q", out)
	}
}

func TestRenderGlobalContextMenuForEmptyModel(t *testing.T) {
	out := RenderString(Model{LabID: "empty"}, ViewState{Focus: FocusGraph, ContextMenu: true}, 80, 20, false)
	for _, want := range []string{
		"create >",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing global context item %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "list") {
		t.Fatalf("render contains removed global list item:\n%s", out)
	}
	if strings.Contains(out, " help") {
		t.Fatalf("render contains removed global help menu item:\n%s", out)
	}
}

func TestRenderGlobalCreateSubmenuForEmptyModel(t *testing.T) {
	out := RenderString(Model{LabID: "empty"}, ViewState{Focus: FocusGraph, ContextMenu: true, ContextGroup: "create-menu"}, 80, 20, false)
	for _, want := range []string{
		"create-vm",
		"create-switch",
		"create-external",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing global create submenu item %q:\n%s", want, out)
		}
	}
}

func TestMoveSelectionUsesVisualNearestNeighbor(t *testing.T) {
	m := Model{Nodes: []Node{
		{ID: "current", Type: NodeVM, X: 10, Y: 10},
		{ID: "yaml-next", Type: NodeVM, X: 80, Y: 80},
		{ID: "visual-down", Type: NodeVM, X: 11, Y: 14},
		{ID: "visual-right", Type: NodeVM, X: 16, Y: 11},
		{ID: "visual-left", Type: NodeVM, X: 6, Y: 9},
		{ID: "visual-up", Type: NodeVM, X: 12, Y: 6},
	}}

	tests := []struct {
		key  string
		want int
	}{
		{"down", 2},
		{"right", 3},
		{"left", 4},
		{"up", 5},
	}

	for _, tt := range tests {
		if got := MoveSelection(m, 0, tt.key); got != tt.want {
			t.Fatalf("%s selection = %d, want %d", tt.key, got, tt.want)
		}
	}
}

func TestMoveContextSelectionWraps(t *testing.T) {
	if got := MoveContextSelection(0, 3, "up"); got != 2 {
		t.Fatalf("up context selection = %d, want 2", got)
	}
	if got := MoveContextSelection(2, 3, "down"); got != 0 {
		t.Fatalf("down context selection = %d, want 0", got)
	}
}

func TestModelFromLabBuildsGraph(t *testing.T) {
	m := ModelFromLab(&lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			Name:     "debian",
			MemoryMB: 1024,
			CPUs:     1,
			Disk:     "labs/demo/disks/vm1.img",
			ISO:      "images/install.iso",
			Networks: []lab.VMNetwork{{Switch: "sw1"}},
		}},
		Switches:      []lab.Switch{{ID: "sw1", Mode: "bridge", ExternalLink: "uplink1"}},
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "br0"}},
		Layout: lab.Layout{Nodes: map[string]lab.Position{
			"vm1":     {X: 80, Y: 80},
			"sw1":     {X: 320, Y: 160},
			"uplink1": {X: 560, Y: 160},
		}},
	})
	if m.LabID != "demo" {
		t.Fatalf("LabID = %q, want demo", m.LabID)
	}
	if len(m.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(m.Nodes))
	}
	if len(m.Edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(m.Edges))
	}
	if m.Nodes[0].Key() != NodeKey(NodeVM, "vm1") || m.Nodes[0].Label != "debian" {
		t.Fatalf("first node = %#v, want vm1/debian", m.Nodes[0])
	}
	details := strings.Join(m.Nodes[0].Details, "\n")
	for _, want := range []string{"vnc=false", "disk=labs/demo/disks/vm1.img", "iso=images/install.iso"} {
		if !strings.Contains(details, want) {
			t.Fatalf("vm details missing %q: %#v", want, m.Nodes[0].Details)
		}
	}
	if got := m.Edges[0]; got.From != NodeKey(NodeVM, "vm1") || got.To != NodeKey(NodeSwitch, "sw1") {
		t.Fatalf("first edge = %#v, want vm1 → sw1", got)
	}
}
