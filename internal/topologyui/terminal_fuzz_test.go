package topologyui

import (
	"bytes"
	"testing"
)

func FuzzDecodeKeyEventsPreservesRawBytes(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("plain text"),
		[]byte("\x00\x03\x1d\x7f"),
		[]byte("\x1b[A\x1b[15~\x1b[<0;4;2M"),
		[]byte(bracketedPasteStart + "zażółć\r\n\x1b1" + bracketedPasteEnd),
		{0xff, 0xc3, 0x28, 0x1b, '['},
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		events := decodeKeyEvents(string(input), true)
		var raw []byte
		for _, event := range events {
			if event.key == "" || len(event.raw) == 0 {
				t.Fatalf("empty event for input %x: %#v", input, event)
			}
			raw = append(raw, event.raw...)
		}
		if !bytes.Equal(raw, input) {
			t.Fatalf("raw round trip: input=%x output=%x events=%#v", input, raw, events)
		}
	})
}

func FuzzBufferedDecoderFinalFlushesAllInput(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("\x1b"),
		[]byte("\x1b["),
		[]byte("\x1b[200"),
		[]byte{0xf0, 0x9f, 0x92},
		[]byte("\x1b[<32;10;20"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		buffer := append([]byte(nil), input...)
		events := decodeBufferedKeyEvents(&buffer, true, true)
		if len(buffer) != 0 {
			t.Fatalf("final decoder retained %x from %x", buffer, input)
		}
		var raw []byte
		for _, event := range events {
			raw = append(raw, event.raw...)
		}
		if !bytes.Equal(raw, input) {
			t.Fatalf("final round trip: input=%x output=%x", input, raw)
		}
	})
}

func FuzzBufferedDecoderPreservesArbitrarySplit(f *testing.F) {
	f.Add([]byte(bracketedPasteStart+"zażółć"+bracketedPasteEnd), uint16(3))
	f.Add([]byte("\x1b[<32;10;20Mtext"), uint16(8))
	f.Add([]byte{0xf0, 0x9f, 0x92, 0xa5, 0x1b, '[', 'A'}, uint16(2))
	f.Fuzz(func(t *testing.T, input []byte, requested uint16) {
		split := 0
		if len(input) > 0 {
			split = int(requested) % (len(input) + 1)
		}
		buffer := append([]byte(nil), input[:split]...)
		events := decodeBufferedKeyEvents(&buffer, false, true)
		buffer = append(buffer, input[split:]...)
		events = append(events, decodeBufferedKeyEvents(&buffer, true, true)...)
		if len(buffer) != 0 {
			t.Fatalf("split decoder retained %x", buffer)
		}
		var raw []byte
		for _, event := range events {
			raw = append(raw, event.raw...)
		}
		if !bytes.Equal(raw, input) {
			t.Fatalf("split=%d input=%x output=%x", split, input, raw)
		}
	})
}

func FuzzTerminalBufferAcceptsArbitraryOutput(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("hello\r\n"),
		[]byte("\x1b[?1049h\x1b[2J界\x1b[?1049l"),
		[]byte("\x1b[38;2;1;2;3mcolor\x1b[0m"),
		[]byte("\x1b]0;title\a\x1b[?2026hpartial\x1b[?2026l"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, output []byte) {
		buffer := newTerminalBuffer(17, 5, 64, nil)
		if _, err := buffer.Write(output); err != nil {
			t.Fatalf("terminal write %x: %v", output, err)
		}
		snapshot := buffer.snapshot(17, 5, 0)
		if len(snapshot.lines) != 5 {
			t.Fatalf("snapshot lines=%d", len(snapshot.lines))
		}
	})
}
