package topologyui

const (
	ansiGreen       = "\x1b[32m"
	ansiRed         = "\x1b[31m"
	ansiBrightBlack = "\x1b[90m"

	themeChrome       = ansiBgGray + ansiWhite
	themeChromeActive = ansiBgCyan + ansiWhite + ansiBold
	themeMuted        = ansiDim
	themeRoute        = ansiDim
	themeRouteActive  = ansiBrightCyan
	themeRoutePreview = ansiDim + ansiBrightCyan
	themeMenuRow      = ansiBgGray + ansiWhite
	themeMenuActive   = ansiBgGray + ansiWhite + ansiBold
	themeFooter       = ansiBgGray + ansiWhite
	themeFooterActive = ansiBgGray + ansiWhite + ansiBold
)

func nodeBadgeStyle(nodeType string) string {
	switch nodeType {
	case NodeVM:
		return ansiBrightCyan + ansiBold
	case NodeContainer:
		return ansiGreen + ansiBold
	case NodeSwitch:
		return ansiYellow + ansiBold
	case NodeExternal:
		return ansiBrightMagenta + ansiBold
	default:
		return ansiWhite + ansiBold
	}
}

func nodeLabelStyle(nodeType string) string {
	switch nodeType {
	case NodeSwitch, NodeExternal:
		return ansiWhite
	default:
		return ansiWhite + ansiBold
	}
}

func stateStyle(state string) string {
	switch state {
	case "running", "link":
		return ansiGreen + ansiBold
	case "starting", "stopping", "loading", "pulling", "creating", "applying", "refreshing":
		return ansiBrightCyan + ansiBold
	case "missing", "error", "failed":
		return ansiRed + ansiBold
	case "nat", "bridge", "direct", "macnat", "macnat-bridge":
		return ansiCyan
	case "defined", "stopped", "shutoff", "created":
		return ansiBrightBlack
	default:
		return themeMuted
	}
}

func nodeStateStyle(nodeType, state string) string {
	if nodeType == NodeSwitch {
		switch state {
		case "nat", "bridge", "direct", "macnat", "macnat-bridge":
			return ansiYellow + ansiBold
		}
	}
	if nodeType == NodeExternal {
		switch state {
		case "link", "nat", "direct", "macnat":
			return ansiBrightMagenta + ansiBold
		}
	}
	return stateStyle(state)
}
