package topologyui

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
	if current == FocusTop {
		return FocusGraph
	}
	if current == FocusGraph {
		return FocusTop
	}
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
	bestAligned := false
	bestPrimary := 0
	bestSecondary := 0
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
		aligned := secondary <= visualNeighborAlignmentTolerance(direction) && primary >= secondary
		if best == -1 ||
			(aligned && !bestAligned) ||
			(aligned == bestAligned && primary < bestPrimary) ||
			(aligned == bestAligned && primary == bestPrimary && secondary < bestSecondary) {
			best = i
			bestAligned = aligned
			bestPrimary = primary
			bestSecondary = secondary
		}
	}
	return best, best != -1
}

func visualNeighborAlignmentTolerance(direction string) int {
	switch direction {
	case "left", "right":
		return nodeHeight
	case "up", "down":
		return nodeWidth
	default:
		return 0
	}
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
