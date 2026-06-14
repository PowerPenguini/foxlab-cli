package topologyui

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

func TestRoutePlannerKeepsRouteCellsOutsideNodeBoxes(t *testing.T) {
	from := rect{X: 10, Y: 2, W: nodeWidth, H: nodeHeight}
	to := rect{X: 10, Y: 10, W: nodeWidth, H: nodeHeight}
	bounds := rect{X: 0, Y: 0, W: 40, H: 20}
	planner := newRoutePlanner(bounds, []rect{from, to})

	route, ok := planner.planRoute(from, to)
	if !ok {
		t.Fatal("stacked edge was not routed")
	}
	if len(route.cells) == 0 {
		t.Fatal("route has no path cells")
	}
	for _, point := range route.cells {
		if pointInRect(point, from) || pointInRect(point, to) {
			t.Fatalf("route entered a node box at %#v: %#v", point, route.cells)
		}
	}

	g := newGrid(40, 20)
	drawBox(g, from, "", "")
	drawBox(g, to, "", "")
	drawRoutedEdge(g, route, ansiDim)
	drawRoutedEdgePorts(g, route)
	if got := g.Cells[route.start.border.Y*g.Width+route.start.border.X].Line & maskBetween(route.start.border, route.start.entry); got == 0 {
		t.Fatalf("source border is not connected to route:\n%s", g.String(false))
	}
	if got := g.Cells[route.end.border.Y*g.Width+route.end.border.X].Line & maskBetween(route.end.border, route.end.entry); got == 0 {
		t.Fatalf("target border is not connected to route:\n%s", g.String(false))
	}
}

func TestDrawRoutePortUsesCornerWhereEdgeTurnsIntoNodeStub(t *testing.T) {
	g := newGrid(40, 12)
	node := rect{X: 10, Y: 4, W: nodeWidth, H: nodeHeight}
	drawBox(g, node, "", "")

	fromLeft := routePort{
		border: routePoint{X: node.X + 3, Y: node.Y},
		entry:  routePoint{X: node.X + 3, Y: node.Y - 1},
		side:   routeSideTop,
	}
	fromRight := routePort{
		border: routePoint{X: node.X + node.W - 4, Y: node.Y},
		entry:  routePoint{X: node.X + node.W - 4, Y: node.Y - 1},
		side:   routeSideTop,
	}
	g.SetLine(fromLeft.entry.X, fromLeft.entry.Y, lineLeft, ansiDim)
	g.SetLine(fromRight.entry.X, fromRight.entry.Y, lineRight, ansiDim)

	drawRoutePort(g, fromLeft)
	drawRoutePort(g, fromRight)

	if got := g.Cells[fromLeft.entry.Y*g.Width+fromLeft.entry.X].Ch; got != boxTopRight {
		t.Fatalf("entry reached from left = %q, want corner %q", got, boxTopRight)
	}
	if got := g.Cells[fromRight.entry.Y*g.Width+fromRight.entry.X].Ch; got != boxTopLeft {
		t.Fatalf("entry reached from right = %q, want corner %q", got, boxTopLeft)
	}
	if got := g.Cells[fromLeft.border.Y*g.Width+fromLeft.border.X].Ch; got != '┴' {
		t.Fatalf("node border port = %q, want original border tee", got)
	}
}

func TestRenderSeparatesSharedNodeRoutes(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "vm3", Type: NodeVM, Label: "vm3", State: "defined", X: 4, Y: 7},
			{ID: "hello", Type: NodeVM, Label: "hello", State: "defined", X: 73, Y: 7},
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "bridge", X: 53, Y: 14},
		},
		Edges: []Edge{
			{From: NodeKey(NodeVM, "vm3"), To: NodeKey(NodeVM, "hello")},
			{From: NodeKey(NodeVM, "vm3"), To: NodeKey(NodeSwitch, "sw1")},
		},
	}
	bounds := rect{X: 0, Y: 0, W: 90, H: 22}
	rects := layoutNodeRects(m, bounds)
	planner := newRoutePlanner(bounds, visibleNodeRects(rects, bounds))
	routes := planVisibleRoutes(planner, m.Edges, rects, bounds)
	if len(routes) != len(m.Edges) {
		t.Fatalf("planned routes = %d, want %d", len(routes), len(m.Edges))
	}
	assertRoutesDoNotShareCells(t, routes)

	out := RenderString(m, ViewState{Focus: FocusGraph}, bounds.W, bounds.H, false)
	if strings.Contains(out, string(lineCross)) {
		t.Fatalf("render crossed routes:\n%s", out)
	}
}

