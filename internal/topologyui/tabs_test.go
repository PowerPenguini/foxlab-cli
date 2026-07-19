package topologyui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cfoust/cy/pkg/emu"

	containerdruntime "foxlab-cli/internal/containerd"
	"foxlab-cli/internal/lab"
)

type recordingTabSession struct {
	bytes.Buffer
	closed bool
}

func (s *recordingTabSession) Close() error {
	s.closed = true
	return nil
}

func testAppWithShellTab(t *testing.T, status tabStatus) (*App, *appTab, *recordingTabSession) {
	t.Helper()
	app := &App{Lab: &lab.Lab{ID: "demo"}}
	app.ensureTabs()
	session := &recordingTabSession{}
	tab := &appTab{
		key:     "container:kali",
		kind:    tabKindContainer,
		nodeID:  "kali",
		label:   "CT: kali",
		status:  status,
		session: session,
	}
	if status == tabStatusExited {
		tab.session = nil
	}
	tab.buffer = newTerminalBuffer(40, 8, terminalScrollback, app.tabs.notify)
	app.tabs.tabs = append(app.tabs.tabs, tab)
	app.tabs.active = 1
	return app, tab, session
}

func TestTabsStartWithPinnedLabCard(t *testing.T) {
	app := &App{Lab: &lab.Lab{ID: "training"}}
	app.ensureTabs()
	if len(app.tabs.tabs) != 1 {
		t.Fatalf("tabs = %d, want one", len(app.tabs.tabs))
	}
	if got := app.tabs.tabs[0].label; got != "Lab: training" {
		t.Fatalf("lab tab = %q", got)
	}
	app.closeTab(0)
	if len(app.tabs.tabs) != 1 {
		t.Fatal("pinned Lab tab was closed")
	}
}

