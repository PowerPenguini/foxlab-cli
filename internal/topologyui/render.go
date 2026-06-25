package topologyui

import "io"

const (
	minWidth   = 56
	minHeight  = 14
	nodeWidth  = 16
	nodeHeight = 4
)

func Render(w io.Writer, m Model, state ViewState, width, height int, ansi bool) error {
	_, err := io.WriteString(w, RenderString(m, state, width, height, ansi))
	return err
}

func RenderString(m Model, state ViewState, width, height int, ansi bool) string {
	return renderGrid(m, state, width, height).String(ansi)
}

func renderGrid(m Model, state ViewState, width, height int) *grid {
	_, routes := planRenderRoutes(m, width, height)
	return renderGridWithRoutes(m, state, width, height, routes)
}

func planRenderRoutes(m Model, width, height int) (map[string]rect, []visibleEdge) {
	if width < minWidth {
		width = minWidth
	}
	if height < minHeight {
		height = minHeight
	}
	graph := rect{X: 0, Y: 0, W: width, H: height}
	nodeRects := layoutNodeRects(m, graph)
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
	graph := rect{X: 0, Y: 0, W: width, H: height}
	nodeRects := layoutNodeRects(m, graph)
	planner := newRoutePlanner(graph, visibleNodeRects(nodeRects, graph))
	for _, visible := range visibleEdges {
		if routeTouchesMovingNode(visible.edge, state) {
			continue
		}
		planner.reserve(visible.route)
		drawRoutedEdge(g, visible.route, ansiDim)
	}
	moveRoutes := movingNodeRoutes(m, state, nodeRects, graph)
	for _, visible := range moveRoutes {
		drawRoutedEdge(g, visible.route, ansiDim)
	}
	drawConnectPreview(g, m, state, nodeRects, graph, planner)
	selectedIndex := normalizedSelected(m, state.Selected)
	for i, node := range m.Nodes {
		if i == selectedIndex {
			continue
		}
		nodeRect := nodeRects[node.Key()]
		if rectFullyVisible(nodeRect, graph) {
			drawNode(g, node, nodeRect, false, state.Focus == FocusGraph)
		}
	}
	if len(m.Nodes) > 0 {
		node := m.Nodes[selectedIndex]
		nodeRect := nodeRects[node.Key()]
		if rectFullyVisible(nodeRect, graph) {
			drawNode(g, node, nodeRect, true, state.Focus == FocusGraph)
		}
	}
	for _, visible := range visibleEdges {
		if routeTouchesMovingNode(visible.edge, state) {
			continue
		}
		drawRoutedEdgePorts(g, visible.route)
	}
	for _, visible := range moveRoutes {
		drawRoutedEdgePorts(g, visible.route)
	}
	for i, node := range m.Nodes {
		if i == selectedIndex {
			continue
		}
		nodeRect := nodeRects[node.Key()]
		if rectFullyVisible(nodeRect, graph) {
			styleBoxBorder(g, nodeRect, selectedBorderStyle(false, state.Focus == FocusGraph))
		}
	}
	if len(m.Nodes) > 0 {
		node := m.Nodes[selectedIndex]
		nodeRect := nodeRects[node.Key()]
		if rectFullyVisible(nodeRect, graph) {
			styleBoxBorder(g, nodeRect, selectedBorderStyle(true, state.Focus == FocusGraph))
		}
	}
	drawTopRibbon(g, graph, state)
	drawContextMenu(g, m, state, nodeRects, graph)
	drawConnectTargetMenu(g, m, state, nodeRects, graph)
	drawConsole(g, state, width, height)
	drawMouseClickFeedback(g, state)
	return g
}
