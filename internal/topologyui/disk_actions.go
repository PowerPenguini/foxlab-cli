package topologyui

import (
	"fmt"
	"strings"

	"foxlab-cli/internal/lab"
)

func (a *App) openCreateDiskCommand() {
	a.diskCreate(a.nextDiskIDForNode(""), map[string]string{"size": "10", "format": "qcow2"})
}

func (a *App) openAddDiskCommand(node Node) {
	if diskTargetForNode(node) == "" {
		a.openCreateDiskCommand()
		return
	}
	a.diskCreate(a.nextDiskIDForNode(""), map[string]string{
		"size":   "10",
		"format": "qcow2",
	})
}

func (a *App) createNamedDiskForNode(node Node, value string) {
	id := strings.TrimSpace(value)
	if id == "" {
		a.State.Message = "disk name is required"
		return
	}
	if diskTargetForNode(node) == "" {
		a.State.Message = "disk create needs vm or container"
		return
	}
	a.diskCreate(id, map[string]string{
		"size":   "10",
		"format": "qcow2",
	})
}

func (a *App) openDiskCommand(node Node) {
	a.State.Message = "use Disk menu"
}

type diskMenuEntry struct {
	label  string
	action string
	diskID string
}

const (
	diskMenuActionAttach = "attach"
	diskMenuActionDetach = "detach"
	diskMenuActionCreate = "create"
	diskMenuActionNone   = "none"

	diskMenuLayerTreePrefix = "  | "
)

func (a *App) selectDiskMenuEntry(node Node, entry diskMenuEntry) {
	switch entry.action {
	case diskMenuActionAttach:
		a.diskAttach(entry.diskID, map[string]string{"to": diskTargetForNode(node)})
	case diskMenuActionDetach:
		a.detachDiskFromNode(node)
	case diskMenuActionCreate:
		a.openAddDiskCommand(node)
	case diskMenuActionNone:
		a.State.Message = "no disk selected"
	}
}

func (a *App) startAddDiskInlineEdit() {
	value := a.nextDiskIDForNode("")
	a.State.ContextEdit = true
	a.State.ContextEditValue = value
	a.State.ContextEditCursor = runeLen(value)
	a.State.ContextAddDiskLayer = false
	a.State.ContextMergeDisk = false
	a.State.ContextDetachDisk = false
	a.State.ContextDeleteDisk = false
}

func (a *App) startAddLayerInlineEdit(entry diskMenuEntry) {
	value := a.nextLayerIDForDisk(entry.diskID)
	a.State.ContextEdit = true
	a.State.ContextEditValue = value
	a.State.ContextEditCursor = runeLen(value)
	a.State.ContextAddDiskLayer = true
	a.State.ContextMergeDisk = false
	a.State.ContextDetachDisk = false
	a.State.ContextDeleteDisk = false
}

func (a *App) createNamedLayerForNode(node Node, entry diskMenuEntry, value string) {
	layerID := strings.TrimSpace(value)
	if layerID == "" {
		a.State.Message = "layer name is required"
		return
	}
	target := diskTargetForNode(node)
	if target == "" {
		a.State.Message = "disk attach needs vm or container"
		return
	}
	targetType, targetID, ok := strings.Cut(target, ":")
	if !ok {
		a.State.Message = "disk attach needs vm or container"
		return
	}
	a.State.Message = a.ensureService().DiskLayerCreateAndAttach(entry.diskID, layerID, targetType, targetID)
	a.syncAfterServiceMutation()
}

func (a *App) detachDiskFromNode(node Node) {
	target := diskTargetForNode(node)
	if target == "" {
		a.State.Message = "disk detach needs vm or container"
		return
	}
	args := map[string]string{"from": target}
	a.State.Message = a.ensureService().DiskDetach(node.ID, args)
	if strings.HasPrefix(a.State.Message, "attached disk not found:") {
		switch node.Type {
		case NodeVM:
			a.State.Message = a.ensureService().VMSet(node.ID, map[string]string{"disk": ""})
		case NodeContainer:
			a.State.Message = a.ensureService().ContainerSet(node.ID, map[string]string{"disk": ""})
		}
	}
	a.syncAfterServiceMutation()
}

func (a *App) deleteDiskMenuEntry(node Node, entry diskMenuEntry) {
	if entry.diskID == "" {
		a.State.Message = "disk delete needs disk id"
		return
	}
	kind := a.diskMenuEntryKind(node, entry)
	if entry.action == diskMenuActionNone {
		target := diskTargetForNode(node)
		if target != "" {
			msg := a.ensureService().DiskDetach(node.ID, map[string]string{"from": target})
			if strings.HasPrefix(msg, "disk detach failed:") {
				a.State.Message = msg
				a.syncAfterServiceMutation()
				return
			}
		}
	}
	if kind == "layer" {
		a.State.Message = a.ensureService().DiskLayerDelete(entry.diskID)
	} else {
		a.State.Message = a.ensureService().DiskDelete(entry.diskID)
	}
	a.syncAfterServiceMutation()
}

