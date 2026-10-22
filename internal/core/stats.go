package core

import (
	"context"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// session groups the background workers that live as long as one core process.
type session struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func (s *session) stop() {
	s.cancel()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

func (b *Backend) startSession(k *kernel) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{cancel: cancel}
	b.sess = s
	go b.superviseKernel(ctx, k) // not in the group: it is the one that stops the session
	run := func(fn func()) {
		s.wg.Add(1)
		go func() { defer s.wg.Done(); fn() }()
	}
	run(func() {
		retryLoop(ctx, func() error { return k.ctl.StreamTraffic(ctx, b.onTraffic) })
	})
	run(func() {
		retryLoop(ctx, func() error {
			return k.ctl.StreamMemory(ctx, func(v uint64) { b.acc.mem.Store(v) })
		})
	})
	run(func() { b.connLoop(ctx, k) })
}

func retryLoop(ctx context.Context, fn func() error) {
	for ctx.Err() == nil {
		_ = fn()
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

type connSnap struct{ up, down uint64 }

// trafficAcc accumulates what the core's counters cannot tell us directly: the
// direct/proxy split, derived from per-connection deltas.
type trafficAcc struct {
	mu               sync.Mutex
	lastUp, lastDown uint64
	dirTx, dirRx     uint64
	prxTx, prxRx     uint64
	dirConns         int
	prxConns         int
	conns            int
	dirBps, prxBps   float64
	prev             map[string]connSnap
	lastPoll         time.Time
	mem              atomic.Uint64
}

func (a *trafficAcc) reset() {
	a.mu.Lock()
	a.lastUp, a.lastDown = 0, 0
	a.dirTx, a.dirRx, a.prxTx, a.prxRx = 0, 0, 0, 0
	a.dirConns, a.prxConns, a.conns = 0, 0, 0
	a.dirBps, a.prxBps = 0, 0
	a.prev, a.lastPoll = nil, time.Time{}
	a.mu.Unlock()
	a.mem.Store(0)
}

func counterDelta(cur, last uint64) uint64 {
	if cur >= last {
		return cur - last
	}
	return cur // the counter restarted
}

func share(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func (b *Backend) onTraffic(t ctlTraffic) {
	a := &b.acc
	a.mu.Lock()
	dTx, dRx := counterDelta(t.UpTotal, a.lastUp), counterDelta(t.DownTotal, a.lastDown)
	a.lastUp, a.lastDown = t.UpTotal, t.DownTotal
	traffic := TrafficState{
		TotalTx: t.UpTotal, TotalRx: t.DownTotal,
		EstimatedDirectTx: a.dirTx, EstimatedDirectRx: a.dirRx,
		EstimatedProxyTx: a.prxTx, EstimatedProxyRx: a.prxRx,
		DirectConns: a.dirConns, ProxyConns: a.prxConns,
	}
	dirTxShare := share(a.dirTx, a.dirTx+a.prxTx)
	dirRxShare := share(a.dirRx, a.dirRx+a.prxRx)
	conns, dirBps, prxBps := a.conns, a.dirBps, a.prxBps
	a.mu.Unlock()
	mem := a.mem.Load()

	if dTx > 0 || dRx > 0 {
		dd, dr := uint64(float64(dTx)*dirTxShare), uint64(float64(dRx)*dirRxShare)
		b.usage.add(dTx, dRx, dd, dr, dTx-dd, dRx-dr)
	}
	b.mu.Lock()
	b.state.Traffic = traffic
	b.state.Kernel.MemoryBytes = mem
	b.state.ConnectionCount = conns
	b.mu.Unlock()
	b.emitEvent(EventTrafficUpdated, TrafficTick{
		Sample: BandwidthSample{
			At: time.Now(), TxBps: float64(t.Up), RxBps: float64(t.Down),
			Direct: dirBps, Proxy: prxBps, TotalTx: t.UpTotal, TotalRx: t.DownTotal,
		},
		Traffic: traffic, MemoryBytes: mem, ConnectionCount: conns,
	})
}

func connDirection(chains []string) string {
	for _, c := range chains {
		switch strings.ToUpper(c) {
		case "DIRECT":
			return "direct"
		case "REJECT":
			return "reject"
		}
	}
	if len(chains) == 0 {
		return "direct"
	}
	return "proxy"
}

func toActiveConnection(c ctlConnection) ActiveConnection {
	host := c.Metadata.Host
	if host == "" {
		host = c.Metadata.DestinationIP
	}
	dst := host
	if c.Metadata.DestinationPort != "" {
		dst += ":" + c.Metadata.DestinationPort
	}
	node := ""
	if len(c.Chains) > 0 {
		node = c.Chains[0]
	}
	return ActiveConnection{
		ID:          c.ID,
		Network:     strings.ToUpper(c.Metadata.Network),
		Type:        c.Metadata.Type,
		Source:      c.Metadata.SourceIP + ":" + c.Metadata.SourcePort,
		Destination: dst,
		Direction:   connDirection(c.Chains),
		Node:        node,
		Rule:        c.Rule,
		Upload:      c.Upload,
		Download:    c.Download,
		StartedAt:   c.Start,
	}
}

func (b *Backend) connLoop(ctx context.Context, k *kernel) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		snap, err := k.ctl.Connections(rctx)
		cancel()
		if err != nil {
			continue
		}
		b.accumulateConnections(snap)
	}
}

func (b *Backend) accumulateConnections(snap *ctlConnections) {
	a := &b.acc
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := make(map[string]connSnap, len(snap.Connections))
	var dirBytes, prxBytes uint64
	dirConns, prxConns := 0, 0
	for _, c := range snap.Connections {
		dUp, dDown := c.Upload, c.Download
		if p, ok := a.prev[c.ID]; ok {
			dUp, dDown = counterDelta(c.Upload, p.up), counterDelta(c.Download, p.down)
		}
		cur[c.ID] = connSnap{c.Upload, c.Download}
		if connDirection(c.Chains) == "direct" {
			a.dirTx += dUp
			a.dirRx += dDown
			dirBytes += dUp + dDown
			dirConns++
		} else {
			a.prxTx += dUp
			a.prxRx += dDown
			prxBytes += dUp + dDown
			prxConns++
		}
	}
	if !a.lastPoll.IsZero() {
		if el := now.Sub(a.lastPoll).Seconds(); el > 0 {
			a.dirBps, a.prxBps = float64(dirBytes)/el, float64(prxBytes)/el
		}
	}
	a.prev, a.lastPoll = cur, now
	a.dirConns, a.prxConns, a.conns = dirConns, prxConns, len(snap.Connections)
}

// ---------------------------------------------------------------------------------------
// usage history

type usageTracker struct {
	mu        sync.Mutex
	path      string
	days      []UsageDay
	dirty     bool
	lastFlush time.Time
}

func (u *usageTracker) init(path string) {
	u.path = path
	u.days = trimUsageDays(loadUsageHistory(path), 120)
	u.lastFlush = time.Now()
}

func (u *usageTracker) add(tx, rx, dTx, dRx, pTx, pRx uint64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.days = addUsageToDay(u.days, usageDayKey(time.Now()), tx, rx, dTx, dRx, pTx, pRx)
	u.days = trimUsageDays(u.days, 120)
	u.dirty = true
	if time.Since(u.lastFlush) > 30*time.Second {
		u.flushLocked()
	}
}

func (u *usageTracker) flush() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.flushLocked()
}

func (u *usageTracker) flushLocked() {
	if !u.dirty {
		return
	}
	_ = saveUsageHistory(u.path, u.days)
	u.dirty, u.lastFlush = false, time.Now()
}

func (u *usageTracker) list(limit int) []UsageDay {
	u.mu.Lock()
	days := append([]UsageDay(nil), u.days...)
	u.mu.Unlock()
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	if limit > 0 && limit < len(days) {
		days = days[len(days)-limit:]
	}
	if days == nil {
		return []UsageDay{}
	}
	return days
}
