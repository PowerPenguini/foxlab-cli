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
		"[UP] wlp0s20f3",
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
		"╭",
		"╰",
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
	if got := g.Cells[route.start.entry.Y*g.Width+route.start.entry.X].Line & maskBetween(route.start.entry, route.start.border); got == 0 {
		t.Fatalf("source entry is not connected to route:\n%s", g.String(false))
	}
	if got := g.Cells[route.end.entry.Y*g.Width+route.end.entry.X].Line & maskBetween(route.end.entry, route.end.border); got == 0 {
		t.Fatalf("target entry is not connected to route:\n%s", g.String(false))
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

	drawRoutePort(g, fromLeft, themeRoute)
	drawRoutePort(g, fromRight, themeRoute)

	if got := g.Cells[fromLeft.entry.Y*g.Width+fromLeft.entry.X].Ch; got != boxTopRight {
		t.Fatalf("entry reached from left = %q, want corner %q", got, boxTopRight)
	}
	if got := g.Cells[fromRight.entry.Y*g.Width+fromRight.entry.X].Ch; got != boxTopLeft {
		t.Fatalf("entry reached from right = %q, want corner %q", got, boxTopLeft)
	}
	if got := g.Cells[fromLeft.border.Y*g.Width+fromLeft.border.X].Ch; got != lineHorizontal {
		t.Fatalf("node border port = %q, want untouched border", got)
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
	if got := g.Cells[(hello.Y+1)*g.Width+hello.X].Style; got != nodeAccentStyle(NodeVM, false) {
		t.Fatalf("hello card panel was overwritten by route: %q\n%s", got, g.String(false))
	}
}

func TestInfrastructureRoutesDoNotDetourAroundWholeLayout(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "vm2", Type: NodeVM, Label: "vm2", State: "defined", X: 4, Y: 3},
			{ID: "vm3", Type: NodeVM, Label: "vm3", State: "defined", X: 4, Y: 7},
			{ID: "vm4", Type: NodeVM, Label: "vm4", State: "defined", X: 4, Y: 23},
			{ID: "hello", Type: NodeVM, Label: "hello", State: "defined", X: 71, Y: 4},
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "macnat-bridge", X: 53, Y: 14},
			{ID: "link1", Type: NodeExternal, Label: "in2", State: "link", X: 61, Y: 29},
		},
		Edges: []Edge{
			{From: NodeKey(NodeVM, "vm2"), To: NodeKey(NodeSwitch, "sw1")},
			{From: NodeKey(NodeVM, "vm3"), To: NodeKey(NodeSwitch, "sw1")},
			{From: NodeKey(NodeVM, "vm4"), To: NodeKey(NodeSwitch, "sw1")},
			{From: NodeKey(NodeSwitch, "sw1"), To: NodeKey(NodeExternal, "link1")},
			{From: NodeKey(NodeVM, "hello"), To: NodeKey(NodeExternal, "link1")},
			{From: NodeKey(NodeVM, "hello"), To: NodeKey(NodeVM, "vm3")},
		},
	}
	bounds := rect{X: 0, Y: 0, W: 90, H: 40}
	rects := layoutNodeRects(m, bounds)
	planner := newRoutePlanner(bounds, visibleNodeRects(rects, bounds))
	routes := planVisibleRoutes(planner, m.Edges, rects, bounds)

	vm3ToSW := routeForEdge(t, routes, NodeKey(NodeVM, "vm3"), NodeKey(NodeSwitch, "sw1"))
	vm4ToSW := routeForEdge(t, routes, NodeKey(NodeVM, "vm4"), NodeKey(NodeSwitch, "sw1"))
	helloToVM3 := routeForEdge(t, routes, NodeKey(NodeVM, "hello"), NodeKey(NodeVM, "vm3"))
	if len(vm3ToSW.cells) > 60 {
		t.Fatalf("vm3 switch route detoured around layout: len=%d cells=%#v", len(vm3ToSW.cells), vm3ToSW.cells)
	}
	if len(vm4ToSW.cells) > 70 {
		t.Fatalf("vm4 switch route detoured around layout: len=%d cells=%#v", len(vm4ToSW.cells), vm4ToSW.cells)
	}
	if routeHeight(helloToVM3.cells) > 6 {
		t.Fatalf("direct workload route made a large box: height=%d cells=%#v", routeHeight(helloToVM3.cells), helloToVM3.cells)
	}

	out := RenderString(m, ViewState{Focus: FocusGraph}, bounds.W, bounds.H, false)
	if strings.Contains(out, "┌───────────────────────────────────┐") || strings.Contains(out, "└───────────────────────────────────────────┘") {
		t.Fatalf("render still contains whole-layout detour boxes:\n%s", out)
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

func routeHeight(cells []routePoint) int {
	if len(cells) == 0 {
		return 0
	}
	minY, maxY := cells[0].Y, cells[0].Y
	for _, cell := range cells {
		minY = min(minY, cell.Y)
		maxY = max(maxY, cell.Y)
	}
	return maxY - minY + 1
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
			{ID: "hello", Type: NodeVM, Label: "hello", State: "missing", X: 24, Y: 8},
		},
		Edges: []Edge{{From: NodeKey(NodeSwitch, "sw1"), To: NodeKey(NodeVM, "hello")}},
	}

	out := RenderString(m, ViewState{Selected: 1, Focus: FocusGraph}, 60, 16, false)
	if strings.Contains(out, "╭") || strings.Contains(out, "╰") {
		t.Fatalf("node cards still render frame corners:\n%s", out)
	}
	ansiOut := RenderString(m, ViewState{Selected: 1, Focus: FocusGraph}, 60, 16, true)
	if !strings.Contains(ansiOut, nodePanelStyle(NodeVM, true)+ansiBold+ansiBrightCyan) {
		t.Fatalf("selected card accent was not highlighted:\n%q", ansiOut)
	}
	if strings.Contains(ansiOut, ansiDim+"[VM] hello") {
		t.Fatalf("selected node text was overwritten by edge style:\n%q", ansiOut)
	}
}

