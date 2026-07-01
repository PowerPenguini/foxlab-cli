package topologyui

import "io"

const (
	minWidth         = 56
	minHeight        = 14
	nodeWidth        = 16
	uplinkNodeWidth  = 18
	nodeHeight       = 4
	uplinkNodeHeight = 5
)

func nodeWidthForNode(node Node) int {
	if node.Type == NodeExternal {
		return uplinkNodeWidth
	}
	return nodeWidth
}

func nodeHeightForNode(node Node) int {
	if node.Type == NodeExternal {
		return uplinkNodeHeight
	}
	return nodeHeight
}

func Render(w io.Writer, m Model, state ViewState, width, height int, ansi bool) error {
	_, err := io.WriteString(w, RenderString(m, state, width, height, ansi))
	return err
}

func RenderString(m Model, state ViewState, width, height int, ansi bool) string {
	return renderGrid(m, state, width, height).String(ansi)
}

func renderGrid(m Model, state ViewState, width, height int) *grid {
	_, routes := planRenderRoutes(m, state, width, height)
	return renderGridWithRoutes(m, state, width, height, routes)
}

func planRenderRoutes(m Model, state ViewState, width, height int) (map[string]rect, []visibleEdge) {
	if width < minWidth {
		width = minWidth
	}
	if height < minHeight {
		height = minHeight
	}
	graph := graphBounds(width, height)
	nodeRects := layoutNodeRectsWithPan(m, graph, state.PanX, state.PanY)
	planner := newRoutePlanner(graph, visibleNodeRects(nodeRects, graph))
	return nodeRects, planVisibleRoutes(planner, m.Edges, nodeRects, graph)
}

func renderGridWithRoutes(m Model, state ViewState, width, height int, visibleEdges []visibleEdge) *grid {
	if width < minWidth {
		width = minWidth
	}
	if height < minHeight {
		height = minHeight
	}
	g := newGrid(width, height)
	canvas := rect{X: 0, Y: 0, W: width, H: height}
	graph := graphBounds(width, height)
	nodeRects := layoutNodeRectsWithPan(m, graph, state.PanX, state.PanY)
	planner := newRoutePlanner(graph, visibleNodeRects(nodeRects, graph))
	for _, visible := range visibleEdges {
		if routeTouchesMovingNode(visible.edge, state) {
			continue
		}
		planner.reserve(visible.route)
		drawRoutedEdge(g, visible.route, routeStyle(m, state, visible.edge))
	}
	moveRoutes := movingNodeRoutes(m, state, nodeRects, graph)
	for _, visible := range moveRoutes {
		drawRoutedEdge(g, visible.route, routeStyle(m, state, visible.edge))
	}
	drawConnectPreview(g, m, state, nodeRects, graph, planner)
	selectedIndex := normalizedSelected(m, state.Selected)
	for i, node := range m.Nodes {
		if i == selectedIndex {
			continue
		}
		nodeRect := nodeRects[node.Key()]
		if rectIntersects(nodeRect, graph) {
			drawNode(g, node, nodeRect, false, state.Focus == FocusGraph, state.AnimationFrame)
		}
	}
	if len(m.Nodes) > 0 {
		node := m.Nodes[selectedIndex]
		nodeRect := nodeRects[node.Key()]
		if rectIntersects(nodeRect, graph) {
			drawNode(g, node, nodeRect, true, state.Focus == FocusGraph, state.AnimationFrame)
		}
	}
	for _, visible := range visibleEdges {
		if routeTouchesMovingNode(visible.edge, state) {
			continue
		}
		drawRoutedEdgePortsStyled(g, visible.route, routeStyle(m, state, visible.edge))
	}
	for _, visible := range moveRoutes {
		drawRoutedEdgePortsStyled(g, visible.route, routeStyle(m, state, visible.edge))
	}
	for i, node := range m.Nodes {
		if i == selectedIndex {
			continue
		}
		nodeRect := nodeRects[node.Key()]
		if rectIntersects(nodeRect, graph) {
			styleBoxBorder(g, nodeRect, selectedBorderStyle(false, state.Focus == FocusGraph))
		}
	}
	if len(m.Nodes) > 0 {
		node := m.Nodes[selectedIndex]
		nodeRect := nodeRects[node.Key()]
		if rectIntersects(nodeRect, graph) {
			styleBoxBorder(g, nodeRect, selectedBorderStyle(true, state.Focus == FocusGraph))
		}
	}
	drawInspector(g, m, state, inspectorBounds(width, height))
	drawTopRibbon(g, m, canvas, state)
	drawContextMenu(g, m, state, nodeRects, graph)
	drawConnectTargetMenu(g, m, state, nodeRects, graph)
	drawConsole(g, state, width, height)
	drawMouseClickFeedback(g, state)
	return g
}

func graphBounds(width, height int) rect {
	bounds := rect{X: 0, Y: 0, W: width, H: height}
	if panel := inspectorBounds(width, height); panel.W > 0 {
		bounds.W = max(minWidth, panel.X-1)
	}
	return bounds
}

func inspectorBounds(width, height int) rect {
	if width < 112 || height < 18 {
		return rect{}
	}
	panelW := min(32, max(28, width/4))
	return rect{X: width - panelW, Y: 1, W: panelW, H: height - 2}
}

func routeStyle(m Model, state ViewState, edge Edge) string {
	if state.Focus != FocusGraph {
		return themeRoute
	}
	node, ok := selectedNode(m, state.Selected)
	if !ok {
		return themeRoute
	}
	key := node.Key()
	if edge.From == key || edge.To == key {
		return themeRouteActive
	}
	return themeRoute
}
