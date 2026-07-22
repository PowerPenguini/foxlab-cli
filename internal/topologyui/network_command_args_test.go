package topologyui

import (
	"testing"

	"foxlab-cli/internal/topology"
)

func TestSwitchRequestsPreserveAliasesPrecedenceAndExplicitClears(t *testing.T) {
	request, err := switchCreateRequest("fallback", map[string]string{
		"name":         "lan",
		"mode":         "bridge",
		"uplink":       "wan",
		"external":     "old-uplink",
		"externallink": "legacy-uplink",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Name != "lan" || request.Mode != "bridge" || request.Uplink != "wan" {
		t.Fatalf("switch create request = %#v", request)
	}

	update, err := switchUpdateRequest(map[string]string{
		"name":     "",
		"mode":     "",
		"uplink":   "",
		"external": "wan",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !update.Name.Set || update.Name.Value != "" || !update.Mode.Set || update.Mode.Value != "" {
		t.Fatalf("switch clear update = %#v", update)
	}
	if !update.AttachUplink.Set || update.AttachUplink.Value != "wan" {
		t.Fatalf("switch uplink update = %#v", update.AttachUplink)
	}

	clear, err := switchUpdateRequest(map[string]string{"externallink": ""})
	if err != nil {
		t.Fatal(err)
	}
	if !clear.AttachUplink.Set || clear.AttachUplink.Value != "" {
		t.Fatalf("switch uplink clear = %#v", clear.AttachUplink)
	}
}

func TestExternalRequestsPreserveFieldsAndExplicitClears(t *testing.T) {
	request, err := externalCreateRequest("fallback", map[string]string{
		"name":      "wan",
		"interface": "eth0",
		"mode":      "nat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Name != "wan" || request.Interface != "eth0" || request.Mode != "nat" {
		t.Fatalf("external create request = %#v", request)
	}

	update, err := externalUpdateRequest(map[string]string{"name": "", "interface": "", "mode": ""})
	if err != nil {
		t.Fatal(err)
	}
	if !update.Name.Set || update.Name.Value != "" ||
		!update.Interface.Set || update.Interface.Value != "" ||
		!update.Mode.Set || update.Mode.Value != "" {
		t.Fatalf("external clear update = %#v", update)
	}
}

func TestNICRequestsParseIndexesAndEndpointAliases(t *testing.T) {
	add, err := nicAddRequest(NodeVM, map[string]string{"mac": "02:00:00:00:00:01"})
	if err != nil {
		t.Fatal(err)
	}
	if add.MAC != "02:00:00:00:00:01" {
		t.Fatalf("nic add request = %#v", add)
	}

	request, err := nicConnectRequest(NodeVM, " 2 ", map[string]string{
		"to":  "lan",
		"mac": "02:00:00:00:00:02",
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.NIC != 2 || request.MAC != "02:00:00:00:00:02" ||
		request.Endpoint.Type != topology.NetworkEndpointAuto || request.Endpoint.ID != "lan" {
		t.Fatalf("nic connect request = %#v", request)
	}

	switchRequest, err := nicConnectRequest(NodeContainer, "0", map[string]string{"switch": "lan"})
	if err != nil {
		t.Fatal(err)
	}
	if switchRequest.Endpoint.Type != topology.NetworkEndpointSwitch || switchRequest.Endpoint.ID != "lan" {
		t.Fatalf("switch alias request = %#v", switchRequest)
	}

	uplinkRequest, err := nicConnectRequest(NodeContainer, "1", map[string]string{
		"uplink":   "wan",
		"external": "old-uplink",
	})
	if err != nil {
		t.Fatal(err)
	}
	if uplinkRequest.Endpoint.Type != topology.NetworkEndpointUplink || uplinkRequest.Endpoint.ID != "wan" {
		t.Fatalf("uplink alias request = %#v", uplinkRequest)
	}
}

func TestDirectNetworkEndpointParsesTypedOptionalNIC(t *testing.T) {
	endpoint, ok := directNetworkEndpoint(NodeVM, "vm1", " 3 ")
	if !ok || endpoint.Type != topology.NetworkEndpointVM || endpoint.ID != "vm1" ||
		!endpoint.NIC.Set || endpoint.NIC.Value != 3 {
		t.Fatalf("direct endpoint = %#v, ok = %t", endpoint, ok)
	}

	autoNIC, ok := directNetworkEndpoint(NodeContainer, "web", "")
	if !ok || autoNIC.Type != topology.NetworkEndpointContainer || autoNIC.NIC.Set {
		t.Fatalf("direct auto-nic endpoint = %#v, ok = %t", autoNIC, ok)
	}

	if _, ok := directNetworkEndpoint("pod", "pod1", "0"); ok {
		t.Fatal("unsupported endpoint type accepted")
	}
	if _, ok := directNetworkEndpoint(NodeVM, "vm1", "-1"); ok {
		t.Fatal("negative nic index accepted")
	}
}

func TestNetworkRequestErrorsRemainStable(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "sorted unsupported switch create argument",
			run: func() error {
				_, err := switchCreateRequest("lan", map[string]string{"zzz": "1", "aaa": "2"})
				return err
			},
			want: "unsupported switch create argument: aaa",
		},
		{
			name: "unsupported switch set argument",
			run: func() error {
				_, err := switchUpdateRequest(map[string]string{"mod": "bridge"})
				return err
			},
			want: "unsupported switch set argument: mod",
		},
		{
			name: "unsupported uplink create argument",
			run: func() error {
				_, err := externalCreateRequest("wan", map[string]string{"iface": "eth0"})
				return err
			},
			want: "unsupported uplink create argument: iface",
		},
		{
			name: "unsupported uplink set argument",
			run: func() error {
				_, err := externalUpdateRequest(map[string]string{"iface": "eth0"})
				return err
			},
			want: "unsupported uplink set argument: iface",
		},
		{
			name: "unsupported nic add argument",
			run: func() error {
				_, err := nicAddRequest(NodeVM, map[string]string{"address": "mac"})
				return err
			},
			want: "unsupported vm nic add argument: address",
		},
		{
			name: "invalid nic index",
			run: func() error {
				_, err := nicConnectRequest(NodeVM, "bad", map[string]string{"to": "lan"})
				return err
			},
			want: "usage: vm nic connect <id> <index> to=ID",
		},
		{
			name: "canonical and compatibility endpoint conflict",
			run: func() error {
				_, err := nicConnectRequest(NodeContainer, "0", map[string]string{"to": "lan", "switch": "lan"})
				return err
			},
			want: "container nic connect accepts to=ID or a compatibility alias, not both",
		},
		{
			name: "multiple compatibility endpoints",
			run: func() error {
				_, err := nicConnectRequest(NodeVM, "0", map[string]string{"switch": "lan", "uplink": "wan"})
				return err
			},
			want: "vm nic connect needs exactly one endpoint",
		},
		{
			name: "missing endpoint",
			run: func() error {
				_, err := nicConnectRequest(NodeVM, "0", nil)
				return err
			},
			want: "vm nic connect needs exactly one endpoint",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