func TestRenderSelectedRouteUsesActiveStyle(t *testing.T) {
	m := MockModel()
	g := renderGrid(m, ViewState{Selected: 0, Focus: FocusGraph}, 100, 30)
	found := false
	for _, cell := range g.Cells {
		if cell.Line != 0 && cell.Style == themeRouteActive {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("selected route was not rendered with active style:\n%s", g.String(true))
	}
}

func TestRenderNodeCardsKeepPanelStyleAfterEdges(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "sw1", Type: NodeSwitch, Label: "sw1", State: "macnat-bridge", X: 12, Y: 2},
			{ID: "hello", Type: NodeVM, Label: "hello", State: "missing", X: 24, Y: 8},
		},
		Edges: []Edge{{From: NodeKey(NodeSwitch, "sw1"), To: NodeKey(NodeVM, "hello")}},
	}

	g := renderGrid(m, ViewState{Selected: 1, Focus: FocusGraph}, 60, 16)
	rects := layoutNodeRects(m, rect{X: 0, Y: 0, W: 60, H: 16})
	selected := rects[NodeKey(NodeVM, "hello")]
	if got := g.Cells[(selected.Y+1)*g.Width+selected.X+1].Style; !strings.HasPrefix(got, nodePanelStyle(NodeVM, true)) {
		t.Fatalf("selected card style = %q, want prefix %q", got, nodePanelStyle(NodeVM, true))
	}
	normal := rects[NodeKey(NodeSwitch, "sw1")]
	if got := g.Cells[(normal.Y+1)*g.Width+normal.X+1].Style; !strings.HasPrefix(got, nodePanelStyle(NodeSwitch, false)) {
		t.Fatalf("normal card style = %q, want prefix %q", got, nodePanelStyle(NodeSwitch, false))
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

func TestRenderCanvasUsesBlackTerminalBackground(t *testing.T) {
	g := renderGrid(MockModel(), ViewState{Focus: FocusGraph}, 100, 30)
	if got := g.Cells[1*g.Width+1].Style; got != themeTerminal {
		t.Fatalf("empty canvas style = %q, want %q", got, themeTerminal)
	}
	out := g.String(true)
	if !strings.Contains(out, themeTerminal) {
		t.Fatalf("ANSI render missing terminal background:\n%q", out)
	}
}

func TestRenderSelectedNodeUsesSubtlyBrighterPanelColor(t *testing.T) {
	g := renderGrid(MockModel(), ViewState{Selected: 0, Focus: FocusGraph}, 100, 30)
	rects := layoutNodeRects(MockModel(), rect{X: 0, Y: 0, W: 100, H: 30})
	checks := []struct {
		key  string
		typ  string
		name string
	}{
		{NodeKey(NodeVM, "router"), NodeVM, "router"},
		{NodeKey(NodeContainer, "web"), NodeContainer, "container"},
		{NodeKey(NodeSwitch, "edge"), NodeSwitch, "switch"},
		{NodeKey(NodeExternal, "uplink0"), NodeExternal, "uplink"},
	}
	normalPanel := nodePanelStyle(NodeVM, false)
	selectedPanel := nodePanelStyle(NodeVM, true)
	if selectedPanel == normalPanel {
		t.Fatalf("selected panel style matches normal panel: %q", selectedPanel)
	}
	for _, check := range checks {
		r := rects[check.key]
		if r.W == 0 {
			t.Fatalf("missing rect for %s", check.key)
		}
		selected := check.key == NodeKey(NodeVM, "router")
		want := nodePanelStyle(check.typ, selected)
		if !selected && want != normalPanel {
			t.Fatalf("%s panel style = %q, want shared normal %q", check.name, want, normalPanel)
		}
		if selected && want != selectedPanel {
			t.Fatalf("%s panel style = %q, want selected %q", check.name, want, selectedPanel)
		}
		if got := g.Cells[(r.Y+1)*g.Width+r.X+2].Style; !strings.HasPrefix(got, want) {
			t.Fatalf("%s card style = %q, want prefix %q", check.name, got, want)
		}
		accentWant := nodeAccentStyle(check.typ, selected)
		if got := g.Cells[(r.Y+1)*g.Width+r.X].Style; got != accentWant {
			t.Fatalf("%s accent style = %q, want %q", check.name, got, accentWant)
		}
	}
}

func TestRenderNodeAccentUsesNodeTypeColor(t *testing.T) {
	checks := map[string]string{
		NodeVM:        ansiBgTerminal + ansiBrightCyan,
		NodeContainer: ansiBgTerminal + ansiGreen,
		NodeSwitch:    ansiBgTerminal + ansiYellow,
		NodeExternal:  ansiBgTerminal + ansiBrightMagenta,
	}
	for nodeType, want := range checks {
		if got := nodeAccentStyle(nodeType, false); got != want {
			t.Fatalf("%s accent style = %q, want %q", nodeType, got, want)
		}
		if got := nodeAccentStyle(nodeType, true); got != want+ansiBold {
			t.Fatalf("%s selected accent style = %q, want %q", nodeType, got, want+ansiBold)
		}
	}
}

func TestRenderTopFocusDoesNotHighlightSelectedNode(t *testing.T) {
	m := MockModel()
	g := renderGrid(m, ViewState{Selected: 1, Focus: FocusTop}, 100, 30)
	nodeRect := layoutNodeRects(m, rect{X: 0, Y: 0, W: 100, H: 30})[m.Nodes[1].Key()]
	want := nodePanelStyle(m.Nodes[1].Type, false)
	for y := nodeRect.Y; y < nodeRect.Y+nodeRect.H; y++ {
		if got := g.Cells[y*g.Width+nodeRect.X].Style; got != nodeAccentStyle(m.Nodes[1].Type, false) {
			t.Fatalf("accent style at y=%d = %q, want %q", y, got, nodeAccentStyle(m.Nodes[1].Type, false))
		}
		if got := g.Cells[y*g.Width+nodeRect.X+1].Style; !strings.HasPrefix(got, want) {
			t.Fatalf("card style at y=%d = %q, want prefix %q", y, got, want)
		}
	}
}

func TestRenderSidebarDoesNotShowFooterContext(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 0, Focus: FocusGraph}, 120, 30, false)
	for _, notWant := range []string{"lab mock", ": actions"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("render still shows sidebar footer context %q:\n%s", notWant, out)
		}
	}
	if strings.Contains(out, "mode:") {
		t.Fatalf("render still shows mode in context:\n%s", out)
	}
}

func TestRenderShowsModernSpinnersForProgressStates(t *testing.T) {
	m := Model{
		ID:    "demo",
		Nodes: []Node{{ID: "vm1", Type: NodeVM, Badge: "VM", Label: "vm1", State: "starting", X: 4, Y: 3}},
	}
	state := ViewState{Selected: 0, Focus: FocusGraph, StatusRefreshing: true, AnimationFrame: 1}
	out := RenderString(m, state, 120, 20, false)
	for _, want := range []string{
		spinner(1) + " starting",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing spinner text %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "lab demo | VM vm1") {
		t.Fatalf("render still shows selected node in status:\n%s", out)
	}
	if strings.Contains(out, "mode:") {
		t.Fatalf("render still shows mode in status:\n%s", out)
	}
	if strings.Contains(out, "refreshing runtime status") {
		t.Fatalf("render shows refresh text in bottom console:\n%s", out)
	}
}