func TestTerminalUpdateWakesBlockedInputPoll(t *testing.T) {
	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer inputWriter.Close()
	app := &App{In: input, Lab: &lab.Lab{ID: "demo"}}
	app.ensureTabs()
	if err := app.tabs.openWakePipe(); err != nil {
		t.Fatal(err)
	}
	defer app.tabs.closeWakePipe()

	type pollResult struct {
		ready bool
		err   error
	}
	result := make(chan pollResult, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		ready, err := waitAppReadable(app, 5*time.Second)
		result <- pollResult{ready: ready, err: err}
	}()
	<-started
	app.tabs.notify()

	select {
	case got := <-result:
		if got.err != nil || got.ready {
			t.Fatalf("wake poll ready=%v err=%v, want update wake without stdin", got.ready, got.err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("terminal update did not wake the blocked input poll")
	}
}

func TestShellInputDoesNotRenderStaleFrame(t *testing.T) {
	app, _, session := testAppWithShellTab(t, tabStatusRunning)
	app.Out = tempOutputFile(t)
	start := func(*App) (func(), error) { return func() {}, nil }
	step := 0
	read := func(app *App) (string, error) {
		step++
		switch step {
		case 1:
			app.inputState.currentRaw = []byte("x")
			return "char:x", nil
		default:
			app.inputState.currentRaw = nil
			return "quit", nil
		}
	}
	if err := app.runInteractive(start, read, func(*App) (int, int) { return 80, 20 }); err != nil {
		t.Fatal(err)
	}
	if got := session.String(); got != "x" {
		t.Fatalf("shell input = %q, want x", got)
	}
	if count := strings.Count(outputFileString(t, app.Out), ansiMoveHome); count != 1 {
		t.Fatalf("render count = %d, want only the initial frame", count)
	}
}

func TestInitialShellSizeUsesCurrentContentViewport(t *testing.T) {
	app := &App{Lab: &lab.Lab{ID: "demo"}, ViewWidth: 132, ViewHeight: 41}
	app.ensureTabs()
	cols, rows := app.initialShellSize()
	if cols != 132 || rows != 40 {
		t.Fatalf("initial shell size = %dx%d, want 132x40", cols, rows)
	}
}

func TestOneFrameIncludesLabTab(t *testing.T) {
	var out bytes.Buffer
	if err := OneFrameForLab(&out, MockModel(), &lab.Lab{ID: "training"}, 80, 20); err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(out.String(), "\n")
	if !strings.Contains(first, "Lab: training") {
		t.Fatalf("first row = %q", first)
	}
}

func TestRunningShellReceivesRawInputAndCtrlBracketReturnsToLab(t *testing.T) {
	app, _, session := testAppWithShellTab(t, tabStatusRunning)
	if !app.handleTabInput("down", []byte("j")) {
		t.Fatal("running shell input was not handled")
	}
	if got := session.String(); got != "j" {
		t.Fatalf("raw input = %q, want j", got)
	}
	if !app.handleTabInput("ctrl+]", []byte{0x1d}) {
		t.Fatal("Ctrl-] was not handled")
	}
	if app.tabs.active != 0 {
		t.Fatalf("active tab = %d, want Lab", app.tabs.active)
	}
	if session.closed {
		t.Fatal("Ctrl-] closed the background session")
	}
}

func TestAltColonOpensPaletteOverRunningShell(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	app.Model = MockModel()
	app.ViewWidth = 80
	app.ViewHeight = 20
	_, _ = io.WriteString(tab.buffer, "shell remains visible")

	if !app.handleTabInput("alt+:", []byte("\x1b:")) {
		t.Fatal("Alt+: was not handled by FoxLab")
	}
	if !app.State.PaletteOpen || app.tabs.active != 1 {
		t.Fatalf("palette open=%v active tab=%d, want palette over shell tab 1", app.State.PaletteOpen, app.tabs.active)
	}
	if session.Len() != 0 {
		t.Fatalf("Alt+: leaked to guest: %q", session.String())
	}
	g := app.renderShellTabs(app.ViewWidth, app.ViewHeight)
	if rendered := g.String(false); !strings.Contains(rendered, "shell remains visible") || !strings.Contains(rendered, ":") {
		t.Fatalf("shell palette render is missing background or prompt:\n%s", rendered)
	}

	if app.handleTabInput("char:d", []byte("d")) {
		t.Fatal("palette query was consumed by the shell input router")
	}
	app.handleKey("char:d")
	if app.State.PaletteQuery != "d" || session.Len() != 0 {
		t.Fatalf("palette query=%q guest input=%q, want d and no guest input", app.State.PaletteQuery, session.String())
	}
	if app.handleTabInput("escape", []byte("\x1b")) {
		t.Fatal("palette Escape was consumed by the shell input router")
	}
	app.handleKey("escape")
	if app.State.PaletteOpen || app.tabs.active != 1 {
		t.Fatalf("Escape left palette open=%v active=%d, want same shell tab", app.State.PaletteOpen, app.tabs.active)
	}
	if !app.handleTabInput("char::", []byte(":")) || session.String() != ":" {
		t.Fatalf("plain colon did not reach guest: handled input=%q", session.String())
	}
}

func TestShellPaletteOwnsPasteAndMouseInput(t *testing.T) {
	app, _, session := testAppWithShellTab(t, tabStatusRunning)
	app.Model = MockModel()
	app.ViewWidth = 80
	app.ViewHeight = 20
	app.handleTabInput("alt+:", []byte("\x1b:"))

	app.handleTabInput("paste-start", []byte(bracketedPasteStart))
	if app.handleTabInput("char:d", []byte("d")) {
		t.Fatal("palette paste character was sent to the shell")
	}
	app.handleKey("char:d")
	app.handleTabInput("paste-end", []byte(bracketedPasteEnd))
	if app.State.PaletteQuery != "d" || session.Len() != 0 {
		t.Fatalf("palette paste query=%q guest input=%q", app.State.PaletteQuery, session.String())
	}

	app.State.PaletteQuery = ""
	layout, ok := paletteLayout(app.ViewWidth, app.ViewHeight)
	if !ok {
		t.Fatal("shell palette layout unavailable")
	}
	click := fmt.Sprintf("mouse:%d:%d:0", layout.X+2, paletteRowsY(layout))
	if app.handleTabInput(click, []byte("mouse")) {
		t.Fatal("palette mouse input was sent to the shell router")
	}
	app.handleKey(click)
	if app.State.PaletteQuery != "add" || session.Len() != 0 {
		t.Fatalf("mouse selection query=%q guest input=%q", app.State.PaletteQuery, session.String())
	}
}

func TestShellPaletteKeepsBatchedTextAsUIKeyEvents(t *testing.T) {
	app, _, session := testAppWithShellTab(t, tabStatusRunning)
	app.handleTabInput("alt+:", []byte("\x1b:"))
	key, err := queueAppKeyEvents(app, decodeKeyEvents("disk", true))
	if err != nil || key != "char:d" {
		t.Fatalf("first palette key=%q err=%v, want char:d", key, err)
	}
	for {
		if app.handleTabInput(key, app.inputState.currentRaw) {
			t.Fatalf("palette key %q was consumed by shell input", key)
		}
		app.handleKey(key)
		if len(app.inputState.pendingKeys) == 0 {
			break
		}
		key, err = readAppKey(app)
		if err != nil {
			t.Fatal(err)
		}
	}
	if app.State.PaletteQuery != "disk" || session.Len() != 0 {
		t.Fatalf("palette query=%q guest input=%q, want disk and no guest input", app.State.PaletteQuery, session.String())
	}
}

func TestBatchedInputAfterCtrlBracketUsesNewTabContext(t *testing.T) {
	app, _, _ := testAppWithShellTab(t, tabStatusRunning)
	events := decodeKeyEvents("\x1dj", true)
	key, err := queueAppKeyEvents(app, events)
	if err != nil || key != "ctrl+]" {
		t.Fatalf("first batched key=%q err=%v", key, err)
	}
	if !app.handleTabInput(key, app.inputState.currentRaw) || app.tabs.active != 0 {
		t.Fatal("Ctrl-] did not switch to Lab")
	}
	key, err = readAppKey(app)
	if err != nil || key != "down" {
		t.Fatalf("post-switch key=%q err=%v, want Lab down", key, err)
	}
}

func TestShellMouseInputRequiresGuestMouseMode(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	click := "mouse:10:5:0"
	if !app.handleTabInput(click, []byte("\x1b[<0;11;6M")) {
		t.Fatal("shell mouse input was not handled")
	}
	if session.Len() != 0 {
		t.Fatalf("plain shell received mouse protocol: %q", session.String())
	}

	_, _ = io.WriteString(tab.buffer, "\x1b[?1000h\x1b[?1006h")
	if !app.handleTabInput(click, []byte("\x1b[<0;11;6M")) {
		t.Fatal("guest mouse input was not handled")
	}
	if got := session.String(); got != "\x1b[<0;11;5M" {
		t.Fatalf("guest mouse protocol = %q", got)
	}
}

func TestTabBarOwnsModifiedAndReleaseMouseEvents(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "\x1b[?1003h\x1b[?1006h")
	app.drawTabBar(newGrid(80, 5))
	if !app.handleTabInput("mouse:2:0:4", []byte("\x1b[<4;3;1M")) {
		t.Fatal("modified tab click was not handled")
	}
	if !app.handleTabInput("mouse-release:2:0:0", []byte("\x1b[<0;3;1m")) {
		t.Fatal("tab release was not handled")
	}
	if session.Len() != 0 {
		t.Fatalf("tab-bar mouse leaked to guest: %q", session.String())
	}
}

func TestShellMouseInputSupportsLegacyX10Encoding(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "\x1b[?1000h")
	app.handleTabInput("mouse:10:5:0", []byte("\x1b[<0;11;6M"))
	want := []byte{'\x1b', '[', 'M', 32, 43, 37}
	if got := session.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("legacy mouse protocol = %v, want %v", got, want)
	}
}

