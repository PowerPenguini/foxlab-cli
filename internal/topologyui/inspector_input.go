package topologyui

import "strings"

func (a *App) handleInspectorKey(key string) bool {
	if a.State.InspectorCapOpen {
		return a.handleInspectorPickerKey(key)
	}
	fields := a.selectedInspectorFields()
	if len(fields) == 0 {
		a.State.Focus = FocusGraph
		return false
	}
	a.State.InspectorSelected = normalizedMenuSelection(a.State.InspectorSelected, len(fields))
	if a.State.InspectorEditing {
		return a.handleInspectorEditKey(key)
	}
	switch key {
	case "up", "char:k", "char:K":
		a.State.InspectorSelected = MoveContextSelection(a.State.InspectorSelected, len(fields), "up")
	case "down", "char:j", "char:J":
		a.State.InspectorSelected = MoveContextSelection(a.State.InspectorSelected, len(fields), "down")
	case "enter", "space", "right":
		a.activateInspectorField(fields[a.State.InspectorSelected])
	case "char:x", "char:X", "delete":
		a.deleteInspectorResource(fields[a.State.InspectorSelected])
	case "char:a", "char:A":
		a.addInspectorDiskLayer(fields[a.State.InspectorSelected])
	case "char:m", "char:M":
		a.mergeInspectorDisk(fields[a.State.InspectorSelected])
	case "char:d", "char:D":
		a.detachInspectorDisk(fields[a.State.InspectorSelected])
	case "left", "escape":
		a.State.Focus = FocusGraph
	case "quit":
		return true
	}
	return false
}

func (a *App) handleInspectorEditKey(key string) bool {
	switch key {
	case "enter":
		a.applyInspectorEdit()
	case "escape":
		a.clearInspectorEdit()
	case "left":
		a.State.InspectorEditCursor = clamp(a.State.InspectorEditCursor-1, 0, runeLen(a.State.InspectorEditValue))
	case "right":
		a.State.InspectorEditCursor = clamp(a.State.InspectorEditCursor+1, 0, runeLen(a.State.InspectorEditValue))
	case "home":
		a.State.InspectorEditCursor = 0
	case "end":
		a.State.InspectorEditCursor = runeLen(a.State.InspectorEditValue)
	case "delete":
		a.State.InspectorEditValue = deleteRuneAt(a.State.InspectorEditValue, a.State.InspectorEditCursor)
	case "backspace":
		if a.State.InspectorEditCursor > 0 {
			a.State.InspectorEditValue = deleteRuneAt(a.State.InspectorEditValue, a.State.InspectorEditCursor-1)
			a.State.InspectorEditCursor--
		}
	case "space":
		a.insertInspectorEditText(" ")
	case "quit":
		return true
	default:
		if strings.HasPrefix(key, "char:") {
			a.insertInspectorEditText(strings.TrimPrefix(key, "char:"))
		}
	}
	return false
}

func (a *App) insertInspectorEditText(value string) {
	runes := []rune(a.State.InspectorEditValue)
	cursor := clamp(a.State.InspectorEditCursor, 0, len(runes))
	insert := []rune(value)
	runes = append(runes[:cursor], append(insert, runes[cursor:]...)...)
	a.State.InspectorEditValue = string(runes)
	a.State.InspectorEditCursor = cursor + len(insert)
}

func (a *App) activateInspectorField(field inspectorField) {
	switch field.kind {
	case inspectorFieldText:
		a.State.InspectorEditing = true
		a.State.InspectorEditValue = field.value
		a.State.InspectorEditCursor = runeLen(field.value)
	case inspectorFieldBool:
		a.applyInspectorField(field, boolString(!strings.EqualFold(field.value, "true")))
	case inspectorFieldCapabilityPicker:
		a.openInspectorCapabilityPicker()
	case inspectorFieldInterfacePicker:
		a.openInspectorInterfacePicker()
	case inspectorFieldChoice:
		if len(field.choices) > 0 {
			next := 0
			for i, choice := range field.choices {
				if choice == field.value {
					next = (i + 1) % len(field.choices)
					break
				}
			}
			a.applyInspectorField(field, field.choices[next])
		}
	case inspectorFieldPower:
		if field.value == "stop" {
			a.stopWorkload(field.nodeType, field.nodeID)
		} else {
			a.runWorkload(field.nodeType, field.nodeID)
		}
	case inspectorFieldShellAction:
		a.startShell(Node{Type: field.nodeType, ID: field.nodeID})
	case inspectorFieldVNCAction:
		a.startVNC(Node{Type: field.nodeType, ID: field.nodeID})
	case inspectorFieldNICAdd:
		a.openAddNICCommand(Node{Type: field.nodeType, ID: field.nodeID})
	case inspectorFieldNIC:
		a.startConnectNICIndex(Node{Type: field.nodeType, ID: field.nodeID}, field.nicIndex)
	case inspectorFieldDiskAdd:
		a.State.InspectorEditing = true
		a.State.InspectorEditValue = a.nextDiskIDForNode("")
		a.State.InspectorEditCursor = runeLen(a.State.InspectorEditValue)
		a.State.InspectorEditAction = "add-disk"
	case inspectorFieldDisk:
		if field.diskAction == diskMenuActionAttach && field.diskID != "" {
			a.diskAttach(field.diskID, map[string]string{"to": diskTargetForNode(Node{Type: field.nodeType, ID: field.nodeID})})
		}
	case inspectorFieldMoveAction:
		a.startMove(Node{Type: field.nodeType, ID: field.nodeID})
	case inspectorFieldDeleteAction:
		switch field.nodeType {
		case NodeVM:
			a.vmDelete(field.nodeID)
		case NodeContainer:
			a.containerDelete(field.nodeID)
		case NodeSwitch:
			a.switchDelete(field.nodeID)
		case NodeExternal:
			a.externalDelete(field.nodeID)
		}
	}
}

