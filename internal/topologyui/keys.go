package topologyui

import (
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	bracketedPasteStart = "\x1b[200~"
	bracketedPasteEnd   = "\x1b[201~"
	inputSequenceWait   = 50 * time.Millisecond
)

func readAppKey(a *App) (string, error) {
	if len(a.inputState.pendingKeys) > 0 {
		key := a.inputState.pendingKeys[0]
		a.inputState.pendingKeys = a.inputState.pendingKeys[1:]
		a.inputState.currentRaw = nil
		if len(a.inputState.pendingRaw) > 0 {
			a.inputState.currentRaw = a.inputState.pendingRaw[0]
			a.inputState.pendingRaw = a.inputState.pendingRaw[1:]
		}
		return normalizeAppKey(a, key), nil
	}
	timeout := spinnerInterval
	if len(a.inputState.readBuffer) > 0 && !a.inputState.readDeadline.IsZero() {
		remaining := time.Until(a.inputState.readDeadline)
		if remaining <= 0 {
			events := decodeBufferedKeyEvents(&a.inputState.readBuffer, true, true)
			a.inputState.readDeadline = time.Time{}
			return queueAppKeyEvents(a, events)
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	if ok, err := waitAppReadable(a, timeout); err != nil || !ok {
		if err == nil && len(a.inputState.readBuffer) > 0 && !a.inputState.readDeadline.IsZero() && !time.Now().Before(a.inputState.readDeadline) {
			events := decodeBufferedKeyEvents(&a.inputState.readBuffer, true, true)
			a.inputState.readDeadline = time.Time{}
			return queueAppKeyEvents(a, events)
		}
		return "", err
	}
	var buf [4096]byte
	n, err := unix.Read(int(a.In.Fd()), buf[:])
	if err != nil {
		if err == unix.EINTR || err == unix.EAGAIN {
			return "", nil
		}
		return "", err
	}
	if n == 0 {
		return "", io.EOF
	}
	a.inputState.readBuffer = append(a.inputState.readBuffer, buf[:n]...)
	events := decodeBufferedKeyEvents(&a.inputState.readBuffer, false, true)
	if len(a.inputState.readBuffer) > 0 {
		a.inputState.readDeadline = time.Now().Add(inputSequenceWait)
	} else {
		a.inputState.readDeadline = time.Time{}
	}
	return queueAppKeyEvents(a, events)
}

func queueAppKeyEvents(a *App, events []appKeyEvent) (string, error) {
	if len(events) == 0 {
		return "", nil
	}
	if a.tabs != nil && a.tabs.activeRunningShell() && !a.State.PaletteOpen {
		events = coalesceShellKeyEvents(events, a.inputState.pasteActive)
	}
	a.inputState.currentRaw = events[0].raw
	if len(events) > 1 {
		for _, event := range events[1:] {
			a.inputState.pendingKeys = append(a.inputState.pendingKeys, event.key)
			a.inputState.pendingRaw = append(a.inputState.pendingRaw, event.raw)
		}
	}
	return normalizeAppKey(a, events[0].key), nil
}

func normalizeAppKey(a *App, key string) string {
	if a.tabs != nil && a.tabs.activeRunningShell() {
		return key
	}
	if a.State.ContextEdit || a.State.DiskExplorerOpen || a.State.PaletteOpen {
		return key
	}
	switch key {
	case "char:j":
		return "down"
	case "char:k":
		return "up"
	case "char:h":
		return "left"
	case "char:l":
		return "right"
	case "char: ":
		return "space"
	default:
		return key
	}
}

func decodeBufferedKeyEvents(buffer *[]byte, final, commandMode bool) []appKeyEvent {
	input := *buffer
	complete := completeInputBytes(input, final)
	if complete == 0 {
		return nil
	}
	events := decodeKeyEvents(string(input[:complete]), commandMode)
	*buffer = append((*buffer)[:0], input[complete:]...)
	return events
}

func completeInputBytes(input []byte, final bool) int {
	for index := 0; index < len(input); {
		remaining := input[index:]
		if remaining[0] != '\x1b' {
			if !utf8.FullRune(remaining) && !final {
				return index
			}
			_, size := utf8.DecodeRune(remaining)
			index += max(1, size)
			continue
		}
		if len(remaining) < len(bracketedPasteStart) &&
			(strings.HasPrefix(bracketedPasteStart, string(remaining)) || strings.HasPrefix(bracketedPasteEnd, string(remaining))) && !final {
			return index
		}
		if strings.HasPrefix(string(remaining), bracketedPasteStart) || strings.HasPrefix(string(remaining), bracketedPasteEnd) {
			index += len(bracketedPasteStart)
			continue
		}
		if len(remaining) == 1 {
			if !final {
				return index
			}
			return len(input)
		}
		switch remaining[1] {
		case '[':
			end := -1
			for offset := 2; offset < len(remaining); offset++ {
				if remaining[offset] >= 0x40 && remaining[offset] <= 0x7e {
					end = offset + 1
					break
				}
			}
			if end < 0 {
				if !final {
					return index
				}
				return len(input)
			}
			index += end
		case 'O':
			if len(remaining) < 3 && !final {
				return index
			}
			index += min(3, len(remaining))
		default:
			if !utf8.FullRune(remaining[1:]) && !final {
				return index
			}
			_, size := utf8.DecodeRune(remaining[1:])
			index += 1 + max(1, size)
		}
	}
	return len(input)
}

func coalesceShellKeyEvents(events []appKeyEvent, pasteActive bool) []appKeyEvent {
	if len(events) < 2 {
		return events
	}
	out := make([]appKeyEvent, 0, len(events))
	for index := 0; index < len(events); {
		if pasteActive || events[index].key == "paste-start" {
			if !pasteActive {
				out = append(out, events[index])
				index++
				pasteActive = true
			}
			var raw []byte
			for index < len(events) && events[index].key != "paste-end" {
				raw = append(raw, events[index].raw...)
				index++
			}
			if len(raw) > 0 {
				out = append(out, appKeyEvent{key: "raw", raw: raw})
			}
			if index < len(events) {
				out = append(out, events[index])
				index++
				pasteActive = false
			}
			continue
		}
		if shellContextSwitchKey(events[index].key) {
			out = append(out, events[index:]...)
			break
		}
		if shellGlobalKey(events[index].key) {
			out = append(out, events[index])
			index++
			continue
		}
		var raw []byte
		for index < len(events) && !shellGlobalKey(events[index].key) {
			raw = append(raw, events[index].raw...)
			index++
		}
		out = append(out, appKeyEvent{key: "raw", raw: raw})
	}
	return out
}

func shellContextSwitchKey(key string) bool {
	return key == "ctrl+]" || strings.HasPrefix(key, "alt+") || isMouseKey(key)
}

func shellGlobalKey(key string) bool {
	return key == "ctrl+]" || key == "shift-pageup" || key == "shift-pagedown" ||
		key == "paste-start" || key == "paste-end" || key == "focus-in" || key == "focus-out" ||
		strings.HasPrefix(key, "alt+") || isMouseKey(key)
}

func waitReadable(fd int, timeout time.Duration) (bool, error) {
	ready, _, err := waitReadableWithWake(fd, -1, timeout)
	return ready, err
}

func waitAppReadable(a *App, timeout time.Duration) (bool, error) {
	wakeFD := -1
	if a != nil && a.tabs != nil {
		wakeFD = a.tabs.wakeReadDescriptor()
	}
	ready, woke, err := waitReadableWithWake(int(a.In.Fd()), wakeFD, timeout)
	if woke && a != nil && a.tabs != nil {
		a.tabs.drainWake()
	}
	return ready, err
}

func waitReadableWithWake(fd, wakeFD int, timeout time.Duration) (bool, bool, error) {
	ms := int(timeout / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	if wakeFD >= 0 {
		pollFDs = append(pollFDs, unix.PollFd{Fd: int32(wakeFD), Events: unix.POLLIN})
	}
	n, err := unix.Poll(pollFDs, ms)
	if err != nil {
		if err == unix.EINTR {
			return false, false, nil
		}
		return false, false, err
	}
	if n <= 0 {
		return false, false, nil
	}
	if pollFDs[0].Revents&unix.POLLNVAL != 0 {
		return false, false, unix.EBADF
	}
	inputReady := pollFDs[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0
	woke := len(pollFDs) > 1 && pollFDs[1].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0
	return inputReady, woke, nil
}

func readKey(fd int, commandMode bool) (string, error) {
	keys, err := readKeys(fd, commandMode)
	if err != nil || len(keys) == 0 {
		return "", err
	}
	return keys[0], nil
}

func readKeys(fd int, commandMode bool) ([]string, error) {
	events, err := readKeyEvents(fd, commandMode)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(events))
	for _, event := range events {
		if event.key == "paste-start" || event.key == "paste-end" {
			continue
		}
		keys = append(keys, event.key)
	}
	return keys, nil
}

type appKeyEvent struct {
	key string
	raw []byte
}

func readKeyEvents(fd int, commandMode bool) ([]appKeyEvent, error) {
	var buf [4096]byte
	n, err := unix.Read(fd, buf[:])
	if err != nil {
		if err == unix.EINTR {
			return nil, nil
		}
		return nil, err
	}
	if n == 0 {
		return nil, nil
	}
	if buf[0] == '\x1b' && n < 6 {
		m, err := unix.Read(fd, buf[n:])
		if err != nil && err != unix.EINTR && err != unix.EAGAIN {
			return nil, err
		}
		n += m
	}
	return decodeKeyEvents(string(buf[:n]), commandMode), nil
}

func decodeKeys(seq string, commandMode bool) []string {
	events := decodeKeyEvents(seq, commandMode)
	keys := make([]string, 0, len(events))
	for _, event := range events {
		if event.key == "paste-start" || event.key == "paste-end" {
			continue
		}
		keys = append(keys, event.key)
	}
	return keys
}

func decodeKeyEvents(seq string, commandMode bool) []appKeyEvent {
	var events []appKeyEvent
	for len(seq) > 0 {
		switch {
		case strings.HasPrefix(seq, bracketedPasteStart):
			events = append(events, appKeyEvent{key: "paste-start", raw: []byte(bracketedPasteStart)})
			seq = seq[len(bracketedPasteStart):]
			continue
		case strings.HasPrefix(seq, bracketedPasteEnd):
			events = append(events, appKeyEvent{key: "paste-end", raw: []byte(bracketedPasteEnd)})
			seq = seq[len(bracketedPasteEnd):]
			continue
		}
		if seq[0] == '\x1b' {
			key, size := decodeEscapeKey(seq)
			if key == "" {
				key = "raw"
			}
			events = append(events, appKeyEvent{key: key, raw: []byte(seq[:size])})
			seq = seq[size:]
			continue
		}
		if strings.HasPrefix(seq, "\r\n") {
			events = append(events, appKeyEvent{key: "enter", raw: []byte("\r\n")})
			seq = seq[2:]
			continue
		}
		r, size := utf8.DecodeRuneInString(seq)
		if r == utf8.RuneError && size == 1 {
			events = append(events, appKeyEvent{key: "raw", raw: []byte(seq[:size])})
			seq = seq[size:]
			continue
		}
		key := runeKey(r, commandMode)
		if key == "" {
			key = "raw"
		}
		events = append(events, appKeyEvent{key: key, raw: []byte(seq[:size])})
		seq = seq[size:]
	}
	return events
}

func decodeEscapeKey(seq string) (string, int) {
	switch {
	case strings.HasPrefix(seq, "\x1b[<"):
		return decodeMouseKey(seq)
	case strings.HasPrefix(seq, "\x1b[1;2B"):
		return "shift-down", len("\x1b[1;2B")
	case strings.HasPrefix(seq, "\x1b[1;2A"):
		return "shift-up", len("\x1b[1;2A")
	case strings.HasPrefix(seq, "\x1b[1;2D"):
		return "shift-left", len("\x1b[1;2D")
	case strings.HasPrefix(seq, "\x1b[1;2C"):
		return "shift-right", len("\x1b[1;2C")
	case strings.HasPrefix(seq, "\x1b[B"):
		return "down", len("\x1b[B")
	case strings.HasPrefix(seq, "\x1b[A"):
		return "up", len("\x1b[A")
	case strings.HasPrefix(seq, "\x1b[D"):
		return "left", len("\x1b[D")
	case strings.HasPrefix(seq, "\x1b[C"):
		return "right", len("\x1b[C")
	case strings.HasPrefix(seq, "\x1b[H"):
		return "home", len("\x1b[H")
	case strings.HasPrefix(seq, "\x1b[1~"):
		return "home", len("\x1b[1~")
	case strings.HasPrefix(seq, "\x1b[F"):
		return "end", len("\x1b[F")
	case strings.HasPrefix(seq, "\x1b[4~"):
		return "end", len("\x1b[4~")
	case strings.HasPrefix(seq, "\x1b[3~"):
		return "delete", len("\x1b[3~")
	case strings.HasPrefix(seq, "\x1b[I"):
		return "focus-in", len("\x1b[I")
	case strings.HasPrefix(seq, "\x1b[O"):
		return "focus-out", len("\x1b[O")
	case strings.HasPrefix(seq, "\x1b[5;2~"):
		return "shift-pageup", len("\x1b[5;2~")
	case strings.HasPrefix(seq, "\x1b[6;2~"):
		return "shift-pagedown", len("\x1b[6;2~")
	case strings.HasPrefix(seq, "\x1b:"):
		return "alt+:", len("\x1b:")
	case len(seq) >= 2 && seq[0] == '\x1b' && seq[1] >= '1' && seq[1] <= '9':
		return "alt+" + string(seq[1]), 2
	case len(seq) >= 2 && seq[0] == '\x1b' && strings.ContainsRune("gtT", rune(seq[1])):
		return "alt+" + string(seq[1]), 2
	case seq == "\x1b":
		return "escape", 1
	default:
		if len(seq) >= 2 && seq[0] == '\x1b' && seq[1] != '[' && seq[1] != 'O' {
			return "escape", 1
		}
		return "", consumeEscapeSequence(seq)
	}
}

func decodeMouseKey(seq string) (string, int) {
	end := strings.IndexAny(seq, "Mm")
	if end < 0 {
		return "", consumeEscapeSequence(seq)
	}
	raw := seq[:end+1]
	final := raw[len(raw)-1]
	payload := strings.TrimPrefix(strings.TrimRight(raw, "Mm"), "\x1b[<")
	parts := strings.Split(payload, ";")
	if len(parts) != 3 {
		return "", len(raw)
	}
	button, errB := strconv.Atoi(parts[0])
	x, errX := strconv.Atoi(parts[1])
	y, errY := strconv.Atoi(parts[2])
	if errB != nil || errX != nil || errY != nil || x <= 0 || y <= 0 {
		return "", len(raw)
	}
	buttonName := "mouse"
	if final == 'm' {
		buttonName = "mouse-release"
	} else if button&32 != 0 {
		buttonName = "mouse-drag"
	}
	button &^= 32
	return buttonName + ":" + strconv.Itoa(x-1) + ":" + strconv.Itoa(y-1) + ":" + strconv.Itoa(button), len(raw)
}

func consumeEscapeSequence(seq string) int {
	if len(seq) < 2 {
		return len(seq)
	}
	if seq[1] == '[' {
		for i := 2; i < len(seq); i++ {
			if seq[i] >= 0x40 && seq[i] <= 0x7e {
				return i + 1
			}
		}
		return len(seq)
	}
	if seq[1] == 'O' && len(seq) >= 3 {
		return 3
	}
	return 1
}

func runeKey(r rune, commandMode bool) string {
	switch r {
	case 'j':
		if !commandMode {
			return "down"
		}
	case 'k':
		if !commandMode {
			return "up"
		}
	case 'h':
		if !commandMode {
			return "left"
		}
	case 'l':
		if !commandMode {
			return "right"
		}
	case ' ':
		if !commandMode {
			return "space"
		}
	case '\t':
		return "tab"
	case '\r', '\n':
		return "enter"
	case '\x7f', '\b':
		return "backspace"
	case '\x10':
		return "ctrl+p"
	case '\x03':
		return "quit"
	case '\x1d':
		return "ctrl+]"
	}
	if r < 0x20 || r == 0x7f {
		return "control:" + strconv.Itoa(int(r))
	}
	if r >= 0x20 && r != 0x7f {
		return "char:" + string(r)
	}
	return ""
}
