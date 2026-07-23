package topologyui

import (
	"fmt"
	"strconv"
	"strings"

	"foxlab-cli/internal/lab"
	"foxlab-cli/internal/topology"
)

const (
	diskExplorerTabKey       = "disks"
	diskExplorerActionCreate = "create"
	diskExplorerActionImport = "import"
	diskExplorerActionLayer  = "layer"
	diskExplorerActionRename = "rename"
	diskExplorerActionResize = "resize"
	diskExplorerActionMerge  = "merge"
	diskExplorerActionDelete = "delete"
)

type diskExplorerRow struct {
	Disk    lab.Disk
	Depth   int
	Missing bool
}

func (a *App) openDiskExplorer() {
	a.ensureTabs()
	a.State.openOverlay(overlayNone)
	a.tabs.mu.Lock()
	for index, tab := range a.tabs.tabs {
		if tab.key == diskExplorerTabKey {
			a.tabs.active = index
			tab.unread = false
			a.tabs.gPrefix = false
			a.tabs.mu.Unlock()
			a.syncActiveTabView(tabKindDisks)
			a.clampDiskExplorerSelection()
			a.ensureDiskExplorerSelectionVisible()
			a.tabs.notify()
			return
		}
	}
	tab := &appTab{key: diskExplorerTabKey, kind: tabKindDisks, label: "Disks", status: tabStatusRunning}
	a.tabs.tabs = append(a.tabs.tabs, tab)
	a.tabs.active = len(a.tabs.tabs) - 1
	a.tabs.gPrefix = false
	a.tabs.mu.Unlock()
	a.syncActiveTabView(tabKindDisks)
	a.State.DiskExplorerEdit = ""
	a.State.DiskExplorerEditValue = ""
	a.State.DiskExplorerEditCursor = 0
	a.clampDiskExplorerSelection()
	a.tabs.notify()
}

func (a *App) closeDiskExplorer() {
	a.State.DiskExplorerOpen = false
	a.clearDiskExplorerEdit()
	if a.tabs == nil {
		return
	}
	a.tabs.mu.Lock()
	index := -1
	for candidate, tab := range a.tabs.tabs {
		if tab.key == diskExplorerTabKey {
			index = candidate
			break
		}
	}
	a.tabs.mu.Unlock()
	if index >= 0 {
		a.closeTab(index)
	}
}

func (a *App) clearDiskExplorerEdit() {
	a.State.DiskExplorerEdit = ""
	a.State.DiskExplorerEditValue = ""
	a.State.DiskExplorerEditCursor = 0
	a.State.DiskImportPath = ""
	a.State.DiskImportPathEditing = false
	a.State.DiskImportSelected = 0
	a.State.DiskImportScroll = 0
	a.State.DiskImportEntries = nil
	a.State.DiskImportError = ""
}

func (a *App) diskExplorerRows() []diskExplorerRow {
	if a.currentLab() == nil {
		return nil
	}
	layerRows := map[string][]lab.Disk{}
	seen := map[string]bool{}
	baseOrder := []lab.Disk{}
	otherRows := []lab.Disk{}
	for _, disk := range a.currentLab().Disks {
		switch diskKindUI(disk) {
		case "layer":
			layerRows[disk.Base] = append(layerRows[disk.Base], disk)
		case "base":
			baseOrder = append(baseOrder, disk)
		default:
			otherRows = append(otherRows, disk)
		}
	}
	rows := make([]diskExplorerRow, 0, len(a.currentLab().Disks))
	for _, disk := range baseOrder {
		rows = append(rows, diskExplorerRow{Disk: disk})
		seen[disk.ID] = true
		for _, layer := range layerRows[disk.ID] {
			rows = append(rows, diskExplorerRow{Disk: layer, Depth: 1})
			seen[layer.ID] = true
		}
	}
	for _, disk := range otherRows {
		rows = append(rows, diskExplorerRow{Disk: disk})
		seen[disk.ID] = true
	}
	for baseID, layers := range layerRows {
		if baseID == "" || seen[baseID] {
			continue
		}
		for _, layer := range layers {
			rows = append(rows, diskExplorerRow{Disk: layer, Depth: 1, Missing: true})
			seen[layer.ID] = true
		}
	}
	return rows
}

func (a *App) selectedDiskExplorerRow() (diskExplorerRow, bool) {
	rows := a.diskExplorerRows()
	if len(rows) == 0 {
		return diskExplorerRow{}, false
	}
	a.clampDiskExplorerSelection()
	return rows[normalizedMenuSelection(a.State.DiskExplorerSelected, len(rows))], true
}