func (a *App) diskKindByID(id string) string {
	if a.Lab == nil {
		return ""
	}
	for _, disk := range a.Lab.Disks {
		if disk.ID == id {
			return diskKindUI(disk)
		}
	}
	return ""
}

func (a *App) diskMenuEntryKind(node Node, entry diskMenuEntry) string {
	kind := a.diskKindByID(entry.diskID)
	if node.Type == NodeContainer && kind == "base" {
		return "data"
	}
	return kind
}

func (a *App) mergeDiskForNode(node Node) {
	layerID := a.attachedLayerDiskID(node)
	if layerID == "" {
		a.State.Message = "attached disk layer not found: " + a.displayNodeName(node.Type, node.ID)
		return
	}
	a.diskMerge(layerID)
}

func (a *App) diskMenuEntries(node Node) []diskMenuEntry {
	entries := []diskMenuEntry{{label: "Add Disk", action: diskMenuActionCreate}}
	activeID := a.attachedDiskID(node)
	layerRows := a.layerDisksForMenu(node)
	for _, disk := range a.attachableDisks(node) {
		action := diskMenuActionAttach
		if disk.ID == activeID {
			action = diskMenuActionNone
		}
		entries = append(entries, diskMenuEntry{
			label:  diskMenuDiskLabel(disk),
			action: action,
			diskID: disk.ID,
		})
		for _, layer := range layerRows[disk.ID] {
			action := diskMenuActionAttach
			if layer.ID == activeID {
				action = diskMenuActionNone
			}
			entries = append(entries, diskMenuEntry{
				label:  diskMenuLayerVariantLabel(layer),
				action: action,
				diskID: layer.ID,
			})
		}
	}
	if orphans := layerRows[""]; len(orphans) > 0 {
		for _, layer := range orphans {
			action := diskMenuActionAttach
			if layer.ID == activeID {
				action = diskMenuActionNone
			}
			entries = append(entries, diskMenuEntry{
				label:  diskLayerLabel(layer),
				action: action,
				diskID: layer.ID,
			})
		}
	}
	if len(entries) == 1 {
		return []diskMenuEntry{
			entries[0],
			{label: "No disks", action: diskMenuActionNone},
		}
	}
	return entries
}

func (a *App) attachedDiskID(node Node) string {
	if a.Lab == nil {
		return ""
	}
	targetType := diskTargetType(node.Type)
	for _, disk := range a.Lab.Disks {
		if disk.AttachedType == targetType && disk.AttachedTo == node.ID {
			return disk.ID
		}
	}
	current := a.currentDiskPath(node)
	for _, disk := range a.Lab.Disks {
		if current != "" && a.Lab.ResolvePath(disk.Path) == current {
			return disk.ID
		}
	}
	return ""
}

func (a *App) attachedLayerDiskID(node Node) string {
	if a.Lab == nil {
		return ""
	}
	targetType := diskTargetType(node.Type)
	for _, disk := range a.Lab.Disks {
		if diskKindUI(disk) == "layer" && disk.AttachedType == targetType && disk.AttachedTo == node.ID {
			return disk.ID
		}
	}
	current := a.currentDiskPath(node)
	for _, disk := range a.Lab.Disks {
		if diskKindUI(disk) == "layer" && current != "" && a.Lab.ResolvePath(disk.Path) == current {
			return disk.ID
		}
	}
	return ""
}

func (a *App) currentDiskMenuLabel(node Node) string {
	current := a.currentDiskPath(node)
	if a.Lab != nil {
		targetType := diskTargetType(node.Type)
		for _, disk := range a.Lab.Disks {
			if disk.AttachedType == targetType && disk.AttachedTo == node.ID {
				if diskKindUI(disk) == "base" || diskKindUI(disk) == "data" {
					return diskMenuDiskLabel(disk)
				}
				return diskLayerLabel(disk)
			}
		}
		for _, disk := range a.Lab.Disks {
			if disk.Path == current {
				if diskKindUI(disk) == "base" || diskKindUI(disk) == "data" {
					return diskMenuDiskLabel(disk)
				}
				return diskLayerLabel(disk)
			}
		}
	}
	return current + " | " + current
}

func (a *App) attachableDisks(node Node) []lab.Disk {
	if a.Lab == nil {
		return nil
	}
	out := []lab.Disk{}
	for _, disk := range a.Lab.Disks {
		kind := diskKindUI(disk)
		if kind != "base" && (node.Type != NodeContainer || kind != "data") {
			continue
		}
		out = append(out, disk)
	}
	return out
}

