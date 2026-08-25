package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsRouteCheckpointRoundTrip(t *testing.T) {
	root := t.TempDir()
	b := &Backend{store: &Store{runtimeDir: root}}
	route := &routeContext{
		TunIndex:              19,
		TunAlias:              "sudoku0",
		DefaultGateway:        "192.168.1.1",
		WindowsDefaultIfIndex: 7,
		WindowsDNSBackup:      "sudoku4x4-dns-test.json",
	}
	tun := TunSettings{InterfaceName: "sudoku0", MapDNSEnabled: true, MapDNSAddress: "127.0.0.1"}

	if err := b.saveWindowsRouteCheckpoint(route, tun); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, windowsRouteCheckpointFile))
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("checkpoint is empty")
	}
	var checkpoint windowsRouteCheckpoint
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		t.Fatalf("decode checkpoint: %v", err)
	}
	if checkpoint.Route.TunIndex != route.TunIndex || checkpoint.Route.WindowsDefaultIfIndex != route.WindowsDefaultIfIndex {
		t.Fatalf("unexpected route checkpoint: %#v", checkpoint.Route)
	}
	if checkpoint.Tun.InterfaceName != tun.InterfaceName || checkpoint.Tun.MapDNSAddress != tun.MapDNSAddress {
		t.Fatalf("unexpected TUN checkpoint: %#v", checkpoint.Tun)
	}

	b.removeWindowsRouteCheckpoint()
	if _, err := os.Stat(filepath.Join(root, windowsRouteCheckpointFile)); !os.IsNotExist(err) {
		t.Fatalf("checkpoint was not removed: %v", err)
	}
}

func TestWindowsRouteCheckpointRejectsInvalidInterfaces(t *testing.T) {
	b := &Backend{store: &Store{runtimeDir: t.TempDir()}}
	if err := b.saveWindowsRouteCheckpoint(&routeContext{}, TunSettings{}); err == nil {
		t.Fatal("expected invalid checkpoint to be rejected")
	}
}
