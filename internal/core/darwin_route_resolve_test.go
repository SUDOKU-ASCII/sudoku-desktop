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

func TestDarwinHasDefaultRouteOnInterface(t *testing.T) {
	routes := []darwinNetstatRoute{
		{Destination: "default", Gateway: "192.168.1.1", Netif: "en0"},
		{Destination: "default", Gateway: "link#20", Netif: "utun6"},
	}
	if !darwinHasDefaultRouteOnInterface(routes, "en0") {
		t.Fatalf("expected default route on en0")
	}
	if darwinHasDefaultRouteOnInterface(routes, "en1") {
		t.Fatalf("unexpected default route on en1")
	}
}

func TestDarwinHasDefaultRouteOnTunLikeInterface(t *testing.T) {
	routes := []darwinNetstatRoute{
		{Destination: "default", Gateway: "192.168.1.1", Netif: "en0"},
		{Destination: "default", Gateway: "link#20", Netif: "utun6"},
	}
	if !darwinHasDefaultRouteOnTunLikeInterface(routes) {
		t.Fatalf("expected default route on tunnel interface")
	}
}

func TestDarwinHasUnscopedDefaultRouteIPv4(t *testing.T) {
	routes := []darwinNetstatRoute{
		{Destination: "default", Gateway: "10.0.0.1", Flags: "UGScIg", Netif: "en0"},
	}
	if darwinHasUnscopedDefaultRouteIPv4(routes, "utun6") {
		t.Fatalf("unexpected unscoped default route")
	}

	routes = append(routes, darwinNetstatRoute{Destination: "default", Gateway: "10.0.0.1", Flags: "UGScg", Netif: "en0"})
	if !darwinHasUnscopedDefaultRouteIPv4(routes, "utun6") {
		t.Fatalf("expected unscoped default route")
	}

	if darwinHasUnscopedDefaultRouteIPv4(routes, "en0") {
		t.Fatalf("unexpected unscoped default route when excluded")
	}
}
