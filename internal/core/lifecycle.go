package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrStartCancelled is returned by a start that was superseded by stop/restart.
var ErrStartCancelled = errors.New("start cancelled")

const kernelConfigKey = "node-00.json" // journal key: always present in the core's command line

func (b *Backend) beginStart() (context.Context, func()) {
	b.startMu.Lock()
	if b.startStop != nil {
		b.startStop()
	}
	b.startSeq++
	seq := b.startSeq
	ctx, cancel := context.WithCancel(context.Background())
	b.startStop = cancel
	b.startMu.Unlock()
	return ctx, func() {
		b.startMu.Lock()
		if b.startSeq == seq {
			b.startStop = nil
		}
		b.startMu.Unlock()
		cancel()
	}
}

func (b *Backend) cancelStart() {
	b.startMu.Lock()
	if b.startStop != nil {
		b.startStop()
	}
	b.startMu.Unlock()
}

// StartProxy starts (or, when already running, adjusts) the proxy session.
func (b *Backend) StartProxy(req StartRequest) error {
	ctx, release := b.beginStart()
	defer release()
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if ctx.Err() != nil {
		return ErrStartCancelled
	}
	if b.kernelRef() != nil {
		if b.GetState().TunRequested == req.WithTun {
			return nil
		}
		return b.applyTunLocked(ctx, req.WithTun)
	}
	if req.WithTun && !admin.Has() {
		return b.requireAdmin(adminRequest{start: &req})
	}
	b.update(func(s *RuntimeState) {
		s.Phase = PhaseStarting
		s.LastError = ""
		s.TunRequested, s.TunRunning = req.WithTun, false
	})
	if err := b.bringUpLocked(ctx, req); err != nil {
		b.teardownLocked()
		if errors.Is(err, ErrAdminRequired) || errors.Is(err, errAdminBadPassword) {
			return b.requireAdmin(adminRequest{start: &req, err: err})
		}
		cancelled := ctx.Err() != nil
		b.update(func(s *RuntimeState) {
			s.Phase = PhaseStopped
			s.TunRequested = false
			if !cancelled {
				s.LastError = err.Error()
			}
		})
		if cancelled {
			return ErrStartCancelled
		}
		b.addLog("error", "app", "start failed: "+err.Error())
		return err
	}
	return nil
}

func (b *Backend) StopProxy() error {
	b.cancelStart()
	b.opMu.Lock()
	defer b.opMu.Unlock()
	b.teardownLocked()
	return nil
}

func (b *Backend) RestartProxy(req StartRequest) error {
	if err := b.StopProxy(); err != nil {
		return err
	}
	return b.StartProxy(req)
}

// scheduleRestart restarts a running session shortly after a config change (debounced so
// bulk edits cause one restart).
func (b *Backend) scheduleRestart(reason string) {
	b.restartMu.Lock()
	defer b.restartMu.Unlock()
	if b.restartTimer != nil {
		b.restartTimer.Stop()
	}
	b.restartTimer = time.AfterFunc(400*time.Millisecond, func() {
		st := b.GetState()
		if st.Phase == PhaseStopped {
			return
		}
		b.addLog("info", "app", reason)
		if len(b.cfgClone().Nodes) == 0 {
			_ = b.StopProxy()
			return
		}
		if err := b.RestartProxy(StartRequest{WithTun: st.TunRequested}); err != nil && !errors.Is(err, ErrStartCancelled) {
			b.addLog("error", "app", "restart failed: "+err.Error())
		}
	})
}

