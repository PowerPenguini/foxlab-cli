package topologyui

import (
	"container/heap"
	"io"
	"sort"
	"strconv"
	"strings"
)

const (
	FocusGraph = 0
)

const (
	minWidth   = 56
	minHeight  = 14
	nodeWidth  = 16
	nodeHeight = 4
)

const (
	previewLineHorizontal = '╌'
	previewLineVertical   = '╎'
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
	ContextDeleteNIC   bool
	ContextEdit        bool
	ContextEditValue   string
	ContextEditCursor  int
	MoveMode           bool
	MoveNodeID         string
	MoveNodeType       string
	MoveStartX         int
	MoveStartY         int
	ConnectMode        bool
	ConnectNodeID      string
	ConnectNodeType    string
	ConnectNICIndex    string
	ConnectTargetMenu  bool
	ConnectTargetID    string
	ConnectTargetType  string
	ConnectTargetIndex int
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
	for i, node := range m.Nodes {
		nodeRect := nodeRects[node.Key()]
		if rectFullyVisible(nodeRect, graph) {
			drawNode(g, node, nodeRect, i == normalizedSelected(m, state.Selected), state.Focus == FocusGraph)
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
		nodeRect := nodeRects[node.Key()]
		if rectFullyVisible(nodeRect, graph) {
			styleBoxBorder(g, nodeRect, selectedBorderStyle(i == normalizedSelected(m, state.Selected), state.Focus == FocusGraph))
		}
	}
	drawContextMenu(g, m, state, nodeRects, graph)
	drawConnectTargetMenu(g, m, state, nodeRects, graph)
	drawConsole(g, state, width, height)
	return g
}

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
		if !fromOK || !toOK || !rectFullyVisible(from, bounds) || !rectFullyVisible(to, bounds) {
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

func renderRouteCacheKey(m Model, width, height int) string {
	if width < minWidth {
		width = minWidth
	}
	if height < minHeight {
		height = minHeight
	}
	var b strings.Builder
	b.WriteString(strconv.Itoa(width))
	b.WriteByte('x')
	b.WriteString(strconv.Itoa(height))
	b.WriteByte('|')
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
	return b.String()
}

func layoutNodeRects(m Model, pane rect) map[string]rect {
	out := make(map[string]rect, len(m.Nodes))
	if len(m.Nodes) == 0 {
		return out
	}
	for _, node := range m.Nodes {
		x := pane.X + node.X
		y := pane.Y + node.Y
		out[node.Key()] = rect{X: x, Y: y, W: nodeWidth, H: nodeHeight}
	}
	return out
}

func rectFullyVisible(r rect, bounds rect) bool {
	return r.X >= bounds.X &&
		r.Y >= bounds.Y &&
		r.X+r.W <= bounds.X+bounds.W &&
		r.Y+r.H <= bounds.Y+bounds.H
}

func drawNode(g *grid, node Node, r rect, selected, graphFocused bool) {
	stateStyleValue := stateStyle(node.State)
	clearRect(g, r)
	drawBox(g, r, "", selectedBorderStyle(selected, graphFocused))
	kind := "[" + firstNonEmpty(node.Badge, NodeKind(node.Type)) + "]"
	g.Text(r.X+1, r.Y+1, fit(kind+" "+node.Label, r.W-2), "")
	g.Text(r.X+1, r.Y+2, fit(node.State, r.W-2), stateStyleValue)
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
	if r.W < 2 || r.H < 2 {
		return
	}
	for x := r.X; x < r.X+r.W; x++ {
		g.SetStyle(x, r.Y, style)
		g.SetStyle(x, r.Y+r.H-1, style)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		g.SetStyle(r.X, y, style)
		g.SetStyle(r.X+r.W-1, y, style)
	}
}

func drawContextMenu(g *grid, m Model, state ViewState, nodeRects map[string]rect, bounds rect) {
	if !state.ContextMenu {
		return
	}
	items := globalContextMenuItems("")
	nodeRect := rect{X: bounds.X + 2, Y: bounds.Y + 1, W: 0, H: 0}
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
	rootMenuH := min(len(items), max(1, bounds.H))
	rootActive := normalizedMenuSelection(state.ContextSelected, len(items))
	rootStart := contextMenuStart(rootActive, len(items), rootMenuH)
	rootMenuW := contextMenuWidth(items)
	x := nodeRect.X + nodeRect.W + 1
	if x+rootMenuW > bounds.X+bounds.W {
		x = nodeRect.X - rootMenuW - 1
	}
	x = clamp(x, bounds.X, bounds.X+bounds.W-rootMenuW)
	y := nodeRect.Y
	if y+rootMenuH > bounds.Y+bounds.H {
		y = bounds.Y + bounds.H - rootMenuH
	}
	y = clamp(y, bounds.Y, bounds.Y+bounds.H-rootMenuH)
	rootMenu := rect{X: x, Y: y, W: rootMenuW, H: rootMenuH}

	drawContextMenuItems(g, rootMenu, items, rootActive, rootStart, state.ContextInSubmenu == false, false, "", 0, false)

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
	subMenuH := min(len(subItems), max(1, bounds.H))
	subActive := normalizedMenuSelection(state.ContextSubSelected, len(subItems))
	subStart := contextMenuStart(subActive, len(subItems), subMenuH)
	subMenuW := contextMenuWidth(subItems)
	if state.ContextEdit {
		subMenuW = max(subMenuW, runeLen(contextEditLabel(subItems[subActive], state.ContextEditValue, state.ContextEditCursor))+3)
	}
	subX := rootMenu.X + rootMenuW
	if subX+subMenuW > bounds.X+bounds.W {
		subX = rootMenu.X - subMenuW
	}
	subX = clamp(subX, bounds.X, bounds.X+bounds.W-subMenuW)
	subY := y + (rootActive - rootStart)
	if subY < bounds.Y {
		subY = bounds.Y
	}
	if subY+subMenuH > bounds.Y+bounds.H {
		subY = bounds.Y + bounds.H - subMenuH
	}
	subY = clamp(subY, bounds.Y, bounds.Y+bounds.H-subMenuH)
	subMenu := rect{X: subX, Y: subY, W: subMenuW, H: subMenuH}
	drawContextMenuItems(g, subMenu, subItems, subActive, subStart, state.ContextInSubmenu, state.ContextEdit, state.ContextEditValue, state.ContextEditCursor, contextGroup == "nic-menu" && state.ContextDeleteNIC)
}

func drawConnectTargetMenu(g *grid, m Model, state ViewState, nodeRects map[string]rect, bounds rect) {
	if !state.ConnectTargetMenu {
		return
	}
	node, ok := nodeByKey(m, NodeKey(state.ConnectTargetType, state.ConnectTargetID))
	if !ok {
		return
	}
	items := connectTargetNICMenuItems(node)
	if len(items) == 0 {
		return
	}
	nodeRect, ok := nodeRects[node.Key()]
	if !ok {
		return
	}
	menuH := min(len(items), max(1, bounds.H))
	active := normalizedMenuSelection(state.ConnectTargetIndex, len(items))
	start := contextMenuStart(active, len(items), menuH)
	menuW := contextMenuWidth(items)
	x := nodeRect.X + nodeRect.W + 1
	if x+menuW > bounds.X+bounds.W {
		x = nodeRect.X - menuW - 1
	}
	x = clamp(x, bounds.X, bounds.X+bounds.W-menuW)
	y := nodeRect.Y
	if y+menuH > bounds.Y+bounds.H {
		y = bounds.Y + bounds.H - menuH
	}
	y = clamp(y, bounds.Y, bounds.Y+bounds.H-menuH)
	drawContextMenuItems(g, rect{X: x, Y: y, W: menuW, H: menuH}, items, active, start, true, false, "", 0, false)
}

func drawConnectPreview(g *grid, m Model, state ViewState, nodeRects map[string]rect, bounds rect, planner *routePlanner) {
	if !state.ConnectMode {
		return
	}
	sourceKey := NodeKey(state.ConnectNodeType, state.ConnectNodeID)
	targetKey := connectPreviewTargetKey(m, state)
	if targetKey == "" || targetKey == sourceKey {
		return
	}
	from, ok := nodeRects[sourceKey]
	if !ok || !rectFullyVisible(from, bounds) {
		return
	}
	to, ok := nodeRects[targetKey]
	if !ok || !rectFullyVisible(to, bounds) {
		return
	}
	route, ok := planner.planRoute(from, to)
	if !ok {
		return
	}
	drawDashedRoute(g, route)
}

func connectPreviewTargetKey(m Model, state ViewState) string {
	if state.ConnectTargetMenu {
		return NodeKey(state.ConnectTargetType, state.ConnectTargetID)
	}
	node, ok := selectedNode(m, state.Selected)
	if !ok {
		return ""
	}
	return node.Key()
}

func drawContextMenuItems(g *grid, menu rect, items []string, active, start int, isActive bool, editing bool, editValue string, editCursor int, deleteNICSelected bool) {
	for row := 0; row < menu.H; row++ {
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
		fillRow(g, menu.X, menu.Y+row, menu.W, rowStyle)
		g.Set(menu.X, menu.Y+row, ' ', indicatorStyle)
		textWidth := menu.W - 3
		if isNICDetail(item) {
			textWidth = max(0, menu.W-6)
		}
		g.Text(menu.X+2, menu.Y+row, fit(item, textWidth), rowStyle)
		if isNICDetail(item) {
			xStyle := rowStyle
			if isActive && i == active && deleteNICSelected {
				xStyle = ansiBgRed + ansiWhite + ansiBold
			}
			g.Text(menu.X+menu.W-3, menu.Y+row, " X ", xStyle)
		}
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
		g.Text(1, y+i, fit(line, width-2), style)
	}
}

type routePoint struct {
	X int
	Y int
}

type routeSide int

const (
	routeSideLeft routeSide = iota
	routeSideRight
	routeSideTop
	routeSideBottom
)

type routePort struct {
	border routePoint
	entry  routePoint
	side   routeSide
	rank   int
}

type edgeRoute struct {
	cells     []routePoint
	start     routePort
	end       routePort
	cost      int
	pairScore int
}

type visibleEdge struct {
	edge  Edge
	route edgeRoute
}

type routePlanner struct {
	bounds   rect
	nodes    []rect
	occupied map[routePoint]lineMask
}

type routePortPair struct {
	start routePort
	end   routePort
	score int
	index int
}

type routeOptions struct {
	allowOccupied bool
}

const (
	noDirection    = -1
	directionUp    = 0
	directionRight = 1
	directionDown  = 2
	directionLeft  = 3
	maxRoutePairs  = 10
	maxRoutePorts  = 12
)

func visibleNodeRects(nodeRects map[string]rect, bounds rect) []rect {
	out := make([]rect, 0, len(nodeRects))
	for _, r := range nodeRects {
		if rectFullyVisible(r, bounds) {
			out = append(out, r)
		}
	}
	return out
}

func newRoutePlanner(bounds rect, nodes []rect) *routePlanner {
	return &routePlanner{
		bounds:   bounds,
		nodes:    nodes,
		occupied: map[routePoint]lineMask{},
	}
}

func planVisibleRoutes(planner *routePlanner, edges []Edge, nodeRects map[string]rect, bounds rect) []visibleEdge {
	visible := []visibleEdge{}
	orderedEdges := append([]Edge(nil), edges...)
	sort.SliceStable(orderedEdges, func(i, j int) bool {
		return edgeRoutePriority(orderedEdges[i], nodeRects) < edgeRoutePriority(orderedEdges[j], nodeRects)
	})
	for _, edge := range orderedEdges {
		from := nodeRects[edge.From]
		to := nodeRects[edge.To]
		if !rectFullyVisible(from, bounds) || !rectFullyVisible(to, bounds) {
			continue
		}
		workloadLink := workloadNodeKey(edge.From) && workloadNodeKey(edge.To)
		route, ok := planner.planRoute(from, to)
		if ok && workloadLink && routeHasLargeDetour(route.cells) {
			if relaxedRoute, relaxedOK := planner.planRouteWithOptions(from, to, routeOptions{allowOccupied: true}); relaxedOK && len(relaxedRoute.cells) < len(route.cells) {
				route = relaxedRoute
			}
		}
		if !ok {
			continue
		}
		planner.reserve(route)
		visible = append(visible, visibleEdge{edge: edge, route: route})
	}
	return visible
}

func edgeRoutePriority(edge Edge, nodeRects map[string]rect) int {
	from := nodeRects[edge.From]
	to := nodeRects[edge.To]
	fromCenterX := from.X + from.W/2
	fromCenterY := from.Y + from.H/2
	toCenterX := to.X + to.W/2
	toCenterY := to.Y + to.H/2
	directPenalty := 0
	if workloadNodeKey(edge.From) && workloadNodeKey(edge.To) {
		directPenalty = 100000000
	}
	return directPenalty + min(fromCenterY, toCenterY)*10000 + abs(fromCenterY-toCenterY)*100 + abs(fromCenterX-toCenterX)
}

func workloadNodeKey(key string) bool {
	return strings.HasPrefix(key, NodeVM+":") || strings.HasPrefix(key, NodeContainer+":")
}

func drawEdge(g *grid, from, to rect, bounds rect) (edgeRoute, bool) {
	planner := newRoutePlanner(bounds, []rect{from, to})
	route, ok := planner.planRoute(from, to)
	if !ok {
		return edgeRoute{}, false
	}
	drawRoutedEdge(g, route, ansiDim)
	return route, true
}

func (p *routePlanner) planRoute(from, to rect) (edgeRoute, bool) {
	return p.planRouteWithOptions(from, to, routeOptions{})
}

func (p *routePlanner) planRouteWithOptions(from, to rect, options routeOptions) (edgeRoute, bool) {
	if from.W < 2 || from.H < 2 || to.W < 2 || to.H < 2 {
		return edgeRoute{}, false
	}
	pairs := rankedPortPairs(portCandidates(from, to, p.bounds), portCandidates(to, from, p.bounds))
	if len(pairs) == 0 {
		return edgeRoute{}, false
	}
	best := edgeRoute{}
	bestOK := false
	for _, pair := range pairs {
		cells, cost, ok := p.simplePath(pair.start.entry, pair.end.entry, portExitDirection(pair.start), portApproachDirection(pair.end), options)
		if !ok {
			continue
		}
		cost += pair.score
		if !bestOK || cost < best.cost || (cost == best.cost && routeTieBreak(cells, pair, best)) {
			best = edgeRoute{cells: cells, start: pair.start, end: pair.end, cost: cost, pairScore: pair.score}
			bestOK = true
		}
	}
	if bestOK && !routeHasLargeDetour(best.cells) {
		return best, true
	}
	for i, pair := range pairs {
		if i >= maxRoutePairs && bestOK {
			break
		}
		cells, cost, ok := p.shortestPath(pair.start.entry, pair.end.entry, portExitDirection(pair.start), portApproachDirection(pair.end), options)
		if !ok {
			continue
		}
		cost += pair.score
		if !bestOK || cost < best.cost || (cost == best.cost && routeTieBreak(cells, pair, best)) {
			best = edgeRoute{cells: cells, start: pair.start, end: pair.end, cost: cost, pairScore: pair.score}
			bestOK = true
		}
	}
	return best, bestOK
}

func routeHasLargeDetour(cells []routePoint) bool {
	if len(cells) < 2 {
		return false
	}
	direct := manhattan(cells[0], cells[len(cells)-1]) + 1
	return len(cells) > direct+nodeWidth || routeSpanY(cells) > nodeHeight+2
}

func routeSpanY(cells []routePoint) int {
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

func (p *routePlanner) simplePath(start, goal routePoint, startDir, goalDir int, options routeOptions) ([]routePoint, int, bool) {
	waypointSets := [][]routePoint{
		{start, goal},
		{start, {X: goal.X, Y: start.Y}, goal},
		{start, {X: start.X, Y: goal.Y}, goal},
	}
	for _, x := range routeXLanes(start, goal, startDir, goalDir) {
		waypointSets = append(waypointSets, []routePoint{{X: start.X, Y: start.Y}, {X: x, Y: start.Y}, {X: x, Y: goal.Y}, {X: goal.X, Y: goal.Y}})
	}
	for _, y := range routeYLanes(start, goal, startDir, goalDir) {
		waypointSets = append(waypointSets, []routePoint{{X: start.X, Y: start.Y}, {X: start.X, Y: y}, {X: goal.X, Y: y}, {X: goal.X, Y: goal.Y}})
	}

	bestCells := []routePoint{}
	bestCost := 0
	bestOK := false
	for _, waypoints := range waypointSets {
		cells, ok := pathCellsFromWaypoints(waypoints)
		if !ok || !p.pathClear(cells, start, goal, options) {
			continue
		}
		cost := p.routePathCost(cells, start, goal, startDir, goalDir, options)
		if !bestOK || cost < bestCost || (cost == bestCost && len(cells) < len(bestCells)) {
			bestCells = cells
			bestCost = cost
			bestOK = true
		}
	}
	return bestCells, bestCost, bestOK
}

func routeXLanes(start, goal routePoint, startDir, goalDir int) []int {
	out := []int{(start.X + goal.X) / 2}
	if dx := directionDX(startDir); dx != 0 {
		out = append(out, start.X+dx*nodeWidth)
	}
	if dx := directionDX(goalDir); dx != 0 {
		out = append(out, goal.X-dx*nodeWidth)
	}
	return uniqueInts(out)
}

func routeYLanes(start, goal routePoint, startDir, goalDir int) []int {
	out := []int{(start.Y + goal.Y) / 2}
	if dy := directionDY(startDir); dy != 0 {
		out = append(out, start.Y+dy*nodeHeight)
	}
	if dy := directionDY(goalDir); dy != 0 {
		out = append(out, goal.Y-dy*nodeHeight)
	}
	return uniqueInts(out)
}

func uniqueInts(values []int) []int {
	out := []int{}
	seen := map[int]bool{}
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func directionDX(dir int) int {
	switch dir {
	case directionRight:
		return 1
	case directionLeft:
		return -1
	default:
		return 0
	}
}

func directionDY(dir int) int {
	switch dir {
	case directionDown:
		return 1
	case directionUp:
		return -1
	default:
		return 0
	}
}

func pathCellsFromWaypoints(waypoints []routePoint) ([]routePoint, bool) {
	if len(waypoints) == 0 {
		return nil, false
	}
	cells := []routePoint{waypoints[0]}
	for i := 1; i < len(waypoints); i++ {
		from := waypoints[i-1]
		to := waypoints[i]
		dx := sign(to.X - from.X)
		dy := sign(to.Y - from.Y)
		if dx != 0 && dy != 0 {
			return nil, false
		}
		for point := from; point != to; {
			point.X += dx
			point.Y += dy
			if point != cells[len(cells)-1] {
				cells = append(cells, point)
			}
		}
	}
	return cells, true
}

func sign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}

func (p *routePlanner) pathClear(cells []routePoint, start, goal routePoint, options routeOptions) bool {
	for _, point := range cells {
		if p.blocked(point, start, goal, options) {
			return false
		}
	}
	return true
}

func (p *routePlanner) routePathCost(cells []routePoint, start, goal routePoint, startDir, goalDir int, options routeOptions) int {
	if len(cells) == 0 {
		return 0
	}
	cost := 0
	previousDir := startDir
	for i := 1; i < len(cells); i++ {
		dir := directionBetween(cells[i-1], cells[i])
		stepCost := 10
		if previousDir != noDirection && previousDir != dir {
			stepCost += routeTurnPenalty(cells[i-1], start, goal)
		}
		if p.nearNode(cells[i]) {
			stepCost += 25
		}
		if options.allowOccupied && p.occupied[cells[i]] != 0 {
			stepCost += 90
		}
		cost += stepCost
		previousDir = dir
	}
	if goalDir != noDirection && previousDir != goalDir {
		cost += 18
	}
	return cost
}

func routeTurnPenalty(point, start, goal routePoint) int {
	penalty := 18
	if manhattan(point, start) < nodeWidth {
		penalty += 160
	}
	if manhattan(point, goal) < nodeWidth {
		penalty += 160
	}
	return penalty
}

func routeTieBreak(cells []routePoint, pair routePortPair, best edgeRoute) bool {
	if len(cells) != len(best.cells) {
		return len(cells) < len(best.cells)
	}
	if pair.score != best.pairScore {
		return pair.score < best.pairScore
	}
	if len(cells) == 0 || len(best.cells) == 0 {
		return false
	}
	first := cells[0]
	bestFirst := best.cells[0]
	if first.Y != bestFirst.Y {
		return first.Y < bestFirst.Y
	}
	return first.X < bestFirst.X
}

func rankedPortPairs(starts, ends []routePort) []routePortPair {
	pairs := make([]routePortPair, 0, len(starts)*len(ends))
	index := 0
	for _, start := range starts {
		for _, end := range ends {
			score := start.rank + end.rank + manhattan(start.entry, end.entry)
			pairs = append(pairs, routePortPair{start: start, end: end, score: score, index: index})
			index++
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].score != pairs[j].score {
			return pairs[i].score < pairs[j].score
		}
		return pairs[i].index < pairs[j].index
	})
	return pairs
}

func portCandidates(node, other rect, bounds rect) []routePort {
	out := []routePort{}
	add := func(border, entry routePoint, side routeSide, order int) {
		if !pointInRect(entry, bounds) {
			return
		}
		rank := sidePreferencePenalty(node, other, side)
		switch side {
		case routeSideLeft, routeSideRight:
			rank += abs(border.Y-(other.Y+other.H/2)) * 3
		case routeSideTop, routeSideBottom:
			rank += abs(border.X - (other.X + other.W/2))
		}
		rank = rank*10 + order
		out = append(out, routePort{border: border, entry: entry, side: side, rank: rank})
	}
	order := 0
	for y := node.Y + 1; y < node.Y+node.H-1; y++ {
		add(routePoint{X: node.X, Y: y}, routePoint{X: node.X - 1, Y: y}, routeSideLeft, order)
		order++
		add(routePoint{X: node.X + node.W - 1, Y: y}, routePoint{X: node.X + node.W, Y: y}, routeSideRight, order)
		order++
	}
	for x := node.X + 1; x < node.X+node.W-1; x++ {
		add(routePoint{X: x, Y: node.Y}, routePoint{X: x, Y: node.Y - 1}, routeSideTop, order)
		order++
		add(routePoint{X: x, Y: node.Y + node.H - 1}, routePoint{X: x, Y: node.Y + node.H}, routeSideBottom, order)
		order++
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		if out[i].border.Y != out[j].border.Y {
			return out[i].border.Y < out[j].border.Y
		}
		return out[i].border.X < out[j].border.X
	})
	if len(out) > maxRoutePorts {
		out = out[:maxRoutePorts]
	}
	return out
}

func sidePreferencePenalty(node, other rect, side routeSide) int {
	dx := other.X + other.W/2 - (node.X + node.W/2)
	dy := other.Y + other.H/2 - (node.Y + node.H/2)
	horizontal := abs(dx) >= abs(dy)
	if horizontal {
		if dx >= 0 && side == routeSideRight {
			return 0
		}
		if dx < 0 && side == routeSideLeft {
			return 0
		}
		if side == routeSideTop || side == routeSideBottom {
			return 35
		}
		return 75
	}
	if dy >= 0 && side == routeSideBottom {
		return 0
	}
	if dy < 0 && side == routeSideTop {
		return 0
	}
	if side == routeSideLeft || side == routeSideRight {
		return 28
	}
	return 75
}

type routeState struct {
	point routePoint
	dir   int
}

type routeStep struct {
	state routeState
	cost  int
	index int
}

type routeQueue []routeStep

func (q routeQueue) Len() int { return len(q) }
func (q routeQueue) Less(i, j int) bool {
	if q[i].cost != q[j].cost {
		return q[i].cost < q[j].cost
	}
	return q[i].index < q[j].index
}
func (q routeQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *routeQueue) Push(x any)   { *q = append(*q, x.(routeStep)) }
func (q *routeQueue) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	*q = old[:n-1]
	return item
}