func TestRenderSelectedOverlappingNodeStaysOnTop(t *testing.T) {
	m := Model{Nodes: []Node{
		{ID: "first", Type: NodeVM, Badge: "VM", Label: "first", State: "defined", X: 4, Y: 3},
		{ID: "second", Type: NodeContainer, Badge: "CT", Label: "second", State: "missing", X: 4, Y: 3},
	}}
	out := RenderString(m, ViewState{Selected: 0, Focus: FocusGraph}, 60, 16, false)
	if !strings.Contains(out, "[VM] first") {
		t.Fatalf("selected overlapping node is not visible:\n%s", out)
	}
	if strings.Contains(out, "[CT] second") {
		t.Fatalf("unselected overlapping node covered selected node:\n%s", out)
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

func TestRenderUsesViewportPan(t *testing.T) {
	m := Model{Nodes: []Node{{ID: "right", Type: NodeSwitch, Label: "right", X: 70, Y: 2, State: "bridge"}}}
	if out := RenderString(m, ViewState{Focus: FocusGraph}, 56, 20, false); strings.Contains(out, "right") {
		t.Fatalf("render drew unpanned offscreen node:\n%s", out)
	}
	out := RenderString(m, ViewState{Focus: FocusGraph, PanX: -30}, 56, 20, false)
	if !strings.Contains(out, "right") {
		t.Fatalf("render did not draw panned node:\n%s", out)
	}
}

func TestRenderDrawsPartialEdgeToOffscreenEndpoint(t *testing.T) {
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
	if !strings.Contains(out, "───────") {
		t.Fatalf("render did not draw partial edge to offscreen endpoint:\n%s", out)
	}
}

func TestRenderKeepsPannedVisibleNodeConnectedToOffscreenEndpoint(t *testing.T) {
	m := Model{
		Nodes: []Node{
			{ID: "hidden", Type: NodeSwitch, X: 4, Y: 8, State: "bridge"},
			{ID: "uplink", Type: NodeExternal, Badge: "UP", Label: "uplink2", X: 70, Y: 10, State: "link"},
		},
		Edges: []Edge{{From: NodeKey(NodeSwitch, "hidden"), To: NodeKey(NodeExternal, "uplink")}},
	}
	out := RenderString(m, ViewState{Focus: FocusGraph, PanX: -30}, 56, 20, false)
	if strings.Contains(out, "[SW] hidden") {
		t.Fatalf("render drew hidden offscreen node:\n%s", out)
	}
	if !strings.Contains(out, "[UP] uplink2") {
		t.Fatalf("render did not draw panned visible endpoint:\n%s", out)
	}
	if !strings.Contains(out, "───────") {
		t.Fatalf("render did not keep panned endpoint connected:\n%s", out)
	}
}

func TestRenderClipsPartiallyVisibleNodes(t *testing.T) {
	m := Model{Nodes: []Node{{ID: "partial", Type: NodeSwitch, X: 50, Y: 5, Label: "partial", State: "bridge"}}}
	out := RenderString(m, ViewState{Focus: FocusGraph}, 56, 20, false)
	if !strings.Contains(out, "[SW]") || !strings.Contains(out, "Mode") {
		t.Fatalf("render did not clip partially visible node:\n%s", out)
	}
	if strings.Contains(out, "partial") || strings.Contains(out, "Bridge") {
		t.Fatalf("render did not cut node at viewport edge:\n%s", out)
	}
}

func TestRenderContextMenuForSelectedNode(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true}, 100, 30, false)
	for _, want := range []string{
		" Configuration ",
		" NIC ",
		" Disk ",
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
		" Configuration ",
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

func TestRenderSwitchUplinkSubmenuShowsUplinks(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 3, Focus: FocusGraph, ContextMenu: true}, 100, 30, false)
	if !strings.Contains(out, " Uplink ") {
		t.Fatalf("render missing switch uplink root item:\n%s", out)
	}
	if strings.Contains(out, " Connect") {
		t.Fatalf("render kept switch connect root action:\n%s", out)
	}

	out = RenderString(MockModel(), ViewState{
		Selected:         3,
		Focus:            FocusGraph,
		ContextMenu:      true,
		ContextGroup:     "config-menu",
		ContextInSubmenu: true,
	}, 100, 30, false)
	if strings.Contains(out, " uplink=") || strings.Contains(out, " external=") {
		t.Fatalf("render showed single uplink field in switch configuration:\n%s", out)
	}

	out = RenderString(MockModel(), ViewState{
		Selected:         3,
		Focus:            FocusGraph,
		ContextMenu:      true,
		ContextSelected:  1,
		ContextGroup:     "uplink-menu",
		ContextInSubmenu: true,
	}, 100, 30, false)
	for _, want := range []string{
		" Attach Uplink",
		" wlp0s20f3",
		" X ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing switch uplink submenu item %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " uplink0") {
		t.Fatalf("render showed uplink id instead of name:\n%s", out)
	}
	if strings.Contains(out, " hostnet") {
		t.Fatalf("render showed unrelated uplink in switch submenu:\n%s", out)
	}
	ansiOut := RenderString(MockModel(), ViewState{
		Selected:            3,
		Focus:               FocusGraph,
		ContextMenu:         true,
		ContextSelected:     1,
		ContextGroup:        "uplink-menu",
		ContextInSubmenu:    true,
		ContextSubSelected:  1,
		ContextDeleteUplink: true,
	}, 100, 30, true)
	if !strings.Contains(ansiOut, ansiBgRed+ansiWhite+ansiBold+" X ") {
		t.Fatalf("render did not mark selected uplink X with red background and white foreground:\n%q", ansiOut)
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
		t.Fatalf("expected context submenu to sit next to node menu:\n%s", out)
	}
}

func TestRenderWideInspectorShowsSelectedNodeDetails(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 0, Focus: FocusGraph}, 120, 30, false)
	for _, want := range []string{
		"[VM] router",
		"State",
		"state ● running",
		"Identity",
		"id router",
		"Configuration",
		"cpu 2",
		"mem 2G",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("wide render missing inspector detail %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{"actions Space menu", ": actions"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("wide render still shows inspector action hint %q:\n%s", notWant, out)
		}
	}
}

func TestRenderInspectorSeparatesNodeTypeAndName(t *testing.T) {
	m := Model{
		ID: "demo",
		Nodes: []Node{{
			ID:      "kali",
			Type:    NodeContainer,
			Badge:   "CT",
			Label:   "Kali",
			State:   "running",
			X:       4,
			Y:       3,
			Details: []string{"image=docker.io/kalilinux/kali-rolling:latest"},
		}},
	}
	out := RenderString(m, ViewState{Selected: 0, Focus: FocusGraph}, 120, 30, false)
	if !strings.Contains(out, "[CT] Kali") {
		t.Fatalf("render missing separated inspector header:\n%s", out)
	}
	ansiOut := RenderString(m, ViewState{Selected: 0, Focus: FocusGraph}, 120, 30, true)
	if !strings.Contains(ansiOut, nodeBadgeStyle(NodeContainer)+"[CT]") {
		t.Fatalf("ANSI render missing CT badge style:\n%q", ansiOut)
	}
	if !strings.Contains(ansiOut, nodeLabelStyle(NodeContainer)+"Kali") {
		t.Fatalf("ANSI render missing separate container label style:\n%q", ansiOut)
	}
}

func TestSwitchInspectorOmitsUplinkConfigurationDetails(t *testing.T) {
	lines := inspectorLines(Node{
		Type:    NodeSwitch,
		Details: []string{"mode=bridge", "uplink=uplink1", "external=uplink2", "nic0 → vm1"},
	})
	out := strings.Join(lines, "\n")
	if !strings.Contains(out, "mode") || !strings.Contains(out, "Bridge") {
		t.Fatalf("switch inspector missing mode:\n%s", out)
	}
	if strings.Contains(out, "uplink") || strings.Contains(out, "external") {
		t.Fatalf("switch inspector showed uplink configuration details:\n%s", out)
	}
}

func TestRenderUplinkUsesMagentaAccent(t *testing.T) {
	m := Model{
		ID: "demo",
		Nodes: []Node{{
			ID:    "uplink1",
			Type:  NodeExternal,
			Badge: "UP",
			Label: "wg0",
			State: "link",
			X:     4,
			Y:     3,
		}},
	}

	ansiOut := RenderString(m, ViewState{Selected: 0, Focus: FocusGraph}, 80, 20, true)
	if !strings.Contains(ansiOut, nodePanelStyle(NodeExternal, true)+ansiBrightMagenta+ansiBold+"[UP]") {
		t.Fatalf("ANSI render missing magenta uplink badge:\n%q", ansiOut)
	}
	if !strings.Contains(ansiOut, ansiBrightMagenta+ansiBold+"● link") {
		t.Fatalf("ANSI render missing magenta legacy uplink state:\n%q", ansiOut)
	}
}

func TestRenderSwitchUsesYellowModeAccent(t *testing.T) {
	m := Model{
		ID: "demo",
		Nodes: []Node{{
			ID:      "sw1",
			Type:    NodeSwitch,
			Badge:   "SW",
			Label:   "sw1",
			State:   "bridge",
			X:       4,
			Y:       3,
			Details: []string{"uplink=uplink6"},
		}},
	}

	ansiOut := RenderString(m, ViewState{Selected: 0, Focus: FocusGraph}, 80, 20, true)
	if !strings.Contains(ansiOut, nodePanelStyle(NodeSwitch, true)+ansiYellow+ansiBold+"[SW]") {
		t.Fatalf("ANSI render missing yellow switch badge:\n%q", ansiOut)
	}
	if !strings.Contains(ansiOut, nodePanelStyle(NodeSwitch, true)+ansiYellow+ansiDim+"Mode: ") {
		t.Fatalf("ANSI render missing dim switch mode label:\n%q", ansiOut)
	}
	if !strings.Contains(ansiOut, ansiYellow+ansiBold+"Bridge") {
		t.Fatalf("ANSI render missing yellow switch mode:\n%q", ansiOut)
	}
	if strings.Contains(ansiOut, "Mode: Bridge uplink6") {
		t.Fatalf("ANSI render should not include switch uplink in card status:\n%q", ansiOut)
	}
}

