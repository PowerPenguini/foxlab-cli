package topologyui

import "strings"

func topRibbonRootItems() []string {
	return []string{"add >", "exit"}
}

func topRibbonAddItems() []string {
	return []string{"VM", "Container", "Switch", "Disk", "Link"}
}

func topRibbonAddActions() []string {
	return []string{"add vm", "add cont", "add sw", "add disk", "link"}
}

func topMenuLabel(item string) string {
	label := strings.TrimSpace(item)
	label = strings.TrimSpace(strings.TrimSuffix(label, ">"))
	switch label {
	case "add":
		return "Add"
	case "add vm":
		return "Add VM"
	case "add cont":
		return "Add CT"
	case "add sw":
		return "Add SW"
	case "add disk":
		return "Add Disk"
	case "create external":
		return "External"
	case "exit":
		return "Exit"
	default:
		return label
	}
}

func topMenuButtonRects(items []string, width int) []rect {
	if width <= 0 || len(items) == 0 {
		return nil
	}
	rects := make([]rect, 0, len(items))
	x := 0
	for _, item := range items {
		w := runeLen(topMenuLabel(item)) + 2
		if w <= 0 || x+w > width {
			break
		}
		rects = append(rects, rect{X: x, Y: 0, W: w, H: 1})
		x += w
	}
	return rects
}
