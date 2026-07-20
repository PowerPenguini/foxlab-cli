package daemoncontrol

import (
	"strings"
	"testing"
)

func TestSystemdUnitDataQuotesBinaryPath(t *testing.T) {
	data := string(systemdUnitData("/opt/Fox Lab/foxlabd"))
	want := `ExecStart="/opt/Fox Lab/foxlabd" --lab ${FOXLAB_LAB} --status-socket ${FOXLAB_STATUS_SOCKET}`
	if !strings.Contains(data, want) {
		t.Fatalf("unit data missing %q:\n%s", want, data)
	}
}

func TestSystemdDropInDataKeepsEnvironmentValuesSeparate(t *testing.T) {
	data := string(systemdDropInData("/tmp/fox lab/demo.lab", "/tmp/fox lab/status.sock", "/home/demo", "demo"))
	for _, want := range []string{
		`Environment="FOXLAB_LAB=/tmp/fox lab/demo.lab"`,
		`Environment="FOXLAB_STATUS_SOCKET=/tmp/fox lab/status.sock"`,
		`Environment="HOME=/home/demo"`,
		`Environment="SUDO_USER=demo"`,
	} {
		if !strings.Contains(data, want) {
			t.Fatalf("drop-in data missing %q:\n%s", want, data)
		}
	}
}