func TestHostMouseMotionFollowsGuestMode(t *testing.T) {
	app, tab, _ := testAppWithShellTab(t, tabStatusRunning)
	out, err := os.CreateTemp(t.TempDir(), "mouse-mode")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	app.Out = out
	_, _ = io.WriteString(tab.buffer, "\x1b[?1003h")
	app.syncHostTerminalModes()
	app.activateTab(0)
	app.syncHostTerminalModes()
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "\x1b[?1003h\x1b[?1003l" {
		t.Fatalf("host mouse transitions = %q", got)
	}
}

func TestHostApplicationKeypadFollowsGuestMode(t *testing.T) {
	app, tab, _ := testAppWithShellTab(t, tabStatusRunning)
	out, err := os.CreateTemp(t.TempDir(), "keypad-mode")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	app.Out = out
	_, _ = io.WriteString(tab.buffer, ansiEnableAppKeypad)
	app.syncHostTerminalModes()
	app.activateTab(0)
	app.syncHostTerminalModes()
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != ansiEnableAppKeypad+ansiDisableAppKeypad {
		t.Fatalf("host keypad transitions = %q", got)
	}
}

func TestShellReceivesControlAndUnknownKeyBytes(t *testing.T) {
	app, _, session := testAppWithShellTab(t, tabStatusRunning)
	for _, event := range decodeKeyEvents("\x03\x04\x1a\x1b[15~", false) {
		if !app.handleTabInput(event.key, event.raw) {
			t.Fatalf("key %q was not handled", event.key)
		}
	}
	if got := session.Bytes(); !bytes.Equal(got, []byte("\x03\x04\x1a\x1b[15~")) {
		t.Fatalf("shell bytes = %q", got)
	}
}

