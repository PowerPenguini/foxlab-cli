package virt

import (
	"errors"
	"testing"
)

func TestConsolePTYFromDomainXMLUsesSourcePath(t *testing.T) {
	path, ok := consolePTYFromDomainXML(`<domain><devices><console type="pty" tty="/dev/pts/4"><source path="/dev/pts/5"/></console></devices></domain>`)
	if !ok {
		t.Fatal("console pty not found")
	}
	if path != "/dev/pts/5" {
		t.Fatalf("path = %q", path)
	}
}

func TestConsolePTYFromDomainXMLFallsBackToTTY(t *testing.T) {
	path, ok := consolePTYFromDomainXML(`<domain><devices><console type="pty" tty="/dev/pts/4"/></devices></domain>`)
	if !ok {
		t.Fatal("console pty not found")
	}
	if path != "/dev/pts/4" {
		t.Fatalf("path = %q", path)
	}
}

func TestConsolePTYFromDomainXMLRejectsMissingPTY(t *testing.T) {
	if path, ok := consolePTYFromDomainXML(`<domain><devices><console type="tcp"/></devices></domain>`); ok {
		t.Fatalf("unexpected path = %q", path)
	}
}

func TestConsoleReadRetriesTemporaryRecvErrors(t *testing.T) {
	calls := 0
	console := &Console{
		recv: func(p []byte) (int, error) {
			calls++
			if calls < 3 {
				return 0, errors.New("temporarily unavailable")
			}
			return copy(p, []byte("boot\n")), nil
		},
		done: make(chan struct{}),
	}
	defer close(console.done)

	buf := make([]byte, 16)
	n, err := console.Read(buf)
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if got := string(buf[:n]); got != "boot\n" {
		t.Fatalf("Read = %q, want boot output", got)
	}
	if calls < 3 {
		t.Fatalf("Recv calls = %d, want polling retries", calls)
	}
}
