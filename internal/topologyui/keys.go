package topologyui

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	bracketedPasteStart = "\x1b[200~"
	bracketedPasteEnd   = "\x1b[201~"
)

func readAppKey(a *App) (string, error) {
	if len(a.pendingKeys) > 0 {
		key := a.pendingKeys[0]
		a.pendingKeys = a.pendingKeys[1:]
		return key, nil
	}
	if ok, err := waitReadable(int(a.In.Fd()), spinnerInterval); err != nil || !ok {
		return "", err
	}
	keys, err := readKeys(int(a.In.Fd()), a.State.ContextEdit || a.State.DiskExplorerOpen)
	if err != nil || len(keys) == 0 {
		return "", err
	}
	if len(keys) > 1 {
		a.pendingKeys = append(a.pendingKeys, keys[1:]...)
	}
	return keys[0], nil
}

func waitReadable(fd int, timeout time.Duration) (bool, error) {
	ms := int(timeout / time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(pollFDs, ms)
	if err != nil {
		if err == unix.EINTR {
			return false, nil
		}
		return false, err
	}
	if n <= 0 {
		return false, nil
	}
	return pollFDs[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0, nil
}

func readKey(fd int, commandMode bool) (string, error) {
	keys, err := readKeys(fd, commandMode)
	if err != nil || len(keys) == 0 {
		return "", err
	}
	return keys[0], nil
}

func readKeys(fd int, commandMode bool) ([]string, error) {
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
	return decodeKeys(string(buf[:n]), commandMode), nil
}

func decodeKeys(seq string, commandMode bool) []string {
	var keys []string
	for len(seq) > 0 {
		switch {
		case strings.HasPrefix(seq, bracketedPasteStart):
			seq = seq[len(bracketedPasteStart):]
			continue
		case strings.HasPrefix(seq, bracketedPasteEnd):
			seq = seq[len(bracketedPasteEnd):]
			continue
		}
		if seq[0] == '\x1b' {
			key, size := decodeEscapeKey(seq)
			if key != "" {
				keys = append(keys, key)
			}
			seq = seq[size:]
			continue
		}
		r, size := utf8.DecodeRuneInString(seq)
		if r == utf8.RuneError && size == 1 {
			seq = seq[size:]
			continue
		}
		if key := runeKey(r, commandMode); key != "" {
			keys = append(keys, key)
		}
		seq = seq[size:]
	}
	return keys
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
	case seq == "\x1b":
		return "escape", 1
	default:
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
	}
	if r >= 0x20 && r != 0x7f {
		return "char:" + string(r)
	}
	return ""
}
