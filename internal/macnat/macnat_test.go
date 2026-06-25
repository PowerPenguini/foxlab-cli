package macnat

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestConfigureWritesDriverCommand(t *testing.T) {
	device, err := os.CreateTemp(t.TempDir(), "macnat")
	if err != nil {
		t.Fatal(err)
	}
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}
	ctrl := NewController(device.Name())

	err = ctrl.Configure(context.Background(), []Session{{
		LabID:    "demo",
		SwitchID: "external-uplink1",
		Bridge:   "flfoxlabdemou",
		Uplink:   "eth0",
		MACs:     []string{"02:00:00:00:00:10"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(device.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"configure labID=demo switchID=external-uplink1 bridge=flfoxlabdemou uplink=eth0",
		"mac=02:00:00:00:00:10",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("command missing %q:\n%s", want, string(data))
		}
	}
}

func TestAvailableReportsMissingDevice(t *testing.T) {
	err := NewController("/tmp/foxlab-missing-macnat-device").Available()
	if err == nil || !strings.Contains(err.Error(), ModuleName) {
		t.Fatalf("expected missing %s error, got %v", ModuleName, err)
	}
}
