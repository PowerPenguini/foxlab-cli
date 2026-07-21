package topologyui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"foxlab-cli/internal/lab"
)

type diskImportEntry struct {
	Name      string
	Path      string
	Directory bool
	Size      int64
}

func (a *App) openDiskImportBrowser() {
	foxlabHome, err := lab.FoxlabHome()
	if err != nil {
		a.setDiskImportBrowserError(err)
		return
	}
	a.openDiskImportBrowserAt(filepath.Dir(foxlabHome))
}

func (a *App) openDiskImportBrowserAt(path string) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		a.setDiskImportBrowserError(err)
		return
	}
	if _, err := readDiskImportEntries(absPath); err != nil {
		a.setDiskImportBrowserError(err)
		return
	}
	a.State.DiskExplorerEdit = diskExplorerActionImport
	a.State.DiskExplorerEditValue = ""
	a.State.DiskExplorerEditCursor = 0
	a.State.DiskImportPath = absPath
	a.State.DiskImportPathEditing = false
	a.State.DiskImportSelected = 0
	a.State.DiskImportScroll = 0
	a.State.DiskImportError = ""
}

func (a *App) diskImportBrowserEntries() ([]diskImportEntry, error) {
	if a.State.DiskImportPath == "" {
		return nil, fmt.Errorf("missing browser path")
	}
	return readDiskImportEntries(a.State.DiskImportPath)
}

func readDiskImportEntries(path string) ([]diskImportEntry, error) {
	directory, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	cleanPath := filepath.Clean(path)
	parent := filepath.Dir(cleanPath)
	rows := make([]diskImportEntry, 0, len(entries)+1)
	if parent != cleanPath {
		rows = append(rows, diskImportEntry{Name: "..", Path: parent, Directory: true})
	}
	for _, entry := range entries {
		if entry.IsDir() {
			rows = append(rows, diskImportEntry{Name: entry.Name(), Path: filepath.Join(cleanPath, entry.Name()), Directory: true})
		}
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		rows = append(rows, diskImportEntry{Name: entry.Name(), Path: filepath.Join(cleanPath, entry.Name()), Size: info.Size()})
	}
	return rows, nil
}

func (a *App) handleDiskImportBrowserKey(key string) bool {
	if a.State.DiskImportPathEditing {
		return a.handleDiskImportPathEditKey(key)
	}
	entries, err := a.diskImportBrowserEntries()
	if err != nil {
		a.setDiskImportBrowserError(err)
		if key == "escape" {
			a.clearDiskExplorerEdit()
		}
		return key == "quit"
	}
	last := max(0, len(entries)-1)
	switch key {
	case "quit":
		return true
	case "escape":
		a.clearDiskExplorerEdit()
		return false
	case "char:p", "char:P":
		a.beginDiskImportPathEdit("")
		return false
	case "char:/":
		a.beginDiskImportPathEdit("/")
		return false
	case "char:~":
		a.beginDiskImportPathEdit("~")
		return false
	case "up", "char:k", "char:K":
		a.State.DiskImportSelected = max(0, a.State.DiskImportSelected-1)
	case "down", "char:j", "char:J":
		a.State.DiskImportSelected = min(last, a.State.DiskImportSelected+1)
	case "home":
		a.State.DiskImportSelected = 0
	case "end":
		a.State.DiskImportSelected = last
	case "pageup", "shift-pageup":
		a.State.DiskImportSelected = max(0, a.State.DiskImportSelected-a.diskImportVisibleRows())
	case "pagedown", "shift-pagedown":
		a.State.DiskImportSelected = min(last, a.State.DiskImportSelected+a.diskImportVisibleRows())
	case "backspace", "left", "char:h", "char:H":
		a.navigateDiskImportBrowser(filepath.Dir(a.State.DiskImportPath))
		return false
	case "enter", "right", "char:l", "char:L":
		if len(entries) == 0 {
			return false
		}
		entry := entries[clamp(a.State.DiskImportSelected, 0, len(entries)-1)]
		if entry.Directory {
			a.navigateDiskImportBrowser(entry.Path)
			return false
		}
		result := a.diskImport(entry.Path)
		if result.OK() {
			id := strings.TrimPrefix(result.Message, "imported disk:")
			a.clearDiskExplorerEdit()
			a.selectDiskExplorerID(id)
		}
		return false
	}
	a.clampDiskImportSelection(len(entries))
	a.ensureDiskImportSelectionVisible()
	return false
}

func (a *App) beginDiskImportPathEdit(initial string) {
	a.State.DiskImportPathEditing = true
	a.State.DiskExplorerEditValue = initial
	a.State.DiskExplorerEditCursor = runeLen(initial)
	a.State.DiskImportError = ""
}

func (a *App) cancelDiskImportPathEdit() {
	a.State.DiskImportPathEditing = false
	a.State.DiskExplorerEditValue = ""
	a.State.DiskExplorerEditCursor = 0
	a.State.DiskImportError = ""
}

func (a *App) handleDiskImportPathEditKey(key string) bool {
	switch key {
	case "quit":
		return true
	case "escape":
		a.cancelDiskImportPathEdit()
	case "enter":
		a.commitDiskImportPathEdit()
	case "backspace":
		if a.State.DiskExplorerEditCursor > 0 {
			a.State.DiskExplorerEditValue = deleteRuneAt(a.State.DiskExplorerEditValue, a.State.DiskExplorerEditCursor-1)
			a.State.DiskExplorerEditCursor--
		}
	case "delete":
		a.State.DiskExplorerEditValue = deleteRuneAt(a.State.DiskExplorerEditValue, a.State.DiskExplorerEditCursor)
	case "left":
		a.State.DiskExplorerEditCursor = max(0, a.State.DiskExplorerEditCursor-1)
	case "right":
		a.State.DiskExplorerEditCursor = min(runeLen(a.State.DiskExplorerEditValue), a.State.DiskExplorerEditCursor+1)
	case "home":
		a.State.DiskExplorerEditCursor = 0
	case "end":
		a.State.DiskExplorerEditCursor = runeLen(a.State.DiskExplorerEditValue)
	case "space":
		a.insertDiskImportPathText(" ")
	default:
		if value, ok := strings.CutPrefix(key, "char:"); ok {
			a.insertDiskImportPathText(value)
		}
	}
	return false
}

