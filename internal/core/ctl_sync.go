package core

import (
	"context"
	"strings"
	"time"
)

const ctlSyncInterval = 1500 * time.Millisecond

// syncLoop mirrors changes made behind the app's back (the core's web panel switching the
// node or the routing mode) into the config and the UI. Only a change in the controller's
// own answer counts as external: comparing against the config instead would race with the
// app's own switches, which update the config a moment before the controller.
func (b *Backend) syncLoop(ctx context.Context, k *kernel) {
	var lastMode, lastNode string
	primed := false
	tick := time.NewTicker(ctlSyncInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		mode, node, ok := b.pollController(ctx, k)
		if !ok {
			continue
		}
		if !primed {
			lastMode, lastNode, primed = mode, node, true
			continue
		}
		if mode != "" && mode != lastMode {
			lastMode = mode
			b.adoptExternalMode(mode)
		}
		if node != "" && node != lastNode {
			lastNode = node
			b.adoptExternalNode(node)
		}
	}
}

// pollController returns the controller's mode (as an app routing mode) and the node ID the
// PROXY group currently points at. Either may be empty when it cannot be determined.
func (b *Backend) pollController(ctx context.Context, k *kernel) (mode, nodeID string, ok bool) {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	raw, err := k.ctl.Mode(reqCtx)
	if err != nil {
		return "", "", false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "global":
		mode = "global"
	case "direct":
		mode = "direct"
	case "rule":
		mode = "pac"
	}
	if proxies, err := k.ctl.Proxies(reqCtx); err == nil {
		if group, found := proxies["PROXY"]; found {
			nodeID = k.nodeIDForOutbound(group.Now)
		}
	}
	return mode, nodeID, true
}

func (k *kernel) nodeIDForOutbound(name string) string {
	if name == "" {
		return ""
	}
	if name == "Balance" {
		return BalanceNodeID
	}
	for id, n := range k.names {
		if n == name {
			return id
		}
	}
	return ""
}

func (b *Backend) adoptExternalMode(mode string) {
	b.mu.Lock()
	prev := b.cfg.Routing.ProxyMode
	if normalizeMode(prev) == mode {
		b.mu.Unlock()
		return
	}
	b.cfg.Routing.ProxyMode = mode
	err := b.store.Save(b.cfg)
	b.mu.Unlock()
	if err != nil {
		b.addLog("warn", "route", "save routing mode failed: "+err.Error())
	}
	b.addLog("info", "route", "routing mode changed from the web panel: "+mode)
	b.markDirty()
	// The tunnel's DNS plan depends on whether rules are in play, exactly as for an in-app switch.
	if (normalizeMode(prev) == "pac") != (mode == "pac") && b.GetState().TunRunning {
		go func() { _ = b.reapplyTun() }()
	}
}

func (b *Backend) adoptExternalNode(nodeID string) {
	b.mu.Lock()
	if b.cfg.ActiveNodeID == nodeID {
		b.mu.Unlock()
		return
	}
	b.cfg.ActiveNodeID = nodeID
	err := b.store.Save(b.cfg)
	b.mu.Unlock()
	if err != nil {
		b.addLog("warn", "app", "save active node failed: "+err.Error())
	}
	b.addLog("info", "app", "upstream node changed from the web panel")
	b.markDirty()
}