func TestRenderUplinkShowsModeAndInterfaceLines(t *testing.T) {
	m := ModelFromLab(&lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Name: "Internet", Interface: "wlp0s20f3", Mode: lab.ExternalModeNAT}},
	})

	out := RenderString(m, ViewState{Focus: FocusGraph}, 80, 20, false)
	if !strings.Contains(out, "Mode:  NAT") {
		t.Fatalf("render missing uplink mode line:\n%s", out)
	}
	if !strings.Contains(out, "Iface: wlp0s20") {
		t.Fatalf("render missing uplink interface line:\n%s", out)
	}
	if strings.Contains(out, "nat wlp0s20f3") {
		t.Fatalf("render kept compressed uplink summary:\n%s", out)
	}
	ansiOut := RenderString(m, ViewState{Focus: FocusGraph}, 80, 20, true)
	if !strings.Contains(ansiOut, ansiBrightMagenta+ansiDim+"Mode:  ") {
		t.Fatalf("ANSI render missing dim uplink mode label:\n%q", ansiOut)
	}
	if !strings.Contains(ansiOut, ansiBrightMagenta+ansiBold+"NAT") {
		t.Fatalf("ANSI render missing accented uplink mode value:\n%q", ansiOut)
	}
}

func TestOnlyUplinkNodesUseTallCards(t *testing.T) {
	m := Model{Nodes: []Node{
		{ID: "vm1", Type: NodeVM, X: 2, Y: 2},
		{ID: "uplink1", Type: NodeExternal, X: 20, Y: 2},
	}}

	rects := layoutNodeRects(m, rect{X: 0, Y: 0, W: 80, H: 20})
	if got := rects[NodeKey(NodeVM, "vm1")].H; got != nodeHeight {
		t.Fatalf("vm card height = %d, want %d", got, nodeHeight)
	}
	if got := rects[NodeKey(NodeExternal, "uplink1")].H; got != uplinkNodeHeight {
		t.Fatalf("uplink card height = %d, want %d", got, uplinkNodeHeight)
	}
	if got := rects[NodeKey(NodeExternal, "uplink1")].W; got != uplinkNodeWidth {
		t.Fatalf("uplink card width = %d, want %d", got, uplinkNodeWidth)
	}
}

func TestRenderCommandPaletteShowsGlobalActions(t *testing.T) {
	state := ViewState{Focus: FocusGraph, PaletteOpen: true}
	out := RenderString(MockModel(), state, 100, 30, false)
	for _, want := range []string{":add", "add", "apply", "disk", "quit", "complete"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing palette item %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " q ") || strings.Contains(out, "\nq ") {
		t.Fatalf("palette shows q as explicit suggestion:\n%s", out)
	}
	for _, notWant := range []string{"commands", "Actions", "Add VM", "Add CT", "Add SW", "Add Disk", "Add Uplink", "Exit", "configuration", "connect", "move"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("palette still contains flat action %q:\n%s", notWant, out)
		}
	}
	layout, ok := paletteLayout(100, 30)
	if !ok {
		t.Fatal("palette layout unavailable")
	}
	if layout.Y != (30-layout.H)/2 {
		t.Fatalf("palette Y = %d, want centered %d", layout.Y, (30-layout.H)/2)
	}
	g := renderGrid(MockModel(), state, 100, 30)
	rects := layoutNodeRects(MockModel(), graphBounds(100, 30))
	router := rects[NodeKey(NodeVM, "router")]
	if got := g.Cells[(router.Y+1)*g.Width+router.X+1].Style; !strings.Contains(got, ansiDim) {
		t.Fatalf("palette overlay did not dim graph node style: %q", got)
	}
	input := paletteInputRect(layout)
	if input.X != layout.X || input.Y != layout.Y || input.W != layout.W {
		t.Fatalf("palette input rect = %#v, want flush with palette %#v", input, layout)
	}
	if input.H != 3 {
		t.Fatalf("palette input height = %d, want restored vertical padding", input.H)
	}
	if got := g.Cells[input.Y*g.Width+input.X+paletteInputPaddingX].Style; got != themePaletteInput {
		t.Fatalf("palette input top padding style = %q, want %q", got, themePaletteInput)
	}
	if got := g.Cells[(input.Y+paletteInputPaddingY)*g.Width+input.X].Ch; got != ' ' {
		t.Fatalf("palette input horizontal padding char = %q, want space", got)
	}
	if got := g.Cells[(input.Y+paletteInputPaddingY)*g.Width+input.X+paletteInputPaddingX].Ch; got != ':' {
		t.Fatalf("palette prompt starts at %q, want input after padding", got)
	}
	if got := g.Cells[(input.Y+input.H-1)*g.Width+input.X+paletteInputPaddingX].Ch; got != ' ' {
		t.Fatalf("palette input bottom padding char = %q, want space", got)
	}
	rowY := paletteRowsY(layout)
	if got := g.Cells[rowY*g.Width+layout.X].Style; got != themePaletteActive {
		t.Fatalf("palette selected record left padding style = %q, want %q", got, themePaletteActive)
	}
	if got := g.Cells[rowY*g.Width+layout.X+paletteRecordPaddingX].Ch; got != 'a' {
		t.Fatalf("palette first result starts at %q, want one record padding", got)
	}
	if got := g.Cells[rowY*g.Width+layout.X+layout.W-1].Style; got != themePaletteActive {
		t.Fatalf("palette selected record right padding style = %q, want %q", got, themePaletteActive)
	}
	hintY := paletteRowsY(layout) + 2
	hintX := layout.X + layout.W - paletteRecordPaddingX - min(18, runeLen("explorer")+2)
	if got := g.Cells[hintY*g.Width+hintX].Style; got != themePaletteHint {
		t.Fatalf("palette hint style = %q, want %q", got, themePaletteHint)
	}
}

func TestRenderCommandPaletteShowsGhostCompletion(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Focus: FocusGraph, PaletteOpen: true, PaletteQuery: "ad"}, 100, 30, false)
	if !strings.Contains(out, ":add") {
		t.Fatalf("render missing ghost completion:\n%s", out)
	}
	if !strings.Contains(out, "complete") {
		t.Fatalf("render missing completion hint:\n%s", out)
	}
}

func TestRenderCommandPaletteNoCompletionsUsesPadding(t *testing.T) {
	state := ViewState{Focus: FocusGraph, PaletteOpen: true, PaletteQuery: "no"}
	layout, ok := paletteLayout(100, 30)
	if !ok {
		t.Fatal("palette layout unavailable")
	}
	g := renderGrid(MockModel(), state, 100, 30)
	emptyY := paletteEmptyY(layout)

	if got := g.Cells[paletteRowsY(layout)*g.Width+layout.X+paletteRecordPaddingX].Ch; got != ' ' {
		t.Fatalf("palette no-completion row padding = %q, want blank row", got)
	}
	if got := g.Cells[emptyY*g.Width+layout.X].Ch; got != ' ' {
		t.Fatalf("palette no-completion left padding char = %q, want space", got)
	}
	if got := g.Cells[emptyY*g.Width+layout.X+paletteRecordPaddingX].Ch; got != 'n' {
		t.Fatalf("palette no-completion text starts at %q, want n after padding", got)
	}
	if got := g.Cells[emptyY*g.Width+layout.X+paletteRecordPaddingX].Style; got != themePalette {
		t.Fatalf("palette no-completion style = %q, want %q", got, themePalette)
	}
}

func TestRenderCommandPaletteShowsAddSuggestions(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Focus: FocusGraph, PaletteOpen: true, PaletteQuery: "add"}, 100, 30, false)
	for _, want := range []string{":add vm", "add vm", "add sw", "add ct", "add disk", "add uplink"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing add suggestion %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{"Add VM", "Add CT", "Exit", "apply"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("palette kept unrelated action %q:\n%s", notWant, out)
		}
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
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true}, 100, 30, false)
	for _, want := range []string{
		" Run",
		" Name        client01",
		" CPU         2",
		" Memory      2048M",
		" VNC         [ ]",
		" ISO",
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
	if strings.Contains(out, "labs/mock/disks/client01.img") {
		t.Fatalf("render kept disk detail in config submenu:\n%s", out)
	}
	if strings.Contains(out, "VNC:") {
		t.Fatalf("render showed VNC info for disabled VNC:\n%s", out)
	}
}

