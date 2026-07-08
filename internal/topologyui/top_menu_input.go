package topologyui

func (a *App) handleTopMenuKey(key string) bool {
	switch key {
	case "quit":
		return true
	case "tab", "escape", "left", "right", "up", "down", "space", "enter":
		a.State.Focus = FocusGraph
	}
	a.State.TopMenuOpen = false
	return false
}
