package topologyui

type overlayKind uint8

const (
	overlayNone overlayKind = iota
	overlayContextMenu
	overlayPalette
	overlayConnectTarget
)

func (s *ViewState) activeOverlay() overlayKind {
	switch {
	case s.PaletteOpen:
		return overlayPalette
	case s.ConnectTargetMenu:
		return overlayConnectTarget
	case s.ContextMenu:
		return overlayContextMenu
	default:
		return overlayNone
	}
}

func (s *ViewState) openOverlay(kind overlayKind) {
	if kind != overlayContextMenu {
		s.closeContextMenu()
	}
	if kind != overlayPalette {
		s.PaletteOpen = false
		s.PaletteQuery = ""
		s.PaletteSelected = 0
	}
	if kind != overlayConnectTarget {
		s.ConnectTargetMenu = false
		s.ConnectTargetID = ""
		s.ConnectTargetType = ""
		s.ConnectTargetIndex = 0
	}
	s.TopMenuOpen = false
	s.ContextMenu = kind == overlayContextMenu
	s.PaletteOpen = kind == overlayPalette
	s.ConnectTargetMenu = kind == overlayConnectTarget
}
