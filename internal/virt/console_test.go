package virt

import "testing"

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