func (p *routePlanner) shortestPath(start, goal routePoint, startDir, goalDir int, options routeOptions) ([]routePoint, int, bool) {
	if p.blocked(start, start, goal, options) || p.blocked(goal, start, goal, options) {
		return nil, 0, false
	}
	searchBounds := routeSearchBounds(start, goal, p.bounds)
	startState := routeState{point: start, dir: startDir}
	dist := map[routeState]int{startState: 0}
	prev := map[routeState]routeState{}
	q := &routeQueue{}
	heap.Init(q)
	heap.Push(q, routeStep{state: startState})
	pushIndex := 1
	bestGoal := routeState{}
	bestGoalCost := 0
	found := false
	for q.Len() > 0 {
		item := heap.Pop(q).(routeStep)
		if item.cost != dist[item.state] {
			continue
		}
		if item.state.point == goal {
			bestGoal = item.state
			bestGoalCost = item.cost
			if goalDir != noDirection && item.state.dir != goalDir {
				bestGoalCost += 18
			}
			found = true
			break
		}
		for dir, next := range routeNeighbors(item.state.point) {
			if !pointInRect(next, searchBounds) {
				continue
			}
			if p.blocked(next, start, goal, options) {
				continue
			}
			nextState := routeState{point: next, dir: dir}
			stepCost := 10
			if item.state.dir != noDirection && item.state.dir != dir {
				stepCost += routeTurnPenalty(item.state.point, start, goal)
			}
			if p.nearNode(next) {
				stepCost += 25
			}
			if options.allowOccupied && p.occupied[next] != 0 {
				stepCost += 90
			}
			nextCost := item.cost + stepCost
			if current, ok := dist[nextState]; ok && current <= nextCost {
				continue
			}
			dist[nextState] = nextCost
			prev[nextState] = item.state
			heap.Push(q, routeStep{state: nextState, cost: nextCost, index: pushIndex})
			pushIndex++
		}
	}
	if !found {
		return nil, 0, false
	}
	cells := []routePoint{}
	for state := bestGoal; ; state = prev[state] {
		cells = append(cells, state.point)
		if state == startState {
			break
		}
	}
	for i, j := 0, len(cells)-1; i < j; i, j = i+1, j-1 {
		cells[i], cells[j] = cells[j], cells[i]
	}
	return cells, bestGoalCost, true
}

