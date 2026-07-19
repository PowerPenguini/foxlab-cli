package topologyui

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cfoust/cy/pkg/emu"
	"github.com/cfoust/cy/pkg/geom"
	cykeys "github.com/cfoust/cy/pkg/keys"
	"golang.org/x/text/unicode/norm"
)

type terminalBuffer struct {
	mu            sync.Mutex
	term          emu.Terminal
	notify        func()
	reply         *terminalReplyWriter
	syncMu        sync.Mutex
	syncTimer     *time.Timer
	pendingEscape bool
	inOSC         bool
	oscDiscard    bool
	oscEscape     bool
	oscBuffer     []byte
}

const maxOSCBytes = 64 << 10

type terminalReplyWriter struct {
	mu     sync.RWMutex
	writer io.Writer
}

func (w *terminalReplyWriter) Write(p []byte) (int, error) {
	w.mu.RLock()
	writer := w.writer
	w.mu.RUnlock()
	if writer == nil {
		return len(p), nil
	}
	return writer.Write(p)
}

func (w *terminalReplyWriter) set(writer io.Writer) {
	w.mu.Lock()
	w.writer = writer
	w.mu.Unlock()
}

type terminalSnapshot struct {
	lines         []emu.Line
	cursorRow     int
	cursorCol     int
	cursorVisible bool
	reverse       bool
}

func newTerminalBuffer(cols, rows, history int, notify func()) *terminalBuffer {
	reply := &terminalReplyWriter{}
	return &terminalBuffer{
		term: emu.New(
			emu.WithSize(geom.Vec2{C: max(1, cols), R: max(1, rows)}),
			emu.WithHistoryLimit(history),
			emu.WithWriter(reply),
		),
		notify: notify,
		reply:  reply,
	}
}

func (b *terminalBuffer) Write(p []byte) (int, error) {
	input := p
	if !norm.NFC.IsNormal(input) {
		input = norm.NFC.Bytes(input)
	}
	b.mu.Lock()
	input = b.sanitizeOutputLocked(input)
	if len(input) == 0 {
		b.mu.Unlock()
		return len(p), nil
	}
	n, syncing, err := writeTerminalSafely(b.term, input)
	b.mu.Unlock()
	b.updateSyncNotification(syncing)
	if err == nil && n == len(input) {
		return len(p), nil
	}
	return min(n, len(p)), err
}

func writeTerminalSafely(term emu.Terminal, input []byte) (n int, syncing bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("terminal parser rejected malformed output: %v", recovered)
		}
	}()
	return term.WriteSync(input)
}

func (b *terminalBuffer) sanitizeOutputLocked(input []byte) []byte {
	if !b.pendingEscape && !b.inOSC && bytes.IndexByte(input, '\x1b') < 0 {
		return input
	}
	out := make([]byte, 0, len(input))
	for _, value := range input {
		if b.inOSC {
			if !b.oscDiscard {
				if len(b.oscBuffer) < maxOSCBytes {
					b.oscBuffer = append(b.oscBuffer, value)
				} else {
					b.oscDiscard = true
					b.oscBuffer = b.oscBuffer[:0]
				}
			}
			terminated := value == '\a' || (value == '\\' && b.oscEscape)
			if terminated {
				if !b.oscDiscard && validOSCSequence(b.oscBuffer) {
					out = append(out, b.oscBuffer...)
				}
				b.inOSC = false
				b.oscDiscard = false
				b.oscEscape = false
				b.oscBuffer = b.oscBuffer[:0]
			} else {
				b.oscEscape = value == '\x1b'
			}
			continue
		}
		if b.pendingEscape {
			b.pendingEscape = false
			if value == ']' {
				b.inOSC = true
				b.oscBuffer = append(b.oscBuffer[:0], '\x1b', ']')
				continue
			}
			out = append(out, '\x1b')
		}
		if value == '\x1b' {
			b.pendingEscape = true
			continue
		}
		out = append(out, value)
	}
	return out
}