func (a *App) insertDiskImportPathText(value string) {
	runes := []rune(a.State.DiskExplorerEditValue)
	cursor := clamp(a.State.DiskExplorerEditCursor, 0, len(runes))
	insert := []rune(value)
	runes = append(runes[:cursor], append(insert, runes[cursor:]...)...)
	a.State.DiskExplorerEditValue = string(runes)
	a.State.DiskExplorerEditCursor = cursor + len(insert)
	a.State.DiskImportError = ""
}

func (a *App) commitDiskImportPathEdit() {
	path := strings.TrimSpace(a.State.DiskExplorerEditValue)
	if path == "" {
		a.setDiskImportBrowserError(fmt.Errorf("path is empty"))
		return
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		foxlabHome, err := lab.FoxlabHome()
		if err != nil {
			a.setDiskImportBrowserError(err)
			return
		}
		home := filepath.Dir(foxlabHome)
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(a.State.DiskImportPath, path)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		a.setDiskImportBrowserError(err)
		return
	}
	info, err := os.Stat(absPath)
	if err != nil {
		a.setDiskImportBrowserError(err)
		return
	}
	if info.IsDir() {
		a.cancelDiskImportPathEdit()
		a.navigateDiskImportBrowser(absPath)
		return
	}
	if !info.Mode().IsRegular() {
		a.setDiskImportBrowserError(fmt.Errorf("path is not a regular file: %s", absPath))
		return
	}
	result := a.diskImport(absPath)
	if result.OK() {
		id := strings.TrimPrefix(result.Message, "imported disk:")
		a.clearDiskExplorerEdit()
		a.selectDiskExplorerID(id)
	}
}

func (a *App) navigateDiskImportBrowser(path string) {
	cleanPath := filepath.Clean(path)
	if cleanPath == a.State.DiskImportPath {
		return
	}
	if _, err := readDiskImportEntries(cleanPath); err != nil {
		a.setDiskImportBrowserError(err)
		return
	}
	a.State.DiskImportPath = cleanPath
	a.State.DiskImportPathEditing = false
	a.State.DiskImportSelected = 0
	a.State.DiskImportScroll = 0
	a.State.DiskImportError = ""
}

func (a *App) setDiskImportBrowserError(err error) {
	if err == nil {
		return
	}
	a.State.DiskImportError = err.Error()
	a.setNotification(Notification{Text: "disk import browser failed: " + err.Error(), Level: NotificationError})
}

func (a *App) clampDiskImportSelection(count int) {
	if count <= 0 {
		a.State.DiskImportSelected = 0
		a.State.DiskImportScroll = 0
		return
	}
	a.State.DiskImportSelected = clamp(a.State.DiskImportSelected, 0, count-1)
	a.State.DiskImportScroll = clamp(a.State.DiskImportScroll, 0, count-1)
}

func (a *App) diskImportVisibleRows() int {
	layout, ok := diskExplorerLayout(a.ViewWidth, a.contentHeight())
	if !ok {
		return 1
	}
	return max(1, diskExplorerVisibleRows(layout))
}

func (a *App) ensureDiskImportSelectionVisible() {
	visible := a.diskImportVisibleRows()
	selected := a.State.DiskImportSelected
	if selected < a.State.DiskImportScroll {
		a.State.DiskImportScroll = selected
	}
	if selected >= a.State.DiskImportScroll+visible {
		a.State.DiskImportScroll = selected - visible + 1
	}
	a.State.DiskImportScroll = max(0, a.State.DiskImportScroll)
}

func (a *App) handleDiskImportBrowserScroll(event mouseEvent) bool {
	if a.State.DiskExplorerEdit != diskExplorerActionImport {
		return false
	}
	if a.State.DiskImportPathEditing {
		return true
	}
	layout, ok := diskExplorerLayout(a.ViewWidth, a.contentHeight())
	if !ok || !xyInRect(event.x, event.y, layout) {
		return false
	}
	entries, err := a.diskImportBrowserEntries()
	if err != nil {
		a.setDiskImportBrowserError(err)
		return true
	}
	delta := 1
	if event.button == 64 {
		delta = -1
	}
	a.State.DiskImportSelected = clamp(a.State.DiskImportSelected+delta, 0, max(0, len(entries)-1))
	a.ensureDiskImportSelectionVisible()
	return true
}

func formatDiskImportSize(size int64) string {
	const (
		kib = int64(1024)
		mib = 1024 * kib
		gib = 1024 * mib
		tib = 1024 * gib
	)
	switch {
	case size >= tib:
		return fmt.Sprintf("%.1fT", float64(size)/float64(tib))
	case size >= gib:
		return fmt.Sprintf("%.1fG", float64(size)/float64(gib))
	case size >= mib:
		return fmt.Sprintf("%.1fM", float64(size)/float64(mib))
	case size >= kib:
		return fmt.Sprintf("%.1fK", float64(size)/float64(kib))
	default:
		return fmt.Sprintf("%dB", size)
	}
}