func TestRenderAvoidsCrossingNearSharedTarget(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "vm2", Type: NodeVM, Label: "vm2", State: "defined", X: 4, Y: 3},
			{ID: "vm3", Type: NodeVM, Label: "vm3", State: "defined", X: 4, Y: 7},
			{ID: "hello", Type: NodeVM, Label: "hello", State: "defined", X: 73, Y: 7},
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "bridge", X: 53, Y: 14},
		},
		Edges: []Edge{
			{From: NodeKey(NodeVM, "vm2"), To: NodeKey(NodeVM, "hello")},
			{From: NodeKey(NodeVM, "vm3"), To: NodeKey(NodeVM, "hello")},
			{From: NodeKey(NodeVM, "vm3"), To: NodeKey(NodeSwitch, "sw1")},
		},
	}
	out := RenderString(m, ViewState{Focus: FocusGraph}, 90, 22, false)
	if strings.Contains(out, string(lineCross)) {
		t.Fatalf("render crossed routes near shared target:\n%s", out)
	}
	g := renderGrid(m, ViewState{Focus: FocusGraph}, 90, 22)
	rects := layoutNodeRects(m, rect{X: 0, Y: 0, W: 90, H: 22})
	hello := rects[NodeKey(NodeVM, "hello")]
	if got := g.Cells[hello.Y*g.Width+hello.X].Ch; got != boxTopLeft {
		t.Fatalf("hello top-left corner was used as a connection: %q\n%s", got, g.String(false))
	}
}

func TestSharedTargetKeepsUpperSourceOnUpperSidePort(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "vm2", Type: NodeVM, Label: "vm2", State: "defined", X: 4, Y: 3},
			{ID: "vm3", Type: NodeVM, Label: "vm3", State: "defined", X: 4, Y: 7},
			{ID: "hello", Type: NodeVM, Label: "hello", State: "defined", X: 73, Y: 7},
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "bridge", X: 53, Y: 14},
		},
		Edges: []Edge{
			{From: NodeKey(NodeVM, "hello"), To: NodeKey(NodeVM, "vm3")},
			{From: NodeKey(NodeVM, "hello"), To: NodeKey(NodeVM, "vm2")},
			{From: NodeKey(NodeVM, "vm3"), To: NodeKey(NodeSwitch, "sw1")},
		},
	}
	bounds := rect{X: 0, Y: 0, W: 90, H: 22}
	rects := layoutNodeRects(m, bounds)
	planner := newRoutePlanner(bounds, visibleNodeRects(rects, bounds))
	routes := planVisibleRoutes(planner, m.Edges, rects, bounds)

	helloToVM2 := routeForEdge(t, routes, NodeKey(NodeVM, "hello"), NodeKey(NodeVM, "vm2"))
	helloToVM3 := routeForEdge(t, routes, NodeKey(NodeVM, "hello"), NodeKey(NodeVM, "vm3"))
	if helloToVM2.start.side != routeSideLeft || helloToVM3.start.side != routeSideLeft {
		t.Fatalf("hello routes should use left side ports: vm2=%#v vm3=%#v", helloToVM2.start, helloToVM3.start)
	}
	if helloToVM2.start.border.Y >= helloToVM3.start.border.Y {
		t.Fatalf("upper source should use upper hello port: vm2=%#v vm3=%#v", helloToVM2.start, helloToVM3.start)
	}
}

func routeForEdge(t *testing.T, routes []visibleEdge, from, to string) edgeRoute {
	t.Helper()
	for _, visible := range routes {
		if visible.edge.From == from && visible.edge.To == to {
			return visible.route
		}
	}
	t.Fatalf("missing route %s -> %s", from, to)
	return edgeRoute{}
}

