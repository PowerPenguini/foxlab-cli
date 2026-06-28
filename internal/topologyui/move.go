package topologyui

import "foxlab-cli/internal/lab"

func (a *App) startMove(node Node) {
	a.State.MoveMode = true
	a.State.MoveNodeID = node.ID
	a.State.MoveNodeType = node.Type
	a.State.MoveStartX = node.X
	a.State.MoveStartY = node.Y
	a.State.Message = "move " + node.Key()
}

func (a *App) handleMoveKey(key string) bool {
	switch key {
	case "quit":
		return true
	case "left":
		a.moveActiveNode(-1, 0)
	case "right":
		a.moveActiveNode(1, 0)
	case "up":
		a.moveActiveNode(0, -1)
	case "down":
		a.moveActiveNode(0, 1)
	case "enter":
		a.saveActiveMove()
	case "escape":
		a.cancelActiveMove()
	}
	return false
}

func (a *App) moveActiveNode(dx, dy int) {
	index, ok := a.moveNodeIndex()
	if !ok {
		a.clearMoveMode()
		return
	}
	maxX, maxY := a.moveBounds()
	a.Model.Nodes[index].X = clamp(a.Model.Nodes[index].X+dx, 0, maxX)
	a.Model.Nodes[index].Y = clamp(a.Model.Nodes[index].Y+dy, 0, maxY)
	a.State.Selected = index
	a.State.Message = "move " + a.Model.Nodes[index].Key()
}

func (a *App) moveBounds() (int, int) {
	width := a.ViewWidth
	height := a.ViewHeight
	if width <= 0 {
		width = minWidth
	}
	if height <= 0 {
		height = minHeight
	}
	return max(0, width-nodeWidth), max(0, height-nodeHeight-1)
}

func (a *App) saveActiveMove() {
	index, ok := a.moveNodeIndex()
	if !ok {
		a.clearMoveMode()
		return
	}
	node := a.Model.Nodes[index]
	if a.Lab != nil {
		snapshot := lab.Clone(a.Lab)
		if a.Lab.Layout.Nodes == nil {
			a.Lab.Layout.Nodes = map[string]lab.Position{}
		}
		a.Lab.Layout.Nodes[node.ID] = lab.Position{X: node.X * 16, Y: node.Y * 24}
		if err := a.saveAndRefresh(); err != nil {
			a.Lab = snapshot
			if a.Service != nil {
				a.Service.Lab = snapshot
			}
			a.State.Message = "move failed: " + err.Error()
			return
		}
	}
	a.clearMoveMode()
	a.State.Message = "moved " + node.Key()
}

func (a *App) cancelActiveMove() {
	if index, ok := a.moveNodeIndex(); ok {
		a.Model.Nodes[index].X = a.State.MoveStartX
		a.Model.Nodes[index].Y = a.State.MoveStartY
		a.State.Selected = index
	}
	a.clearMoveMode()
	a.State.Message = ""
}

func (a *App) moveNodeIndex() (int, bool) {
	for i, node := range a.Model.Nodes {
		if node.ID == a.State.MoveNodeID && node.Type == a.State.MoveNodeType {
			return i, true
		}
	}
	return 0, false
}

func (a *App) clearMoveMode() {
	a.State.MoveMode = false
	a.State.MoveNodeID = ""
	a.State.MoveNodeType = ""
	a.State.MoveStartX = 0
	a.State.MoveStartY = 0
}