func (a *App) applyInspectorEdit() {
	fields := a.selectedInspectorFields()
	if len(fields) == 0 {
		a.clearInspectorEdit()
		return
	}
	selected := normalizedMenuSelection(a.State.InspectorSelected, len(fields))
	field := fields[selected]
	value := a.State.InspectorEditValue
	action := a.State.InspectorEditAction
	target := a.State.InspectorEditTarget
	a.clearInspectorEdit()
	if action == "add-disk" {
		a.createNamedDiskForNode(Node{Type: field.nodeType, ID: field.nodeID}, value)
		return
	}
	if action == "add-layer" {
		a.createNamedLayerForNode(Node{Type: field.nodeType, ID: field.nodeID}, diskMenuEntry{diskID: target}, value)
		return
	}
	a.applyInspectorField(field, value)
}

func (a *App) deleteInspectorResource(field inspectorField) {
	node := Node{Type: field.nodeType, ID: field.nodeID}
	if field.kind == inspectorFieldNIC {
		a.deleteNIC(node, field.nicIndex)
	}
}

func (a *App) addInspectorDiskLayer(field inspectorField) {
	if field.kind != inspectorFieldDisk || field.diskKind != "base" || field.diskAction == diskMenuActionNone || field.diskID == "" {
		return
	}
	a.State.InspectorEditing = true
	a.State.InspectorEditValue = a.nextLayerIDForDisk(field.diskID)
	a.State.InspectorEditCursor = runeLen(a.State.InspectorEditValue)
	a.State.InspectorEditAction = "add-layer"
	a.State.InspectorEditTarget = field.diskID
}

func (a *App) mergeInspectorDisk(field inspectorField) {
	if field.kind != inspectorFieldDisk || field.diskKind != "layer" || field.diskID == "" {
		return
	}
	if field.diskAction == diskMenuActionNone {
		a.mergeDiskForNode(Node{Type: field.nodeType, ID: field.nodeID})
		return
	}
	a.diskMerge(field.diskID)
}

func (a *App) detachInspectorDisk(field inspectorField) {
	if field.kind == inspectorFieldDisk && field.diskAction == diskMenuActionNone {
		a.detachDiskFromNode(Node{Type: field.nodeType, ID: field.nodeID})
	}
}

func (a *App) applyInspectorField(field inspectorField, value string) {
	args := map[string]string{field.id: value}
	switch field.nodeType {
	case NodeVM:
		update, err := vmUpdateRequest(args)
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.vmSet(field.nodeID, update)
	case NodeContainer:
		update, err := containerUpdateRequest(args)
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.containerSet(field.nodeID, update)
	case NodeSwitch:
		a.switchSet(field.nodeID, args)
	case NodeExternal:
		a.externalSet(field.nodeID, args)
	}
}

func (a *App) selectedInspectorFields() []inspectorField {
	node, ok := selectedNode(a.Model, a.State.Selected)
	if !ok {
		return nil
	}
	return a.inspectorFields(node)
}

func (a *App) inspectorFields(node Node) []inspectorField {
	return inspectorFieldsForState(node, a.inspectorRenderState())
}

func (a *App) clearInspectorEdit() {
	a.State.InspectorEditing = false
	a.State.InspectorEditValue = ""
	a.State.InspectorEditCursor = 0
	a.State.InspectorEditAction = ""
	a.State.InspectorEditTarget = ""
}

