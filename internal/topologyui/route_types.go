package topologyui

const (
	previewLineHorizontal = '╌'
	previewLineVertical   = '╎'
)

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
	maxRoutePorts  = 12
)

func pointInRect(point routePoint, r rect) bool {
	return point.X >= r.X && point.X < r.X+r.W && point.Y >= r.Y && point.Y < r.Y+r.H
}

func manhattan(a, b routePoint) int {
	return abs(a.X-b.X) + abs(a.Y-b.Y)
}
