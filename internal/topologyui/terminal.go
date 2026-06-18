package topologyui

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	ansiReset          = "\x1b[0m"
	ansiInverse        = "\x1b[7m"
	ansiCyan           = "\x1b[36m"
	ansiBrightCyan     = "\x1b[96m"
	ansiWhite          = "\x1b[97m"
	ansiBgCyan         = "\x1b[46m"
	ansiBgRed          = "\x1b[41m"
	ansiBgGray         = "\x1b[100m"
	ansiYellow         = "\x1b[33m"
	ansiDim            = "\x1b[2m"
	ansiBold           = "\x1b[1m"
	ansiClear          = "\x1b[2J\x1b[H"
	ansiHide           = "\x1b[?25l"
	ansiShow           = "\x1b[?25h"
	ansiMoveHome       = "\x1b[H"
	ansiEnterAltScreen = "\x1b[?1049h"
	ansiExitAltScreen  = "\x1b[?1049l"
)

func startAppTerminalSession(a *App) (func(), error) {
	signal.Ignore(syscall.SIGINT)
	return startTerminalSession(int(a.In.Fd()), a.Out)
}

func startTerminalSession(inFD int, out io.Writer) (func(), error) {
	restoreRaw, err := makeRaw(inFD)
	if err != nil {
		return nil, err
	}
	_, _ = io.WriteString(out, ansiEnterAltScreen+ansiHide+ansiClear)
	return func() {
		_, _ = io.WriteString(out, ansiShow+ansiReset+ansiExitAltScreen)
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
	next.Iflag &^= unix.ICRNL | unix.IXON
	next.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN
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
	old, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, fmt.Errorf("raw terminal mode: %w", err)
	}
	next := *old
	next.Iflag &^= unix.ICRNL | unix.IXON
	next.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	next.Cc[unix.VMIN] = min
	next.Cc[unix.VTIME] = timeout
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &next); err != nil {
		return nil, fmt.Errorf("raw terminal mode: %w", err)
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, unix.TCSETS, old)
	}, nil
}
