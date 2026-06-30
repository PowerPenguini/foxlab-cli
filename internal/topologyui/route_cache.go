package topologyui

import (
	"strconv"
	"strings"
)

func renderRouteCacheKey(m Model, width, height, panX, panY int) string {
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
	b.WriteString("pan=")
	b.WriteString(strconv.Itoa(panX))
	b.WriteByte(',')
	b.WriteString(strconv.Itoa(panY))
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
