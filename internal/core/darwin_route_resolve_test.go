//go:build darwin

package core

import "testing"

func TestDarwinPickPhysicalDefaultRouteIPv4(t *testing.T) {
	routes := []darwinNetstatRoute{
		{Destination: "default", Gateway: "link#20", Netif: "utun6"},
		{Destination: "default", Gateway: "192.168.1.1", Netif: "en0"},
		{Destination: "default", Gateway: "192.168.1.2", Netif: "utun0"},
	}
	gw, ifName := darwinPickPhysicalDefaultRouteIPv4(routes)
	if gw != "192.168.1.1" || ifName != "en0" {
		t.Fatalf("unexpected pick: gw=%q if=%q", gw, ifName)
	}
}