func TestCRLFIsOneUIKeyButPreservesShellBytes(t *testing.T) {
	events := decodeKeyEvents("\r\n", true)
	if len(events) != 1 || events[0].key != "enter" || string(events[0].raw) != "\r\n" {
		t.Fatalf("CRLF events = %#v", events)
	}
	app, _, session := testAppWithShellTab(t, tabStatusRunning)
	key, err := queueAppKeyEvents(app, events)
	if err != nil || !app.handleTabInput(key, app.inputState.currentRaw) {
		t.Fatalf("shell CRLF key=%q err=%v", key, err)
	}
	if got := session.String(); got != "\r\n" {
		t.Fatalf("shell CRLF = %q", got)
	}
}

func TestBracketedPasteMarkersFollowGuestMode(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	if !app.handleTabInput("paste-start", []byte(bracketedPasteStart)) {
		t.Fatal("paste start was not handled")
	}
	if session.Len() != 0 {
		t.Fatalf("plain shell received paste marker: %q", session.String())
	}
	_, _ = io.WriteString(tab.buffer, "\x1b[?2004h")
	app.handleTabInput("paste-start", []byte(bracketedPasteStart))
	app.handleTabInput("char:x", []byte("x"))
	app.handleTabInput("paste-end", []byte(bracketedPasteEnd))
	if got := session.String(); got != bracketedPasteStart+"x"+bracketedPasteEnd {
		t.Fatalf("bracketed paste = %q", got)
	}
}

func TestBracketedPastePayloadCannotTriggerFoxLabShortcuts(t *testing.T) {
	app, _, session := testAppWithShellTab(t, tabStatusRunning)
	app.handleTabInput("paste-start", []byte(bracketedPasteStart))
	app.handleTabInput("alt+1", []byte("\x1b1"))
	app.handleTabInput("ctrl+]", []byte{0x1d})
	app.handleTabInput("paste-end", []byte(bracketedPasteEnd))
	if app.tabs.active != 1 {
		t.Fatalf("pasted shortcut switched to tab %d", app.tabs.active)
	}
	if got := session.String(); got != "\x1b1\x1d" {
		t.Fatalf("literal paste payload = %q", got)
	}
}

func TestBracketedPasteDoesNotTriggerLabShortcuts(t *testing.T) {
	app := &App{Lab: &lab.Lab{ID: "demo"}, Model: MockModel(), State: ViewState{Focus: FocusGraph}}
	app.ensureTabs()
	selected := app.State.Selected
	app.handleTabInput("paste-start", []byte(bracketedPasteStart))
	if !app.handleTabInput("down", []byte("j")) {
		t.Fatal("pasted Lab shortcut was not suppressed")
	}
	app.handleTabInput("paste-end", []byte(bracketedPasteEnd))
	if app.State.Selected != selected {
		t.Fatalf("paste moved Lab selection from %d to %d", selected, app.State.Selected)
	}
}