func TestRenderContextFormShowsEmptyPlaceholder(t *testing.T) {
	model := Model{Nodes: []Node{{ID: "kali", Type: NodeContainer, Badge: "CT", Label: "kali"}}}
	state := ViewState{Selected: 0, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true}
	out := RenderString(model, state, 100, 30, false)
	for _, want := range []string{
		" Image       empty",
		" Command     empty",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing empty placeholder %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "?") {
		t.Fatalf("render still contains question fallback:\n%s", out)
	}
	ansiOut := RenderString(model, state, 100, 30, true)
	if !strings.Contains(ansiOut, ansiDim+"empty") {
		t.Fatalf("ANSI render missing dim empty placeholder:\n%q", ansiOut)
	}
}

func TestRenderDiskSubmenuShowsDiskItems(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:         1,
		Focus:            FocusGraph,
		ContextMenu:      true,
		ContextSelected:  2,
		ContextGroup:     "disk-menu",
		ContextInSubmenu: true,
		DiskMenuItems:    []string{"Add Disk", "data 4G", diskMenuLayerTreePrefix + "data-layer"},
		DiskMenuActions:  []string{diskMenuActionCreate, diskMenuActionAttach, diskMenuActionNone},
		DiskMenuKinds:    []string{"", "base", "layer"},
	}, 100, 30, false)
	for _, want := range []string{" Disk ", " Add Disk", " data 4G", " L ", diskMenuLayerTreePrefix + "data-layer", " M ", " D ", " X "} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing disk submenu item %q:\n%s", want, out)
		}
	}
}

func TestRenderDiskSubmenuMarksMergeButtonGreen(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:           1,
		Focus:              FocusGraph,
		ContextMenu:        true,
		ContextSelected:    2,
		ContextGroup:       "disk-menu",
		ContextInSubmenu:   true,
		ContextSubSelected: 2,
		ContextMergeDisk:   true,
		DiskMenuItems:      []string{"Add Disk", "data 4G", diskMenuLayerTreePrefix + "data-layer"},
		DiskMenuActions:    []string{diskMenuActionCreate, diskMenuActionAttach, diskMenuActionNone},
		DiskMenuKinds:      []string{"", "base", "layer"},
	}, 100, 30, true)
	if !strings.Contains(out, ansiGreen+ansiBold+" M ") {
		t.Fatalf("render did not mark selected merge M green:\n%q", out)
	}
}

func TestRenderDiskSubmenuShowsActiveBaseActions(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:           1,
		Focus:              FocusGraph,
		ContextMenu:        true,
		ContextSelected:    2,
		ContextGroup:       "disk-menu",
		ContextInSubmenu:   true,
		ContextSubSelected: 1,
		ContextDetachDisk:  true,
		DiskMenuItems:      []string{"Add Disk", "data 4G"},
		DiskMenuActions:    []string{diskMenuActionCreate, diskMenuActionNone},
		DiskMenuKinds:      []string{"", "base"},
	}, 100, 30, true)
	for _, want := range []string{" data 4G", " D ", " X "} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing active base disk item %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " L ") || strings.Contains(out, " M ") {
		t.Fatalf("render showed layer controls for active base:\n%s", out)
	}
	if !strings.Contains(out, ansiBgYellow+ansiBlack+ansiBold+" D ") {
		t.Fatalf("render did not mark selected base detach D yellow:\n%q", out)
	}
}

func TestRenderNICSubmenuShowsNICDetails(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true, ContextSelected: 1, ContextGroup: "nic-menu", ContextInSubmenu: true}, 100, 30, false)
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
	app := App{
		Model: MockModel(),
		State: ViewState{
			Selected:           1,
			Focus:              FocusGraph,
			ContextMenu:        true,
			ContextSelected:    1,
			ContextGroup:       "nic-menu",
			ContextInSubmenu:   true,
			ContextSubSelected: 1,
			ContextDeleteNIC:   true,
		},
		ViewWidth:  100,
		ViewHeight: 30,
	}
	layout, _, _, ok := app.currentContextMenuLayout()
	if !ok || !layout.hasSub {
		t.Fatal("nic submenu layout missing")
	}
	subMenuW := layout.sub.rect.W
	subX := layout.sub.rect.X
	nicRowY := layout.sub.rect.Y + 1
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
	out := RenderString(MockModel(), ViewState{Selected: 2, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true}, 100, 30, false)
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
	if got := g.Cells[y*g.Width+source.X+source.W-1].Style; !strings.HasPrefix(got, nodePanelStyle(NodeVM, false)) {
		t.Fatalf("connect preview changed source card panel = %q", got)
	}
}

func TestRenderContextPowerActionUsesStopForRunningNode(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 0, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true}, 100, 30, false)
	if !strings.Contains(out, "Stop") {
		t.Fatalf("render missing stop action for running node:\n%s", out)
	}
	if strings.Contains(out, "State") {
		t.Fatalf("render contains removed state field:\n%s", out)
	}
}

func TestRenderContextCheckboxUsesUppercaseX(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 0, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true}, 100, 30, false)
	if !strings.Contains(out, "VNC         [X]") {
		t.Fatalf("render missing checked checkbox with uppercase X:\n%s", out)
	}
	if !strings.Contains(out, "VNC: restart needed") {
		t.Fatalf("render missing enabled VNC status without runtime port:\n%s", out)
	}
	if strings.Contains(out, "[x]") {
		t.Fatalf("render contains lowercase checkbox marker:\n%s", out)
	}
}

func TestRenderContextVNCInfoUsesRuntimePort(t *testing.T) {
	m := MockModel()
	m.Nodes[0].Details = append(m.Nodes[0].Details, "vnc-port=5903")

	out := RenderString(m, ViewState{Selected: 0, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true}, 100, 30, false)
	if !strings.Contains(out, "VNC: 127.0.0.1:5903") {
		t.Fatalf("render missing runtime VNC port:\n%s", out)
	}
	if strings.Contains(out, "autoport") {
		t.Fatalf("render still contains autoport:\n%s", out)
	}
}

func TestRenderContextVNCInfoPromptsStartForStoppedVM(t *testing.T) {
	m := MockModel()
	m.Nodes[0].State = "shutoff"

	out := RenderString(m, ViewState{Selected: 0, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true}, 100, 30, false)
	if !strings.Contains(out, "VNC: start VM") {
		t.Fatalf("render missing VNC start prompt:\n%s", out)
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

func TestRenderContextInlineEditEmptyPlaceholder(t *testing.T) {
	state := ViewState{
		Selected:           2,
		Focus:              FocusGraph,
		ContextMenu:        true,
		ContextGroup:       "config-menu",
		ContextInSubmenu:   true,
		ContextSubSelected: 3,
		ContextEdit:        true,
		ContextEditValue:   "",
		ContextEditCursor:  0,
	}
	out := RenderString(MockModel(), state, 100, 30, false)
	if !strings.Contains(out, "Command     |empty") {
		t.Fatalf("render missing empty placeholder:\n%s", out)
	}
	if strings.Contains(out, "?") {
		t.Fatalf("render still contains question fallback:\n%s", out)
	}
	if strings.Contains(out, "Command     |empty  M") || strings.Contains(out, "Command     |empty  X") {
		t.Fatalf("render leaked disk controls into config edit row:\n%s", out)
	}
	ansiOut := RenderString(MockModel(), state, 100, 30, true)
	if !strings.Contains(ansiOut, ansiDim+"empty") {
		t.Fatalf("ANSI render missing dim empty placeholder:\n%q", ansiOut)
	}
}

func TestRenderNoHooksMenuItem(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true}, 100, 30, false)
	if strings.Contains(out, "hooks >") {
		t.Fatalf("render contains removed hooks menu item: %q", out)
	}
}

