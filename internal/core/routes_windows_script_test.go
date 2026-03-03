package core

import (
	"strings"
	"testing"
)

func TestBuildWindowsRouteScriptPinsPhysicalDefaultRoute(t *testing.T) {
	script := buildWindowsRouteScript(
		true,
		"1.1.1.1",
		"",
		"",
		"4x4-sudoku Block QUIC (UDP/443)",
		true,
		19,
		"192.168.1.1",
		12,
		"fe80::1",
		13,
		true,
		"127.0.0.1",
		"sudoku4x4-dns-test.json",
	)

	for _, needle := range []string{
		"$gw4 = '192.168.1.1'",
		"$if4 = 12",
		"$gw6 = 'fe80::1'",
		"$if6 = 13",
		"route.exe change 0.0.0.0 mask 0.0.0.0 0.0.0.0 if $tunIf",
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
		"",
		"",
		"4x4-sudoku Block QUIC (UDP/443)",
		false,
		21,
		"10.0.0.1",
		7,
		"",
		0,
		false,
		"",
		"",
	)

	if !strings.Contains(script, "route.exe change 0.0.0.0 mask 0.0.0.0 $gw4 if $if4") {
		t.Fatalf("script should restore default route with interface pin")
	}
}
