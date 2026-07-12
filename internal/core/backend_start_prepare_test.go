package core

import (
	"reflect"
	"testing"
)

func TestBaseCoreEnvironmentIncludesTrafficAndRuleCache(t *testing.T) {
	got := baseCoreEnvironment("warn", "/tmp/traffic.json", "/tmp/rules")
	want := []string{
		"SUDOKU_LOG_LEVEL=warn",
		"SUDOKU_TRAFFIC_REPORT=1",
		"SUDOKU_TRAFFIC_INTERVAL_MS=1000",
		"SUDOKU_TRAFFIC_FILE=/tmp/traffic.json",
		"SUDOKU_RULE_CACHE_DIR=/tmp/rules",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected core environment:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"":             "''",
		"plain-value":  "plain-value",
		"two words":    "'two words'",
		"single'quote": `'single'"'"'quote'`,
	}
	for input, want := range tests {
		if got := shellQuote(input); got != want {
			t.Fatalf("shellQuote(%q) = %q, want %q", input, got, want)
		}
	}
}
