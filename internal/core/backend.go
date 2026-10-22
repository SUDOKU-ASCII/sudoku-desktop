package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	storeName    = "4x4-sudoku"
	oldStoreName = "sudoku-desktop"
	logRingSize  = 5000
)

// Backend orchestrates one proxy session: the Sudoku core (controlled over its external
// controller), the system proxy, and the optional privileged TUN helper. All side effects
// that must survive a crash are recorded in the journal so they can always be undone.
type Backend struct {
	store       *Store
	exe         string
	runtimeFS   fs.FS
	runtimeRoot string

	// mu guards cfg, state, latency and kern. Never call out (controller, helper, OS)
	// while holding it.
	mu      sync.RWMutex
	cfg     *AppConfig
	state   RuntimeState
	latency map[string]LatencyResult
	kern    *kernel

	// opMu serializes lifecycle operations (start/stop/tun toggles). Fields below it are
	// only touched with opMu held.
	opMu      sync.Mutex
	sysSnap   *systemProxySnapshot
	sess      *session
	custom    *customRuleServer
	startMu   sync.Mutex
	startSeq  uint64
	startStop context.CancelFunc

	journal   *journal
	guardOnce sync.Once
	tun       *tunClient
	pf        *portForwardManager
	rev       *ManagedProcess

	acc trafficAcc

	logMu    sync.Mutex
	logs     []LogEntry
	logHead  int
	logSeq   uint64
	usage    usageTracker
	emitMu   sync.RWMutex
	emitter  func(name string, payload any)
	dirtyCh  chan struct{}
	logCh    chan LogEntry
	closeCh  chan struct{}
	loopOnce sync.Once

	shutdownOnce sync.Once
	restartMu    sync.Mutex
	restartTimer *time.Timer

	// adminPending is the action that hit ErrAdminRequired; it runs once the user entered
	// the password in the app (RuntimeState.NeedsAdmin is true meanwhile).
	adminMu      sync.Mutex
	adminPending *adminRequest
}

func NewBackendWithRuntimeFS(runtimeFS fs.FS, runtimeRoot string) (*Backend, error) {
	if err := migrateStoreIfNeeded(oldStoreName, storeName); err != nil {
		return nil, err
	}
	store, err := NewStore(storeName)
	if err != nil {
		return nil, err
	}
	cfg, err := store.Load()
	if err != nil {
		return nil, err
	}
	if err := ensureDir(cfg.Core.WorkingDir); err != nil {
		return nil, err
	}
	exe, err := currentExecutablePath()
	if err != nil {
		return nil, err
	}
	b := &Backend{
		store:       store,
		exe:         exe,
		runtimeFS:   runtimeFS,
		runtimeRoot: runtimeRoot,
		cfg:         cfg,
		latency:     map[string]LatencyResult{},
		journal:     newJournal(store.JournalPath()),
		rev:         NewManagedProcess("sudoku-reverse"),
		logs:        make([]LogEntry, 0, 256),
		dirtyCh:     make(chan struct{}, 1),
		logCh:       make(chan LogEntry, 1024),
		closeCh:     make(chan struct{}),
	}
	b.tun = newTunClient(exe, store.RuntimeDir(), func(l string) { b.addLog("info", "tun", l) }, b.onTunEvent)
	b.pf = newPortForwardManager(func(line string) { b.addLog("info", "forward", line) })
	b.usage.init(store.UsageHistoryPath())
	b.state.Phase = PhaseStopped
	return b, nil
}

// ---------------------------------------------------------------------------------------
// events & state

func (b *Backend) SetEventEmitter(emit func(name string, payload any)) {
	b.emitMu.Lock()
	b.emitter = emit
	b.emitMu.Unlock()
}

func (b *Backend) emitEvent(name string, payload any) {
	b.emitMu.RLock()
	emit := b.emitter
	b.emitMu.RUnlock()
	if emit != nil {
		emit(name, payload)
	}
}