func TestRenderContextMenuANSIStyle(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 1, Focus: FocusGraph, ContextMenu: true}, 100, 30, true)
	for _, want := range []string{
		themeChrome,
		themeMenuActive,
		"Configuration >",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ANSI render missing context menu style %q:\n%q", want, out)
		}
	}
	for _, notWant := range []string{ansiBgGray + ansiWhite, ansiBgCyan, "▸"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("ANSI render contains old menu accent %q:\n%q", notWant, out)
		}
	}
}

func TestRenderDisabledContextItemKeepsMenuBackground(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 5, Focus: FocusGraph, ContextMenu: true}, 100, 30, true)
	if !strings.Contains(out, themeMenuMuted+"Connect") {
		t.Fatalf("disabled context item missing muted menu style:\n%q", out)
	}
	if strings.Contains(out, themeMenuRow+themeMuted+"Connect") {
		t.Fatalf("disabled context item used terminal-muted style:\n%q", out)
	}
}

func TestRenderRemovesTopRibbonAndSidebarFooter(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Focus: FocusTop}, 120, 30, true)
	for _, notWant := range []string{"Apply lab", " Add ", " Disks ", " Exit ", "lab mock", ": actions", "mode:"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("render still contains removed chrome %q:\n%q", notWant, out)
		}
	}
	firstLine := strings.SplitN(out, "\n", 2)[0]
	if strings.Contains(firstLine, "lab mock") {
		t.Fatalf("render still shows status in top ribbon:\n%q", out)
	}
}

func TestRenderDisabledPaletteActionKeepsPaletteBackground(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Focus: FocusGraph, PaletteOpen: true, PaletteQuery: "apply", ApplyLabDisabled: true}, 100, 30, true)
	if !strings.Contains(out, themePaletteActive+ansiBrightBlack+"apply") {
		t.Fatalf("disabled palette item missing muted active palette style:\n%q", out)
	}
	if strings.Contains(out, themePalette+themeMuted+"apply") {
		t.Fatalf("disabled palette item used terminal-muted style:\n%q", out)
	}
}

func TestRenderInspectorUsesColorPanelWithoutFrame(t *testing.T) {
	width, height := 120, 30
	panel := inspectorBounds(width, height)
	if panel.W <= 0 {
		t.Fatal("inspector unavailable")
	}
	g := renderGrid(MockModel(), ViewState{Selected: 0, Focus: FocusGraph}, width, height)

	topLeft := g.Cells[panel.Y*g.Width+panel.X]
	if topLeft.Ch == '╭' {
		t.Fatalf("inspector still renders a frame corner:\n%s", g.String(false))
	}
	if topLeft.Style != themePanelInspector {
		t.Fatalf("inspector corner style = %q, want %q", topLeft.Style, themePanelInspector)
	}
	header := g.Cells[(panel.Y+1)*g.Width+panel.X+2]
	if header.Style != themePanelInspectorHeader+nodeBadgeStyle(NodeVM) {
		t.Fatalf("inspector header style = %q, want %q", header.Style, themePanelInspectorHeader+nodeBadgeStyle(NodeVM))
	}
	body := g.Cells[(panel.Y+2)*g.Width+panel.X]
	if body.Style != themePanelInspector {
		t.Fatalf("inspector body style = %q, want %q", body.Style, themePanelInspector)
	}
	bottomRight := g.Cells[(height-1)*g.Width+panel.X+panel.W-1]
	if bottomRight.Style != themePanelInspector {
		t.Fatalf("inspector bottom-right style = %q, want %q", bottomRight.Style, themePanelInspector)
	}
	brandX := panel.X + 2
	brandY := height - 1
	for offset, want := range []rune("// FoxLab") {
		if got := g.Cells[brandY*g.Width+brandX+offset].Ch; got != want {
			t.Fatalf("brand char at offset %d = %q, want %q", offset, got, want)
		}
	}
	if got := g.Cells[brandY*g.Width+brandX+3].Style; got != themePanelInspector+ansiOrange+ansiBold {
		t.Fatalf("brand Fox style = %q, want orange", got)
	}
	if got := g.Cells[brandY*g.Width+brandX+6].Style; got != themePanelInspector+ansiWhite+ansiBold {
		t.Fatalf("brand Lab style = %q, want white", got)
	}
}

func TestRenderDiskExplorerUsesColorPanelWithoutFrame(t *testing.T) {
	width, height := 100, 30
	layout, ok := diskExplorerLayout(width, height)
	if !ok {
		t.Fatal("disk explorer layout unavailable")
	}
	state := ViewState{
		DiskExplorerOpen: true,
		DiskExplorerRows: []string{"data  base  10G  qcow2  disks/data.qcow2"},
		DiskExplorerRowViews: []DiskExplorerRowView{{
			ID:       "data",
			Kind:     "base",
			Size:     "10G",
			Format:   "qcow2",
			Relation: "-",
			Path:     "disks/data.qcow2",
		}},
	}
	g := renderGrid(Model{ID: "demo"}, state, width, height)

	topLeft := g.Cells[layout.Y*g.Width+layout.X]
	if topLeft.Ch == '╭' {
		t.Fatalf("disk explorer still renders a frame corner:\n%s", g.String(false))
	}
	if topLeft.Style != themePanelDiskHeader {
		t.Fatalf("disk explorer header style = %q, want %q", topLeft.Style, themePanelDiskHeader)
	}
	body := g.Cells[(layout.Y+3)*g.Width+layout.X]
	if body.Style != themePanelDisk {
		t.Fatalf("disk explorer body style = %q, want %q", body.Style, themePanelDisk)
	}
	if themePanelDisk != ansiBgNode+ansiWhite {
		t.Fatalf("disk explorer body is not the shared dark panel: %q", themePanelDisk)
	}
	selected := g.Cells[diskExplorerRowsY(layout)*g.Width+layout.X+1]
	if !strings.HasPrefix(selected.Style, themePanelDiskSelected) {
		t.Fatalf("disk explorer selected row style = %q, want prefix %q", selected.Style, themePanelDiskSelected)
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

func TestContextMenuLayoutClampsSubmenuInsideBounds(t *testing.T) {
	m := Model{
		Nodes: []Node{{
			ID:      "right",
			Type:    NodeExternal,
			Badge:   "IF",
			Label:   "uplink",
			X:       82,
			Y:       2,
			Details: []string{"interface=eth0"},
		}},
	}
	state := ViewState{
		Focus:            FocusGraph,
		Selected:         0,
		ContextMenu:      true,
		ContextGroup:     "config-menu",
		ContextInSubmenu: true,
	}
	bounds := rect{X: 0, Y: 0, W: 100, H: 20}
	layout, _, _, ok := contextMenuLayoutFor(m, state, layoutNodeRects(m, bounds), bounds)
	if !ok || !layout.hasSub {
		t.Fatalf("context submenu layout missing: ok=%t layout=%#v", ok, layout)
	}
	if layout.root.rect.X < bounds.X || layout.root.rect.X+layout.root.rect.W > bounds.X+bounds.W {
		t.Fatalf("root menu outside bounds: %#v bounds=%#v", layout.root.rect, bounds)
	}
	if layout.sub.rect.X < bounds.X || layout.sub.rect.X+layout.sub.rect.W > bounds.X+bounds.W {
		t.Fatalf("submenu outside bounds: %#v bounds=%#v", layout.sub.rect, bounds)
	}
}

func TestRenderNotificationIgnoresCommandConsole(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Focus:       FocusGraph,
		CommandMode: true,
		Command:     "help",
		Console:     []string{"help: :help :quit"},
		Message:     "old message",
	}, 100, 30, false)
	if !strings.Contains(out, "Old message") {
		t.Fatalf("render missing status message:\n%s", out)
	}
	for _, notWant := range []string{":help", "help: :help :quit"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("status render contains removed command console text %q:\n%s", notWant, out)
		}
	}
}

