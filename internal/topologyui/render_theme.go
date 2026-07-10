package topologyui

const (
	ansiGreen       = "\x1b[32m"
	ansiRed         = "\x1b[31m"
	ansiBrightBlack = "\x1b[90m"
	ansiOrange      = "\x1b[38;5;208m"

	ansiBgPanelTop             = "\x1b[48;5;235m"
	ansiBgPanelInspector       = "\x1b[48;5;233m"
	ansiBgPanelInspectorHeader = "\x1b[48;5;233m"
	ansiBgPanelMenu            = "\x1b[48;5;237m"
	ansiBgPanelMenuActive      = "\x1b[48;5;31m"
	ansiBgPanelStatus          = "\x1b[48;5;236m"
	ansiBgTerminal             = "\x1b[48;5;232m"
	ansiBgNode                 = "\x1b[48;5;235m"
	ansiBgNodeSelected         = "\x1b[48;5;237m"

	themeTerminal               = ansiBgTerminal
	themeChrome                 = ansiBgPanelTop + ansiWhite
	themeChromeActive           = ansiBgPanelTop + ansiBrightCyan + ansiBold
	themeChromeMuted            = ansiBgPanelTop + ansiBrightBlack
	themeMuted                  = ansiBgTerminal + ansiBrightBlack
	themeRoute                  = ansiBgTerminal + ansiBrightBlack
	themeRouteActive            = ansiBgTerminal + ansiBrightCyan
	themeRoutePreview           = ansiBgTerminal + ansiBrightCyan + ansiDim
	themeMenuRow                = ansiBgPanelMenu + ansiWhite
	themeMenuActive             = ansiBgPanelMenuActive + ansiWhite + ansiBold
	themeMenuMuted              = ansiBgPanelMenu + ansiBrightBlack
	themeMenuMutedActive        = ansiBgPanelMenuActive + ansiBrightBlack
	themeNotification           = ansiBgPanelStatus + ansiWhite
	themeNotificationBar        = ansiBgRed + ansiWhite + ansiBold
	themeNotificationSuccessBar = ansiBgGreen + ansiBlack + ansiBold
	themePalette                = ansiBgPanelMenu + ansiWhite
	themePaletteHeader          = ansiBgPanelMenu + ansiBrightCyan + ansiBold
	themePaletteInput           = ansiBgPanelTop + ansiWhite
	themePaletteInputHint       = ansiBgPanelTop + ansiWhite + ansiDim
	themePaletteHint            = ansiBgPanelMenu + ansiWhite + ansiDim
	themePaletteMuted           = ansiBgPanelMenu + ansiBrightBlack
	themePaletteActive          = ansiBgPanelMenuActive + ansiWhite + ansiBold
	themePaletteDisabled        = ansiBgPanelMenu + ansiBrightBlack

	themePanelInspector       = ansiBgPanelInspector + ansiWhite
	themePanelInspectorHeader = ansiBgPanelInspectorHeader + ansiWhite + ansiBold
	themePanelInspectorMuted  = ansiBgPanelInspector + ansiBrightBlack
	themePanelDisk            = themePalette
	themePanelDiskHeader      = themePaletteHeader
	themePanelDiskMuted       = themePaletteHint
	themePanelDiskSelected    = themePaletteActive
	themePanelDiskActions     = themePaletteInput
)

func nodePanelStyle(_ string, selected bool) string {
	if selected {
		return ansiBgNodeSelected + ansiWhite
	}
	return ansiBgNode + ansiWhite
}

func nodeAccentStyle(nodeType string, selected bool) string {
	style := ansiBgTerminal + nodeTypeColor(nodeType)
	if selected {
		style += ansiBold
	}
	return style
}

func nodeBadgeStyle(nodeType string) string {
	return nodeTypeColor(nodeType) + ansiBold
}

func nodeTypeColor(nodeType string) string {
	switch nodeType {
	case NodeVM:
		return ansiBrightCyan
	case NodeContainer:
		return ansiGreen
	case NodeSwitch:
		return ansiYellow
	case NodeExternal:
		return ansiBrightMagenta
	default:
		return ansiWhite
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