func assertRoutesDoNotShareCells(t *testing.T, routes []visibleEdge) {
	t.Helper()
	seen := map[routePoint]int{}
	for routeIndex, visible := range routes {
		for _, point := range visible.route.cells {
			if previous, ok := seen[point]; ok {
				t.Fatalf("routes %d and %d share route cell %#v", previous, routeIndex, point)
			}
			seen[point] = routeIndex
		}
	}
}

func TestDrawEdgeAvoidsOverlappingNodeBoxes(t *testing.T) {
	from := rect{X: 30, Y: 8, W: nodeWidth, H: nodeHeight}
	to := rect{X: 20, Y: 10, W: nodeWidth, H: nodeHeight}
	bounds := rect{X: 0, Y: 0, W: 60, H: 20}
	planner := newRoutePlanner(bounds, []rect{from, to})

	route, ok := planner.planRoute(from, to)
	if !ok {
		t.Fatal("overlapping nearby nodes should still route around boxes when a lane exists")
	}
	for _, point := range route.cells {
		if pointInRect(point, from) || pointInRect(point, to) {
			t.Fatalf("route entered an overlapping node box at %#v: %#v", point, route.cells)
		}
	}
}

func TestRenderSelectedRouteKeepsBorderStyle(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "macnat-bridge", X: 12, Y: 2},
			{ID: "hello", Type: NodeVM, Label: "hello", State: "missing", X: 24, Y: 6},
		},
		Edges: []Edge{{From: NodeKey(NodeSwitch, "sw1"), To: NodeKey(NodeVM, "hello")}},
	}

	ansiOut := RenderString(m, ViewState{Selected: 1, Focus: FocusGraph}, 60, 16, true)
	if !strings.Contains(ansiOut, ansiBold+ansiBrightCyan) {
		t.Fatalf("selected border was not highlighted:\n%q", ansiOut)
	}
	if strings.Contains(ansiOut, ansiDim+"[VM] hello") {
		t.Fatalf("selected node text was overwritten by edge style:\n%q", ansiOut)
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
	rects := layoutNodeRects(m, rect{X: 0, Y: 0, W: 60, H: 16})
	assertBorderStyle(t, g, rects[NodeKey(NodeVM, "hello")], ansiBold+ansiBrightCyan)
	assertBorderStyle(t, g, rects[NodeKey(NodeSwitch, "sw1")], "")
	if got := g.Cells[rects[NodeKey(NodeVM, "hello")].Y*g.Width+rects[NodeKey(NodeVM, "hello")].X-1].Style; got == ansiBold+ansiBrightCyan {
		t.Fatalf("selected border style leaked to connector before box")
	}
}

func assertBorderStyle(t *testing.T, g *grid, r rect, want string) {
	t.Helper()
	for x := r.X; x < r.X+r.W; x++ {
		for _, y := range []int{r.Y, r.Y + r.H - 1} {
			if got := g.Cells[y*g.Width+x].Style; got != want {
				t.Fatalf("border style at (%d,%d) = %q, want %q", x, y, got, want)
			}
		}
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		for _, x := range []int{r.X, r.X + r.W - 1} {
			if got := g.Cells[y*g.Width+x].Style; got != want {
				t.Fatalf("border style at (%d,%d) = %q, want %q", x, y, got, want)
			}
		}
	}
}

func TestRenderSelectedRouteDoesNotCreateCrossing(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "hello", Type: NodeVM, Label: "hello", State: "missing", X: 24, Y: 2},
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "macnat-bridge", X: 12, Y: 6},
		},
		Edges: []Edge{{From: NodeKey(NodeVM, "hello"), To: NodeKey(NodeSwitch, "sw1")}},
	}

	out := RenderString(m, ViewState{Selected: 0, Focus: FocusGraph}, 60, 16, false)
	if strings.Contains(out, string(lineCross)) {
		t.Fatalf("route crossed itself or another route:\n%s", out)
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
	bounds := rect{X: 0, Y: 0, W: 90, H: 24}

	baseRects := layoutNodeRects(base, bounds)
	movedRects := layoutNodeRects(moved, bounds)

	if baseRects[NodeKey(NodeVM, "bottom")] != movedRects[NodeKey(NodeVM, "bottom")] {
		t.Fatalf("bottom node moved when only top node changed: before=%#v after=%#v", baseRects[NodeKey(NodeVM, "bottom")], movedRects[NodeKey(NodeVM, "bottom")])
	}
}