func TestRenderMessageUsesBottomLeftNotification(t *testing.T) {
	const width, height = 100, 30
	message := "failed to apply lab"
	rendered := "Failed to apply lab"
	g := renderGrid(MockModel(), ViewState{Focus: FocusGraph, Message: message}, width, height)
	topPaddingY := height - 3
	textY := height - 2
	bottomPaddingY := height - 1
	for _, y := range []int{topPaddingY, textY, bottomPaddingY} {
		if got := g.Cells[y*g.Width+1].Style; got != themeNotificationBar {
			t.Fatalf("notification line %d accent style = %q, want %q", y, got, themeNotificationBar)
		}
		if got := g.Cells[y*g.Width+2].Style; got != themeNotification {
			t.Fatalf("notification line %d body style = %q, want %q", y, got, themeNotification)
		}
	}
	if got := g.Cells[textY*g.Width+1].Style; got != themeNotificationBar {
		t.Fatalf("notification accent style = %q, want %q", got, themeNotificationBar)
	}
	if got := g.Cells[textY*g.Width+2].Style; got != themeNotification {
		t.Fatalf("notification body style = %q, want %q", got, themeNotification)
	}
	if got := g.Cells[topPaddingY*g.Width+4].Ch; got != ' ' {
		t.Fatalf("notification top padding = %q, want space", got)
	}
	if got := g.Cells[bottomPaddingY*g.Width+4].Ch; got != ' ' {
		t.Fatalf("notification bottom padding = %q, want space", got)
	}
	if got := g.Cells[textY*g.Width+2].Ch; got != ' ' {
		t.Fatalf("notification left padding = %q, want space", got)
	}
	if got := g.Cells[textY*g.Width+3].Ch; got != ' ' {
		t.Fatalf("notification second left padding = %q, want space", got)
	}
	if got := g.Cells[textY*g.Width+4].Ch; got != 'F' {
		t.Fatalf("notification text starts at %q, want first message character", got)
	}
	out := g.String(false)
	if !strings.Contains(out, rendered) {
		t.Fatalf("render missing notification message:\n%s", out)
	}
	lastLine := strings.Split(strings.TrimSuffix(out, "\n"), "\n")[height-1]
	if strings.Contains(lastLine, rendered) {
		t.Fatalf("notification text rendered into bottom padding row:\n%s", out)
	}
	for x := 1 + 1 + min(max(24, width/2)-1, runeLen(rendered)+4); x < width; x++ {
		if got := g.Cells[textY*g.Width+x].Style; got == themeNotification {
			t.Fatalf("notification filled past its compact width at x=%d", x)
		}
	}
}

func TestRenderDiskSuccessMessageUsesSuccessNotification(t *testing.T) {
	const width, height = 100, 30
	for _, tt := range []struct {
		message string
		want    string
	}{
		{message: "created disk:disk", want: "Disk created: disk"},
		{message: "deleted disk:disk", want: "Disk deleted: disk"},
	} {
		g := renderGrid(MockModel(), ViewState{Focus: FocusGraph, Message: tt.message}, width, height)
		textY := height - 2
		if got := g.Cells[textY*g.Width+1].Style; got != themeNotificationSuccessBar {
			t.Fatalf("%q success notification accent style = %q, want %q", tt.message, got, themeNotificationSuccessBar)
		}
		if got := g.Cells[textY*g.Width+2].Style; got != themeNotification {
			t.Fatalf("%q success notification body style = %q, want %q", tt.message, got, themeNotification)
		}
		out := g.String(false)
		if !strings.Contains(out, tt.want) {
			t.Fatalf("render missing formatted success notification %q:\n%s", tt.want, out)
		}
		if strings.Contains(out, tt.message) {
			t.Fatalf("render kept raw disk success message:\n%s", out)
		}
	}
}

func TestRenderLongMessageUsesMultilineNotification(t *testing.T) {
	const width, height = 80, 24
	message := "failed to apply lab because libvirt network default is not active"
	g := renderGrid(MockModel(), ViewState{Focus: FocusGraph, Message: message}, width, height)
	topPaddingY := height - 4
	firstTextY := height - 3
	secondTextY := height - 2
	bottomPaddingY := height - 1
	for _, y := range []int{topPaddingY, firstTextY, secondTextY, bottomPaddingY} {
		if got := g.Cells[y*g.Width+1].Style; got != themeNotificationBar {
			t.Fatalf("notification line %d accent style = %q, want %q", y, got, themeNotificationBar)
		}
		if got := g.Cells[y*g.Width+2].Style; got != themeNotification {
			t.Fatalf("notification line %d body style = %q, want %q", y, got, themeNotification)
		}
	}
	for _, y := range []int{topPaddingY, bottomPaddingY} {
		if got := g.Cells[y*g.Width+4].Ch; got != ' ' {
			t.Fatalf("notification vertical padding at line %d = %q, want space", y, got)
		}
	}
	out := g.String(false)
	for _, want := range []string{
		"Failed to apply lab because libvirt",
		"network default is not active",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing wrapped notification line %q:\n%s", want, out)
		}
	}
	lastLine := strings.Split(strings.TrimSuffix(out, "\n"), "\n")[height-1]
	if strings.Contains(lastLine, "network default is not active") {
		t.Fatalf("notification text rendered into bottom padding row:\n%s", out)
	}
}

func TestRenderAddDiskInlineNameEdit(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:           1,
		Focus:              FocusGraph,
		ContextMenu:        true,
		ContextSelected:    2,
		ContextGroup:       "disk-menu",
		ContextInSubmenu:   true,
		ContextSubSelected: 0,
		ContextEdit:        true,
		ContextEditValue:   "data",
		ContextEditCursor:  4,
		DiskMenuItems:      []string{"Add Disk", "data 4G"},
	}, 100, 30, false)
	if !strings.Contains(out, "Add Disk data|") {
		t.Fatalf("render missing inline disk name edit:\n%s", out)
	}
}

func TestRenderAddDiskEmptyInlineNameEditHasNoLayerActions(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Selected:           1,
		Focus:              FocusGraph,
		ContextMenu:        true,
		ContextSelected:    2,
		ContextGroup:       "disk-menu",
		ContextInSubmenu:   true,
		ContextSubSelected: 0,
		ContextEdit:        true,
		ContextEditValue:   "",
		ContextEditCursor:  0,
		DiskMenuItems:      []string{"Add Disk", "No disks"},
		DiskMenuActions:    []string{diskMenuActionCreate, diskMenuActionNone},
	}, 100, 30, false)
	if !strings.Contains(out, "Add Disk |empty") {
		t.Fatalf("render missing empty inline disk name edit:\n%s", out)
	}
	if strings.Contains(out, "Add Disk |empty  M") || strings.Contains(out, "Add Disk |empty  X") {
		t.Fatalf("render showed layer actions on empty Add Disk edit:\n%s", out)
	}
}

func TestRenderStatusBarHiddenWithoutMessage(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Focus: FocusGraph}, 100, 30, true)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	lastLine := lines[len(lines)-1]
	if strings.Contains(lastLine, ansiBgGray+ansiWhite) {
		t.Fatalf("render shows default bottom bar:\n%q", lastLine)
	}
	for _, notWant := range []string{"graph: arrows", "Space/menu click opens actions", ": commands"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("render shows bottom help %q:\n%s", notWant, out)
		}
	}
}

func TestRenderMouseClickFeedback(t *testing.T) {
	g := renderGrid(MockModel(), ViewState{
		Focus:            FocusGraph,
		MouseClickActive: true,
		MouseClickX:      10,
		MouseClickY:      5,
		MouseClickW:      4,
		MouseClickH:      2,
	}, 100, 30)
	style := ansiInverse
	for y := 5; y < 7; y++ {
		for x := 10; x < 14; x++ {
			if got := g.Cells[y*g.Width+x].Style; got != style {
				t.Fatalf("click feedback style at (%d,%d) = %q, want %q", x, y, got, style)
			}
		}
	}
	if got := g.Cells[5*g.Width+9].Style; got == style {
		t.Fatalf("click feedback leaked outside rect at (9,5)")
	}
}

