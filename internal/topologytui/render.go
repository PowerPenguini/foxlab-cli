package topologytui

import "io"

const (
	FocusGraph = 0
)

const (
	minWidth   = 56
	minHeight  = 14
	nodeWidth  = 16
	nodeHeight = 4
)

type ViewState struct {
	Selected           int
	Focus              int
	Message            string
	ContextMenu        bool
	ContextGroup       string
	ContextInSubmenu   bool
	ContextSelected    int
	ContextSubSelected int
	ContextEdit        bool
	ContextEditValue   string
	ContextEditCursor  int
	MoveMode           bool
	MoveNodeID         string
	MoveNodeType       string
	MoveStartX         int
	MoveStartY         int
	CommandMode        bool
	Command            string
	Console            []string
}

func Render(w io.Writer, m Model, state ViewState, width, height int, ansi bool) error {
	_, err := io.WriteString(w, RenderString(m, state, width, height, ansi))
	return err
}

func RenderString(m Model, state ViewState, width, height int, ansi bool) string {
	return renderGrid(m, state, width, height).String(ansi)
}

func renderGrid(m Model, state ViewState, width, height int) *grid {
	if width < minWidth {
		width = minWidth
	}
	if height < minHeight {
		height = minHeight
	}
	g := newGrid(width, height)
	graph := rect{x: 0, y: 0, w: width, h: height}

	nodeRects := layoutNodeRects(m, graph)
	visibleEdges := map[Edge]edgeRoute{}
	for _, edge := range m.Edges {
		from := nodeRects[edge.From]
		to := nodeRects[edge.To]
		if rectFullyVisible(from, graph) && rectFullyVisible(to, graph) {
			if route, ok := drawEdge(g, from, to, graph); ok {
				visibleEdges[edge] = route
			}
		}
	}
	for i, node := range m.Nodes {
		nodeRect := nodeRects[node.Key()]
		if rectFullyVisible(nodeRect, graph) {
			drawNode(g, node, nodeRect, i == normalizedSelected(m, state.Selected), state.Focus == FocusGraph)
		}
	}
	for edge, route := range visibleEdges {
		if route.overlay {
			drawEdgeRoute(g, route)
		}
		drawEdgePorts(g, nodeRects[edge.From], nodeRects[edge.To], route)
	}
	for i, node := range m.Nodes {
		nodeRect := nodeRects[node.Key()]
		if rectFullyVisible(nodeRect, graph) {
			styleBoxBorder(g, nodeRect, selectedBorderStyle(i == normalizedSelected(m, state.Selected), state.Focus == FocusGraph))
		}
	}
	drawContextMenu(g, m, state, nodeRects, graph)
	drawConsole(g, state, width, height)
	return g
}

func layoutNodeRects(m Model, pane rect) map[string]rect {
	out := make(map[string]rect, len(m.Nodes))
	if len(m.Nodes) == 0 {
		return out
	}
	for _, node := range m.Nodes {
		x := pane.x + node.X
		y := pane.y + node.Y
		out[node.Key()] = rect{x: x, y: y, w: nodeWidth, h: nodeHeight}
	}
	return out
}

func rectFullyVisible(r rect, bounds rect) bool {
	return r.x >= bounds.x &&
		r.y >= bounds.y &&
		r.x+r.w <= bounds.x+bounds.w &&
		r.y+r.h <= bounds.y+bounds.h
}

func drawNode(g *grid, node Node, r rect, selected, graphFocused bool) {
	stateStyleValue := stateStyle(node.State)
	clearRect(g, r)
	drawBox(g, r, "", selectedBorderStyle(selected, graphFocused))
	kind := "[" + NodeKind(node.Type) + "]"
	g.text(r.x+1, r.y+1, fit(kind+" "+node.Label, r.w-2), "")
	g.text(r.x+1, r.y+2, fit(node.State, r.w-2), stateStyleValue)
}

func selectedBorderStyle(selected, graphFocused bool) string {
	if !selected {
		return ""
	}
	if graphFocused {
		return ansiBold + ansiBrightCyan
	}
	return ansiCyan
}

func styleBoxBorder(g *grid, r rect, style string) {
	if r.w < 2 || r.h < 2 {
		return
	}
	for x := r.x; x < r.x+r.w; x++ {
		g.setStyle(x, r.y, style)
		g.setStyle(x, r.y+r.h-1, style)
	}
	for y := r.y + 1; y < r.y+r.h-1; y++ {
		g.setStyle(r.x, y, style)
		g.setStyle(r.x+r.w-1, y, style)
	}
}

