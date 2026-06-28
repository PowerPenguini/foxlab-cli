package topologyui

func drawEdge(g *grid, from, to rect, bounds rect) (edgeRoute, bool) {
	planner := newRoutePlanner(bounds, []rect{from, to})
	route, ok := planner.planRoute(from, to)
	if !ok {
		return edgeRoute{}, false
	}
	drawRoutedEdge(g, route, themeRoute)
	return route, true
}

func drawRoutedEdge(g *grid, route edgeRoute, style string) {
	for i := 1; i < len(route.cells); i++ {
		a := route.cells[i-1]
		b := route.cells[i]
		g.SetLine(a.X, a.Y, maskBetween(a, b), style)
		g.SetLine(b.X, b.Y, maskBetween(b, a), style)
	}
}

func drawRoutedEdgePorts(g *grid, route edgeRoute) {
	drawRoutedEdgePortsStyled(g, route, themeRoute)
}

func drawRoutedEdgePortsStyled(g *grid, route edgeRoute, style string) {
	drawRoutePort(g, route.start, style)
	drawRoutePort(g, route.end, style)
}

func drawRoutePort(g *grid, port routePort, style string) {
	g.SetLine(port.border.X, port.border.Y, maskBetween(port.border, port.entry), style)
	g.SetLine(port.entry.X, port.entry.Y, maskBetween(port.entry, port.border), style)
}

func drawDashedRoute(g *grid, route edgeRoute) {
	for i, point := range route.cells {
		if i%2 != 0 {
			continue
		}
		mask := routeCellMask(route.cells, i)
		ch := previewLineHorizontal
		if mask&(lineUp|lineDown) != 0 && mask&(lineLeft|lineRight) == 0 {
			ch = previewLineVertical
		}
		g.Set(point.X, point.Y, ch, themeRoutePreview)
	}
}

func routeCellMask(cells []routePoint, index int) lineMask {
	mask := lineMask(0)
	if index > 0 {
		mask |= maskBetween(cells[index], cells[index-1])
	}
	if index+1 < len(cells) {
		mask |= maskBetween(cells[index], cells[index+1])
	}
	return mask
}

func maskBetween(from, to routePoint) lineMask {
	switch {
	case to.X == from.X && to.Y == from.Y-1:
		return lineUp
	case to.X == from.X+1 && to.Y == from.Y:
		return lineRight
	case to.X == from.X && to.Y == from.Y+1:
		return lineDown
	case to.X == from.X-1 && to.Y == from.Y:
		return lineLeft
	default:
		return 0
	}
}

func directionBetween(from, to routePoint) int {
	switch {
	case to.X == from.X && to.Y == from.Y-1:
		return directionUp
	case to.X == from.X+1 && to.Y == from.Y:
		return directionRight
	case to.X == from.X && to.Y == from.Y+1:
		return directionDown
	case to.X == from.X-1 && to.Y == from.Y:
		return directionLeft
	default:
		return noDirection
	}
}

func portExitDirection(port routePort) int {
	switch maskBetween(port.border, port.entry) {
	case lineUp:
		return directionUp
	case lineRight:
		return directionRight
	case lineDown:
		return directionDown
	case lineLeft:
		return directionLeft
	default:
		return noDirection
	}
}

func portApproachDirection(port routePort) int {
	switch portExitDirection(port) {
	case directionUp:
		return directionDown
	case directionRight:
		return directionLeft
	case directionDown:
		return directionUp
	case directionLeft:
		return directionRight
	default:
		return noDirection
	}
}
