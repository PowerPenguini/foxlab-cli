package topologyui

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	ansiReset            = "\x1b[0m"
	ansiInverse          = "\x1b[7m"
	ansiCyan             = "\x1b[36m"
	ansiBrightCyan       = "\x1b[96m"
	ansiMagenta          = "\x1b[35m"
	ansiBrightMagenta    = "\x1b[95m"
	ansiBlack            = "\x1b[30m"
	ansiWhite            = "\x1b[97m"
	ansiBgCyan           = "\x1b[46m"
	ansiBgGreen          = "\x1b[42m"
	ansiBgRed            = "\x1b[41m"
	ansiBgYellow         = "\x1b[43m"
	ansiBgGray           = "\x1b[100m"
	ansiYellow           = "\x1b[33m"
	ansiDim              = "\x1b[2m"
	ansiBold             = "\x1b[1m"
	ansiClear            = "\x1b[2J\x1b[H"
	ansiHide             = "\x1b[?25l"
	ansiShow             = "\x1b[?25h"
	ansiMoveHome         = "\x1b[H"
	ansiEnterAltScreen   = "\x1b[?1049h"
	ansiExitAltScreen    = "\x1b[?1049l"
	ansiEnableMouse      = "\x1b[?1003l\x1b[?1002h\x1b[?1006h"
	ansiDisableMouse     = "\x1b[?1003l\x1b[?1002l\x1b[?1006l"
	ansiEnablePaste      = "\x1b[?2004h"
	ansiDisablePaste     = "\x1b[?2004l"
	ansiEnableFocus      = "\x1b[?1004h"
	ansiDisableFocus     = "\x1b[?1004l"
	ansiEnableAppKeypad  = "\x1b="
	ansiDisableAppKeypad = "\x1b>"
	ansiPushLegacyKeys   = "\x1b[>0u"
	ansiPopKeys          = "\x1b[<u"
)

func startAppTerminalSession(a *App) (func(), error) {
	signal.Ignore(syscall.SIGINT)
	cleanup, err := startTerminalSession(int(a.In.Fd()), a.Out)
	if err != nil {
		signal.Reset(syscall.SIGINT)
		return nil, err
	}
	a.inputState.hostMouseMany = false
	a.inputState.hostAppKeypad = false
	return cleanup, nil
}

func (a *App) syncHostTerminalModes() {
	wantMouseMany := false
	wantAppKeypad := false
	if a.tabs != nil {
		a.tabs.mu.Lock()
		if a.tabs.active >= 0 && a.tabs.active < len(a.tabs.tabs) {
			tab := a.tabs.tabs[a.tabs.active]
			if tab.kind != tabKindLab && tab.status == tabStatusRunning && tab.buffer != nil {
				wantMouseMany = tab.buffer.acceptsAnyMouseMotion()
				wantAppKeypad = tab.buffer.acceptsAppKeypad()
			}
		}
		a.tabs.mu.Unlock()
	}
	if wantMouseMany != a.inputState.hostMouseMany {
		a.inputState.hostMouseMany = wantMouseMany
		if wantMouseMany {
			_, _ = io.WriteString(a.Out, "\x1b[?1003h")
		} else {
			_, _ = io.WriteString(a.Out, "\x1b[?1003l")
		}
	}
	if wantAppKeypad != a.inputState.hostAppKeypad {
		a.inputState.hostAppKeypad = wantAppKeypad
		if wantAppKeypad {
			_, _ = io.WriteString(a.Out, ansiEnableAppKeypad)
		} else {
			_, _ = io.WriteString(a.Out, ansiDisableAppKeypad)
		}
	}
}

func startTerminalSession(inFD int, out io.Writer) (func(), error) {
	restoreRaw, err := makeRaw(inFD)
	if err != nil {
		return nil, err
	}
	_, _ = io.WriteString(out, ansiEnterAltScreen+ansiPushLegacyKeys+ansiDisableAppKeypad+ansiEnableMouse+ansiEnablePaste+ansiEnableFocus+ansiHide+ansiClear)
	return func() {
		_, _ = io.WriteString(out, ansiDisableFocus+ansiDisablePaste+ansiDisableMouse+ansiDisableAppKeypad+ansiPopKeys+ansiShow+ansiReset+ansiExitAltScreen)
		signal.Reset(syscall.SIGINT)
		restoreRaw()
	}, nil
}

func appTerminalSize(a *App) (int, int) {
	return terminalSize(int(a.Out.Fd()))
}

func terminalSize(fd int) (int, int) {
	if ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ); err == nil && ws.Col > 0 && ws.Row > 0 {
		return int(ws.Col), int(ws.Row)
	}
	width := envInt("COLUMNS", 100)
	height := envInt("LINES", 30)
	return width, height
}

func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func makeRaw(fd int) (func(), error) {
	return makeRawWithReadMode(fd, 0, 1)
}

func makeBlockingRaw(fd int) (func(), error) {
	return makeRawWithReadMode(fd, 1, 0)
}

func makeShellRaw(fd int) (func(), error) {
	return makeShellRawWithReadMode(fd, 0, 1)
}

func makeShellBlockingRaw(fd int) (func(), error) {
	return makeShellRawWithReadMode(fd, 1, 0)
}

func makeRawWithReadMode(fd int, min, timeout uint8) (func(), error) {
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, fmt.Errorf("raw terminal mode: %w", err)
	}
	next := *old
	next.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	next.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.IEXTEN | unix.ISIG
	next.Cflag &^= unix.CSIZE | unix.PARENB
	next.Cflag |= unix.CS8
	next.Cc[unix.VMIN] = min
	next.Cc[unix.VTIME] = timeout
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &next); err != nil {
		return nil, fmt.Errorf("raw terminal mode: %w", err)
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, unix.TCSETS, old)
	}, nil
}

func makeShellRawWithReadMode(fd int, min, timeout uint8) (func(), error) {
	return makeRawWithReadMode(fd, min, timeout)
}
