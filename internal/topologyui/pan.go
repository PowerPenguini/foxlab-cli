package topologyui

const (
	keyboardPanStepX = 8
	keyboardPanStepY = 4
)

func (a *App) panGraph(dx, dy int) {
	nextX, nextY := clampPanForModel(a.Model, a.graphBounds(), a.State.PanX+dx, a.State.PanY+dy)
	a.State.PanX = nextX
	a.State.PanY = nextY
	a.State.Focus = FocusGraph
	a.State.TopMenuOpen = false
	a.State.Message = ""
}

func clampPanForModel(m Model, bounds rect, panX, panY int) (int, int) {
	minX, maxX, minY, maxY := panBoundsForModel(m, bounds)
	return clamp(panX, minX, maxX), clamp(panY, minY, maxY)
}

func panBoundsForModel(m Model, bounds rect) (int, int, int, int) {
	if len(m.Nodes) == 0 {
		return 0, 0, 0, 0
	}
	minNodeX, minNodeY := m.Nodes[0].X, m.Nodes[0].Y
	maxNodeX, maxNodeY := m.Nodes[0].X+nodeWidth, m.Nodes[0].Y+nodeHeight
	for _, node := range m.Nodes[1:] {
		minNodeX = min(minNodeX, node.X)
		minNodeY = min(minNodeY, node.Y)
		maxNodeX = max(maxNodeX, node.X+nodeWidth)
		maxNodeY = max(maxNodeY, node.Y+nodeHeight)
	}
	minPanX := min(0, -maxNodeX)
	maxPanX := max(0, bounds.W-minNodeX)
	minPanY := min(0, -maxNodeY)
	maxPanY := max(0, bounds.H-minNodeY)
	return minPanX, maxPanX, minPanY, maxPanY
}
