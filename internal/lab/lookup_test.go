package lab

import "testing"

func TestNetworkLookupsUseCompleteEndpointIdentity(t *testing.T) {
	l := &Lab{
		Switches:      []Switch{{ID: "sw1"}},
		ExternalLinks: []ExternalLink{{ID: "uplink1"}},
		NetworkLinks: []NetworkLink{{
			From: NetworkEndpoint{Type: "vm", ID: "node", NIC: 0},
			To:   NetworkEndpoint{Type: "container", ID: "node", NIC: 0},
		}},
	}
	if _, ok := FindSwitch(l, "sw1"); !ok {
		t.Fatal("switch lookup failed")
	}
	if _, ok := FindExternalLink(l, "uplink1"); !ok {
		t.Fatal("external link lookup failed")
	}
	if _, ok := FindNetworkLinkForEndpoint(l, NetworkEndpoint{Type: "container", ID: "node", NIC: 0}); !ok {
		t.Fatal("network link endpoint lookup failed")
	}
	if SameNetworkEndpoint(
		NetworkEndpoint{Type: "vm", ID: "node", NIC: 0},
		NetworkEndpoint{Type: "container", ID: "node", NIC: 0},
	) {
		t.Fatal("endpoint equality ignored workload type")
	}
	if _, ok := FindNetworkLinkForEndpoint(l, NetworkEndpoint{Type: "vm", ID: "node", NIC: 1}); ok {
		t.Fatal("endpoint lookup ignored NIC index")
	}
}

func TestNetworkLookupsAreNilSafe(t *testing.T) {
	if _, ok := FindSwitch(nil, "sw1"); ok {
		t.Fatal("nil lab returned switch")
	}
	if _, ok := FindExternalLink(nil, "uplink1"); ok {
		t.Fatal("nil lab returned external link")
	}
	if _, ok := FindNetworkLinkForEndpoint(nil, NetworkEndpoint{}); ok {
		t.Fatal("nil lab returned network link")
	}
}
