package topologyui

import "strings"

func (a *App) handleKey(key string) bool {
	if isMouseKey(key) {
		return a.handleMouseKey(key)
	}
	if !a.State.PaletteOpen && key == "char::" {
		a.togglePalette()
		return false
	}
	if a.State.PaletteOpen {
		return a.handlePaletteKey(key)
	}
	if a.State.DiskExplorerOpen {
		return a.handleDiskExplorerKey(key)
	}
	if key == "tab" {
		return a.handleTabKey()
	}
	if a.State.ConnectTargetMenu {
		return a.handleConnectTargetMenuKey(key)
	}
	if a.State.ContextMenu {
		return a.handleContextMenuKey(key)
	}
	if a.State.ConnectMode {
		return a.handleConnectKey(key)
	}
	if a.State.MoveMode {
		return a.handleMoveKey(key)
	}
	if a.State.Focus == FocusTop {
		return a.handleTopMenuKey(key)
	}
	if a.State.Focus == FocusInspector {
		return a.handleInspectorKey(key)
	}
	switch key {
	case "quit":
		return true
	case "shift-left":
		a.panGraph(keyboardPanStepX, 0)
	case "shift-right":
		a.panGraph(-keyboardPanStepX, 0)
	case "shift-up":
		a.panGraph(0, keyboardPanStepY)
	case "shift-down":
		a.panGraph(0, -keyboardPanStepY)
	case "down", "up", "left", "right", "char:j", "char:k", "char:h", "char:l":
		selected := MoveSelection(a.Model, a.State.Selected, navigationDirection(key))
		if selected != a.State.Selected {
			a.clearInspectorEdit()
			a.closeInspectorCapabilityPicker()
			a.State.InspectorSelected = 0
		}
		a.State.Selected = selected
	case "tab":
		a.State.Focus = NextFocus(a.State.Focus)
	case "char:m":
		if node, ok := selectedNode(a.Model, a.State.Selected); ok {
			a.startMove(node)
		}
	}
	return false
}

func isMouseKey(key string) bool {
	return strings.HasPrefix(key, "mouse:") ||
		strings.HasPrefix(key, "mouse-drag:") ||
		strings.HasPrefix(key, "mouse-release:")
}

func navigationDirection(key string) string {
	switch key {
	case "char:j":
		return "down"
	case "char:k":
		return "up"
	case "char:h":
		return "left"
	case "char:l":
		return "right"
	default:
		return key
	}
}

func (a *App) handleTabKey() bool {
	if a.State.MoveMode || a.State.ConnectMode {
		return false
	}
	a.State.TopMenuOpen = false
	if a.State.ContextMenu {
		a.State.closeContextMenu()
	}
	if a.State.InspectorEditing {
		a.applyInspectorEdit()
	}
	if a.State.InspectorCapOpen {
		a.closeInspectorCapabilityPicker()
	}
	if inspectorBounds(a.ViewWidth, a.contentHeight()).W > 0 {
		switch a.State.Focus {
		case FocusGraph:
			a.State.Focus = FocusInspector
		case FocusInspector:
			a.State.Focus = FocusGraph
		default:
			a.State.Focus = FocusGraph
		}
	} else {
		a.State.Focus = FocusGraph
	}
	return false
}

func (a *App) handleCommandKey(key string) bool {
	return false
}