func TestLayoutNodeRectsBottomNodeCanMoveVertically(t *testing.T) {
	bounds := rect{X: 0, Y: 0, W: 90, H: 35}
	up := layoutNodeRects(Model{Nodes: []Node{{ID: "bottom", Type: NodeVM, X: 4, Y: 28}}}, bounds)[NodeKey(NodeVM, "bottom")]
	base := layoutNodeRects(Model{Nodes: []Node{{ID: "bottom", Type: NodeVM, X: 4, Y: 29}}}, bounds)[NodeKey(NodeVM, "bottom")]
	down := layoutNodeRects(Model{Nodes: []Node{{ID: "bottom", Type: NodeVM, X: 4, Y: 30}}}, bounds)[NodeKey(NodeVM, "bottom")]

	if !(up.Y < base.Y && base.Y < down.Y) {
		t.Fatalf("bottom vertical movement is not visible: up=%#v base=%#v down=%#v", up, base, down)
	}
}

func TestLayoutNodeRectsDoNotClampOffscreenNodes(t *testing.T) {
	bounds := rect{X: 0, Y: 0, W: 40, H: 20}
	rects := layoutNodeRects(Model{Nodes: []Node{{ID: "right", Type: NodeSwitch, X: 80, Y: 2}}}, bounds)
	got := rects[NodeKey(NodeSwitch, "right")]
	if got.X != 80 {
		t.Fatalf("offscreen node x = %d, want unclamped x=80", got.X)
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
		ContextSelected: 3,
	}, 100, 30, false)
	if !strings.Contains(out, " Delete") {
		t.Fatalf("expected delete root item:\n%s", out)
	}
	if strings.Contains(out, "add vm") || strings.Contains(out, "create-vm") {
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
		ContextSelected:  3,
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
	if strings.Contains(out, "nic0") {
		t.Fatalf("render kept NIC detail in config submenu:\n%s", out)
	}
}

func TestRenderNICSubmenuShowsNICDetails(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true, ContextSelected: 1, ContextGroup: "nic-menu"}, 100, 30, false)
	for _, want := range []string{
		" Add NIC",
		" nic0 → lan",
		" X ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing NIC submenu item %q:\n%s", want, out)
		}
	}
	ansiOut := RenderString(MockModel(), ViewState{
		Selected:           1,
		Focus:              FocusGraph,
		ContextMenu:        true,
		ContextSelected:    1,
		ContextGroup:       "nic-menu",
		ContextInSubmenu:   true,
		ContextSubSelected: 1,
		ContextDeleteNIC:   true,
	}, 100, 30, true)
	if !strings.Contains(ansiOut, ansiBgRed+ansiWhite+ansiBold+" X ") {
		t.Fatalf("render did not mark selected nic delete X with red background and white foreground:\n%q", ansiOut)
	}
	if strings.Contains(ansiOut, "\x1b[91m"+ansiBold+" X ") {
		t.Fatalf("render made selected nic delete X red instead of keeping the glyph white:\n%q", ansiOut)
	}
	g := renderGrid(MockModel(), ViewState{
		Selected:           1,
		Focus:              FocusGraph,
		ContextMenu:        true,
		ContextSelected:    1,
		ContextGroup:       "nic-menu",
		ContextInSubmenu:   true,
		ContextSubSelected: 1,
		ContextDeleteNIC:   true,
	}, 100, 30)
	node := MockModel().Nodes[1]
	nodeRect := layoutNodeRects(MockModel(), rect{X: 0, Y: 0, W: 100, H: 30})[node.Key()]
	rootItems := contextMenuItems(node, "")
	rootMenuW := contextMenuWidth(rootItems)
	rootX := nodeRect.X + nodeRect.W + 1
	rootActive := normalizedMenuSelection(1, len(rootItems))
	rootMenuH := min(len(rootItems), 30)
	rootStart := contextMenuStart(rootActive, len(rootItems), rootMenuH)
	subItems := contextMenuSubmenuItems(node, true, "nic-menu")
	subMenuW := contextMenuWidth(subItems)
	subX := rootX + rootMenuW
	subY := nodeRect.Y + (rootActive - rootStart)
	nicRowY := subY + 1
	if got := g.Cells[nicRowY*g.Width+subX+subMenuW-4].Ch; got != ' ' {
		t.Fatalf("nic delete button gap = %q, want one blank cell before button", got)
	}
	rightButton := []rune{' ', 'X', ' '}
	for offset, want := range rightButton {
		x := subX + subMenuW - 3 + offset
		if got := g.Cells[nicRowY*g.Width+x].Ch; got != want {
			t.Fatalf("nic delete button cell at offset %d = %q, want %q", offset, got, want)
		}
	}
	if strings.Contains(out, "Connect NIC") {
		t.Fatalf("render kept separate connect action in NIC submenu:\n%s", out)
	}
	if strings.Contains(out, " CPU") || strings.Contains(out, " Memory") {
		t.Fatalf("render leaked configuration fields into NIC submenu:\n%s", out)
	}
}

