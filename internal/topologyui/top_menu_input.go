package topologyui

func (a *App) handleTopMenuKey(key string) bool {
	rootItems := topRibbonRootItems()
	if len(rootItems) == 0 {
		a.State.Focus = FocusGraph
		return false
	}
	if a.State.TopMenuOpen {
		switch key {
		case "quit":
			return true
		case "tab":
			a.State.Focus = NextFocus(a.State.Focus)
			a.State.TopMenuOpen = false
		case "escape", "left", "right":
			a.State.TopMenuOpen = false
		case "up", "down":
			a.State.TopMenuSelected = MoveContextSelection(a.State.TopMenuSelected, len(topRibbonAddItems()), key)
		case "space", "enter":
			items := topRibbonAddItems()
			actions := topRibbonAddActions()
			if len(items) == 0 || len(items) != len(actions) {
				a.State.TopMenuOpen = false
				return false
			}
			selected := normalizedMenuSelection(a.State.TopMenuSelected, len(items))
			a.State.closeContextMenu()
			a.runGlobalMenuAction(actions[selected])
			a.State.TopMenuOpen = false
		}
		return false
	}
	switch key {
	case "quit":
		return true
	case "tab":
		a.State.Focus = NextFocus(a.State.Focus)
	case "left":
		a.State.TopMenuRootSelected = (normalizedMenuSelection(a.State.TopMenuRootSelected, len(rootItems)) - 1 + len(rootItems)) % len(rootItems)
	case "right":
		a.State.TopMenuRootSelected = (normalizedMenuSelection(a.State.TopMenuRootSelected, len(rootItems)) + 1) % len(rootItems)
	case "escape":
		a.State.Focus = FocusGraph
	case "space", "enter", "down":
		selected := normalizedMenuSelection(a.State.TopMenuRootSelected, len(rootItems))
		action := contextMenuAction(rootItems[selected])
		if action == "exit" {
			return true
		}
		a.State.TopMenuOpen = true
		a.State.TopMenuSelected = 0
	}
	return false
}