func routeSearchBounds(start, goal routePoint, bounds rect) rect {
	marginX := nodeWidth * 2
	marginY := nodeHeight * 4
	minX := max(bounds.X, min(start.X, goal.X)-marginX)
	maxX := min(bounds.X+bounds.W-1, max(start.X, goal.X)+marginX)
	minY := max(bounds.Y, min(start.Y, goal.Y)-marginY)
	maxY := min(bounds.Y+bounds.H-1, max(start.Y, goal.Y)+marginY)
	return rect{X: minX, Y: minY, W: max(1, maxX-minX+1), H: max(1, maxY-minY+1)}
}

func routeNeighbors(p routePoint) []routePoint {
	return []routePoint{
		{X: p.X, Y: p.Y - 1},
		{X: p.X + 1, Y: p.Y},
		{X: p.X, Y: p.Y + 1},
		{X: p.X - 1, Y: p.Y},
	}
}

func (p *routePlanner) blocked(point, start, goal routePoint, options routeOptions) bool {
	if !pointInRect(point, p.bounds) {
		return true
	}
	if p.occupied[point] != 0 && !options.allowOccupied {
		return true
	}
	for _, node := range p.nodes {
		if pointInRect(point, node) {
			return true
		}
	}
	if point == start || point == goal {
		return false
	}
	return false
}

