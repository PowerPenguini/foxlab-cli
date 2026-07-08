package topologyui

import (
	"strconv"
	"strings"
)

func drawInspector(g *grid, m Model, state ViewState, panel rect) {
	if panel.W <= 0 || panel.H <= 0 {
		return
	}
	fillRect(g, panel, themePanelInspector)
	drawInspectorBrand(g, panel)
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
	y += 3
	y = drawInspectorSection(g, x, y, w, "State", []inspectorKV{
		{Key: "state", Value: displayNodeState(node.State, state.AnimationFrame), Style: themePanelInspector + nodeStateStyle(node.Type, node.State)},
	})
	y = drawInspectorSection(g, x, y, w, "Identity", []inspectorKV{
		{Key: "id", Value: node.ID, Style: themePanelInspectorMuted},
	})
	if node.DesiredState != "" {
		y = drawInspectorSection(g, x, y, w, "Desired", []inspectorKV{
			{Key: "want", Value: node.DesiredState, Style: themePanelInspectorMuted},
		})
	}
	details := inspectorLines(node)
	if len(details) > 0 {
		kvs := make([]inspectorKV, 0, len(details))
		for _, line := range details {
			key, value := inspectorLineParts(line)
			kvs = append(kvs, inspectorKV{Key: key, Value: value, Style: inspectorLineStyle(line)})
		}
		drawInspectorSection(g, x, y, w, "Configuration", kvs)
	}
}

func drawInspectorBrand(g *grid, panel rect) {
	const brand = "// FoxLab"
	if panel.W < runeLen(brand)+2 || panel.H <= 0 {
		return
	}
	x := panel.X + 2
	y := panel.Y + panel.H - 1
	g.Text(x, y, "// ", themePanelInspectorMuted)
	g.Text(x+3, y, "Fox", themePanelInspector+ansiOrange+ansiBold)
	g.Text(x+6, y, "Lab", themePanelInspector+ansiWhite+ansiBold)
}

func drawInspectorHeader(g *grid, node Node, x, y, width int) {
	if width <= 0 {
		return
	}
	badge := "[" + firstNonEmpty(node.Badge, NodeKind(node.Type)) + "]"
	name := firstNonEmpty(node.Label, node.ID)
	if runeLen(badge) >= width {
		g.Text(x, y, fit(badge, width), themePanelInspectorHeader+nodeBadgeStyle(node.Type))
		return
	}
	g.Text(x, y, badge, themePanelInspectorHeader+nodeBadgeStyle(node.Type))
	nameWidth := width - runeLen(badge) - 1
	if nameWidth <= 0 {
		return
	}
	g.Text(x+runeLen(badge)+1, y, fit(name, nameWidth), themePanelInspectorHeader+nodeLabelStyle(node.Type))
}

type inspectorKV struct {
	Key   string
	Value string
	Style string
}

func drawInspectorSection(g *grid, x, y, width int, title string, rows []inspectorKV) int {
	if width <= 0 || len(rows) == 0 {
		return y
	}
	g.Text(x, y, fit(title, width), themePanelInspectorHeader)
	y++
	for _, row := range rows {
		if row.Key == "" && row.Value == "" {
			continue
		}
		key := fit(row.Key, min(10, max(4, width/3)))
		keyW := runeLen(key)
		g.Text(x, y, key, themePanelInspectorMuted)
		if width-keyW-1 > 0 {
			g.Text(x+keyW+1, y, fit(row.Value, width-keyW-1), row.Style)
		}
		y++
	}
	return y + 1
}

func inspectorLines(node Node) []string {
	switch node.Type {
	case NodeVM:
		return compactDetailLines(node, []string{"cpu", "mem", "vnc", "disk", "iso"}, 7)
	case NodeContainer:
		return compactDetailLines(node, []string{"image", "command", "disk"}, 7)
	case NodeSwitch:
		links := len(nicDetails(node.Details))
		configNode := node
		configNode.Details = switchConfigurationDetails(node.Details)
		lines := compactDetailLines(configNode, []string{"mode"}, 5)
		lines = append(lines, "links  "+strconv.Itoa(links))
		return lines
	case NodeExternal:
		return compactDetailLines(node, []string{"interface", "mode"}, 5)
	default:
		return compactDetailLines(node, nil, 7)
	}
}

func inspectorLineParts(line string) (string, string) {
	line = strings.TrimSpace(line)
	key, value, ok := strings.Cut(line, " ")
	if !ok {
		return line, ""
	}
	return strings.TrimSpace(key), strings.TrimSpace(value)
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
		return themePanelInspectorMuted
	}
	return themePanelInspector
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