func (a *App) handleInspectorMouse(event mouseEvent, panel rect) bool {
	node, ok := selectedNode(a.Model, a.State.Selected)
	if !ok {
		return false
	}
	fields := a.inspectorFields(node)
	if actionIndex, actionField, hasAction := inspectorActionButtonAt(fields, panel, event.x, event.y); hasAction {
		if a.State.InspectorEditing {
			a.applyInspectorEdit()
			node, ok = selectedNode(a.Model, a.State.Selected)
			if !ok {
				return false
			}
			fields = a.inspectorFields(node)
			actionIndex, actionField, hasAction = inspectorActionButtonAt(fields, panel, event.x, event.y)
			if !hasAction {
				return false
			}
		}
		if a.State.InspectorCapOpen {
			a.closeInspectorCapabilityPicker()
		}
		a.State.Focus = FocusInspector
		a.State.InspectorSelected = actionIndex
		a.activateInspectorField(actionField)
		return false
	}
	if a.State.InspectorCapOpen {
		if a.handleInspectorPickerMouse(event, panel, node, fields) {
			return false
		}
		if clicked, clickedOK := inspectorFieldAt(panel, a.State, fields, event.y); clickedOK && isInspectorPickerField(fields[clicked]) {
			a.closeInspectorCapabilityPicker()
			a.State.Focus = FocusInspector
			a.State.InspectorSelected = clicked
			return false
		}
		a.closeInspectorCapabilityPicker()
	}
	index, ok := inspectorFieldAt(panel, a.State, fields, event.y)
	if !ok {
		a.State.Focus = FocusInspector
		return false
	}
	if a.State.InspectorEditing {
		if index == a.State.InspectorSelected {
			a.State.Focus = FocusInspector
			return false
		}
		a.applyInspectorEdit()
		node, ok = selectedNode(a.Model, a.State.Selected)
		if !ok {
			return false
		}
		fields = a.inspectorFields(node)
		index, ok = inspectorFieldAt(panel, a.State, fields, event.y)
		if !ok {
			return false
		}
	}
	if event.x >= panel.X+panel.W-5 && fields[index].kind == inspectorFieldNIC {
		a.State.Focus = FocusInspector
		a.State.InspectorSelected = index
		a.deleteInspectorResource(fields[index])
		return false
	}
	if button, hasButton := inspectorDiskAttachButtonRect(fields[index], panel, event.y); hasButton {
		a.State.Focus = FocusInspector
		a.State.InspectorSelected = index
		if xyInRect(event.x, event.y, button) {
			a.activateInspectorField(fields[index])
		}
		return false
	}
	a.State.Focus = FocusInspector
	a.State.InspectorSelected = index
	a.activateInspectorField(fields[index])
	return false
}

func (a *App) handleInspectorPickerKey(key string) bool {
	fields := a.selectedInspectorFields()
	if len(fields) == 0 {
		a.closeInspectorCapabilityPicker()
		return false
	}
	selected := normalizedMenuSelection(a.State.InspectorSelected, len(fields))
	switch fields[selected].kind {
	case inspectorFieldCapabilityPicker:
		return a.handleInspectorCapabilityKey(key)
	case inspectorFieldInterfacePicker:
		return a.handleInspectorInterfaceKey(key)
	default:
		a.closeInspectorCapabilityPicker()
		return false
	}
}

func (a *App) handleInspectorPickerMouse(event mouseEvent, panel rect, node Node, fields []inspectorField) bool {
	if len(fields) == 0 {
		return false
	}
	selected := normalizedMenuSelection(a.State.InspectorSelected, len(fields))
	switch fields[selected].kind {
	case inspectorFieldCapabilityPicker:
		return a.handleInspectorCapabilityMouse(event, panel, node, fields)
	case inspectorFieldInterfacePicker:
		return a.handleInspectorInterfaceMouse(event, panel, node, fields)
	default:
		return false
	}
}

func isInspectorPickerField(field inspectorField) bool {
	return field.kind == inspectorFieldCapabilityPicker || field.kind == inspectorFieldInterfacePicker
}

func inspectorActionButtonAt(fields []inspectorField, panel rect, x, y int) (int, inspectorField, bool) {
	if index, field, ok := inspectorPowerField(fields); ok && xyInRect(x, y, inspectorPowerButtonRect(panel)) {
		return index, field, true
	}
	if index, field, ok := inspectorShellField(fields); ok && xyInRect(x, y, inspectorShellButtonRectForFields(panel, fields)) {
		return index, field, true
	}
	if index, field, ok := inspectorVNCField(fields); ok && xyInRect(x, y, inspectorVNCButtonRect(panel)) {
		return index, field, true
	}
	return 0, inspectorField{}, false
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
