package topologyui

import "strings"

const (
	attachUplinkMenuItem     = "Attach Uplink"
	switchUplinkActionPrefix = "uplink:"
)

func switchUplinkMenuItems(node Node) []string {
	items := []string{attachUplinkMenuItem}
	for _, id := range switchConnectedUplinkIDs(node) {
		items = append(items, id)
	}
	return items
}

func switchUplinkMenuActions(node Node) []string {
	items := []string{attachUplinkMenuItem}
	for _, id := range switchConnectedUplinkIDs(node) {
		items = append(items, switchUplinkMenuAction(id))
	}
	return items
}

func switchUplinkMenuItemsForModel(m Model, node Node) []MenuItem {
	items := []MenuItem{{
		ID:      "attach-uplink",
		Label:   attachUplinkMenuItem,
		Action:  "attach-uplink",
		Kind:    menuItemAction,
		Enabled: len(attachableUplinkIDsInModel(m)) > 0,
	}}
	for _, id := range switchConnectedUplinkIDs(node) {
		action := switchUplinkMenuAction(id)
		items = append(items, MenuItem{
			ID:      action,
			Label:   switchUplinkMenuLabel(m, id),
			Action:  action,
			Kind:    menuItemAction,
			Enabled: true,
			RowKind: "uplink",
		})
	}
	return items
}

func switchUplinkMenuLabel(m Model, id string) string {
	if node, ok := nodeByKey(m, NodeKey(NodeExternal, id)); ok {
		return firstNonEmpty(node.Label, id)
	}
	return id
}

func switchUplinkMenuAction(id string) string {
	return switchUplinkActionPrefix + id
}

func switchUplinkMenuKinds(items []string) []string {
	kinds := make([]string, len(items))
	for i, item := range items {
		if isSwitchUplinkMenuDetail(item) {
			kinds[i] = "uplink"
		}
	}
	return kinds
}

func switchUplinkMenuExternalID(item string) (string, bool) {
	item = strings.TrimSpace(item)
	if item == "" || item == attachUplinkMenuItem {
		return "", false
	}
	if strings.HasPrefix(item, switchUplinkActionPrefix) {
		id := strings.TrimSpace(strings.TrimPrefix(item, switchUplinkActionPrefix))
		return id, id != ""
	}
	return item, true
}

func isSwitchUplinkMenuDetail(item string) bool {
	_, ok := switchUplinkMenuExternalID(item)
	return ok
}

func switchConnectedUplinkID(node Node) string {
	ids := switchConnectedUplinkIDs(node)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func switchConnectedUplinkIDs(node Node) []string {
	if node.Type != NodeSwitch {
		return nil
	}
	ids := []string{}
	for _, detail := range node.Details {
		if key, id, ok := strings.Cut(detail, "="); ok && strings.TrimSpace(key) == "uplink" {
			id = strings.TrimSpace(id)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func externalConnectedInModel(m Model, id string) bool {
	key := NodeKey(NodeExternal, id)
	for _, edge := range m.Edges {
		if edge.From == key || edge.To == key {
			return true
		}
	}
	return false
}

func firstAttachableUplinkIDInModel(m Model) string {
	ids := attachableUplinkIDsInModel(m)
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func attachableUplinkIDsInModel(m Model) []string {
	ids := []string{}
	for _, node := range m.Nodes {
		if node.Type == NodeExternal && !externalConnectedInModel(m, node.ID) {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

func switchUplinkMenuItemEnabled(m Model, item string) bool {
	item = strings.TrimSpace(item)
	if item != attachUplinkMenuItem && item != "attach-uplink" {
		return true
	}
	return len(attachableUplinkIDsInModel(m)) > 0
}
