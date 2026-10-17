package core

import "testing"

func TestParseRouteGetOutput(t *testing.T) {
	out := `   route to: 216.236.63.135
destination: 216.236.63.135
    gateway: 192.168.1.1
  interface: en0
      flags: <UP,GATEWAY,HOST,DONE>
`
	gw, iface := parseRouteGetOutput(out)
	if gw != "192.168.1.1" || iface != "en0" {
		t.Fatalf("unexpected gw=%q iface=%q", gw, iface)
	}
	_, iface = parseRouteGetOutput("destination: 128.0.0.0\n  interface: utun4\n")
	if iface != "utun4" {
		t.Fatalf("unexpected iface %q", iface)
	}
}
