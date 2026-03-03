package core

import "testing"

func TestParseLogxLine(t *testing.T) {
	line := "\x1b[2m12:34:56\x1b[0m \x1b[36minfo\x1b[0m \x1b[35m[Client]\x1b[0m Ready"
	parsed, ok := parseLogxLine(line)
	if !ok {
		t.Fatalf("expected logx line to be parsed")
	}
	if parsed.Timestamp != "12:34:56" {
		t.Fatalf("unexpected timestamp: %q", parsed.Timestamp)
	}
	if parsed.Level != "info" {
		t.Fatalf("unexpected level: %q", parsed.Level)
	}
	if parsed.Component != "Client" {
		t.Fatalf("unexpected component: %q", parsed.Component)
	}
	if parsed.Message != "Ready" {
		t.Fatalf("unexpected message: %q", parsed.Message)
	}
}

func TestComponentFromLineForLogxNoComponent(t *testing.T) {
	comp := componentFromLine("12:34:56 info startup complete")
	if comp != "core" {
		t.Fatalf("expected core component, got %q", comp)
	}
}

func TestTrimComponentPrefix(t *testing.T) {
	if got := trimComponentPrefix("[route] linux: ready", "route"); got != "linux: ready" {
		t.Fatalf("unexpected bracket-prefix trim result: %q", got)
	}
	if got := trimComponentPrefix("Route: changed", "route"); got != "changed" {
		t.Fatalf("unexpected colon-prefix trim result: %q", got)
	}
	if got := trimComponentPrefix("unchanged", "route"); got != "unchanged" {
		t.Fatalf("unexpected unchanged result: %q", got)
	}
}
