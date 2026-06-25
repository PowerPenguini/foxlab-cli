package topologyui

import (
	"sort"
	"strings"
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
	workloadSourceUsage := map[string]int{}
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
		if workloadLink {
			relaxedRoute, relaxedOK := planner.planRouteWithOptions(from, to, routeOptions{allowOccupied: true})
			if !ok && relaxedOK {
				route = relaxedRoute
				ok = true
			} else if ok && workloadSourceUsage[edge.From] > 0 && relaxedOK && len(relaxedRoute.cells) < len(route.cells) {
				route = relaxedRoute
				ok = true
			}
		}
		if !ok {
			continue
		}
		planner.reserve(route)
		if workloadLink {
			workloadSourceUsage[edge.From]++
		}
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
	return best, bestOK
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
