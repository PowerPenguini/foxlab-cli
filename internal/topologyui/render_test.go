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
		"╭",
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

	drawRoutePort(g, fromLeft, themeRoute)
	drawRoutePort(g, fromRight, themeRoute)

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
	if got := g.Cells[hello.Y*g.Width+hello.X].Ch; got != '╭' {
		t.Fatalf("hello top-left corner was used as a connection: %q\n%s", got, g.String(false))
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

func TestRenderTopFocusDoesNotHighlightSelectedNode(t *testing.T) {
	m := MockModel()
	g := renderGrid(m, ViewState{Selected: 1, Focus: FocusTop}, 100, 30)
	nodeRect := layoutNodeRects(m, rect{X: 0, Y: 0, W: 100, H: 30})[m.Nodes[1].Key()]
	for x := nodeRect.X; x < nodeRect.X+nodeRect.W; x++ {
		if got := g.Cells[nodeRect.Y*g.Width+x].Style; got != "" {
			t.Fatalf("top border style at x=%d = %q, want empty", x, got)
		}
		if got := g.Cells[(nodeRect.Y+nodeRect.H-1)*g.Width+x].Style; got != "" {
			t.Fatalf("bottom border style at x=%d = %q, want empty", x, got)
		}
	}
}

func TestRenderTopRibbonShowsContextOnWideTerminals(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 0, Focus: FocusGraph}, 100, 30, false)
	if !strings.Contains(out, "lab mock | VM router | mode:graph") {
		t.Fatalf("render missing top context:\n%s", out)
	}
}

func TestRenderShowsModernSpinnersForProgressStates(t *testing.T) {
	m := Model{
		ID:    "demo",
		Nodes: []Node{{ID: "vm1", Type: NodeVM, Badge: "VM", Label: "vm1", State: "starting", X: 4, Y: 3}},
	}
	state := ViewState{Selected: 0, Focus: FocusGraph, StatusRefreshing: true, AnimationFrame: 1}
	out := RenderString(m, state, 100, 20, false)
	for _, want := range []string{
		spinner(1) + " starting",
		spinner(1) + " lab demo | VM vm1 | mode:graph",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing spinner text %q:\n%s", want, out)
		}
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
	if !strings.Contains(out, "[SW]") || !strings.Contains(out, "bridg") {
		t.Fatalf("render did not clip partially visible node:\n%s", out)
	}
	if strings.Contains(out, "partial") || strings.Contains(out, "bridge ") {
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
		ContextSelected:  1,
		ContextGroup:     "uplink-menu",
		ContextInSubmenu: true,
	}, 100, 30, false)
	for _, want := range []string{
		" Attach Uplink",
		" uplink0",
		" X ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing switch uplink submenu item %q:\n%s", want, out)
		}
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
	if !strings.Contains(out, " Add ") || !strings.Contains(out, " Exit ") {
		t.Fatalf("expected global top ribbon:\n%s", out)
	}
	if !strings.Contains(out, "Configuration >   Run") {
		t.Fatalf("expected context submenu to sit next to node menu:\n%s", out)
	}
}

func TestRenderWideInspectorShowsSelectedNodeDetails(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Selected: 0, Focus: FocusGraph}, 120, 30, false)
	for _, want := range []string{
		"[VM] router",
		"state  ● running",
		"cpu    2",
		"mem    2G",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("wide render missing inspector detail %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "actions Space menu") {
		t.Fatalf("wide render still shows inspector actions:\n%s", out)
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

func TestRenderTopRibbonAddDropdown(t *testing.T) {
	out := RenderString(MockModel(), ViewState{Focus: FocusGraph, TopMenuOpen: true}, 100, 30, false)
	for _, want := range []string{" Add ", " VM", " Container", " Switch", " Disk", " Uplink"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing top add dropdown item %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "External") {
		t.Fatalf("top add dropdown still contains External:\n%s", out)
	}
	if strings.Contains(out, " Link") {
		t.Fatalf("top add dropdown still contains Link:\n%s", out)
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
	if !strings.Contains(ansiOut, ansiBgGray+ansiWhite+ansiDim+"empty") {
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
	if !strings.Contains(out, ansiBgGreen+ansiWhite+ansiBold+" M ") {
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
	if got := g.Cells[y*g.Width+source.X+source.W-1].Ch; got != lineVertical {
		t.Fatalf("connect preview changed source border = %q, want vertical border", got)
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
	if !strings.Contains(ansiOut, ansiBgGray+ansiWhite+ansiBold+ansiDim+"empty") {
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
		ansiBgGray + ansiWhite,
		ansiBgGray + ansiWhite + ansiBold,
		ansiBgCyan,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ANSI render missing context menu style %q:\n%q", want, out)
		}
	}
	if strings.Contains(out, ansiBrightCyan+"Configuration") || strings.Contains(out, "▸") {
		t.Fatalf("ANSI render contains unwanted menu accent:\n%q", out)
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

func TestRenderStatusBarIgnoresCommandConsole(t *testing.T) {
	out := RenderString(MockModel(), ViewState{
		Focus:       FocusGraph,
		CommandMode: true,
		Command:     "help",
		Console:     []string{"help: :help :quit"},
		Message:     "old message",
	}, 100, 30, false)
	if !strings.Contains(out, "old message") {
		t.Fatalf("render missing status message:\n%s", out)
	}
	for _, notWant := range []string{":help", "help: :help :quit"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("status render contains removed command console text %q:\n%s", notWant, out)
		}
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
	style := ansiBgCyan + ansiWhite + ansiBold
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
	out := RenderString(Model{ID: "empty"}, ViewState{Focus: FocusGraph, ContextMenu: true}, 80, 20, false)
	if !strings.Contains(out, " Add ") || !strings.Contains(out, " Exit ") {
		t.Fatalf("render missing top add ribbon:\n%s", out)
	}
	for _, notWant := range []string{"add >", "add vm", "add cont", "add sw", "create external", " help", "list"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("render contains removed global context item %q:\n%s", notWant, out)
		}
	}
}

func TestRenderGlobalCreateSubmenuForEmptyModel(t *testing.T) {
	out := RenderString(Model{ID: "empty"}, ViewState{Focus: FocusGraph, ContextMenu: true, ContextGroup: "create-menu"}, 80, 20, false)
	if !strings.Contains(out, " Add ") || !strings.Contains(out, " Exit ") {
		t.Fatalf("render missing top add ribbon:\n%s", out)
	}
	for _, notWant := range []string{"add vm", "add cont", "add sw", "create external"} {
		if strings.Contains(out, notWant) {
			t.Fatalf("render contains removed global create submenu item %q:\n%s", notWant, out)
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
	if !strings.Contains(out, "Mode        nat") {
		t.Fatalf("render missing config mode row:\n%s", out)
	}
	for _, want := range []string{"nat", "direct", "macnat"} {
		if !strings.Contains(out, want) {
			t.Fatalf("render missing mode choice %q:\n%s", want, out)
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

func TestModelFromLabShowsStartingForDesiredRunningContainer(t *testing.T) {
	m := ModelFromLab(&lab.Lab{
		ID:         "demo",
		Containers: []lab.Container{{ID: "kali", Image: "docker.io/kalilinux/kali-rolling:latest", DesiredState: lab.DesiredStateRunning}},
	})
	node, ok := nodeByKey(m, NodeKey(NodeContainer, "kali"))
	if !ok {
		t.Fatal("container node not found")
	}
	if node.State != "starting" {
		t.Fatalf("container state = %q, want starting", node.State)
	}
}
