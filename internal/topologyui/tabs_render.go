package topologyui

import "strings"

func (a *App) drawTabBar(g *grid) {
	if a.tabs == nil || g == nil || g.Width <= 0 || g.Height <= 0 {
		return
	}
	a.tabs.mu.Lock()
	defer a.tabs.mu.Unlock()
	for x := 0; x < g.Width; x++ {
		g.Set(x, 0, ' ', themeChrome)
	}
	labels := make([]string, len(a.tabs.tabs))
	for index, tab := range a.tabs.tabs {
		marker := ""
		if isShellTabKind(tab.kind) {
			switch tab.status {
			case tabStatusStarting:
				marker = "◌ "
			case tabStatusRunning:
				marker = "● "
			case tabStatusExited:
				marker = "! "
			}
			if tab.unread {
				marker = "• "
			}
			labels[index] = " " + marker + tab.label + " × "
		} else {
			labels[index] = " " + tab.label + " × "
		}
	}
	a.tabs.offset = tabOffsetForActive(labels, a.tabs.offset, a.tabs.active, g.Width)
	a.tabs.hits = nil
	x := 0
	if a.tabs.offset > 0 {
		g.Set(0, 0, '‹', themeChromeMuted)
		x = 1
	}
	for index := a.tabs.offset; index < len(labels); index++ {
		label := labels[index]
		remaining := g.Width - x
		if remaining <= 0 {
			break
		}
		if runeLen(label) > remaining {
			if index == a.tabs.offset && remaining > 1 {
				label = fit(label, remaining-1)
			} else {
				g.Set(g.Width-1, 0, '›', themeChromeMuted)
				break
			}
		}
		style := themeChrome
		if index == a.tabs.active {
			style = themeChromeActive
		} else if isShellTabKind(a.tabs.tabs[index].kind) && a.tabs.tabs[index].status == tabStatusExited {
			style = ansiBgPanelTop + ansiRed
		}
		g.Text(x, 0, label, style)
		w := runeLen(label)
		closeX := -1
		if strings.HasSuffix(label, "× ") {
			closeX = x + w - 2
		}
		a.tabs.hits = append(a.tabs.hits, tabHit{index: index, bounds: rect{X: x, Y: 0, W: w, H: 1}, closeX: closeX})
		x += w
		if x < g.Width {
			g.Set(x, 0, '│', themeChromeMuted)
			x++
		}
	}
}

func tabOffsetForActive(labels []string, offset, active, width int) int {
	if len(labels) == 0 || width <= 0 {
		return 0
	}
	offset = clamp(offset, 0, len(labels)-1)
	active = clamp(active, 0, len(labels)-1)
	if active < offset {
		offset = active
	}
	for offset < active {
		used := 0
		if offset > 0 {
			used++
		}
		for index := offset; index <= active; index++ {
			used += runeLen(labels[index])
			if index < active {
				used++
			}
		}
		if used <= width {
			break
		}
		offset++
	}
	return offset
}
