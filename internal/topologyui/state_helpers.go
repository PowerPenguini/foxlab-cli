package topologyui

func (s *ViewState) clearContextRowState() {
	s.ContextDeleteNIC = false
	s.ContextAddDiskLayer = false
	s.ContextMergeDisk = false
	s.ContextDetachDisk = false
	s.ContextDeleteDisk = false
}

func (s *ViewState) clearContextEditState() {
	s.ContextEdit = false
	s.ContextEditValue = ""
	s.ContextEditCursor = 0
}

func (s *ViewState) clearContextMenuCache() {
	s.DiskMenuItems = nil
	s.DiskMenuActions = nil
	s.DiskMenuKinds = nil
}

func (s *ViewState) closeContextMenu() {
	s.ContextMenu = false
	s.ContextGroup = ""
	s.ContextInSubmenu = false
	s.ContextSubSelected = 0
	s.clearContextEditState()
	s.clearContextRowState()
	s.clearContextMenuCache()
}

func (s *ViewState) closeContextSubmenu() {
	s.ContextGroup = ""
	s.ContextInSubmenu = false
	s.ContextSubSelected = 0
	s.clearContextRowState()
	s.clearContextMenuCache()
}
