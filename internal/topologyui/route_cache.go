package topologyui

import (
	"strconv"
	"strings"
)

func renderRouteCacheKey(m Model, width, height, panX, panY int) string {
	var b strings.Builder
	writeRouteCacheSize(&b, width, height)
	b.WriteByte('|')
	b.WriteString("pan=")
	b.WriteString(strconv.Itoa(panX))
	b.WriteByte(',')
	b.WriteString(strconv.Itoa(panY))
	b.WriteByte('|')
	writeRouteCacheModel(&b, m)
	return b.String()
}

func renderRouteCacheStableKey(m Model, width, height int) string {
	var b strings.Builder
	writeRouteCacheSize(&b, width, height)
	b.WriteByte('|')
	writeRouteCacheModel(&b, m)
	return b.String()
}

func writeRouteCacheSize(b *strings.Builder, width, height int) {
	width = max(0, width)
	height = max(0, height)
	b.WriteString(strconv.Itoa(width))
	b.WriteByte('x')
	b.WriteString(strconv.Itoa(height))
}

func writeRouteCacheModel(b *strings.Builder, m Model) {
	for _, node := range m.Nodes {
		b.WriteString(node.Type)
		b.WriteByte(':')
		b.WriteString(node.ID)
		b.WriteByte('@')
		b.WriteString(strconv.Itoa(node.X))
		b.WriteByte(',')
		b.WriteString(strconv.Itoa(node.Y))
		b.WriteByte(';')
	}
	b.WriteByte('|')
	for _, edge := range m.Edges {
		b.WriteString(edge.From)
		b.WriteString("->")
		b.WriteString(edge.To)
		b.WriteByte(';')
	}
}

func translateVisibleEdges(edges []visibleEdge, dx, dy int) []visibleEdge {
	if dx == 0 && dy == 0 {
		return edges
	}
	out := make([]visibleEdge, len(edges))
	for i, edge := range edges {
		out[i] = edge
		out[i].route = translateEdgeRoute(edge.route, dx, dy)
	}
	return out
}

func translateEdgeRoute(route edgeRoute, dx, dy int) edgeRoute {
	route.cells = translateRoutePoints(route.cells, dx, dy)
	route.start = translateRoutePort(route.start, dx, dy)
	route.end = translateRoutePort(route.end, dx, dy)
	return route
}

func translateRoutePort(port routePort, dx, dy int) routePort {
	port.border = translateRoutePoint(port.border, dx, dy)
	port.entry = translateRoutePoint(port.entry, dx, dy)
	return port
}

func translateRoutePoints(points []routePoint, dx, dy int) []routePoint {
	out := make([]routePoint, len(points))
	for i, point := range points {
		out[i] = translateRoutePoint(point, dx, dy)
	}
	return out
}

func translateRoutePoint(point routePoint, dx, dy int) routePoint {
	point.X += dx
	point.Y += dy
	return point
}