func validOSCSequence(sequence []byte) bool {
	if len(sequence) < 4 || sequence[0] != '\x1b' || sequence[1] != ']' {
		return false
	}
	payloadEnd := len(sequence) - 1
	if sequence[payloadEnd] == '\\' {
		if payloadEnd == 0 || sequence[payloadEnd-1] != '\x1b' {
			return false
		}
		payloadEnd--
	}
	payload := sequence[2:payloadEnd]
	separator := bytes.IndexByte(payload, ';')
	command := payload
	if separator >= 0 {
		command = payload[:separator]
	}
	if len(command) == 0 {
		return false
	}
	for _, value := range command {
		if value < '0' || value > '9' {
			return false
		}
	}
	return true
}

func (b *terminalBuffer) updateSyncNotification(syncing bool) {
	b.syncMu.Lock()
	defer b.syncMu.Unlock()
	if !syncing {
		if b.syncTimer != nil {
			b.syncTimer.Stop()
			b.syncTimer = nil
		}
		if b.notify != nil {
			b.notify()
		}
		return
	}
	if b.syncTimer != nil || b.notify == nil {
		return
	}
	b.syncTimer = time.AfterFunc(200*time.Millisecond, func() {
		b.syncMu.Lock()
		b.syncTimer = nil
		notify := b.notify
		b.syncMu.Unlock()
		if notify != nil {
			notify()
		}
	})
}

func (b *terminalBuffer) encodeMouseInput(event mouseEvent) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if event.x < 0 || event.y <= 0 {
		return nil, false
	}
	mode := b.term.Mode()
	tracking := mode & emu.ModeMouseMask
	switch tracking {
	case emu.ModeMouseX10:
		if event.kind != mousePress {
			return nil, false
		}
	case emu.ModeMouseButton:
		if event.kind == mouseDrag {
			return nil, false
		}
	case emu.ModeMouseMotion, emu.ModeMouseMany:
	default:
		return nil, false
	}
	button := event.button
	if mode&emu.ModeMouseSgr != 0 {
		final := 'M'
		if event.kind == mouseDrag {
			button |= 32
		}
		if event.kind == mouseRelease {
			final = 'm'
		}
		// The FoxLab tab bar owns row zero. SGR coordinates are one-based,
		// so event.y already is the correct guest row.
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", button, event.x+1, event.y, final)), true
	}
	if event.x > 222 || event.y > 223 {
		return nil, false
	}
	if event.kind == mouseRelease {
		button = button&^3 | 3
	} else if event.kind == mouseDrag {
		button |= 32
	}
	return []byte{'\x1b', '[', 'M', byte(button + 32), byte(event.x + 33), byte(event.y + 32)}, true
}

func (b *terminalBuffer) acceptsBracketedPaste() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.term.Mode()&emu.ModeBracketedPaste != 0
}

func (b *terminalBuffer) acceptsKeyboardInput() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.term.Mode()&emu.ModeKeyboardLock == 0
}

func (b *terminalBuffer) acceptsFocusInput() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.term.Mode()&emu.ModeFocus != 0
}

func (b *terminalBuffer) acceptsAnyMouseMotion() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.term.Mode()&emu.ModeMouseMask == emu.ModeMouseMany
}

func (b *terminalBuffer) acceptsAppKeypad() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.term.Mode()&emu.ModeAppKeypad != 0
}

func (b *terminalBuffer) encodeKeyInput(key string, raw []byte) []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	mode := b.term.Mode()
	if mode&emu.ModeKeyboardLock != 0 {
		return nil
	}
	protocol := b.term.KeyState()
	if protocol != emu.KeyLegacy {
		return encodeKittyKeyInput(key, raw, mode, protocol)
	}
	if mode&emu.ModeAppCursor != 0 {
		switch string(raw) {
		case "\x1b[A":
			return []byte("\x1bOA")
		case "\x1b[B":
			return []byte("\x1bOB")
		case "\x1b[C":
			return []byte("\x1bOC")
		case "\x1b[D":
			return []byte("\x1bOD")
		case "\x1b[H", "\x1b[1~":
			return []byte("\x1bOH")
		case "\x1b[F", "\x1b[4~":
			return []byte("\x1bOF")
		}
	}
	encoded := append([]byte(nil), raw...)
	if mode&emu.ModeCRLF != 0 {
		encoded = []byte(strings.ReplaceAll(string(encoded), "\r", "\r\n"))
	}
	return encoded
}