func (p *routePlanner) nearNode(point routePoint) bool {
	for _, node := range p.nodes {
		expanded := rect{X: node.X - 1, Y: node.Y - 1, W: node.W + 2, H: node.H + 2}
		if pointInRect(point, expanded) {
			return true
		}
	}
	return false
}

func (p *routePlanner) reserve(route edgeRoute) {
	for i := 1; i < len(route.cells); i++ {
		a := route.cells[i-1]
		b := route.cells[i]
		p.occupied[a] |= maskBetween(a, b)
		p.occupied[b] |= maskBetween(b, a)
	}
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
	drawRoutePort(g, route.start)
	drawRoutePort(g, route.end)
}

func drawRoutePort(g *grid, port routePort) {
	g.SetLine(port.border.X, port.border.Y, maskBetween(port.border, port.entry), ansiDim)
	g.SetLine(port.entry.X, port.entry.Y, maskBetween(port.entry, port.border), ansiDim)
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
		g.Set(point.X, point.Y, ch, ansiDim+ansiBrightCyan)
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

func pointInRect(point routePoint, r rect) bool {
	return point.X >= r.X && point.X < r.X+r.W && point.Y >= r.Y && point.Y < r.Y+r.H
}

func manhattan(a, b routePoint) int {
	return abs(a.X-b.X) + abs(a.Y-b.Y)
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