func drawContextMenu(g *grid, m Model, state ViewState, nodeRects map[string]rect, bounds rect) {
	if !state.ContextMenu {
		return
	}
	items := globalContextMenuItems("")
	nodeRect := rect{x: bounds.x + 2, y: bounds.y + 1, w: 0, h: 0}
	hasNode := false
	node := Node{}
	if len(m.Nodes) > 0 {
		selected := normalizedSelected(m, state.Selected)
		node = m.Nodes[selected]
		hasNode = true
		if rect, ok := nodeRects[node.Key()]; ok {
			nodeRect = rect
			items = contextMenuItems(node, "")
		}
	}
	if len(items) == 0 {
		return
	}
	rootMenuH := min(len(items), max(1, bounds.h))
	rootActive := normalizedMenuSelection(state.ContextSelected, len(items))
	rootStart := contextMenuStart(rootActive, len(items), rootMenuH)
	rootMenuW := contextMenuWidth(items)
	x := nodeRect.x + nodeRect.w + 1
	if x+rootMenuW > bounds.x+bounds.w {
		x = nodeRect.x - rootMenuW - 1
	}
	x = clamp(x, bounds.x, bounds.x+bounds.w-rootMenuW)
	y := nodeRect.y
	if y+rootMenuH > bounds.y+bounds.h {
		y = bounds.y + bounds.h - rootMenuH
	}
	y = clamp(y, bounds.y, bounds.y+bounds.h-rootMenuH)
	rootMenu := rect{x: x, y: y, w: rootMenuW, h: rootMenuH}

	drawContextMenuItems(g, rootMenu, items, rootActive, rootStart, state.ContextInSubmenu == false, false, "", 0)

	rootContextGroup := activeRootContextGroup(items, rootActive)
	if rootContextGroup == "" {
		return
	}
	contextGroup := rootContextGroup
	if state.ContextInSubmenu {
		if state.ContextGroup != rootContextGroup {
			return
		}
		contextGroup = state.ContextGroup
	}
	subItems := contextMenuSubmenuItems(node, hasNode, contextGroup)
	if len(subItems) == 0 {
		return
	}
	subMenuH := min(len(subItems), max(1, bounds.h))
	subActive := normalizedMenuSelection(state.ContextSubSelected, len(subItems))
	subStart := contextMenuStart(subActive, len(subItems), subMenuH)
	subMenuW := contextMenuWidth(subItems)
	if state.ContextEdit {
		subMenuW = max(subMenuW, runeLen(contextEditLabel(subItems[subActive], state.ContextEditValue, state.ContextEditCursor))+3)
	}
	subX := rootMenu.x + rootMenuW
	if subX+subMenuW > bounds.x+bounds.w {
		subX = rootMenu.x - subMenuW
	}
	subX = clamp(subX, bounds.x, bounds.x+bounds.w-subMenuW)
	subY := y + (rootActive - rootStart)
	if subY < bounds.y {
		subY = bounds.y
	}
	if subY+subMenuH > bounds.y+bounds.h {
		subY = bounds.y + bounds.h - subMenuH
	}
	subY = clamp(subY, bounds.y, bounds.y+bounds.h-subMenuH)
	subMenu := rect{x: subX, y: subY, w: subMenuW, h: subMenuH}
	drawContextMenuItems(g, subMenu, subItems, subActive, subStart, state.ContextInSubmenu, state.ContextEdit, state.ContextEditValue, state.ContextEditCursor)
}

func drawContextMenuItems(g *grid, menu rect, items []string, active, start int, isActive bool, editing bool, editValue string, editCursor int) {
	for row := 0; row < menu.h; row++ {
		i := start + row
		item := items[i]
		if editing && i == active {
			item = contextEditLabel(item, editValue, editCursor)
		}
		rowStyle := ansiBgGray + ansiWhite
		indicatorStyle := rowStyle
		if isActive && i == active {
			rowStyle += ansiBold
			indicatorStyle = ansiBgCyan
		}
		fillRow(g, menu.x, menu.y+row, menu.w, rowStyle)
		g.set(menu.x, menu.y+row, ' ', indicatorStyle)
		g.text(menu.x+2, menu.y+row, fit(item, menu.w-3), rowStyle)
	}
}

