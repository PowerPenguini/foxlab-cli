package topologytui

import (
	"strings"

	"golang.org/x/sys/unix"
)

func readAppKey(a *App) (string, error) {
	return readKey(int(a.In.Fd()), a.State.CommandMode || a.State.ContextEdit)
}

func readKey(fd int, commandMode bool) (string, error) {
	var buf [16]byte
	n, err := unix.Read(fd, buf[:])
	if err != nil {
		if err == unix.EINTR {
			return "", nil
		}
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	if buf[0] == '\x1b' && n < 6 {
		m, err := unix.Read(fd, buf[n:])
		if err != nil && err != unix.EINTR && err != unix.EAGAIN {
			return "", err
		}
		n += m
	}
	seq := string(buf[:n])
	switch {
	case seq == "\x03":
		return "quit", nil
	case seq == "j":
		if commandMode {
			return "char:j", nil
		}
		return "down", nil
	case seq == "\x1b[B":
		return "down", nil
	case seq == "k":
		if commandMode {
			return "char:k", nil
		}
		return "up", nil
	case seq == "\x1b[A":
		return "up", nil
	case seq == "h":
		if commandMode {
			return "char:h", nil
		}
		return "left", nil
	case seq == "\x1b[D":
		return "left", nil
	case seq == "l":
		if commandMode {
			return "char:l", nil
		}
		return "right", nil
	case seq == "\x1b[C":
		return "right", nil
	case seq == "\x1b[H" || seq == "\x1b[1~":
		return "home", nil
	case seq == "\x1b[F" || seq == "\x1b[4~":
		return "end", nil
	case seq == "\x1b[3~":
		return "delete", nil
	case seq == "\t":
		return "tab", nil
	case seq == " ":
		if commandMode {
			return "char: ", nil
		}
		return "space", nil
	case seq == "\r" || seq == "\n":
		return "enter", nil
	case seq == "\x7f" || seq == "\b":
		return "backspace", nil
	case seq == "\x1b":
		return "escape", nil
	case strings.HasPrefix(seq, "\x1b"):
		return "", nil
	case len([]rune(seq)) == 1 && seq[0] >= 0x20 && seq[0] <= 0x7e:
		return "char:" + seq, nil
	default:
		return "", nil
	}
}

func (a *App) handleKey(key string) bool {
	if a.State.CommandMode {
		return a.handleCommandKey(key)
	}
	if a.State.ContextMenu {
		return a.handleContextMenuKey(key)
	}
	if a.State.MoveMode {
		return a.handleMoveKey(key)
	}
	switch key {
	case "quit":
		return true
	case "down", "up", "left", "right":
		a.State.Selected = MoveSelection(a.Model, a.State.Selected, key)
	case "tab":
		a.State.Focus = NextFocus(a.State.Focus)
	case "char::":
		a.openCommand("")
	case "char:m":
		if node, ok := a.Model.selected(a.State.Selected); ok {
			a.startMove(node)
		}
	case "space":
		if a.State.Focus == FocusGraph {
			a.State.ContextMenu = true
			a.State.ContextGroup = ""
			a.State.ContextInSubmenu = false
			a.State.ContextSelected = 0
		}
	}
	return false
}

func (a *App) handleContextMenuKey(key string) bool {
	node, ok := a.Model.selected(a.State.Selected)
	rootItems := a.contextMenuRootItems(node, ok)
	subItems := a.contextMenuSubmenuItems(node, ok)
	if a.State.ContextEdit {
		return a.handleContextEditKey(key, node, ok, subItems)
	}
	switch key {
	case "up", "down":
		if a.State.ContextInSubmenu {
			a.State.ContextSubSelected = MoveContextSelection(a.State.ContextSubSelected, len(subItems), key)
		} else {
			oldGroup := activeRootContextGroup(rootItems, a.State.ContextSelected)
			a.State.ContextSelected = MoveContextSelection(a.State.ContextSelected, len(rootItems), key)
			newGroup := activeRootContextGroup(rootItems, a.State.ContextSelected)
			a.State.ContextGroup = newGroup
			if oldGroup != newGroup {
				a.State.ContextSubSelected = 0
			}
		}
	case "space", "escape", "tab":
		a.State.ContextMenu = false
		a.State.ContextGroup = ""
		a.State.ContextInSubmenu = false
		a.State.ContextEdit = false
		a.State.ContextEditValue = ""
		a.State.ContextEditCursor = 0
	case "enter":
		if a.State.ContextInSubmenu {
			selected := normalizedMenuSelection(a.State.ContextSubSelected, len(subItems))
			if len(subItems) > 0 {
				action := contextMenuAction(subItems[selected])
				if isContextGroup(action) {
					a.State.ContextGroup = action
					a.State.ContextSubSelected = 0
					return false
				}
				if ok && isBoolContextItem(subItems[selected]) {
					a.applyContextEdit(node, subItems[selected], toggledBoolValue(contextItemValue(subItems[selected])))
					return false
				}
				if ok && isEditableContextItem(subItems[selected]) {
					a.State.ContextEdit = true
					a.State.ContextEditValue = contextItemValue(subItems[selected])
					a.State.ContextEditCursor = runeLen(a.State.ContextEditValue)
					return false
				}
				if ok {
					a.runMenuAction(node, action)
				} else {
					a.runGlobalMenuAction(action)
				}
				a.State.ContextMenu = false
				a.State.ContextGroup = ""
				a.State.ContextInSubmenu = false
				return false
			}
		} else {
			selected := normalizedMenuSelection(a.State.ContextSelected, len(rootItems))
			if len(rootItems) > 0 {
				action := contextMenuAction(rootItems[selected])
				if isContextGroup(action) {
					a.State.ContextGroup = action
					a.State.ContextInSubmenu = true
					a.State.ContextSubSelected = 0
					return false
				}
				if ok {
					a.runMenuAction(node, action)
				} else {
					a.runGlobalMenuAction(action)
				}
				a.State.ContextMenu = false
				a.State.ContextGroup = ""
				a.State.ContextInSubmenu = false
			} else {
				a.State.ContextMenu = false
				a.State.ContextGroup = ""
				a.State.ContextInSubmenu = false
			}
		}
	case "left", "right":
		if key == "left" && a.State.ContextInSubmenu {
			a.State.ContextInSubmenu = false
			a.State.ContextGroup = ""
			a.State.ContextSubSelected = 0
			return false
		}
		if key == "right" && !a.State.ContextInSubmenu {
			a.State.ContextGroup = activeRootContextGroup(rootItems, a.State.ContextSelected)
			if a.State.ContextGroup == "" {
				return false
			}
			a.State.ContextInSubmenu = true
			return false
		}
		a.State.ContextMenu = false
		a.State.ContextGroup = ""
		a.State.ContextInSubmenu = false
		a.State.ContextSubSelected = 0
		a.State.ContextEdit = false
		a.State.ContextEditValue = ""
		a.State.ContextEditCursor = 0
		if ok {
			a.State.Selected = MoveSelection(a.Model, a.State.Selected, key)
		}
	}
	return false
}

func (a *App) handleCommandKey(key string) bool {
	switch {
	case key == "enter":
		command := strings.TrimSpace(a.State.Command)
		a.State.Command = ""
		a.State.CommandMode = false
		a.rememberCommand(command)
		return a.executeCommand(command)
	case key == "escape":
		a.State.Command = ""
		a.State.CommandMode = false
	case key == "backspace":
		if a.State.Command != "" {
			runes := []rune(a.State.Command)
			a.State.Command = string(runes[:len(runes)-1])
		}
	case key == "up":
		a.recallCommand(-1)
	case key == "down":
		a.recallCommand(1)
	case strings.HasPrefix(key, "char:"):
		a.State.Command += strings.TrimPrefix(key, "char:")
	}
	return false
}