func (a *App) clampDiskExplorerSelection() {
	rows := a.diskExplorerRows()
	if len(rows) == 0 {
		a.State.DiskExplorerSelected = 0
		a.State.DiskExplorerScroll = 0
		return
	}
	a.State.DiskExplorerSelected = clamp(a.State.DiskExplorerSelected, 0, len(rows)-1)
	if a.State.DiskExplorerScroll > a.State.DiskExplorerSelected {
		a.State.DiskExplorerScroll = a.State.DiskExplorerSelected
	}
	if a.State.DiskExplorerScroll < 0 {
		a.State.DiskExplorerScroll = 0
	}
}

func (a *App) handleDiskExplorerKey(key string) bool {
	if a.State.DiskExplorerEdit == diskExplorerActionImport {
		return a.handleDiskImportBrowserKey(key)
	}
	if a.State.DiskExplorerEdit != "" {
		a.handleDiskExplorerEditKey(key)
		return false
	}
	rows := a.diskExplorerRows()
	switch key {
	case "quit":
		return true
	case "space":
		a.closeDiskExplorer()
	case "tab":
		a.closeDiskExplorer()
		a.State.Focus = NextFocus(a.State.Focus)
	case "up", "char:k", "char:K":
		a.State.DiskExplorerSelected = max(0, a.State.DiskExplorerSelected-1)
	case "down", "char:j", "char:J":
		a.State.DiskExplorerSelected = min(max(0, len(rows)-1), a.State.DiskExplorerSelected+1)
	case "char:n", "char:N":
		a.runDiskExplorerAction(diskExplorerActionCreate)
	case "char:i", "char:I":
		a.runDiskExplorerAction(diskExplorerActionImport)
	case "char:l", "char:L":
		a.runDiskExplorerAction(diskExplorerActionLayer)
	case "char:e", "char:E":
		a.runDiskExplorerAction(diskExplorerActionRename)
	case "char:r", "char:R":
		a.runDiskExplorerAction(diskExplorerActionResize)
	case "char:m", "char:M":
		a.runDiskExplorerAction(diskExplorerActionMerge)
	case "char:x", "char:X", "delete":
		a.runDiskExplorerAction(diskExplorerActionDelete)
	}
	a.clampDiskExplorerSelection()
	a.ensureDiskExplorerSelectionVisible()
	return false
}

func (a *App) handleDiskExplorerEditKey(key string) {
	switch key {
	case "escape":
		a.clearDiskExplorerEdit()
	case "enter":
		a.commitDiskExplorerEdit()
	case "backspace":
		if a.State.DiskExplorerEditCursor > 0 {
			runes := []rune(a.State.DiskExplorerEditValue)
			cursor := a.State.DiskExplorerEditCursor
			runes = append(runes[:cursor-1], runes[cursor:]...)
			a.State.DiskExplorerEditValue = string(runes)
			a.State.DiskExplorerEditCursor--
		}
	case "left":
		a.State.DiskExplorerEditCursor = max(0, a.State.DiskExplorerEditCursor-1)
	case "right":
		a.State.DiskExplorerEditCursor = min(runeLen(a.State.DiskExplorerEditValue), a.State.DiskExplorerEditCursor+1)
	case "home":
		a.State.DiskExplorerEditCursor = 0
	case "end":
		a.State.DiskExplorerEditCursor = runeLen(a.State.DiskExplorerEditValue)
	case "delete":
		a.State.DiskExplorerEditValue = deleteRuneAt(a.State.DiskExplorerEditValue, a.State.DiskExplorerEditCursor)
	case "space":
		a.insertDiskExplorerEditText(" ")
	default:
		if value, ok := strings.CutPrefix(key, "char:"); ok {
			a.insertDiskExplorerEditText(value)
		}
	}
}

func (a *App) insertDiskExplorerEditText(value string) {
	runes := []rune(a.State.DiskExplorerEditValue)
	cursor := clamp(a.State.DiskExplorerEditCursor, 0, len(runes))
	insert := []rune(value)
	runes = append(runes[:cursor], append(insert, runes[cursor:]...)...)
	a.State.DiskExplorerEditValue = string(runes)
	a.State.DiskExplorerEditCursor = cursor + len(insert)
}

func (a *App) commitDiskExplorerEdit() {
	row, ok := a.selectedDiskExplorerRow()
	if !ok {
		a.State.Message = "disk edit needs disk id"
		return
	}
	switch a.State.DiskExplorerEdit {
	case diskExplorerActionRename:
		value := strings.TrimSpace(a.State.DiskExplorerEditValue)
		oldID := row.Disk.ID
		a.clearDiskExplorerEdit()
		a.diskRename(oldID, value)
		a.selectDiskExplorerID(value)
	case diskExplorerActionResize:
		size := strings.TrimSpace(a.State.DiskExplorerEditValue)
		a.clearDiskExplorerEdit()
		request, err := diskResizeRequest(row.Disk.ID, map[string]string{"size": size})
		if err != nil {
			a.State.Message = err.Error()
			return
		}
		a.diskResize(request)
	}
}