func encodeKittyKeyInput(key string, raw []byte, mode emu.ModeFlag, protocol emu.KeyProtocol) []byte {
	if event, ok := semanticKittyKey(key); ok {
		if encoded, ok := event.Bytes(mode, protocol); ok {
			return encoded
		}
	}
	var encoded []byte
	for len(raw) > 0 {
		event, width := cykeys.Read(raw)
		if width <= 0 || width > len(raw) {
			encoded = append(encoded, raw[0])
			raw = raw[1:]
			continue
		}
		keyEvent, ok := event.(cykeys.Key)
		if !ok {
			encoded = append(encoded, raw[:width]...)
			raw = raw[width:]
			continue
		}
		data, ok := keyEvent.Bytes(mode, protocol)
		if !ok {
			data = raw[:width]
		}
		encoded = append(encoded, data...)
		raw = raw[width:]
	}
	return encoded
}

func semanticKittyKey(key string) (cykeys.Key, bool) {
	switch key {
	case "enter":
		return cykeys.Key{Code: cykeys.KittyKeyEnter}, true
	case "tab":
		return cykeys.Key{Code: cykeys.KittyKeyTab}, true
	case "backspace":
		return cykeys.Key{Code: cykeys.KittyKeyBackspace}, true
	case "escape":
		return cykeys.Key{Code: cykeys.KittyKeyEscape}, true
	}
	if value, ok := strings.CutPrefix(key, "char:"); ok {
		runes := []rune(value)
		if len(runes) == 1 {
			return cykeys.Key{Code: runes[0], Text: value}, true
		}
	}
	return cykeys.Key{}, false
}

func (b *terminalBuffer) setReplyWriter(writer io.Writer) {
	b.reply.set(writer)
}

func (b *terminalBuffer) snapshot(cols, rows, scroll int) terminalSnapshot {
	cols = max(1, cols)
	rows = max(1, rows)
	b.mu.Lock()
	defer b.mu.Unlock()
	if size := b.term.Size(); size.C != cols || size.R != rows {
		b.term.Resize(geom.Vec2{C: cols, R: rows})
	}
	history := b.term.History()
	screen := b.term.Screen()
	total := len(history) + len(screen)
	maxScroll := max(0, total-rows)
	scroll = clamp(scroll, 0, maxScroll)
	start := max(0, total-rows-scroll)
	end := min(total, start+rows)
	lines := make([]emu.Line, 0, rows)
	for index := start; index < end; index++ {
		if index < len(history) {
			lines = append(lines, history[index].Clone())
		} else {
			lines = append(lines, screen[index-len(history)].Clone())
		}
	}
	for len(lines) < rows {
		lines = append(lines, make(emu.Line, cols))
	}
	cursor := b.term.Cursor()
	return terminalSnapshot{
		lines:         lines,
		cursorRow:     len(history) + cursor.R - start,
		cursorCol:     cursor.C,
		cursorVisible: scroll == 0 && b.term.CursorVisible(),
		reverse:       b.term.Mode()&emu.ModeReverse != 0,
	}
}

func (a *App) renderShellTabs(width, height int) *grid {
	width = max(0, width)
	height = max(0, height)
	g := newGrid(width, height)
	rows := max(0, height-tabBarHeight)
	a.tabs.mu.Lock()
	tab := a.tabs.tabs[a.tabs.active]
	buffer := tab.buffer
	scroll := tab.scroll
	status := tab.status
	session := tab.session
	resize := tab.cols != width || tab.rows != rows
	tab.cols, tab.rows = width, rows
	a.tabs.mu.Unlock()
	if resize {
		if resizer, ok := session.(interface{ Resize(int, int) }); ok {
			resizer.Resize(width, rows)
		}
	}
	if buffer != nil && width > 0 && rows > 0 {
		snapshot := buffer.snapshot(width, rows, scroll)
		for y, line := range snapshot.lines {
			if y >= rows {
				break
			}
			for x := 0; x < len(line) && x < width; {
				glyph := line[x]
				glyphWidth := max(1, glyph.Width())
				ch := glyph.Char
				if ch == 0 {
					ch = ' '
				}
				style := terminalGlyphStyle(glyph, snapshot.reverse)
				if snapshot.cursorVisible && status == tabStatusRunning && y == snapshot.cursorRow && snapshot.cursorCol >= x && snapshot.cursorCol < x+glyphWidth {
					if terminalGlyphInverted(glyph, snapshot.reverse) {
						style += "\x1b[27m"
					} else {
						style += ansiInverse
					}
				}
				g.Set(x, y+tabBarHeight, ch, style)
				for continuation := 1; continuation < glyphWidth && x+continuation < width; continuation++ {
					index := (y+tabBarHeight)*g.Width + x + continuation
					g.Cells[index].Ch = 0
					g.Cells[index].Style = style
				}
				x += glyphWidth
			}
		}
	}
	if a.State.PaletteOpen {
		drawPalette(g, a.Model, a.State, width, height)
	}
	a.drawTabBar(g)
	applyTerminalBackground(g)
	return g
}