func (b *Backend) bringUpLocked(ctx context.Context, req StartRequest) error {
	cfg := b.cfgClone()
	if len(cfg.Nodes) == 0 {
		return errors.New("no nodes configured: add a node first")
	}
	if err := b.EnsureCoreBinaries(); err != nil {
		return err
	}
	cfg = b.cfgClone()
	if err := checkPorts(cfg); err != nil {
		return err
	}
	b.guardOnce.Do(func() {
		if err := startGuard(b.exe); err != nil {
			b.addLog("warn", "app", "crash guard unavailable: "+err.Error())
		}
	})

	customURL := ""
	if cfg.Routing.CustomRulesEnabled && strings.TrimSpace(cfg.Routing.CustomRules) != "" {
		srv, err := startCustomRuleServer(cfg.Routing.CustomRules)
		if err != nil {
			b.addLog("warn", "route", "custom rules unavailable: "+err.Error())
		} else {
			b.custom, customURL = srv, srv.URL()
		}
	}

	k, err := startKernel(ctx, kernelStartOptions{
		Cfg:           cfg,
		Binary:        cfg.Core.SudokuBinary,
		CustomRuleURL: customURL,
		RuleCacheDir:  b.store.RuleCacheDir(),
		ConfigsDir:    b.store.ConfigsDir(),
		WithTun:       req.WithTun,
		OnLine:        func(l string) { b.addCoreLine("core", l) },
		OnSpawn:       func(pid int) { _ = b.journal.SetChild(kernelConfigKey, pid) },
	}, b.logf)
	if err != nil {
		_ = b.journal.SetChild(kernelConfigKey, 0)
		return err
	}
	b.acc.reset()
	b.mu.Lock()
	b.kern = k
	b.state.CoreRunning = true
	b.state.StartedAtUnix = time.Now().Unix()
	b.state.Kernel.Version = k.version
	b.state.Traffic = TrafficState{}
	b.mu.Unlock()
	b.startSession(k)
	b.addLog("info", "app", fmt.Sprintf("sudoku core %s ready (proxy 127.0.0.1:%d, controller :%d)", k.version, ProxyPort, cfg.Controller.Port))

	if err := ctx.Err(); err != nil {
		return err
	}
	b.applySystemProxyLocked(ctx)

	// A tunnel that cannot come up fails the whole start: running without it would
	// silently leave traffic that ignores the system proxy unprotected.
	if req.WithTun {
		if err := b.tunUpLocked(ctx, cfg); err != nil {
			return fmt.Errorf("TUN: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.update(func(s *RuntimeState) { s.Phase = PhaseRunning })
	return nil
}

func (b *Backend) applySystemProxyLocked(ctx context.Context) {
	snap, err := captureSystemProxy(ctx)
	if err != nil {
		b.addLog("warn", "proxy", "cannot read system proxy settings, leaving them untouched: "+err.Error())
		b.update(func(s *RuntimeState) { s.LastError = "system proxy: " + err.Error() })
		return
	}
	b.sysSnap = snap
	_ = b.journal.Update(func(f *journalFile) { f.SystemProxy, f.ProxyPort = snap, ProxyPort })
	if err := setSystemProxy(ctx, snap, ProxyPort, b.logf); err != nil {
		b.addLog("error", "proxy", "set system proxy failed: "+err.Error())
		b.update(func(s *RuntimeState) { s.LastError = "system proxy: " + err.Error() })
		return
	}
	b.update(func(s *RuntimeState) { s.SystemProxyOn = true })
	b.addLog("info", "proxy", fmt.Sprintf("system proxy set to 127.0.0.1:%d", ProxyPort))
}

func (b *Backend) restoreSystemProxyLocked() {
	snap := b.sysSnap
	if snap == nil {
		return
	}
	if err := restoreSystemProxy(snap, ProxyPort, b.logf); err != nil {
		// Keep the journal entry: the guard / next launch retries.
		b.addLog("error", "proxy", "restore system proxy failed: "+err.Error())
		return
	}
	b.sysSnap = nil
	_ = b.journal.Update(func(f *journalFile) { f.SystemProxy = nil })
	b.update(func(s *RuntimeState) { s.SystemProxyOn = false })
	b.addLog("info", "proxy", "system proxy restored")
}

// ---------------------------------------------------------------------------------------
// TUN

func (b *Backend) buildTunPlan(cfg *AppConfig) *tunPlan {
	customURL := ""
	if b.custom != nil {
		customURL = b.custom.URL()
	}
	var direct []string
	for _, raw := range effectiveRuleURLs(cfg.Routing, customURL) {
		if act, u := parseRuleSource(raw); act == ruleSourceDirect && (strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
			direct = append(direct, u)
		}
	}
	servers := make([]string, 0, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		if a := strings.TrimSpace(n.ServerAddress); a != "" {
			servers = append(servers, a)
		}
	}
	return &tunPlan{
		Tun:            cfg.Tun,
		ProxyPort:      ProxyPort,
		ServerAddrs:    servers,
		Mode:           normalizeMode(cfg.Routing.ProxyMode),
		DirectRuleURLs: direct,
		RuleSeedDir:    b.store.RuleCacheDir(),
	}
}

func normalizeMode(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "global":
		return "global"
	case "direct":
		return "direct"
	}
	return "pac"
}

func (b *Backend) tunUpLocked(ctx context.Context, cfg *AppConfig) error {
	if _, err := b.tun.Up(ctx, b.buildTunPlan(cfg)); err != nil {
		b.addLog("error", "tun", "tunnel failed: "+err.Error())
		b.tun.Shutdown(5 * time.Second)
		b.refreshNetRepair()
		return err
	}
	b.update(func(s *RuntimeState) { s.TunRunning = true })
	b.addLog("info", "tun", "tunnel is up")
	return nil
}

// tunDownLocked tears the tunnel down and dismisses the elevated helper right away: with
// the password kept in memory, relaunching it later is silent.
func (b *Backend) tunDownLocked() {
	b.update(func(s *RuntimeState) { s.TunRunning = false })
	if b.tun.Connected() {
		b.tun.Shutdown(helperDownTimeout)
		b.addLog("info", "tun", "tunnel is down")
	}
}

// applyTunLocked switches the tunnel on/off while the core keeps running. A tunnel that
// fails to come up is switched back off (setting included), so the UI never shows a
// tunnel that is not there.
func (b *Backend) applyTunLocked(ctx context.Context, want bool) error {
	if !want {
		b.update(func(s *RuntimeState) { s.TunRequested = false })
		b.tunDownLocked()
		return nil
	}
	if !admin.Has() {
		return b.requireAdmin(adminRequest{start: &StartRequest{WithTun: true}})
	}
	b.update(func(s *RuntimeState) { s.TunRequested, s.LastError = true, "" })
	if err := b.tunUpLocked(ctx, b.cfgClone()); err != nil {
		if errors.Is(err, ErrAdminRequired) || errors.Is(err, errAdminBadPassword) {
			b.update(func(s *RuntimeState) { s.TunRequested = false })
			return b.requireAdmin(adminRequest{start: &StartRequest{WithTun: true}, err: err})
		}
		b.update(func(s *RuntimeState) { s.TunRequested, s.LastError = false, "TUN: "+err.Error() })
		b.setTunEnabled(false)
		return err
	}
	return nil
}

// setTunEnabled persists the TUN switch without the side effects of SaveConfig.
func (b *Backend) setTunEnabled(on bool) {
	b.mu.Lock()
	if b.cfg.Tun.Enabled == on {
		b.mu.Unlock()
		return
	}
	b.cfg.Tun.Enabled = on
	err := b.store.Save(b.cfg)
	b.mu.Unlock()
	if err != nil {
		b.addLog("warn", "app", "save config: "+err.Error())
	}
	b.markDirty()
}

// reapplyTun rebuilds a running tunnel (e.g. the routing mode changed what the helper must
// do). It is a no-op when no tunnel is running.
func (b *Backend) reapplyTun() error {
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if !b.GetState().TunRunning {
		return nil
	}
	b.tunDownLocked()
	ctx, cancel := context.WithTimeout(context.Background(), helperUpTimeout+10*time.Second)
	defer cancel()
	if err := b.tunUpLocked(ctx, b.cfgClone()); err != nil {
		b.failSessionLocked("TUN: " + err.Error())
		return err
	}
	return nil
}

func (b *Backend) onTunEvent(event, line string) {
	switch event {
	case evLog:
		b.addCoreLine("tun", line)
	case evExit, evDegraded:
		if !b.GetState().TunRunning {
			return
		}
		b.addLog("error", "tun", line)
		b.update(func(s *RuntimeState) { s.TunRunning = false })
		go b.recoverTun(line)
	}
}

// recoverTun brings a tunnel that died mid-session back once; if that fails the whole
// session stops instead of carrying on half-protected.
func (b *Backend) recoverTun(reason string) {
	b.opMu.Lock()
	defer b.opMu.Unlock()
	st := b.GetState()
	if st.Phase != PhaseRunning || !st.TunRequested || st.TunRunning {
		return
	}
	b.tun.Shutdown(5 * time.Second)
	time.Sleep(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), helperUpTimeout+10*time.Second)
	defer cancel()
	if err := b.tunUpLocked(ctx, b.cfgClone()); err != nil {
		b.failSessionLocked("TUN: " + reason)
		return
	}
	b.addLog("info", "tun", "tunnel recovered")
}

// failSessionLocked stops everything and reports why.
func (b *Backend) failSessionLocked(msg string) {
	b.addLog("error", "app", "stopping: "+msg)
	b.teardownLocked()
	b.update(func(s *RuntimeState) { s.LastError = msg })
}

// refreshNetRepair raises the "repair network" flag when a dead helper left state behind.
func (b *Backend) refreshNetRepair() {
	stale := tunStateStale() && !b.GetState().TunRunning
	if b.GetState().NeedsNetRepair != stale {
		b.update(func(s *RuntimeState) { s.NeedsNetRepair = stale })
	}
}

// RepairNetwork undoes routes/DNS left behind by a tunnel helper that died.
func (b *Backend) RepairNetwork() error {
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if b.GetState().TunRunning {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := b.tun.Recover(ctx); err != nil {
		if errors.Is(err, ErrAdminRequired) {
			return b.requireAdmin(adminRequest{repair: true})
		}
		b.addLog("error", "tun", "network repair failed: "+err.Error())
		return err
	}
	b.addLog("info", "tun", "network repaired")
	b.refreshNetRepair()
	return nil
}

// ---------------------------------------------------------------------------------------
// teardown & supervision

// teardownLocked undoes everything in reverse order and is idempotent.
func (b *Backend) teardownLocked() {
	k := b.kernelRef()
	if k != nil || b.GetState().TunRunning {
		b.update(func(s *RuntimeState) { s.Phase = PhaseStopping })
	}
	if b.sess != nil {
		b.sess.stop()
		b.sess = nil
	}
	b.tunDownLocked()
	b.restoreSystemProxyLocked()

	if k != nil {
		b.mu.Lock()
		b.kern = nil
		b.mu.Unlock()
		if err := k.stop(3 * time.Second); err != nil {
			b.addLog("warn", "core", "stop core: "+err.Error())
		}
	}
	_ = b.journal.SetChild(kernelConfigKey, 0)
	if b.custom != nil {
		b.custom.stop()
		b.custom = nil
	}
	b.usage.flush()
	b.update(func(s *RuntimeState) {
		s.Phase = PhaseStopped
		s.CoreRunning, s.TunRunning, s.TunRequested, s.SystemProxyOn = false, false, false, false
		s.StartedAtUnix, s.ConnectionCount = 0, 0
		s.Traffic = TrafficState{}
		s.Kernel.MemoryBytes = 0
	})
	b.refreshNetRepair()
}

func (b *Backend) superviseKernel(ctx context.Context, k *kernel) {
	select {
	case <-ctx.Done():
		return
	case <-k.proc.Done():
	}
	b.opMu.Lock()
	defer b.opMu.Unlock()
	if b.kernelRef() != k {
		return
	}
	msg := "sudoku core exited unexpectedly"
	if tail := k.tailText(); tail != "" {
		msg += ": " + lastLine(tail)
	}
	b.failSessionLocked(msg)
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}
