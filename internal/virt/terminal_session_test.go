package virt

import "testing"

func TestTerminalSessionEndpointPrefersConsolePath(t *testing.T) {
	if got := terminalSessionEndpoint("vm1", "/dev/pts/7"); got != "/dev/pts/7" {
		t.Fatalf("terminal endpoint = %q", got)
	}
	if got := terminalSessionEndpoint("vm1", ""); got != "vm1" {
		t.Fatalf("fallback terminal endpoint = %q", got)
	}
}