func drawConsole(g *grid, state ViewState, width, height int) {
	if state.CommandMode {
		drawConsoleLines(g, []string{":" + state.Command}, width, height, true)
		return
	}
	lines := append([]string{}, state.Console...)
	if state.Message != "" {
		lines = append(lines, state.Message)
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	drawConsoleLines(g, lines, width, height, false)
}

func drawConsoleLines(g *grid, lines []string, width, height int, commandMode bool) {
	maxLines := min(len(lines), 5)
	y := height - maxLines
	for i := 0; i < maxLines; i++ {
		line := lines[len(lines)-maxLines+i]
		style := ansiBgGray + ansiWhite
		if commandMode && i == maxLines-1 {
			style += ansiBold
		}
		fillRow(g, 0, y+i, width, style)
		g.text(1, y+i, fit(line, width-2), style)
	}
}

type edgeRoute struct {
	vertical    bool
	overlay     bool
	x1          int
	y1          int
	x2          int
	y2          int
	startAttach lineMask
	endAttach   lineMask
}

func drawEdge(g *grid, from, to rect, bounds rect) (edgeRoute, bool) {
	route, ok := planEdgeRoute(from, to, bounds)
	if !ok {
		return edgeRoute{}, false
	}
	if route.overlay {
		return route, true
	}
	drawEdgeRoute(g, route)
	return route, true
}

func drawEdgeRoute(g *grid, route edgeRoute) {
	if route.overlay && route.vertical {
		lineV(g, route.x1, route.y1, route.y2)
		lineHAttached(g, route.x1, route.x2, route.y2, 0, route.endAttach, ansiDim)
		return
	}
	if route.vertical {
		if route.x1 == route.x2 {
			lineVAttached(g, route.x1, route.y1, route.y2, route.startAttach, route.endAttach, ansiDim)
			return
		}
		mid := (route.y1 + route.y2) / 2
		lineVAttached(g, route.x1, route.y1, mid, route.startAttach, 0, ansiDim)
		lineH(g, route.x1, route.x2, mid)
		lineVAttached(g, route.x2, mid, route.y2, 0, route.endAttach, ansiDim)
		return
	}
	if route.y1 == route.y2 {
		lineHAttached(g, route.x1, route.x2, route.y1, route.startAttach, route.endAttach, ansiDim)
		return
	}
	mid := (route.x1 + route.x2) / 2
	lineHAttached(g, route.x1, mid, route.y1, route.startAttach, 0, ansiDim)
	lineV(g, mid, route.y1, route.y2)
	lineHAttached(g, mid, route.x2, route.y2, 0, route.endAttach, ansiDim)
}

func planEdgeRoute(from, to rect, bounds rect) (edgeRoute, bool) {
	if from.w == 0 || to.w == 0 {
		return edgeRoute{}, false
	}
	fromCenterX := from.x + from.w/2
	fromCenterY := from.y + from.h/2
	toCenterX := to.x + to.w/2
	toCenterY := to.y + to.h/2

	vertical := edgeRoute{vertical: true, x1: fromCenterX, x2: toCenterX}
	if toCenterY > fromCenterY {
		vertical.y1 = from.y + from.h
		vertical.y2 = to.y - 1
		vertical.startAttach = lineUp
		vertical.endAttach = lineDown
	} else {
		vertical.y1 = from.y - 1
		vertical.y2 = to.y + to.h
		vertical.startAttach = lineDown
		vertical.endAttach = lineUp
	}
	verticalOK := routeInside(vertical, bounds) && ((toCenterY > fromCenterY && vertical.y1 <= vertical.y2) || (toCenterY < fromCenterY && vertical.y1 >= vertical.y2))
	if !verticalOK {
		if route, ok := adjacentVerticalRoute(from, to, bounds); ok {
			return route, true
		}
	}

	horizontal := edgeRoute{y1: fromCenterY, y2: toCenterY}
	if toCenterX < fromCenterX {
		horizontal.x1 = from.x - 1
		horizontal.x2 = to.x + to.w
		horizontal.startAttach = lineRight
		horizontal.endAttach = lineLeft
	} else {
		horizontal.x1 = from.x + from.w
		horizontal.x2 = to.x - 1
		horizontal.startAttach = lineLeft
		horizontal.endAttach = lineRight
	}
	horizontalOK := routeInside(horizontal, bounds) && ((toCenterX > fromCenterX && horizontal.x1 <= horizontal.x2) || (toCenterX < fromCenterX && horizontal.x1 >= horizontal.x2))

	verticalPreferred := horizontalOverlap(from, to) || abs(toCenterY-fromCenterY) >= abs(toCenterX-fromCenterX)
	switch {
	case verticalOK && (verticalPreferred || !horizontalOK):
		return vertical, true
	case horizontalOK:
		return horizontal, true
	case verticalOK:
		return vertical, true
	default:
		return edgeRoute{}, false
	}
}

func adjacentVerticalRoute(from, to rect, bounds rect) (edgeRoute, bool) {
	fromCenterX := from.x + from.w/2
	fromCenterY := from.y + from.h/2
	toCenterX := to.x + to.w/2
	toCenterY := to.y + to.h/2
	if abs(toCenterY-fromCenterY) > nodeHeight || abs(toCenterX-fromCenterX) > nodeWidth {
		return edgeRoute{}, false
	}
	upper := from
	lower := to
	if toCenterY < fromCenterY {
		upper = to
		lower = from
	}
	if gap := lower.y - (upper.y + upper.h); gap < 0 || gap > 1 {
		return edgeRoute{}, false
	}
	route := edgeRoute{
		vertical: true,
		overlay:  true,
		x1:       upper.x + upper.w/2,
		y1:       upper.y + upper.h - 1,
		x2:       lower.x + lower.w/2,
		y2:       lower.y,
	}
	if !routeInside(route, bounds) {
		return edgeRoute{}, false
	}
	return route, true
}

func routeInside(route edgeRoute, bounds rect) bool {
	return route.x1 >= bounds.x && route.x1 < bounds.x+bounds.w &&
		route.x2 >= bounds.x && route.x2 < bounds.x+bounds.w &&
		route.y1 >= bounds.y && route.y1 < bounds.y+bounds.h &&
		route.y2 >= bounds.y && route.y2 < bounds.y+bounds.h
}

func horizontalOverlap(a, b rect) bool {
	return max(a.x, b.x) <= min(a.x+a.w-1, b.x+b.w-1)
}

func drawEdgePorts(g *grid, from, to rect, route edgeRoute) {
	if route.overlay && route.vertical {
		drawOverlayEdgePorts(g, from, to)
		return
	}
	drawEdgePort(g, from, to, route.vertical)
	drawEdgePort(g, to, from, route.vertical)
}

func drawOverlayEdgePorts(g *grid, from, to rect) {
	if from.y+from.h/2 < to.y+to.h/2 {
		drawEdgePort(g, from, to, true)
		return
	}
	drawEdgePort(g, to, from, true)
}

func drawEdgePort(g *grid, node, other rect, vertical bool) {
	nodeCenterX := node.x + node.w/2
	nodeCenterY := node.y + node.h/2
	otherCenterY := other.y + other.h/2
	if vertical {
		x := nodeCenterX
		if otherCenterY > nodeCenterY {
			g.setLine(x, node.y+node.h-1, lineLeft|lineRight|lineDown, ansiDim)
			return
		}
		g.setLine(x, node.y, lineLeft|lineRight|lineUp, ansiDim)
		return
	}
	y := nodeCenterY
	otherCenterX := other.x + other.w/2
	if otherCenterX > nodeCenterX {
		g.setLine(node.x+node.w-1, y, lineUp|lineRight|lineDown, ansiDim)
		return
	}
	g.setLine(node.x, y, lineUp|lineLeft|lineDown, ansiDim)
}

func MoveSelection(m Model, current int, key string) int {
	if len(m.Nodes) == 0 {
		return 0
	}
	current = normalizedSelected(m, current)
	if next, ok := visualNeighbor(m, current, key); ok {
		return next
	}
	return current
}

func MoveContextSelection(current, length int, key string) int {
	if length <= 0 {
		return 0
	}
	current = normalizedMenuSelection(current, length)
	switch key {
	case "down":
		return (current + 1) % length
	case "up":
		return (current - 1 + length) % length
	default:
		return current
	}
}

func NextFocus(current int) int {
	return FocusGraph
}

func normalizedMenuSelection(selected, length int) int {
	if length <= 0 || selected < 0 {
		return 0
	}
	if selected >= length {
		return length - 1
	}
	return selected
}

func normalizedSelected(m Model, selected int) int {
	if len(m.Nodes) == 0 {
		return 0
	}
	if selected < 0 {
		return 0
	}
	if selected >= len(m.Nodes) {
		return len(m.Nodes) - 1
	}
	return selected
}

func visualNeighbor(m Model, current int, direction string) (int, bool) {
	node := m.Nodes[current]
	best := -1
	bestScore := 0
	for i, candidate := range m.Nodes {
		if i == current {
			continue
		}
		dx := candidate.X - node.X
		dy := candidate.Y - node.Y
		primary, secondary, ok := visualDirectionDelta(dx, dy, direction)
		if !ok {
			continue
		}
		score := secondary*1000 + primary
		if best == -1 || score < bestScore {
			best = i
			bestScore = score
		}
	}
	return best, best != -1
}

func visualDirectionDelta(dx, dy int, direction string) (int, int, bool) {
	switch direction {
	case "left":
		if dx >= 0 {
			return 0, 0, false
		}
		return -dx, abs(dy), true
	case "right":
		if dx <= 0 {
			return 0, 0, false
		}
		return dx, abs(dy), true
	case "up":
		if dy >= 0 {
			return 0, 0, false
		}
		return -dy, abs(dx), true
	case "down":
		if dy <= 0 {
			return 0, 0, false
		}
		return dy, abs(dx), true
	default:
		return 0, 0, false
	}
}

func stateStyle(state string) string {
	switch state {
	case "running", "link":
		return ansiCyan
	case "nat", "bridge", "macnat-bridge":
		return ansiYellow
	default:
		return ansiDim
	}
}
