package topologyui

import "strings"

const attachUplinkMenuItem = "Attach Uplink"

func switchUplinkMenuItems(node Node) []string {
	items := []string{attachUplinkMenuItem}
	if id := switchConnectedUplinkID(node); id != "" {
		items = append(items, id)
	}
	return items
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
	return item, true
}

func isSwitchUplinkMenuDetail(item string) bool {
	_, ok := switchUplinkMenuExternalID(item)
	return ok
}

func switchConnectedUplinkID(node Node) string {
	if node.Type != NodeSwitch {
		return ""
	}
	value := nodeDetailValue(node, "uplink", "")
	if _, id, ok := strings.Cut(value, "="); ok {
		return strings.TrimSpace(id)
	}
	return ""
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
	if strings.TrimSpace(item) != attachUplinkMenuItem {
		return true
	}
	return len(attachableUplinkIDsInModel(m)) > 0
}