func TestBracketedPasteIntoPaletteCannotSubmitOrCloseIt(t *testing.T) {
	app := &App{Lab: &lab.Lab{ID: "demo"}, State: ViewState{PaletteOpen: true}}
	app.ensureTabs()
	app.handleTabInput("paste-start", []byte(bracketedPasteStart))
	if app.handleTabInput("char:x", []byte("x")) {
		t.Fatal("pasted text was swallowed")
	}
	app.handleKey("char:x")
	if !app.handleTabInput("enter", []byte("\n")) {
		t.Fatal("pasted newline was not contained")
	}
	if !app.handleTabInput("escape", []byte("\x1b")) {
		t.Fatal("pasted escape was not contained")
	}
	if app.handleTabInput("char:y", []byte("y")) {
		t.Fatal("pasted text was swallowed")
	}
	app.handleKey("char:y")
	app.handleTabInput("paste-end", []byte(bracketedPasteEnd))
	if !app.State.PaletteOpen || app.State.PaletteQuery != "x y" {
		t.Fatalf("palette open=%v query=%q", app.State.PaletteOpen, app.State.PaletteQuery)
	}
}

func TestApplicationCursorModeRewritesArrowKeys(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "\x1b[?1h")
	app.handleTabInput("up", []byte("\x1b[A"))
	if got := session.String(); got != "\x1bOA" {
		t.Fatalf("application cursor input = %q", got)
	}
	session.Reset()
	app.handleTabInput("home", []byte("\x1b[1~"))
	app.handleTabInput("end", []byte("\x1b[4~"))
	if got := session.String(); got != "\x1bOH\x1bOF" {
		t.Fatalf("application home/end input = %q", got)
	}
}

func TestTerminalInputModesRewriteNewlineAndLockKeyboard(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "\x1b[20h")
	app.handleTabInput("enter", []byte("\r"))
	if got := session.String(); got != "\r\n" {
		t.Fatalf("newline mode input = %q", got)
	}

	session.Reset()
	_, _ = io.WriteString(tab.buffer, "\x1b[2h")
	app.handleTabInput("char:x", []byte("x"))
	app.handleTabInput("paste-start", []byte(bracketedPasteStart))
	app.handleTabInput("char:y", []byte("y"))
	app.handleTabInput("paste-end", []byte(bracketedPasteEnd))
	if session.Len() != 0 {
		t.Fatalf("keyboard-locked terminal received %q", session.String())
	}
}

func TestKittyKeyboardProtocolEncodesNegotiatedInput(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "\x1b[=1u")
	app.handleTabInput("escape", []byte("\x1b"))
	app.handleTabInput("quit", []byte{0x03})
	app.handleTabInput("enter", []byte("\r"))
	if got := session.String(); got != "\x1b[27u\x1b[99;5u\r" {
		t.Fatalf("Kitty disambiguated input = %q", got)
	}

	session.Reset()
	_, _ = io.WriteString(tab.buffer, "\x1b[=9u")
	app.handleTabInput("char:x", []byte("x"))
	if got := session.String(); got != "\x1b[120u" {
		t.Fatalf("Kitty all-keys input = %q", got)
	}
}

func TestRestartCreatesFreshTerminalState(t *testing.T) {
	app, tab, _ := testAppWithShellTab(t, tabStatusExited)
	_, _ = io.WriteString(tab.buffer, "old\x1b[2h\x1b[?1003h")
	old := tab.buffer
	// Avoid starting a real backend; the reset is independent of workload kind.
	tab.kind = tabKind("test")
	app.restartActiveTab()
	fresh := tab.buffer

	if fresh == old {
		t.Fatal("restart reused terminal emulator")
	}
	if got := fresh.encodeKeyInput("char:x", []byte("x")); string(got) != "x" {
		t.Fatalf("fresh terminal inherited keyboard lock: %q", got)
	}
	if fresh.acceptsAnyMouseMotion() {
		t.Fatal("fresh terminal inherited mouse tracking")
	}
}

