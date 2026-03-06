package core

import (
	"strings"
	"testing"
)

func TestBuildWindowsRouteScriptPinsPhysicalDefaultRoute(t *testing.T) {
	script := buildWindowsRouteScript(
		true,
		"1.1.1.1",
		"4x4-sudoku Block QUIC (UDP/443)",
		true,
		19,
		"192.168.1.1",
		12,
		true,
		"127.0.0.1",
		"sudoku4x4-dns-test.json",
	)

	for _, needle := range []string{
		"$gw4 = '192.168.1.1'",
		"$if4 = 12",
		"New-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $tunIf -NextHop '0.0.0.0' -RouteMetric 1 -PolicyStore ActiveStore",
		"route.exe add 0.0.0.0 mask 0.0.0.0 0.0.0.0 metric 1 if $tunIf",
		"Set-NetIPInterface -InterfaceIndex $tunIf -AutomaticMetric Disabled -InterfaceMetric 1",
		"if (-not $gw4 -or -not $if4 -or $if4 -le 0)",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("script missing %q", needle)
		}
	}
}

func TestBuildWindowsRouteScriptRestoresDefaultWithInterface(t *testing.T) {
	script := buildWindowsRouteScript(
		false,
		"",
		"4x4-sudoku Block QUIC (UDP/443)",
		false,
		21,
		"10.0.0.1",
		7,
		false,
		"",
		"",
	)

	for _, needle := range []string{
		"Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $tunIf -PolicyStore ActiveStore",
		"Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue",
		"Set-NetIPInterface -InterfaceIndex $tunIf -AutomaticMetric Enabled",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("script missing %q", needle)
		}
	}
}