func (a *App) layerDisksForMenu(node Node) map[string][]lab.Disk {
	out := map[string][]lab.Disk{}
	if a.Lab == nil {
		return out
	}
	targetType := diskTargetType(node.Type)
	current := a.currentDiskPath(node)
	baseIDs := map[string]bool{}
	for _, disk := range a.Lab.Disks {
		if diskKindUI(disk) == "base" {
			baseIDs[disk.ID] = true
		}
	}
	for _, disk := range a.Lab.Disks {
		if diskKindUI(disk) != "layer" {
			continue
		}
		currentDisk := current != "" && a.Lab.ResolvePath(disk.Path) == current
		if !currentDisk && disk.AttachedTo != "" && (disk.AttachedType != targetType || disk.AttachedTo != node.ID) {
			continue
		}
		baseID := disk.Base
		if !baseIDs[baseID] {
			baseID = ""
		}
		out[baseID] = append(out[baseID], disk)
	}
	return out
}

func (a *App) currentDiskPath(node Node) string {
	switch node.Type {
	case NodeVM:
		if vm, ok := a.labVM(node.ID); ok {
			return vm.Disk
		}
	case NodeContainer:
		if ct, ok := a.labContainer(node.ID); ok {
			return ct.Disk
		}
	}
	return ""
}

func (a *App) diskMenuItems(node Node) []string {
	entries := a.diskMenuEntries(node)
	items := make([]string, 0, len(entries))
	for _, entry := range entries {
		items = append(items, entry.label)
	}
	return items
}

func (a *App) diskMenuActions(node Node) []string {
	entries := a.diskMenuEntries(node)
	actions := make([]string, 0, len(entries))
	for _, entry := range entries {
		actions = append(actions, entry.action)
	}
	return actions
}

func (a *App) diskMenuKinds(node Node) []string {
	entries := a.diskMenuEntries(node)
	kinds := make([]string, 0, len(entries))
	for _, entry := range entries {
		kinds = append(kinds, a.diskMenuEntryKind(node, entry))
	}
	return kinds
}

func diskMenuDiskLabel(disk lab.Disk) string {
	parts := []string{disk.ID}
	if disk.SizeGB > 0 {
		parts = append(parts, fmt.Sprintf("%dG", disk.SizeGB))
	}
	return strings.Join(parts, " ")
}

func diskMenuLayerVariantLabel(disk lab.Disk) string {
	return diskMenuLayerTreePrefix + disk.ID
}

func diskLayerLabel(disk lab.Disk) string {
	return firstNonEmpty(disk.Base, disk.ID) + " | " + disk.ID
}

func diskKindUI(disk lab.Disk) string {
	if disk.Kind == "" {
		return "base"
	}
	return disk.Kind
}

func diskTargetType(nodeType string) string {
	if nodeType == NodeContainer {
		return "container"
	}
	return "vm"
}

func diskTargetForNode(node Node) string {
	switch node.Type {
	case NodeVM:
		return "vm:" + node.ID
	case NodeContainer:
		return "container:" + node.ID
	default:
		return ""
	}
}

func (a *App) nextDiskIDForNode(_ string) string {
	base := "disk"
	if !a.hasDiskID(base) {
		return base
	}
	for i := 2; ; i++ {
		id := fmt.Sprintf("%s%d", base, i)
		if !a.hasDiskID(id) {
			return id
		}
	}
}

func (a *App) hasDiskID(id string) bool {
	if a.Lab == nil {
		return false
	}
	for _, disk := range a.Lab.Disks {
		if disk.ID == id {
			return true
		}
	}
	return false
}

func (a *App) nextLayerIDForDisk(diskID string) string {
	base := diskID + "-layer"
	if diskID == "" {
		base = "layer"
	}
	if !a.hasDiskID(base) {
		return base
	}
	for i := 2; ; i++ {
		id := fmt.Sprintf("%s-%d", base, i)
		if !a.hasDiskID(id) {
			return id
		}
	}
}

func (a *App) diskCreate(id string, args map[string]string) {
	a.State.Message = a.ensureService().DiskCreate(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) diskAttach(id string, args map[string]string) {
	a.State.Message = a.ensureService().DiskAttach(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) diskDetach(id string, args map[string]string) {
	a.State.Message = a.ensureService().DiskDetach(id, args)
	a.syncAfterServiceMutation()
}

func (a *App) diskMerge(id string) {
	if a.shouldRefreshRuntimeAfterMutation() {
		a.refreshWorkloadStates()
	}
	a.State.Message = a.ensureService().DiskMerge(id)
	a.syncAfterServiceMutation()
}

func (a *App) diskDelete(id string) {
	a.State.Message = a.ensureService().DiskDelete(id)
	a.syncAfterServiceMutation()
}

func (a *App) diskLayerDelete(id string) {
	a.State.Message = a.ensureService().DiskLayerDelete(id)
	a.syncAfterServiceMutation()
}
