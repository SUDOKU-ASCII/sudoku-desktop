package core

import "testing"

func TestParseRuleSourceActions(t *testing.T) {
	tests := []struct {
		raw    string
		action ruleSourceAction
		url    string
	}{
		{"https://example.com/direct.yaml", ruleSourceDirect, "https://example.com/direct.yaml"},
		{"-https://example.com/proxy.yaml", ruleSourceProxy, "https://example.com/proxy.yaml"},
		{"_https://example.com/proxy.yaml", ruleSourceProxy, "https://example.com/proxy.yaml"},
		{"!https://example.com/reject.yaml", ruleSourceReject, "https://example.com/reject.yaml"},
		{"！https://example.com/reject.yaml", ruleSourceReject, "https://example.com/reject.yaml"},
		{"?https://example.com/future.yaml", ruleSourceReserved, "https://example.com/future.yaml"},
	}
	for _, tt := range tests {
		action, url := parseRuleSource(tt.raw)
		if action != tt.action || url != tt.url {
			t.Fatalf("parseRuleSource(%q) = (%v, %q), want (%v, %q)", tt.raw, action, url, tt.action, tt.url)
		}
	}
}