func TestBufferedDecoderKeepsSplitPasteAndUTF8Sequences(t *testing.T) {
	buffer := []byte("\x1b[20")
	if events := decodeBufferedKeyEvents(&buffer, false, true); len(events) != 0 {
		t.Fatalf("partial paste marker decoded as %#v", events)
	}
	buffer = append(buffer, []byte("0~zaż")...)
	last := buffer[len(buffer)-1]
	buffer = buffer[:len(buffer)-1]
	events := decodeBufferedKeyEvents(&buffer, false, true)
	if len(events) != 3 || events[0].key != "paste-start" || events[1].key != "char:z" || events[2].key != "char:a" {
		t.Fatalf("complete prefix events = %#v", events)
	}
	if len(buffer) == 0 {
		t.Fatal("split UTF-8 suffix was not retained")
	}
	buffer = append(buffer, last)
	events = decodeBufferedKeyEvents(&buffer, false, true)
	if len(events) != 1 || events[0].key != "char:ż" || len(buffer) != 0 {
		t.Fatalf("completed UTF-8 events=%#v remainder=%q", events, buffer)
	}
}

func TestFocusEventsFollowGuestMode(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	app.handleTabInput("focus-in", []byte("\x1b[I"))
	if session.Len() != 0 {
		t.Fatalf("plain shell received focus event: %q", session.String())
	}
	_, _ = io.WriteString(tab.buffer, "\x1b[?1004h")
	app.handleTabInput("focus-in", []byte("\x1b[I"))
	app.handleTabInput("focus-out", []byte("\x1b[O"))
	if got := session.String(); got != "\x1b[I\x1b[O" {
		t.Fatalf("focus events = %q", got)
	}
}

func TestTerminalQueriesReplyToSession(t *testing.T) {
	buffer := newTerminalBuffer(40, 8, terminalScrollback, nil)
	session := &recordingTabSession{}
	buffer.setReplyWriter(session)
	_, _ = io.WriteString(buffer, "\x1b[6n")
	if got := session.String(); got != "\x1b[1;1R" {
		t.Fatalf("cursor report = %q", got)
	}
}

func TestMalformedOSCDoesNotCrashTerminalParser(t *testing.T) {
	buffer := newTerminalBuffer(40, 8, terminalScrollback, nil)
	if _, err := buffer.Write([]byte("\x1b]\a")); err != nil {
		t.Fatalf("malformed OSC = %v", err)
	}
	if _, err := buffer.Write([]byte("still alive")); err != nil {
		t.Fatalf("output after malformed OSC = %v", err)
	}
	snapshot := buffer.snapshot(40, 8, 0)
	if got := string(snapshot.lines[0][0].Char); got != "s" {
		t.Fatalf("terminal did not recover, first char = %q", got)
	}
}

func TestSplitOSCIsBufferedAndValidated(t *testing.T) {
	buffer := newTerminalBuffer(40, 8, terminalScrollback, nil)
	_, _ = buffer.Write([]byte("\x1b]0;split"))
	_, _ = buffer.Write([]byte(" title\aok"))
	if title := buffer.term.Title(); title != "split title" {
		t.Fatalf("split OSC title = %q", title)
	}
	snapshot := buffer.snapshot(40, 8, 0)
	if snapshot.lines[0][0].Char != 'o' || snapshot.lines[0][1].Char != 'k' {
		t.Fatalf("text after split OSC = %q%q", snapshot.lines[0][0].Char, snapshot.lines[0][1].Char)
	}
}

func TestSynchronizedTerminalUpdatesAreCoalesced(t *testing.T) {
	notifications := make(chan struct{}, 4)
	buffer := newTerminalBuffer(40, 8, terminalScrollback, func() { notifications <- struct{}{} })
	_, _ = io.WriteString(buffer, "\x1b[?2026h")
	_, _ = io.WriteString(buffer, "partial")
	select {
	case <-notifications:
		t.Fatal("synchronized partial frame triggered an immediate render")
	default:
	}
	_, _ = io.WriteString(buffer, "\x1b[?2026l")
	select {
	case <-notifications:
	default:
		t.Fatal("completed synchronized frame did not trigger a render")
	}
}

