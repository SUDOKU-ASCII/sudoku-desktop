//go:build sudoku_patch

package app

import (
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/saba-futai/sudoku/pkg/logx"
)

// This file is injected into the upstream sudoku core at build time.
//
// It attributes traffic to DIRECT vs PROXY by wrapping the net.Conn returned from dialTarget().
// The desktop app parses periodic log lines to obtain accurate counters.

type TrafficStats struct {
	DirectTx uint64 `json:"direct_tx"`
	DirectRx uint64 `json:"direct_rx"`
	ProxyTx  uint64 `json:"proxy_tx"`
	ProxyRx  uint64 `json:"proxy_rx"`
}

var (
	trafficDirectTx uint64
	trafficDirectRx uint64
	trafficProxyTx  uint64
	trafficProxyRx  uint64
)

const (
	trafficKindDirect = 0
	trafficKindProxy  = 1
)

type countingConn struct {
	net.Conn
	kind int
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		if c.kind == trafficKindProxy {
			atomic.AddUint64(&trafficProxyRx, uint64(n))
		} else {
			atomic.AddUint64(&trafficDirectRx, uint64(n))
		}
	}
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		if c.kind == trafficKindProxy {
			atomic.AddUint64(&trafficProxyTx, uint64(n))
		} else {
			atomic.AddUint64(&trafficDirectTx, uint64(n))
		}
	}
	return n, err
}

var trafficReporterOnce sync.Once

func wrapConnForTrafficStats(conn net.Conn, shouldProxy bool) net.Conn {
	if conn == nil {
		return conn
	}
	trafficReporterOnce.Do(startTrafficReporter)
	kind := trafficKindDirect
	if shouldProxy {
		kind = trafficKindProxy
	}
	return &countingConn{Conn: conn, kind: kind}
}

func SnapshotTrafficStats() TrafficStats {
	return TrafficStats{
		DirectTx: atomic.LoadUint64(&trafficDirectTx),
		DirectRx: atomic.LoadUint64(&trafficDirectRx),
		ProxyTx:  atomic.LoadUint64(&trafficProxyTx),
		ProxyRx:  atomic.LoadUint64(&trafficProxyRx),
	}
}

func ResetTrafficStats() {
	atomic.StoreUint64(&trafficDirectTx, 0)
	atomic.StoreUint64(&trafficDirectRx, 0)
	atomic.StoreUint64(&trafficProxyTx, 0)
	atomic.StoreUint64(&trafficProxyRx, 0)
}

func startTrafficReporter() {
	if strings.TrimSpace(os.Getenv("SUDOKU_TRAFFIC_REPORT")) != "1" {
		return
	}
	interval := time.Second
	if raw := strings.TrimSpace(os.Getenv("SUDOKU_TRAFFIC_INTERVAL_MS")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v >= 200 {
			interval = time.Duration(v) * time.Millisecond
		}
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for range t.C {
			s := SnapshotTrafficStats()
			logx.Infof("Traffic", "__SUDOKU_TRAFFIC__ direct_tx=%d direct_rx=%d proxy_tx=%d proxy_rx=%d",
				s.DirectTx, s.DirectRx, s.ProxyTx, s.ProxyRx)
		}
	}()
}
