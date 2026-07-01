package lab

import "strings"

func SwitchExternalLinks(sw Switch) []string {
	out := make([]string, 0, len(sw.ExternalLinks)+1)
	seen := map[string]struct{}{}
	for _, id := range sw.ExternalLinks {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if id := strings.TrimSpace(sw.ExternalLink); id != "" {
		if _, exists := seen[id]; !exists {
			out = append(out, id)
		}
	}
	return out
}

func SwitchHasExternalLink(sw Switch, id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for _, linkID := range SwitchExternalLinks(sw) {
		if linkID == id {
			return true
		}
	}
	return false
}