func TestTerminalReverseVideoAndWideGlyphRendering(t *testing.T) {
	glyph := emu.EmptyGlyph()
	glyph.Mode = emu.AttrReverse
	if style := terminalGlyphStyle(glyph, false); !strings.Contains(style, ansiInverse) {
		t.Fatalf("default reverse style = %q", style)
	}
	if style := terminalGlyphStyle(glyph, true); strings.Contains(style, ansiInverse) {
		t.Fatalf("double reverse style = %q", style)
	}

	app, tab, _ := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "界x")
	rows := strings.Split(app.renderShellTabs(20, 4).String(false), "\n")
	if len(rows) < 2 || !strings.HasPrefix(rows[1], "界x") {
		t.Fatalf("wide glyph row = %q", rows[1])
	}
}

func TestTerminalNormalizesDecomposedUnicode(t *testing.T) {
	app, tab, _ := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "e\u0301")
	row := canvasRow(app.renderShellTabs(20, 4), tabBarHeight)
	if !strings.HasPrefix(row, "é") {
		t.Fatalf("decomposed Unicode row = %q", row)
	}
}

func TestCursorOnWideGlyphContinuationRemainsVisible(t *testing.T) {
	app, tab, _ := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "界\x1b[D")
	g := app.renderShellTabs(20, 4)
	cell := g.Cells[g.Width*tabBarHeight]
	if cell.Ch != '界' || !strings.Contains(cell.Style, ansiInverse) {
		t.Fatalf("wide cursor cell = %#v", cell)
	}
}

func TestGlobalReverseVideoAffectsRenderedTerminal(t *testing.T) {
	app, tab, _ := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "\x1b[?5hX")
	g := app.renderShellTabs(20, 4)
	cell := g.Cells[g.Width*tabBarHeight]
	if cell.Ch != 'X' || !strings.Contains(cell.Style, ansiInverse) {
		t.Fatalf("global reverse cell = %#v", cell)
	}
}

func TestTerminalPreservesUnderlineShapeAndColor(t *testing.T) {
	glyph := emu.EmptyGlyph()
	glyph.Underline = emu.UnderlineStyle{Mode: emu.UnderlineCurly, Color: emu.RGBColor(12, 34, 56)}
	style := terminalGlyphStyle(glyph, false)
	if !strings.Contains(style, "\x1b[4:3m") || !strings.Contains(style, "\x1b[58;2;12;34;56m") {
		t.Fatalf("curly underline style = %q", style)
	}
}

func TestTabNavigationAndCloseControls(t *testing.T) {
	app, _, session := testAppWithShellTab(t, tabStatusExited)
	app.activateTab(0)
	if !app.handleTabInput("char:g", []byte("g")) || !app.handleTabInput("char:t", []byte("t")) {
		t.Fatal("gt chord was not handled")
	}
	if app.tabs.active != 1 {
		t.Fatalf("active tab after gt = %d", app.tabs.active)
	}
	if !app.handleTabInput("char:x", []byte("x")) {
		t.Fatal("x did not close ended tab")
	}
	if len(app.tabs.tabs) != 1 || app.tabs.active != 0 {
		t.Fatalf("tabs after close = %d active=%d", len(app.tabs.tabs), app.tabs.active)
	}
	if session.closed {
		t.Fatal("already-ended tab unexpectedly closed a live session")
	}
}

func TestNaturalSessionExitClosesBackendHandle(t *testing.T) {
	app, tab, session := testAppWithShellTab(t, tabStatusRunning)
	app.finishTabSession(tab, tab.generation, nil)
	if !session.closed {
		t.Fatal("natural session exit leaked backend handle")
	}
	if tab.status != tabStatusExited || tab.session != nil {
		t.Fatalf("ended tab status=%q session=%v", tab.status, tab.session)
	}
}

func TestClosingContainerTabWaitsForRuntimeCleanup(t *testing.T) {
	app, tab, _ := testAppWithShellTab(t, tabStatusRunning)
	reader, writer := io.Pipe()
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	session := newTabPipeSession(writer, cancel, make(chan containerdruntime.ShellSize, 1))
	tab.session = session
	cleaned := make(chan struct{})
	go func() {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		close(cleaned)
		session.markFinished()
	}()

	app.closeTab(1)
	select {
	case <-cleaned:
	default:
		t.Fatal("closeTab returned before container runtime cleanup")
	}
}