func (a *App) runDiskExplorerAction(action string) {
	a.clearDiskExplorerEdit()
	switch action {
	case diskExplorerActionCreate:
		id := a.nextDiskIDForNode("")
		a.diskCreate(topology.DiskCreateRequest{
			ID:     id,
			SizeGB: topology.SetField(10),
			Format: topology.DiskFormatQCOW2,
		})
		a.selectDiskExplorerID(id)
	case diskExplorerActionImport:
		a.openDiskImportBrowser()
	case diskExplorerActionLayer:
		row, ok := a.selectedDiskExplorerRow()
		if !ok {
			a.State.Message = "disk layer create needs disk id"
			return
		}
		baseID := row.Disk.ID
		if diskKindUI(row.Disk) == "layer" {
			baseID = row.Disk.Base
		}
		if baseID == "" || diskKindUI(row.Disk) != "base" && diskKindUI(row.Disk) != "layer" {
			a.State.Message = "disk is not a base: " + row.Disk.ID
			return
		}
		layerID := a.nextLayerIDForDisk(baseID)
		a.diskLayerCreate(baseID, layerID)
		a.selectDiskExplorerID(layerID)
	case diskExplorerActionRename:
		row, ok := a.selectedDiskExplorerRow()
		if !ok {
			a.State.Message = "disk rename needs disk id"
			return
		}
		a.State.DiskExplorerEdit = diskExplorerActionRename
		a.State.DiskExplorerEditValue = row.Disk.ID
		a.State.DiskExplorerEditCursor = runeLen(row.Disk.ID)
	case diskExplorerActionResize:
		row, ok := a.selectedDiskExplorerRow()
		if !ok {
			a.State.Message = "disk resize needs disk id"
			return
		}
		value := ""
		if row.Disk.SizeGB > 0 {
			value = strconv.Itoa(row.Disk.SizeGB)
		}
		a.State.DiskExplorerEdit = diskExplorerActionResize
		a.State.DiskExplorerEditValue = value
		a.State.DiskExplorerEditCursor = runeLen(value)
	case diskExplorerActionMerge:
		row, ok := a.selectedDiskExplorerRow()
		if !ok {
			a.State.Message = "disk merge needs disk id"
			return
		}
		if diskKindUI(row.Disk) != "layer" {
			a.State.Message = "disk is not a layer: " + row.Disk.ID
			return
		}
		a.diskMerge(row.Disk.ID)
	case diskExplorerActionDelete:
		row, ok := a.selectedDiskExplorerRow()
		if !ok {
			a.State.Message = "disk delete needs disk id"
			return
		}
		a.diskDelete(row.Disk.ID)
	}
	a.clampDiskExplorerSelection()
}

func (a *App) selectDiskExplorerID(id string) {
	for i, row := range a.diskExplorerRows() {
		if row.Disk.ID == id {
			a.State.DiskExplorerSelected = i
			a.ensureDiskExplorerSelectionVisible()
			return
		}
	}
}

func (a *App) ensureDiskExplorerSelectionVisible() {
	layout, ok := diskExplorerLayout(a.ViewWidth, a.contentHeight())
	if !ok {
		return
	}
	visible := diskExplorerVisibleRowsForState(a.State, layout)
	if visible <= 0 {
		return
	}
	selected := a.State.DiskExplorerSelected
	if selected < a.State.DiskExplorerScroll {
		a.State.DiskExplorerScroll = selected
	}
	if selected >= a.State.DiskExplorerScroll+visible {
		a.State.DiskExplorerScroll = selected - visible + 1
	}
	a.State.DiskExplorerScroll = max(0, a.State.DiskExplorerScroll)
}

