package topologyui

func routeTouchesMovingNode(edge Edge, state ViewState) bool {
	if !state.MoveMode {
		return false
	}
	key := NodeKey(state.MoveNodeType, state.MoveNodeID)
	return edge.From == key || edge.To == key
}

func movingNodeRoutes(m Model, state ViewState, nodeRects map[string]rect, bounds rect) []visibleEdge {
	if !state.MoveMode {
		return nil
	}
	key := NodeKey(state.MoveNodeType, state.MoveNodeID)
	out := []visibleEdge{}
	for _, edge := range m.Edges {
		if edge.From != key && edge.To != key {
			continue
		}
		from, fromOK := nodeRects[edge.From]
		to, toOK := nodeRects[edge.To]
		if !fromOK || !toOK || !rectIntersects(from, bounds) || !rectIntersects(to, bounds) {
			continue
		}
		route, ok := quickMoveRoute(from, to, bounds)
		if !ok {
			continue
		}
		out = append(out, visibleEdge{edge: edge, route: route})
	}
	return out
}

func quickMoveRoute(from, to rect, bounds rect) (edgeRoute, bool) {
	start := sidePort(from, to)
	end := sidePort(to, from)
	midX := (start.entry.X + end.entry.X) / 2
	waypoints := []routePoint{
		start.entry,
		{X: midX, Y: start.entry.Y},
		{X: midX, Y: end.entry.Y},
		end.entry,
	}
	cells, ok := pathCellsFromWaypoints(waypoints)
	if !ok {
		return edgeRoute{}, false
	}
	for _, cell := range cells {
		if !pointInRect(cell, bounds) {
			return edgeRoute{}, false
		}
	}
	return edgeRoute{cells: cells, start: start, end: end}, true
}

func sidePort(node, other rect) routePort {
	nodeCenterX := node.X + node.W/2
	otherCenterX := other.X + other.W/2
	otherCenterY := other.Y + other.H/2
	y := clamp(otherCenterY, node.Y+1, node.Y+node.H-2)
	if otherCenterX >= nodeCenterX {
		border := routePoint{X: node.X + node.W - 1, Y: y}
		return routePort{border: border, entry: routePoint{X: border.X + 1, Y: border.Y}, side: routeSideRight}
	}
	border := routePoint{X: node.X, Y: y}
	return routePort{border: border, entry: routePoint{X: border.X - 1, Y: border.Y}, side: routeSideLeft}
}