func TestCloseTabsInvalidatesLateSessionCompletion(t *testing.T) {
	app, tab, _ := testAppWithShellTab(t, tabStatusRunning)
	generation := tab.generation
	app.closeTabs()
	app.finishTabSession(tab, generation, errors.New("late"))
	if tab.status != tabStatusRunning {
		t.Fatalf("late completion changed closed tab status to %q", tab.status)
	}
}

func TestClosingStartingVMTabCancelsConsoleConnection(t *testing.T) {
	app := &App{Lab: &lab.Lab{ID: "demo"}}
	app.ensureTabs()
	started := make(chan struct{})
	canceled := make(chan struct{})
	app.VMConsole = func(ctx context.Context, _ *lab.Lab, _ string) (io.ReadWriteCloser, string, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, "", ctx.Err()
	}
	tab := &appTab{key: "vm:test", kind: tabKindVM, nodeID: "test", label: "VM: test", status: tabStatusStarting}
	tab.buffer = newTerminalBuffer(40, 8, terminalScrollback, func() { app.tabs.markActivity(tab) })
	app.tabs.tabs = append(app.tabs.tabs, tab)
	app.tabs.active = 1
	app.startTabSession(tab)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("VM console connection did not start")
	}
	app.closeTab(1)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("closing VM tab did not cancel console connection")
	}
}

func TestTabBarIsAlwaysFirstRowAndShellOutputStartsBelowIt(t *testing.T) {
	app, tab, _ := testAppWithShellTab(t, tabStatusRunning)
	_, _ = io.WriteString(tab.buffer, "hello")
	g := app.renderShellTabs(60, 8)
	firstRow := canvasRow(g, 0)
	if !strings.Contains(firstRow, "Lab: demo") || !strings.Contains(firstRow, "CT: kali") {
		t.Fatalf("tab row = %q", firstRow)
	}
	if got := canvasRow(g, 1); !strings.HasPrefix(got, "hello") {
		t.Fatalf("first shell row = %q", got)
	}
	if len(app.tabs.hits) != 2 || app.tabs.hits[0].closeX >= 0 || app.tabs.hits[1].closeX < 0 {
		t.Fatalf("tab hit targets = %#v", app.tabs.hits)
	}
}

func TestTabCommandsAcceptIndexAndUnquotedLabel(t *testing.T) {
	app, _, _ := testAppWithShellTab(t, tabStatusExited)
	app.executeCommand("tabprev")
	if app.tabs.active != 0 {
		t.Fatalf("tabprev active = %d", app.tabs.active)
	}
	app.executeCommand("tabclose CT: kali")
	if len(app.tabs.tabs) != 1 {
		t.Fatalf("tabclose left %d tabs", len(app.tabs.tabs))
	}
}

func TestDecodeTabControlSequences(t *testing.T) {
	got := decodeKeys("\x1b1\x1b:\x1b[5;2~\x1b[6;2~\x1d", false)
	want := []string{"alt+1", "alt+:", "shift-pageup", "shift-pagedown", "ctrl+]"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keys = %#v, want %#v", got, want)
	}
}

func TestDecodePreservesPasteMarkersAndUnsupportedSequencesAsRawEvents(t *testing.T) {
	events := decodeKeyEvents(bracketedPasteStart+"ą"+bracketedPasteEnd+"\x1b[15~", true)
	if len(events) != 4 {
		t.Fatalf("events = %#v", events)
	}
	if events[0].key != "paste-start" || events[1].key != "char:ą" || events[2].key != "paste-end" || events[3].key != "raw" {
		t.Fatalf("event keys = %#v", events)
	}
	var raw []byte
	for _, event := range events {
		raw = append(raw, event.raw...)
	}
	if got, want := string(raw), bracketedPasteStart+"ą"+bracketedPasteEnd+"\x1b[15~"; got != want {
		t.Fatalf("raw events = %q, want %q", got, want)
	}
}

func canvasRow(g *grid, y int) string {
	var out strings.Builder
	for x := 0; x < g.Width; x++ {
		out.WriteRune(g.Cells[y*g.Width+x].Ch)
	}
	return out.String()
}