func TestRenderContainerConfigSubmenuDoesNotShowNICSwitch(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 2, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu"}, 100, 30, false)
	if strings.Contains(out, "Switch") || strings.Contains(out, "nic0") {
		t.Fatalf("render kept NIC config in container submenu:\n%s", out)
	}
}

func TestRenderConnectTargetNICMenu(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Focus:             FocusGraph,
		ConnectMode:       true,
		ConnectNodeID:     "router",
		ConnectNodeType:   NodeVM,
		ConnectNICIndex:   "0",
		ConnectTargetMenu: true,
		ConnectTargetID:   "client01",
		ConnectTargetType: NodeVM,
	}, 100, 30, false)
	for _, want := range []string{
		" nic0 → lan",
		" New NIC",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing connect target menu item %q:\n%s", want, out)
		}
	}
}

func TestRenderConnectModeDrawsDashedPreview(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "vm1", Type: NodeVM, Label: "vm1", State: "defined", X: 2, Y: 2},
			{ID: "lan", Type: NodeSwitch, Label: "lan", State: "bridge", X: 30, Y: 2},
		},
	}
	g := renderGrid(m, ViewState{
		Focus:           FocusGraph,
		Selected:        1,
		ConnectMode:     true,
		ConnectNodeID:   "vm1",
		ConnectNodeType: NodeVM,
		ConnectNICIndex: "0",
	}, 70, 20)

	source := layoutNodeRects(m, rect{X: 0, Y: 0, W: 70, H: 20})[NodeKey(NodeVM, "vm1")]
	y := source.Y + source.H/2
	if got := g.Cells[y*g.Width+source.X+source.W].Ch; got != previewLineHorizontal {
		t.Fatalf("connect preview first dash = %q, want %q", got, previewLineHorizontal)
	}
	if got := g.Cells[y*g.Width+source.X+source.W+1].Ch; got != ' ' {
		t.Fatalf("connect preview gap = %q, want blank", got)
	}
	if got := g.Cells[y*g.Width+source.X+source.W-1].Ch; got != lineVertical {
		t.Fatalf("connect preview changed source border = %q, want vertical border", got)
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
	out := RenderString(Model{ID: "empty"}, ViewState{Focus: FocusGraph, ContextMenu: true}, 80, 20, false)
	for _, want := range []string{
		"add >",
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
	out := RenderString(Model{ID: "empty"}, ViewState{Focus: FocusGraph, ContextMenu: true, ContextGroup: "create-menu"}, 80, 20, false)
	for _, want := range []string{
		"add vm",
		"add cont",
		"add sw",
		"create external",
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
	if m.ID != "demo" {
		t.Fatalf("ID = %q, want demo", m.ID)
	}
	if len(m.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(m.Nodes))
	}
	if len(m.Edges) != 2 {
		t.Fatalf("edges = %d, want 2", len(m.Edges))
	}
	if m.Nodes[0].Key() != NodeKey(NodeVM, "vm1") || m.Nodes[0].Badge != "VM" || m.Nodes[0].Label != "debian" {
		t.Fatalf("first node = %#v, want vm1/VM/debian", m.Nodes[0])
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
