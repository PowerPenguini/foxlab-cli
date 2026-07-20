package topologyui

import "testing"

func TestOpenOverlayClosesOtherTransientSurfaces(t *testing.T) {
	state := ViewState{
		ContextMenu:        true,
		ContextGroup:       "disk-menu",
		PaletteOpen:        true,
		PaletteQuery:       "add vm",
		DiskExplorerOpen:   true,
		DiskExplorerEdit:   diskExplorerActionRename,
		ConnectTargetMenu:  true,
		ConnectTargetID:    "vm-2",
		ConnectTargetType:  NodeVM,
		ConnectTargetIndex: 2,
		TopMenuOpen:        true,
	}

	state.openOverlay(overlayPalette)

	if state.activeOverlay() != overlayPalette || !state.PaletteOpen {
		t.Fatalf("active overlay = %v, palette=%v", state.activeOverlay(), state.PaletteOpen)
	}
	if state.ContextMenu || state.ConnectTargetMenu || state.TopMenuOpen {
		t.Fatalf("other overlays remain open: %#v", state)
	}
	if state.ContextGroup != "" || state.ConnectTargetID != "" {
		t.Fatalf("closed overlay state was not reset: %#v", state)
	}
	if !state.DiskExplorerOpen || state.DiskExplorerEdit != diskExplorerActionRename {
		t.Fatalf("independent disk card state was reset: %#v", state)
	}
	if state.PaletteQuery != "add vm" {
		t.Fatalf("active overlay query = %q", state.PaletteQuery)
	}
}

func TestOpenContextOverlayResetsPaletteState(t *testing.T) {
	state := ViewState{PaletteOpen: true, PaletteQuery: "disk", PaletteSelected: 2}
	state.openOverlay(overlayContextMenu)

	if state.activeOverlay() != overlayContextMenu || !state.ContextMenu {
		t.Fatalf("active overlay = %v, context=%v", state.activeOverlay(), state.ContextMenu)
	}
	if state.PaletteOpen || state.PaletteQuery != "" || state.PaletteSelected != 0 {
		t.Fatalf("palette state remains active: %#v", state)
	}
}
