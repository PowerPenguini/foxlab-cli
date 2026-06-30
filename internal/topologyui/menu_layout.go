package topologyui

func contextMenuWidth(items []string) int {
	return contextMenuWidthWithKinds(items, nil)
}

func contextMenuWidthWithKinds(items []string, kinds []string) int {
	w := 0
	for i, item := range items {
		extra := 3
		kind := ""
		if i < len(kinds) {
			kind = kinds[i]
		}
		if isNICDetail(item) {
			extra = 6
		}
		if kind == "uplink" {
			extra = 6
		}
		if kind == "layer" || isDiskMenuDetail(item) {
			extra = 12
		}
		if kind == "base" || isDiskAttachMenuDetail(item) {
			extra = 9
		}
		if kind == "data" {
			extra = 9
		}
		w = max(w, runeLen(item)+extra)
	}
	return max(w, 10)
}

func contextMenuStart(active, itemCount, visibleCount int) int {
	if itemCount <= visibleCount {
		return 0
	}
	half := visibleCount / 2
	return clamp(active-half, 0, itemCount-visibleCount)
}