// emitterLoop coalesces bursts of state changes into one event and forwards log entries.
func (b *Backend) emitterLoop() {
	for {
		select {
		case <-b.closeCh:
			return
		case <-b.dirtyCh:
			time.Sleep(25 * time.Millisecond)
			select {
			case <-b.dirtyCh:
			default:
			}
			b.emitEvent(EventStateUpdated, b.GetState())
		case e := <-b.logCh:
			b.emitEvent(EventLogAdded, e)
		}
	}
}

func (b *Backend) markDirty() {
	select {
	case b.dirtyCh <- struct{}{}:
	default:
	}
}

// update mutates the runtime state and schedules a state event.
func (b *Backend) update(fn func(s *RuntimeState)) {
	b.mu.Lock()
	fn(&b.state)
	b.mu.Unlock()
	b.markDirty()
}

func (b *Backend) GetState() RuntimeState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s := b.state
	s.Running = s.Phase == PhaseRunning
	s.ProxyPort = ProxyPort
	s.Mode = b.cfg.Routing.ProxyMode
	s.ActiveNodeID = b.cfg.ActiveNodeID
	switch {
	case s.ActiveNodeID == BalanceNodeID:
		s.ActiveNodeName = "Balance"
	default:
		for i := range b.cfg.Nodes {
			if b.cfg.Nodes[i].ID == s.ActiveNodeID {
				s.ActiveNodeName = b.cfg.Nodes[i].Name
				break
			}
		}
	}
	s.Latencies = make([]LatencyResult, 0, len(b.cfg.Nodes))
	for i := range b.cfg.Nodes {
		if r, ok := b.latency[b.cfg.Nodes[i].ID]; ok {
			s.Latencies = append(s.Latencies, r)
		}
	}
	return s
}

func (b *Backend) cfgClone() *AppConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return cloneConfig(b.cfg)
}

func (b *Backend) kernelRef() *kernel {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.kern
}

func (b *Backend) logf(line string) { b.addLog("info", "app", line) }

// ---------------------------------------------------------------------------------------
// logs

func (b *Backend) addLog(level, component, message string) {
	b.appendLog(LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Component: component,
		Message:   strings.TrimSpace(message),
		Raw:       message,
	})
}

// addCoreLine ingests one raw line from a child process.
func (b *Backend) addCoreLine(source, line string) {
	clean := strings.TrimSpace(stripANSI(line))
	if clean == "" {
		return
	}
	level := levelFromLine(line)
	comp := componentFromLine(line)
	msg := clean
	if p, ok := parseLogxLine(line); ok {
		msg = p.Message
	}
	if comp == "core" && source != "" {
		comp = source
	}
	b.appendLog(LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Component: comp,
		Message:   trimComponentPrefix(msg, comp),
		Raw:       line,
	})
}

func (b *Backend) appendLog(e LogEntry) {
	b.logMu.Lock()
	b.logSeq++
	e.ID = strconv.FormatUint(b.logSeq, 36)
	if len(b.logs) < logRingSize {
		b.logs = append(b.logs, e)
	} else {
		b.logs[b.logHead] = e
		b.logHead = (b.logHead + 1) % logRingSize
	}
	b.logMu.Unlock()
	select {
	case b.logCh <- e:
	default: // the UI can always fetch history through GetLogs
	}
}

// GetLogs returns the newest entries first, optionally filtered by level.
func (b *Backend) GetLogs(level string, limit int) []LogEntry {
	level = strings.ToLower(strings.TrimSpace(level))
	if limit <= 0 || limit > logRingSize {
		limit = logRingSize
	}
	b.logMu.Lock()
	defer b.logMu.Unlock()
	n := len(b.logs)
	out := make([]LogEntry, 0, min(limit, n))
	for i := 0; i < n && len(out) < limit; i++ {
		idx := (b.logHead + n - 1 - i) % n
		e := b.logs[idx]
		if level != "" && level != "all" && e.Level != level {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ---------------------------------------------------------------------------------------
// startup / shutdown

func migrateStoreIfNeeded(oldName, newName string) error {
	cfgRoot, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("resolve user config dir: %w", err)
	}
	oldConfig := filepath.Join(cfgRoot, oldName, "config.json")
	newConfig := filepath.Join(cfgRoot, newName, "config.json")
	if _, err := os.Stat(newConfig); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat new config: %w", err)
	}
	if _, err := os.Stat(oldConfig); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat old config: %w", err)
	}
	oldStore, err := NewStore(oldName)
	if err != nil {
		return err
	}
	cfg, err := oldStore.Load()
	if err != nil {
		return err
	}
	cfg.Core.SudokuBinary, cfg.Core.HevBinary, cfg.Core.WorkingDir = "", "", ""
	newStore, err := NewStore(newName)
	if err != nil {
		return err
	}
	if err := newStore.Save(cfg); err != nil {
		return err
	}
	if raw, err := os.ReadFile(oldStore.UsageHistoryPath()); err == nil {
		_ = os.WriteFile(newStore.UsageHistoryPath(), raw, 0o644)
	}
	return nil
}

