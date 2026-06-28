package topologyui

import (
	"strconv"
	"strings"
)

func drawInspector(g *grid, m Model, state ViewState, panel rect) {
	if panel.W <= 0 || panel.H <= 0 {
		return
	}
	clearRect(g, panel)
	drawPanelBox(g, panel, themeRoute)
	node, ok := selectedNode(m, state.Selected)
	if !ok {
		return
	}
	x := panel.X + 2
	y := panel.Y + 1
	w := panel.W - 4
	if w <= 0 {
		return
	}
	drawInspectorHeader(g, node, x, y, w)
	y += 2
	g.Text(x, y, fit("state  "+displayNodeState(node.State, state.AnimationFrame), w), stateStyle(node.State))
	y++
	if node.DesiredState != "" {
		g.Text(x, y, fit("want   "+node.DesiredState, w), themeMuted)
		y++
	}
	for _, line := range inspectorLines(node) {
		if y >= panel.Y+panel.H-1 {
			return
		}
		g.Text(x, y, fit(line, w), inspectorLineStyle(line))
		y++
	}
	if y < panel.Y+panel.H-2 {
		y++
		g.Text(x, y, fit("actions Space menu  m move", w), themeMuted)
	}
}

func drawInspectorHeader(g *grid, node Node, x, y, width int) {
	if width <= 0 {
		return
	}
	badge := "[" + NodeKind(node.Type) + "]"
	name := firstNonEmpty(node.Label, node.ID)
	if runeLen(badge) >= width {
		g.Text(x, y, fit(badge, width), nodeBadgeStyle(node.Type))
		return
	}
	g.Text(x, y, badge, nodeBadgeStyle(node.Type))
	nameWidth := width - runeLen(badge) - 1
	if nameWidth <= 0 {
		return
	}
	g.Text(x+runeLen(badge)+1, y, fit(name, nameWidth), nodeLabelStyle(node.Type))
}

func inspectorLines(node Node) []string {
	switch node.Type {
	case NodeVM:
		return compactDetailLines(node, []string{"cpu", "mem", "vnc", "disk", "iso"}, 7)
	case NodeContainer:
		return compactDetailLines(node, []string{"image", "command", "disk"}, 7)
	case NodeSwitch:
		lines := compactDetailLines(node, []string{"mode", "uplink", "external"}, 5)
		lines = append(lines, "links  "+strconv.Itoa(len(nicDetails(node.Details))))
		return lines
	case NodeExternal:
		return compactDetailLines(node, []string{"interface", "mode"}, 5)
	default:
		return compactDetailLines(node, nil, 7)
	}
}

func compactDetailLines(node Node, keys []string, limit int) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, key := range keys {
		if value := nodeDetailRawValue(node, key); value != "" {
			out = append(out, detailLine(key, value))
			seen[key] = struct{}{}
		}
	}
	for _, detail := range node.Details {
		key, value, ok := strings.Cut(detail, "=")
		if !ok || value == "" {
			continue
		}
		if _, ok := seen[key]; ok || isRuntimeDetail(detail) {
			continue
		}
		out = append(out, detailLine(key, value))
		seen[key] = struct{}{}
		if len(out) >= limit {
			break
		}
	}
	if len(out) > limit {
		return out[:limit]
	}
	return out
}

func detailLine(key, value string) string {
	label := key
	switch key {
	case "cpu", "cpus":
		label = "cpu"
	case "mem", "memory":
		label = "mem"
	}
	if runeLen(label) < 6 {
		label += strings.Repeat(" ", 6-runeLen(label))
	}
	return label + " " + shortDetailValue(key, value)
}

func inspectorLineStyle(line string) string {
	if strings.HasPrefix(strings.TrimSpace(line), "links") {
		return themeMuted
	}
	return ""
}

func drawPanelBox(g *grid, r rect, style string) {
	if r.W < 2 || r.H < 2 {
		return
	}
	for x := r.X + 1; x < r.X+r.W-1; x++ {
		g.Set(x, r.Y, lineHorizontal, style)
		g.Set(x, r.Y+r.H-1, lineHorizontal, style)
	}
	for y := r.Y + 1; y < r.Y+r.H-1; y++ {
		g.Set(r.X, y, lineVertical, style)
		g.Set(r.X+r.W-1, y, lineVertical, style)
	}
	g.Set(r.X, r.Y, '╭', style)
	g.Set(r.X+r.W-1, r.Y, '╮', style)
	g.Set(r.X, r.Y+r.H-1, '╰', style)
	g.Set(r.X+r.W-1, r.Y+r.H-1, '╯', style)
}
