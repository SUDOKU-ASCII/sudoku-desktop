package core

import (
	"context"
	"testing"
	"time"
)

func TestCancelStartWaitsForStartCleanup(t *testing.T) {
	b := &Backend{}
	ctx, opID := b.newStartContext()
	done := b.cancelStart()

	if !b.startInFlight() {
		t.Fatal("expected start operation to remain in flight until cleanup completes")
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("start context was not canceled")
	}
	select {
	case <-done:
		t.Fatal("start completion was signaled before cleanup")
	default:
	}

	b.clearStartIfMatch(opID)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("start completion was not signaled after cleanup")
	}
	if b.startInFlight() {
		t.Fatal("expected no start operation after cleanup")
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("expected canceled context, got %v", ctx.Err())
	}
}