func (a *App) diskExplorerRenderState() ViewState {
	state := a.inspectorRenderState()
	if !state.DiskExplorerOpen {
		return state
	}
	if state.DiskExplorerEdit == diskExplorerActionImport {
		entries, err := a.diskImportBrowserEntries()
		state.DiskImportEntries = make([]DiskImportEntryView, 0, len(entries))
		if err != nil {
			state.DiskImportError = err.Error()
			return state
		}
		for _, entry := range entries {
			state.DiskImportEntries = append(state.DiskImportEntries, DiskImportEntryView{
				Name: entry.Name, Directory: entry.Directory, Size: formatDiskImportSize(entry.Size),
			})
		}
		return state
	}
	rows := a.diskExplorerRows()
	state.DiskExplorerRows = make([]string, 0, len(rows))
	state.DiskExplorerKinds = make([]string, 0, len(rows))
	state.DiskExplorerRowViews = make([]DiskExplorerRowView, 0, len(rows))
	for _, row := range rows {
		state.DiskExplorerRows = append(state.DiskExplorerRows, a.diskExplorerRowLabel(row))
		state.DiskExplorerKinds = append(state.DiskExplorerKinds, diskKindUI(row.Disk))
		state.DiskExplorerRowViews = append(state.DiskExplorerRowViews, a.diskExplorerRowView(row))
	}
	return state
}

func (a *App) inspectorRenderState() ViewState {
	a.refreshVNCViewerProcesses()
	state := a.State
	state.InspectorDiskItems = nil
	state.InspectorDiskActions = nil
	state.InspectorDiskKinds = nil
	state.InspectorDiskIDs = nil
	node, ok := selectedNode(a.Model, a.State.Selected)
	if !ok || (node.Type != NodeVM && node.Type != NodeContainer) {
		return state
	}
	entries := a.diskMenuEntries(node)
	state.InspectorDiskItems = make([]string, 0, len(entries))
	state.InspectorDiskActions = make([]string, 0, len(entries))
	state.InspectorDiskKinds = make([]string, 0, len(entries))
	state.InspectorDiskIDs = make([]string, 0, len(entries))
	for _, entry := range entries {
		state.InspectorDiskItems = append(state.InspectorDiskItems, entry.label)
		state.InspectorDiskActions = append(state.InspectorDiskActions, entry.action)
		state.InspectorDiskKinds = append(state.InspectorDiskKinds, a.diskMenuEntryKind(node, entry))
		state.InspectorDiskIDs = append(state.InspectorDiskIDs, entry.diskID)
	}
	return state
}

func diskExplorerLayout(width, height int) (rect, bool) {
	if width < minWidth || height < minHeight {
		return rect{}, false
	}
	return rect{X: 0, Y: 0, W: width, H: height}, true
}

func diskExplorerVisibleRows(layout rect) int {
	return max(0, layout.H-3)
}

func (a *App) handleDiskExplorerMouse(event mouseEvent) bool {
	layout, ok := diskExplorerLayout(a.ViewWidth, a.contentHeight())
	if !ok || !xyInRect(event.x, event.y, layout) {
		return false
	}
	if a.State.DiskExplorerEdit == diskExplorerActionImport {
		if event.y == layout.Y+1 {
			a.beginDiskImportPathEdit("")
			return false
		}
		if a.State.DiskImportPathEditing {
			return false
		}
		entries, err := a.diskImportBrowserEntries()
		if err != nil {
			a.setDiskImportBrowserError(err)
			return false
		}
		if row, ok := diskExplorerRowAt(layout, a.State.DiskImportScroll, event.x, event.y, len(entries), diskExplorerVisibleRows(layout)); ok {
			a.State.DiskImportSelected = row
			a.ensureDiskImportSelectionVisible()
		}
		return false
	}
	if action, ok := diskExplorerActionAt(layout, event.x, event.y); ok {
		a.runDiskExplorerAction(action)
		return false
	}
	if row, ok := diskExplorerRowAt(layout, a.State.DiskExplorerScroll, event.x, event.y, len(a.diskExplorerRows()), diskExplorerVisibleRowsForState(a.State, layout)); ok {
		a.clearDiskExplorerEdit()
		a.State.DiskExplorerSelected = row
		a.ensureDiskExplorerSelectionVisible()
		return false
	}
	return false
}

func (a *App) diskExplorerFeedbackRect(event mouseEvent) (rect, bool) {
	layout, ok := diskExplorerLayout(a.ViewWidth, a.contentHeight())
	if !ok || !xyInRect(event.x, event.y, layout) {
		return rect{}, false
	}
	if a.State.DiskExplorerEdit == diskExplorerActionImport {
		if event.y == layout.Y+1 {
			return rect{X: layout.X + 1, Y: event.y, W: layout.W - 2, H: 1}, true
		}
		if a.State.DiskImportPathEditing {
			return rect{}, false
		}
		entries, err := a.diskImportBrowserEntries()
		if err != nil {
			return rect{}, false
		}
		if _, ok := diskExplorerRowAt(layout, a.State.DiskImportScroll, event.x, event.y, len(entries), diskExplorerVisibleRows(layout)); ok {
			return rect{X: layout.X + 1, Y: event.y, W: layout.W - 2, H: 1}, true
		}
		return rect{}, false
	}
	if r, ok := diskExplorerActionRectAt(layout, event.x, event.y); ok {
		return r, true
	}
	if _, ok := diskExplorerRowAt(layout, a.State.DiskExplorerScroll, event.x, event.y, len(a.diskExplorerRows()), diskExplorerVisibleRowsForState(a.State, layout)); ok {
		return rect{X: layout.X + 1, Y: event.y, W: layout.W - 2, H: 1}, true
	}
	return rect{}, false
}