func terminalGlyphStyle(glyph emu.Glyph, globalReverse bool) string {
	style := themeTerminal
	fg, bg := glyph.FG, glyph.BG
	style += terminalColorStyle(fg, false)
	style += terminalColorStyle(bg, true)
	if terminalGlyphInverted(glyph, globalReverse) {
		style += ansiInverse
	}
	if glyph.Mode&emu.AttrBold != 0 {
		style += ansiBold
	}
	if glyph.Mode&emu.AttrItalic != 0 {
		style += "\x1b[3m"
	}
	if glyph.Mode&emu.AttrStrikethrough != 0 {
		style += "\x1b[9m"
	}
	if glyph.Mode&emu.AttrBlink != 0 {
		style += "\x1b[5m"
	}
	switch glyph.Underline.Mode {
	case emu.UnderlineSingle:
		style += "\x1b[4:1m"
	case emu.UnderlineDouble:
		style += "\x1b[4:2m"
	case emu.UnderlineCurly:
		style += "\x1b[4:3m"
	case emu.UnderlineDotted:
		style += "\x1b[4:4m"
	case emu.UnderlineDashed:
		style += "\x1b[4:5m"
	}
	if glyph.Underline.Mode != emu.UnderlineNone && !glyph.Underline.Color.Default() {
		style += terminalUnderlineColorStyle(glyph.Underline.Color)
	}
	return style
}

func terminalGlyphInverted(glyph emu.Glyph, globalReverse bool) bool {
	if glyph.Mode&emu.AttrReverse != 0 && glyph.FG.Default() && glyph.BG.Default() {
		return !globalReverse
	}
	return globalReverse
}

func terminalColorStyle(color emu.Color, background bool) string {
	if color.Default() {
		return ""
	}
	prefix := 38
	if background {
		prefix = 48
	}
	if value, ok := color.ANSI(); ok {
		if value < 8 {
			return fmt.Sprintf("\x1b[%dm", prefix-8+value)
		}
		return fmt.Sprintf("\x1b[%dm", prefix+44+value)
	}
	if value, ok := color.XTerm(); ok {
		return fmt.Sprintf("\x1b[%d;5;%dm", prefix, value)
	}
	if r, g, b, ok := color.RGB(); ok {
		return fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", prefix, r, g, b)
	}
	return ""
}

func terminalUnderlineColorStyle(color emu.Color) string {
	if value, ok := color.ANSI(); ok {
		return fmt.Sprintf("\x1b[58;5;%dm", value)
	}
	if value, ok := color.XTerm(); ok {
		return fmt.Sprintf("\x1b[58;5;%dm", value)
	}
	if r, g, b, ok := color.RGB(); ok {
		return fmt.Sprintf("\x1b[58;2;%d;%d;%dm", r, g, b)
	}
	return ""
}

func copyCanvas(dst, src *grid, dx, dy int) {
	if dst == nil || src == nil {
		return
	}
	for y := 0; y < src.Height; y++ {
		for x := 0; x < src.Width; x++ {
			tx, ty := x+dx, y+dy
			if tx < 0 || ty < 0 || tx >= dst.Width || ty >= dst.Height {
				continue
			}
			dst.Cells[ty*dst.Width+tx] = src.Cells[y*src.Width+x]
		}
	}
}
