package core

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseCoreTrafficLineIgnoredWhenStatsFileConfigured(t *testing.T) {
	b := &Backend{
		state: RuntimeState{
			Traffic: TrafficState{RecentBandwidth: []BandwidthSample{}},
		},
	}
	b.trafficCache.coreTrafficFile = "/tmp/traffic_stats.json"

	b.parseCoreTrafficLineLocked("__SUDOKU_TRAFFIC__ direct_tx=10 direct_rx=20 proxy_tx=30 proxy_rx=40")

	if b.state.Traffic.TotalTx != 0 || b.state.Traffic.TotalRx != 0 {
		t.Fatalf("expected log traffic line to be ignored when stats file is configured, got tx=%d rx=%d", b.state.Traffic.TotalTx, b.state.Traffic.TotalRx)
	}
	if len(b.state.Traffic.RecentBandwidth) != 0 {
		t.Fatalf("expected no bandwidth samples, got %d", len(b.state.Traffic.RecentBandwidth))
	}
}

func TestApplyCoreTrafficCountersLockedComputesRateFromDelta(t *testing.T) {
	root := t.TempDir()
	b := &Backend{
		store: &Store{
			rootDir:    root,
			configPath: filepath.Join(root, "config.json"),
			runtimeDir: filepath.Join(root, "runtime"),
			logDir:     filepath.Join(root, "logs"),
		},
		state: RuntimeState{
			Traffic: TrafficState{RecentBandwidth: []BandwidthSample{}},
		},
	}
	start := time.Unix(100, 0)

	b.applyCoreTrafficCountersLocked(1000, 2000, 3000, 4000, start)
	b.applyCoreTrafficCountersLocked(2000, 3500, 7000, 6500, start.Add(2*time.Second))

	if got := len(b.state.Traffic.RecentBandwidth); got != 1 {
		t.Fatalf("expected 1 bandwidth sample, got %d", got)
	}

	sample := b.state.Traffic.RecentBandwidth[0]
	if sample.TxBps != 2500 {
		t.Fatalf("expected tx rate 2500 B/s, got %.2f", sample.TxBps)
	}
	if sample.RxBps != 2000 {
		t.Fatalf("expected rx rate 2000 B/s, got %.2f", sample.RxBps)
	}
	if sample.Direct != 1250 {
		t.Fatalf("expected direct rate 1250 B/s, got %.2f", sample.Direct)
	}
	if sample.Proxy != 3250 {
		t.Fatalf("expected proxy rate 3250 B/s, got %.2f", sample.Proxy)
	}
	if b.state.Traffic.TotalTx != 9000 || b.state.Traffic.TotalRx != 10000 {
		t.Fatalf("expected totals tx=9000 rx=10000, got tx=%d rx=%d", b.state.Traffic.TotalTx, b.state.Traffic.TotalRx)
	}
}