func diskExplorerRowAt(layout rect, scroll, x, y, count, visibleRows int) (int, bool) {
	if x < layout.X+1 || x >= layout.X+layout.W-1 {
		return 0, false
	}
	firstY := diskExplorerRowsY(layout)
	lastY := firstY + visibleRows
	if y < firstY || y >= lastY {
		return 0, false
	}
	index := scroll + y - firstY
	return index, index >= 0 && index < count
}

func diskExplorerRowsY(layout rect) int {
	return layout.Y + 2
}

func diskExplorerActionAt(layout rect, x, y int) (string, bool) {
	button, ok := diskExplorerActionButtonAt(layout, x, y)
	if !ok {
		return "", false
	}
	return button.action, true
}

func diskExplorerActionRectAt(layout rect, x, y int) (rect, bool) {
	button, ok := diskExplorerActionButtonAt(layout, x, y)
	if !ok {
		return rect{}, false
	}
	return rect{X: button.x, Y: y, W: button.w, H: 1}, true
}

type diskExplorerPositionedActionButton struct {
	diskExplorerActionButton
	x int
	w int
}

func diskExplorerActionButtonAt(layout rect, x, y int) (diskExplorerPositionedActionButton, bool) {
	if y != layout.Y+layout.H-1 || x < layout.X+1 || x >= layout.X+layout.W-1 {
		return diskExplorerPositionedActionButton{}, false
	}
	pos := layout.X + 1
	for _, button := range diskExplorerActionButtons() {
		if pos >= layout.X+layout.W-1 {
			break
		}
		w := min(runeLen(button.label), layout.X+layout.W-1-pos)
		next := pos + w
		if x >= pos && x < next {
			return diskExplorerPositionedActionButton{
				diskExplorerActionButton: button,
				x:                        pos,
				w:                        w,
			}, true
		}
		pos = next
	}
	return diskExplorerPositionedActionButton{}, false
}

func (a *App) diskExplorerRowLabel(row diskExplorerRow) string {
	prefix := ""
	if row.Depth > 0 {
		prefix = "  | "
	}
	if row.Missing {
		prefix += "! "
	}
	disk := row.Disk
	parts := []string{prefix + disk.ID, diskKindUI(disk)}
	if disk.SizeGB > 0 {
		parts = append(parts, fmt.Sprintf("%dG", disk.SizeGB))
	}
	parts = append(parts, diskFormatLabel(disk))
	if disk.Base != "" {
		parts = append(parts, "base:"+disk.Base)
	}
	if disk.AttachedType != "" && disk.AttachedTo != "" {
		parts = append(parts, "at:"+a.diskExplorerAttachmentLabel(disk))
	}
	if disk.Path != "" {
		parts = append(parts, disk.Path)
	}
	return strings.Join(parts, "  ")
}

func (a *App) diskExplorerRowView(row diskExplorerRow) DiskExplorerRowView {
	disk := row.Disk
	size := "-"
	if disk.SizeGB > 0 {
		size = fmt.Sprintf("%dG", disk.SizeGB)
	}
	relation := "-"
	if disk.Base != "" {
		relation = "base:" + disk.Base
	}
	if disk.AttachedType != "" && disk.AttachedTo != "" {
		relation = a.diskExplorerAttachmentLabel(disk)
	}
	if row.Missing && disk.Base != "" {
		relation = "missing:" + disk.Base
	}
	path := disk.Path
	if path == "" {
		path = "-"
	}
	return DiskExplorerRowView{
		ID:       disk.ID,
		Kind:     diskKindUI(disk),
		Size:     size,
		Format:   diskFormatLabel(disk),
		Relation: relation,
		Path:     path,
		Depth:    row.Depth,
		Missing:  row.Missing,
	}
}

func (a *App) diskExplorerAttachmentLabel(disk lab.Disk) string {
	switch disk.AttachedType {
	case NodeVM, NodeContainer:
		return disk.AttachedType + ":" + a.displayNodeName(disk.AttachedType, disk.AttachedTo)
	default:
		return disk.AttachedType + ":" + disk.AttachedTo
	}
}