func TestRenderGlobalContextMenuForEmptyModel(t *testing.T) {
	out := RenderString(Model{ID: "empty"}, ViewState{Focus: FocusGraph, PaletteOpen: true}, 80, 20, false)
	if !strings.Contains(out, "add") || !strings.Contains(out, "quit") {
		t.Fatalf("render missing global palette actions:\n%s", out)
	}
	for _, notWant := range []string{"add >", "add vm", "add cont", "add sw", "create external", " help", "list", "Add VM", "Exit"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("render contains removed global palette item %q:\n%s", notWant, out)
		}
	}
}

func TestRenderGlobalCreateSubmenuForEmptyModel(t *testing.T) {
	out := RenderString(Model{ID: "empty"}, ViewState{Focus: FocusGraph, PaletteOpen: true, PaletteQuery: "add"}, 80, 20, false)
	if !strings.Contains(out, "add vm") || !strings.Contains(out, "add uplink") {
		t.Fatalf("render missing global create palette actions:\n%s", out)
	}
	for _, notWant := range []string{"add cont", "create external", "Add VM", "Add Uplink"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("render contains removed global create palette item %q:\n%s", notWant, out)
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

func TestMoveSelectionPrefersNearestDirectionalNodeBeforeAxisAlignment(t *testing.T) {
	m := Model{Nodes: []Node{
		{ID: "current", Type: NodeVM, X: 10, Y: 10},
		{ID: "far-aligned", Type: NodeVM, X: 80, Y: 10},
		{ID: "near-offset", Type: NodeVM, X: 18, Y: 14},
	}}

	if got := MoveSelection(m, 0, "right"); got != 2 {
		t.Fatalf("right selection = %d, want near-offset", got)
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

func TestRenderExternalInterfaceChoiceMenu(t *testing.T) {
	fakeHostInterfaces(t, "br0", "eth0")
	m := ModelFromLab(&lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0"}},
	})
	node := m.Nodes[0]
	out := RenderString(m, ViewState{
		Focus:                 FocusGraph,
		Selected:              0,
		ContextMenu:           true,
		ContextGroup:          "config-menu",
		ContextInSubmenu:      true,
		ContextSubSelected:    externalInterfaceFieldIndex(node),
		ContextSelectGroup:    "interface-menu",
		ContextSelectSelected: 0,
	}, 100, 30, false)
	if !strings.Contains(out, "Interface   eth0") || !strings.Contains(out, "br0") || !strings.Contains(out, "eth0") {
		t.Fatalf("render missing interface choices:\n%s", out)
	}
}

func TestRenderExternalModeChoiceMenu(t *testing.T) {
	m := ModelFromLab(&lab.Lab{
		ID:            "demo",
		ExternalLinks: []lab.ExternalLink{{ID: "uplink1", Interface: "eth0", Mode: lab.ExternalModeNAT}},
	})
	node := m.Nodes[0]
	out := RenderString(m, ViewState{
		Focus:                 FocusGraph,
		Selected:              0,
		ContextMenu:           true,
		ContextGroup:          "config-menu",
		ContextInSubmenu:      true,
		ContextSubSelected:    externalModeFieldIndex(node),
		ContextSelectGroup:    "mode-menu",
		ContextSelectSelected: 0,
	}, 100, 30, false)
	if !strings.Contains(out, "Mode        NAT") {
		t.Fatalf("render missing config mode row:\n%s", out)
	}
	for _, want := range []string{"NAT", "Direct", "MACNAT"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing mode choice %q:\n%s", want, out)
		}
	}
}

func TestRenderSwitchModeChoiceMenu(t *testing.T) {
	m := ModelFromLab(&lab.Lab{
		ID:       "demo",
		Switches: []lab.Switch{{ID: "lan", Mode: "bridge"}},
	})
	node := m.Nodes[0]
	out := RenderString(m, ViewState{
		Focus:                 FocusGraph,
		Selected:              0,
		ContextMenu:           true,
		ContextGroup:          "config-menu",
		ContextInSubmenu:      true,
		ContextSubSelected:    switchModeFieldIndex(node),
		ContextSelectGroup:    "mode-menu",
		ContextSelectSelected: 0,
	}, 100, 30, false)
	if !strings.Contains(out, "Mode        Bridge") {
		t.Fatalf("render missing switch config mode row:\n%s", out)
	}
	for _, want := range []string{"Bridge", "NAT", "MACNAT"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing switch mode choice %q:\n%s", want, out)
		}
	}
}

func TestRenderVMCardShowsFullStatusWithoutResourceSummary(t *testing.T) {
	m := Model{ID: "demo", Nodes: []Node{
		{ID: "vm1", Type: NodeVM, Badge: "VM", Label: "vm1", State: "running", X: 4, Y: 3, Details: []string{"cpu=2", "mem=2048M"}},
		{ID: "vm2", Type: NodeVM, Badge: "VM", Label: "vm2", State: "missing", X: 4, Y: 11, Details: []string{"cpu=2", "mem=2048M"}},
	}}

	out := RenderString(m, ViewState{Focus: FocusGraph}, 80, 24, false)
	for _, want := range []string{"● running", "! missing"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing full VM status %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"2c", "2G"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("render leaked VM resource summary %q:\n%s", unwanted, out)
		}
	}
}

func TestModelFromLabOmitsEmptyDiskDetail(t *testing.T) {
	m := ModelFromLab(&lab.Lab{
		ID: "demo",
		VMs: []lab.VM{{
			ID:       "vm1",
			Name:     "debian",
			MemoryMB: 1024,
			CPUs:     1,
			Disk:     "",
		}},
	})
	if len(m.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(m.Nodes))
	}
	details := strings.Join(m.Nodes[0].Details, "\n")
	if strings.Contains(details, "disk=") {
		t.Fatalf("empty disk leaked into details: %#v", m.Nodes[0].Details)
	}
	out := RenderString(m, ViewState{Focus: FocusGraph, ContextMenu: true, ContextGroup: "config-menu", ContextInSubmenu: true}, 100, 30, false)
	if strings.Contains(out, "labs/") || strings.Contains(out, "qcow2") {
		t.Fatalf("render kept disk path after disk clear:\n%s", out)
	}
	if !strings.Contains(out, "Disk") {
		t.Fatalf("render should still allow disk editing:\n%s", out)
	}
}

func TestModelFromLabShowsMissingForDesiredRunningContainer(t *testing.T) {
	m := ModelFromLab(&lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "kali", Image: "docker.io/kalilinux/kali-rolling:latest", DesiredState: lab.DesiredStateRunning}},
	})
	node, ok := nodeByKey(m, NodeKey(NodeContainer, "kali"))
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "missing" {
		t.Fatalf("container state = %q, want missing", node.State)
	}
}

func TestDisplayWorkloadStateShowsDesiredTransitions(t *testing.T) {
	tests := []struct {
		name    string
		desired string
		actual  string
		want    string
	}{
		{name: "running missing remains missing", desired: lab.DesiredStateRunning, actual: "missing", want: "missing"},
		{name: "start defined", desired: lab.DesiredStateRunning, actual: "defined", want: "starting"},
		{name: "stop running", desired: lab.DesiredStateStopped, actual: "running", want: "stopping"},
		{name: "stop starting", desired: lab.DesiredStateStopped, actual: "starting", want: "stopping"},
		{name: "stopped remains stopped", desired: lab.DesiredStateStopped, actual: "shutoff", want: "shutoff"},
	}
	for _, tt := range tests {
		if got := displayWorkloadState(tt.desired, tt.actual); got != tt.want {
			t.Fatalf("%s: displayWorkloadState(%q, %q) = %q, want %q", tt.name, tt.desired, tt.actual, got, tt.want)
		}
	}
}
