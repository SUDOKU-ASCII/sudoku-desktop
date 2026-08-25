package core

import (
	"testing"
	"time"
)

func TestHasUpstreamHTTP403Since(t *testing.T) {
	now := time.Now()
	b := &Backend{logs: []LogEntry{
		{Timestamp: now.Add(-time.Minute), Message: "expected handshake response status code 101 but got 403"},
		{Timestamp: now.Add(time.Second), Message: "dial http tunnel failed: expected handshake response status code 101 but got 403"},
	}}
	if !b.hasUpstreamHTTP403Since(now) {
		t.Fatal("expected the current start attempt to detect HTTP 403")
	}
}

func TestHasUpstreamHTTP403SinceIgnoresPreviousAttempt(t *testing.T) {
	now := time.Now()
	b := &Backend{logs: []LogEntry{
		{Timestamp: now.Add(-time.Minute), Message: "expected handshake response status code 101 but got 403"},
		{Timestamp: now.Add(time.Second), Message: "connection refused"},
	}}
	if b.hasUpstreamHTTP403Since(now) {
		t.Fatal("did not expect a stale HTTP 403 to affect the current start attempt")
	}
}
