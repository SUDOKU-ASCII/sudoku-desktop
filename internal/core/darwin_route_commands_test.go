package core

import (
	"strings"
	"testing"
)

func TestDarwinTunnelRoutesPreservePhysicalDefault(t *testing.T) {
	add := strings.Join(darwinAddTunnelRouteCommands("utun7"), "\n")
	if strings.Contains(add, "change default") || strings.Contains(add, "delete default") {
		t.Fatalf("split-route setup must not replace the physical default route:\n%s", add)
	}
	for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if !strings.Contains(add, prefix) {
			t.Fatalf("missing split route %s:\n%s", prefix, add)
		}
	}

	physical := strings.Join(darwinAddPhysicalBypassRouteCommands("en0", "192.168.1.1"), "\n")
	for _, expected := range []string{"add -net -ifscope en0", "0.0.0.0/1", "128.0.0.0/1", "192.168.1.1"} {
		if !strings.Contains(physical, expected) {
			t.Fatalf("physical bypass route missing %q:\n%s", expected, physical)
		}
	}

	remove := strings.Join(darwinDeleteTunnelRouteCommands("utun7"), "\n")
	if strings.Contains(remove, "change default") || strings.Contains(remove, "ipconfig") {
		t.Fatalf("split-route teardown must not reset the physical network:\n%s", remove)
	}

	physicalRemove := strings.Join(darwinDeletePhysicalBypassRouteCommands("en0", "192.168.1.1"), "\n")
	if !strings.Contains(physicalRemove, "-ifscope en0") {
		t.Fatalf("physical bypass teardown must remove scoped routes:\n%s", physicalRemove)
	}
}