// Startup repairs whatever a previous unclean exit left behind, then honours auto-start.
func (b *Backend) Startup(_ context.Context) {
	b.loopOnce.Do(func() { go b.emitterLoop() })

	if recoverStaleJournal(b.store.JournalPath(), b.logf) {
		b.addLog("warn", "app", "recovered system settings left by an unclean exit")
	}
	b.refreshNetRepair()
	if tunStateStale() {
		b.addLog("warn", "tun", "a previous tunnel session did not clean up; use \"Repair network\"")
	}

	cfg := b.cfgClone()
	b.pf.Apply(cfg.PortForwards)
	if cfg.Core.AutoStart && len(cfg.Nodes) > 0 {
		go func() {
			if err := b.StartProxy(StartRequest{WithTun: cfg.Tun.Enabled}); err != nil && !errors.Is(err, ErrAdminRequired) {
				b.addLog("error", "app", "auto-start failed: "+err.Error())
			}
		}()
	}
}

// Shutdown undoes every side effect, bounded in time. If it cannot finish, the journal is
// left in place so the guard process (or the next launch) finishes the job.
func (b *Backend) Shutdown() {
	b.shutdownOnce.Do(func() {
		b.cancelStart()
		b.restartMu.Lock()
		if b.restartTimer != nil {
			b.restartTimer.Stop()
		}
		b.restartMu.Unlock()

		done := make(chan struct{})
		go func() {
			defer close(done)
			b.opMu.Lock()
			defer b.opMu.Unlock()
			b.teardownLocked()
			b.pf.StopAll()
			if b.rev.IsRunning() {
				_ = b.rev.Stop(2 * time.Second)
			}
		}()
		limit := 8 * time.Second
		if b.kernelRef() != nil || b.GetState().TunRunning {
			limit = 25 * time.Second
		}
		select {
		case <-done:
		case <-time.After(limit):
			b.addLog("error", "app", "shutdown timed out; deferring cleanup to the guard process")
			if k := b.kernelRef(); k != nil {
				killPID(k.proc.PID())
			}
		}
		b.usage.flush()
		close(b.closeCh)
	})
}

func (b *Backend) OpenRuntimeDir() string { return b.store.RuntimeDir() }
func (b *Backend) OpenConfigPath() string { return b.store.ConfigPath() }

func (b *Backend) BuildInfo() map[string]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return map[string]string{
		"runtimeDir": b.store.RuntimeDir(),
		"configPath": b.store.ConfigPath(),
		"logDir":     b.store.LogDir(),
		"sudoku":     b.cfg.Core.SudokuBinary,
	}
}

// EnsureCoreBinaries makes sure the Sudoku core binary is present.
func (b *Backend) EnsureCoreBinaries() error {
	b.mu.Lock()
	before := b.cfg.Core.SudokuBinary
	err := ensureSudokuBinary(&b.cfg.Core, b.store.RuntimeDir(), b.runtimeFS, b.runtimeRoot)
	changed := b.cfg.Core.SudokuBinary != before
	cfg := b.cfg
	b.mu.Unlock()
	if changed {
		_ = b.store.Save(cfg)
	}
	return err
}

// GuardMain is the entry point of `--guard <ppid>`.
func GuardMain(pidStr string) {
	pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
	if err != nil || pid <= 0 {
		return
	}
	store, err := NewStore(storeName)
	if err != nil {
		return
	}
	RunGuard(pid, store.JournalPath())
}
