package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Backend struct {
	mu           sync.RWMutex
	opMu         sync.Mutex
	startMu      sync.Mutex
	startOpID    uint64
	startCancel  context.CancelFunc
	shutdownOnce sync.Once

	eventMu             sync.RWMutex
	eventEmitter        func(name string, payload any)
	emitStateCh         chan RuntimeState
	emitLogCh           chan LogEntry
	dnsProxy            *dnsProxyServer
	store               *Store
	cfg                 *AppConfig
	state               RuntimeState
	coreProc            *ManagedProcess
	tunProc             *ManagedProcess
	tunAdmin            adminDetachedProcess
	runningTunInterface string
	tunRecovering       bool
	darwinNetRepairing  bool
	darwinNetLastCheck  time.Time
	darwinNetLastSig    string
	darwinNetLastErr    string
	darwinNetLastErrAt  time.Time
	revProc             *ManagedProcess
	routeState          *routeContext
	pfMgr               *portForwardManager
	sysProxyRestore     func() error

	logs             []LogEntry
	connections      map[string]*ActiveConnection
	latencyByID      map[string]LatencyResult
	trafficCache     trafficSampleState
	runningLocalPort int

	pacURL      string
	pacServer   *http.Server
	pacListener net.Listener

	usageDays      []UsageDay
	usageDirty     bool
	lastUsageFlush time.Time

	tickerStop         chan struct{}
	bundledRuntimeFS   fs.FS
	bundledRuntimeRoot string
}

func NewBackend() (*Backend, error) {
	return newBackendWithRuntimeFS(nil, "")
}

func NewBackendWithRuntimeFS(runtimeFS fs.FS, runtimeRoot string) (*Backend, error) {
	return newBackendWithRuntimeFS(runtimeFS, runtimeRoot)
}

func newBackendWithRuntimeFS(runtimeFS fs.FS, runtimeRoot string) (*Backend, error) {
	const newStoreName = "4x4-sudoku"
	const oldStoreName = "sudoku-desktop"

	if err := migrateStoreIfNeeded(oldStoreName, newStoreName); err != nil {
		return nil, err
	}

	store, err := NewStore(newStoreName)
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
	b := &Backend{
		store:              store,
		cfg:                cfg,
		coreProc:           NewManagedProcess("sudoku"),
		tunProc:            NewManagedProcess("hev"),
		tunAdmin:           newAdminDetachedProcess(),
		revProc:            NewManagedProcess("reverse"),
		connections:        map[string]*ActiveConnection{},
		latencyByID:        map[string]LatencyResult{},
		tickerStop:         make(chan struct{}),
		logs:               make([]LogEntry, 0, 512),
		emitStateCh:        make(chan RuntimeState, 8),
		emitLogCh:          make(chan LogEntry, 512),
		bundledRuntimeFS:   runtimeFS,
		bundledRuntimeRoot: runtimeRoot,
	}
	if (runtime.GOOS == "darwin" || runtime.GOOS == "linux") && b.tunAdmin != nil {
		// Allow detecting/stopping leftover root HEV processes after crash/force-quit.
		configureAdminDetachedProcess(b.tunAdmin, store, cfg)
	}
	b.usageDays = trimUsageDays(loadUsageHistory(store.UsageHistoryPath()), 120)
	b.pfMgr = newPortForwardManager(func(line string) {
		b.addLog("info", "forward", line)
	})
	b.state.ActiveNodeID = cfg.ActiveNodeID
	if node := b.findNode(cfg.ActiveNodeID); node != nil {
		b.state.ActiveNodeName = node.Name
	}
	return b, nil
}

func (b *Backend) emitterLoop() {
	for {
		select {
		case <-b.tickerStop:
			return
		case state := <-b.emitStateCh:
			b.emitEvent(EventStateUpdated, state)
		case entry := <-b.emitLogCh:
			b.emitEvent(EventLogAdded, entry)
		}
	}
}

func (b *Backend) SetEventEmitter(emit func(name string, payload any)) {
	b.eventMu.Lock()
	b.eventEmitter = emit
	b.eventMu.Unlock()
}

func (b *Backend) emitEvent(name string, payload any) {
	b.eventMu.RLock()
	emit := b.eventEmitter
	b.eventMu.RUnlock()
	if emit == nil {
		return
	}
	emit(name, payload)
}

func (b *Backend) newStartContext() (context.Context, uint64) {
	b.startMu.Lock()
	defer b.startMu.Unlock()
	if b.startCancel != nil {
		b.startCancel()
		b.startCancel = nil
	}
	b.startOpID++
	opID := b.startOpID
	ctx, cancel := context.WithCancel(context.Background())
	b.startCancel = cancel
	return ctx, opID
}

func (b *Backend) cancelStart() {
	b.startMu.Lock()
	defer b.startMu.Unlock()
	if b.startCancel != nil {
		b.startCancel()
		b.startCancel = nil
	}
}

func (b *Backend) clearStartIfMatch(opID uint64) {
	b.startMu.Lock()
	defer b.startMu.Unlock()
	if b.startOpID != opID {
		return
	}
	if b.startCancel != nil {
		b.startCancel()
	}
	b.startCancel = nil
}

func (b *Backend) tunRunningLocked() bool {
	if b.tunAdmin != nil && b.tunAdmin.IsRunning() {
		return true
	}
	return b.tunProc.IsRunning()
}

func (b *Backend) stopTunLocked(timeout time.Duration) error {
	var err1 error
	var err2 error
	err1 = b.tunProc.Stop(timeout)
	if b.tunAdmin != nil && (b.tunAdmin.PID() > 0 || b.tunAdmin.IsRunning()) {
		err2 = b.tunAdmin.Stop(timeout)
	}
	if err1 == nil {
		return err2
	}
	if err2 == nil {
		return err1
	}
	return fmt.Errorf("stop tun: %w; %v", err1, err2)
}

func migrateStoreIfNeeded(oldStoreName, newStoreName string) error {
	cfgRoot, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("resolve user config dir: %w", err)
	}
	oldRoot := filepath.Join(cfgRoot, oldStoreName)
	oldConfig := filepath.Join(oldRoot, "config.json")
	newRoot := filepath.Join(cfgRoot, newStoreName)
	newConfig := filepath.Join(newRoot, "config.json")

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

	oldStore, err := NewStore(oldStoreName)
	if err != nil {
		return err
	}
	cfg, err := oldStore.Load()
	if err != nil {
		return err
	}

	cfg.Core.SudokuBinary = ""
	cfg.Core.HevBinary = ""
	cfg.Core.WorkingDir = ""

	newStore, err := NewStore(newStoreName)
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

func (b *Backend) Startup(_ context.Context) {
	b.mu.Lock()
	autoStart := b.cfg.Core.AutoStart
	withTun := b.cfg.Tun.Enabled
	b.mu.Unlock()

	go b.emitterLoop()
	b.startPACServer()

	go b.monitorLoop()
	if autoStart {
		go func() {
			_ = b.StartProxy(StartRequest{WithTun: withTun})
		}()
	}
}

func (b *Backend) Shutdown() {
	// Security: never keep the sudo password in memory longer than the app lifetime.
	// This runs after StopProxy/cleanup finishes (or times out).
	defer func() {
		_ = darwinAdminForget()
		_ = linuxAdminForget()
	}()

	b.shutdownOnce.Do(func() {
		close(b.tickerStop)
	})

	b.mu.RLock()
	hasActiveRoutes := b.routeState != nil || b.state.TunRunning
	b.mu.RUnlock()
	shutdownTimeout := 4 * time.Second
	if hasActiveRoutes {
		// If TUN routes are active, prioritize restoring the user's network over fast quit.
		// Route/DNS restore can take time on macOS during network transitions.
		shutdownTimeout = 90 * time.Second
	}

	done := make(chan struct{})
	go func() {
		_ = b.StopReverseForwarder()
		// StopProxy can fail transiently on macOS during Wi‑Fi transitions (default route/DNS not ready).
		// On shutdown we keep retrying for a bounded time so we don't exit while the machine would be left offline.
		deadline := time.Now().Add(shutdownTimeout)
		for {
			err := b.StopProxy()
			b.mu.RLock()
			stillHasRoutes := b.routeState != nil || b.state.TunRunning
			b.mu.RUnlock()
			if err == nil || !stillHasRoutes {
				break
			}
			if errors.Is(err, ErrAdminRequired) {
				// Without admin privileges we can't safely fix routes/DNS; avoid busy-looping.
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(600 * time.Millisecond)
		}
		close(done)
	}()

	// Never hang the app on quit forever, but avoid leaving the machine offline.
	select {
	case <-done:
	case <-time.After(shutdownTimeout):
		// Best-effort cleanup without blocking shutdown. If we still have active routes,
		// do NOT kill core/tun processes (that can leave the system offline).
		_ = b.revProc.Stop(800 * time.Millisecond)
		b.mu.RLock()
		stillHasRoutes := b.routeState != nil || b.state.TunRunning
		b.mu.RUnlock()
		if !stillHasRoutes {
			_ = b.stopTunLocked(800 * time.Millisecond)
			_ = b.coreProc.Stop(800 * time.Millisecond)
		} else {
			b.addLog("warn", "app", "shutdown timed out while restoring TUN routes; leaving proxy processes running to avoid offline state")
		}
		b.pfMgr.StopAll()
	}
	b.stopPACServer()
}

func (b *Backend) StartProxy(req StartRequest) error {
	b.opMu.Lock()

	b.mu.Lock()
	if b.state.Running {
		b.mu.Unlock()
		b.opMu.Unlock()
		return nil
	}
	// If the app was force-quit previously, a detached TUN process and/or routes may still be active.
	// Clean them up before starting a new session to avoid "process already running" and offline states.
	prevRouteState := b.routeState
	prevTunRunning := b.tunRunningLocked()
	prevTunIf := strings.TrimSpace(b.runningTunInterface)
	if err := b.ensureCoreBinariesLocked(); err != nil {
		b.state.LastError = err.Error()
		b.emitStateLocked()
		b.mu.Unlock()
		b.opMu.Unlock()
		return err
	}
	node := b.findNode(b.cfg.ActiveNodeID)
	if node == nil {
		b.mu.Unlock()
		b.opMu.Unlock()
		return errors.New("no active node")
	}
	nodeCopy := *node
	withTun := req.WithTun && b.cfg.Tun.Enabled
	runtimeCfg, runtimeWarnings, err := effectiveRuntimeConfig(b.cfg, withTun)
	if err != nil {
		b.state.LastError = err.Error()
		b.emitStateLocked()
		b.mu.Unlock()
		b.opMu.Unlock()
		return err
	}
	sudokuCfgPath, hevCfgPath, localPort, err := writeRuntimeConfigs(b.store, runtimeCfg, nodeCopy, b.pacURL)
	if err != nil {
		b.state.LastError = err.Error()
		b.emitStateLocked()
		b.mu.Unlock()
		b.opMu.Unlock()
		return err
	}
	workDir := runtimeCfg.Core.WorkingDir
	sudokuBin := runtimeCfg.Core.SudokuBinary
	hevBin := runtimeCfg.Core.HevBinary
	coreLogLevel := runtimeCfg.Core.LogLevel
	trafficStatsFile := filepath.Join(b.store.RuntimeDir(), "traffic_stats.json")
	routingCfg := runtimeCfg.Routing
	tunCfg := runtimeCfg.Tun
	portForwards := append([]PortForwardRule(nil), runtimeCfg.PortForwards...)

	b.mu.Unlock()

	for _, warning := range runtimeWarnings {
		b.addLog("warn", "dns", warning)
	}

	if runtime.GOOS == "darwin" {
		detectedTunIf := strings.TrimSpace(darwinFindTunInterfaceByIPv4(tunCfg.IPv4))
		if prevRouteState != nil || prevTunRunning || detectedTunIf != "" {
			tunIf := strings.TrimSpace(prevTunIf)
			if tunIf == "" {
				tunIf = detectedTunIf
			}
			if tunIf == "" {
				if routes, err := darwinNetstatRoutesIPv4(); err == nil {
					for _, r := range routes {
						if r.Destination != "default" {
							continue
						}
						if !darwinIsTunLikeInterface(r.Netif) {
							continue
						}
						if strings.TrimSpace(r.Netif) == "" {
							continue
						}
						tunIf = strings.TrimSpace(r.Netif)
						break
					}
				}
			}
			if tunIf != "" {
				tunCfg.InterfaceName = tunIf
			}

			ctx := prevRouteState
			if ctx == nil {
				physIf, _ := darwinResolveOutboundBypassInterface(2 * time.Second)
				physIf = strings.TrimSpace(physIf)
				emerg := &routeContext{
					DefaultInterface: physIf,
					PFAnchor:         fmt.Sprintf("com.apple/sudoku4x4.tun.%d", os.Getuid()),
				}
				if physIf != "" {
					if svc, err := darwinNetworkServiceForDevice(physIf); err == nil && strings.TrimSpace(svc) != "" {
						emerg.DarwinDNSSnapshots = []darwinDNSSnapshot{{
							Service:      strings.TrimSpace(svc),
							Servers:      nil,
							WasAutomatic: true,
						}}
					}
				}
				ctx = emerg
			}

			b.addLog("warn", "tun", "darwin: detected stale TUN state; tearing down before start")
			if err := teardownRoutes(ctx, tunCfg, func(line string) {
				b.addLog("info", "route", line)
			}); err != nil {
				b.mu.Lock()
				b.state.NeedsAdmin = isLikelyPermissionError(err)
				b.state.RouteSetupError = err.Error()
				b.state.LastError = err.Error()
				b.emitStateLocked()
				b.mu.Unlock()
				b.opMu.Unlock()
				return err
			}
			if err := b.stopTunLocked(6 * time.Second); err != nil && b.tunRunningLocked() {
				b.mu.Lock()
				b.state.LastError = err.Error()
				b.emitStateLocked()
				b.mu.Unlock()
				b.opMu.Unlock()
				return err
			}

			b.mu.Lock()
			b.routeState = nil
			b.runningTunInterface = ""
			b.tunRecovering = false
			b.state.TunRunning = false
			b.state.NeedsAdmin = false
			b.state.RouteSetupError = ""
			b.emitStateLocked()
			b.mu.Unlock()
		}
	}
	if runtime.GOOS == "linux" {
		// Best-effort cleanup of stale policy routes and/or a detached root HEV process
		// after crash/force-quit, to avoid offline states and "hev already running".
		detectedRoutes := false
		if tunCfg.RouteTable > 0 {
			if out, err := exec.Command("ip", "rule", "show").CombinedOutput(); err == nil {
				needle := fmt.Sprintf("lookup %d", tunCfg.RouteTable)
				for _, line := range strings.Split(string(out), "\n") {
					line = strings.TrimSpace(line)
					if !strings.HasPrefix(line, "20:") {
						continue
					}
					if strings.Contains(line, needle) {
						detectedRoutes = true
						break
					}
				}
			}
		}
		if detectedRoutes && strings.TrimSpace(tunCfg.InterfaceName) != "" {
			if out, err := exec.Command("ip", "route", "show", "table", fmt.Sprintf("%d", tunCfg.RouteTable)).CombinedOutput(); err == nil {
				routeOut := string(out)
				if !strings.Contains(routeOut, "default") || !strings.Contains(routeOut, "dev "+strings.TrimSpace(tunCfg.InterfaceName)) {
					detectedRoutes = false
				}
			}
		}
		if prevRouteState != nil || prevTunRunning || detectedRoutes {
			ctx := prevRouteState
			if ctx == nil {
				emerg := &routeContext{
					ServerIP:              resolveServerIPFromAddress(nodeCopy.ServerAddress),
					LinuxResolvConfBackup: fmt.Sprintf("/tmp/sudoku4x4-resolv.conf.%d.bak", os.Getuid()),
				}
				if srcIP, err := linuxDefaultOutboundIPv4(); err == nil && strings.TrimSpace(srcIP) != "" {
					emerg.LinuxOutboundSrcIP = strings.TrimSpace(srcIP)
				}
				ctx = emerg
			}

			b.addLog("warn", "tun", "linux: detected stale TUN state; tearing down before start")
			if err := teardownRoutes(ctx, tunCfg, func(line string) {
				b.addLog("info", "route", line)
			}); err != nil {
				b.mu.Lock()
				b.state.NeedsAdmin = isLikelyPermissionError(err)
				b.state.RouteSetupError = err.Error()
				b.state.LastError = err.Error()
				b.emitStateLocked()
				b.mu.Unlock()
				b.opMu.Unlock()
				return err
			}
			if err := b.stopTunLocked(6 * time.Second); err != nil && b.tunRunningLocked() {
				b.mu.Lock()
				b.state.LastError = err.Error()
				b.emitStateLocked()
				b.mu.Unlock()
				b.opMu.Unlock()
				return err
			}

			b.mu.Lock()
			b.routeState = nil
			b.runningTunInterface = ""
			b.tunRecovering = false
			b.state.TunRunning = false
			b.state.NeedsAdmin = false
			b.state.RouteSetupError = ""
			b.emitStateLocked()
			b.mu.Unlock()
		}
	}

	startCtx, startID := b.newStartContext()
	defer b.clearStartIfMatch(startID)

	if err := ensureDir(workDir); err != nil {
		b.mu.Lock()
		b.state.LastError = err.Error()
		b.emitStateLocked()
		b.mu.Unlock()
		b.opMu.Unlock()
		return err
	}

	coreEnv := []string{
		"SUDOKU_LOG_LEVEL=" + coreLogLevel,
		"SUDOKU_TRAFFIC_REPORT=1",
		"SUDOKU_TRAFFIC_INTERVAL_MS=1000",
		"SUDOKU_TRAFFIC_FILE=" + trafficStatsFile,
	}
	if err := writeCoreTrafficStatsFile(trafficStatsFile, coreTrafficFileSnapshot{}); err != nil {
		b.addLog("warn", "traffic", fmt.Sprintf("prepare traffic stats file failed: %v", err))
	}
	if withTun {
		switch runtime.GOOS {
		case "linux":
			if tunCfg.SocksMark > 0 {
				coreEnv = append(coreEnv, fmt.Sprintf("SUDOKU_OUTBOUND_MARK=%d", tunCfg.SocksMark))
			}
			if srcIP, err := linuxDefaultOutboundIPv4(); err == nil && strings.TrimSpace(srcIP) != "" {
				coreEnv = append(coreEnv, "SUDOKU_OUTBOUND_SRC_IP="+strings.TrimSpace(srcIP))
			}
		case "darwin":
			// Best-effort: resolving the physical interface can transiently fail during Wi‑Fi transitions.
			// Never abort start because of this; host routes + route repair keep the core reachable.
			ifName, derr := darwinResolveOutboundBypassInterface(4 * time.Second)
			if derr != nil {
				b.addLog("warn", "route", fmt.Sprintf("darwin: resolve outbound bypass interface failed; continuing without interface-bind: %v", derr))
			} else if strings.TrimSpace(ifName) == "" {
				b.addLog("warn", "route", "darwin: outbound bypass interface not found; continuing without interface-bind")
			} else {
				ifName = strings.TrimSpace(ifName)
				coreEnv = append(coreEnv, "SUDOKU_OUTBOUND_IFACE="+ifName)
			}
		case "windows":
			if ifIndex, werr := windowsDefaultInterfaceIndex(); werr == nil && ifIndex > 0 {
				coreEnv = append(coreEnv, fmt.Sprintf("SUDOKU_OUTBOUND_IFINDEX=%d", ifIndex))
			}
		}
	}

	b.addLog("info", "app", fmt.Sprintf("starting sudoku core with config %s", sudokuCfgPath))
	if err := b.coreProc.Start(sudokuBin, []string{"-c", sudokuCfgPath}, coreEnv, workDir, b.onCoreLog); err != nil {
		b.mu.Lock()
		b.state.LastError = err.Error()
		b.emitStateLocked()
		b.mu.Unlock()
		b.opMu.Unlock()
		return err
	}

	if !withTun {
		if err := b.applySystemProxyWhenCoreReady(startCtx, localPort, 5*time.Second); err != nil {
			b.addLog("warn", "proxy", fmt.Sprintf("initial system proxy setup deferred: %v; will retry in background", err))
			go b.retryApplySystemProxy(startCtx, localPort, 30*time.Second)
		}
	}

	b.mu.Lock()
	b.trafficCache = trafficSampleState{}
	b.trafficCache.coreTrafficFile = trafficStatsFile
	b.connections = map[string]*ActiveConnection{}
	b.state.Traffic = TrafficState{RecentBandwidth: []BandwidthSample{}}
	b.state.CoreRunning = true
	b.runningLocalPort = localPort
	b.state.ActiveNodeID = nodeCopy.ID
	b.state.ActiveNodeName = nodeCopy.Name
	b.state.StartedAtUnix = time.Now().UnixMilli()
	b.state.Running = true
	b.cfg.LastStartedNode = nodeCopy.ID
	_ = b.store.Save(b.cfg)
	b.emitStateLocked()
	b.mu.Unlock()

	// Core is up; allow StopProxy to proceed even if TUN/route requires admin interaction.
	b.opMu.Unlock()

	if err := startCtx.Err(); err != nil {
		_ = b.coreProc.Stop(2 * time.Second)
		b.mu.Lock()
		b.state.CoreRunning = false
		b.state.Running = false
		b.state.LastError = err.Error()
		b.emitStateLocked()
		b.mu.Unlock()
		return err
	}

	if withTun {
		beforeTunIfs := map[string]struct{}{}
		if runtime.GOOS == "darwin" {
			beforeTunIfs = darwinListTunInterfaces()
		}

		dnsRuntime, err := b.prepareTunDNSRuntime(startCtx, runtimeCfg, localPort)
		if err != nil {
			_ = b.coreProc.Stop(3 * time.Second)
			b.mu.Lock()
			b.state.CoreRunning = false
			b.state.Running = false
			b.state.LastError = err.Error()
			b.emitStateLocked()
			b.mu.Unlock()
			return fmt.Errorf("start local dns proxy: %w", err)
		}
		dnsProxyOK := false
		if dnsRuntime != nil && dnsRuntime.proxy != nil {
			b.mu.Lock()
			b.dnsProxy = dnsRuntime.proxy
			b.mu.Unlock()
			defer func() {
				if dnsProxyOK {
					return
				}
				b.mu.Lock()
				dp := b.dnsProxy
				if dp == dnsRuntime.proxy {
					b.dnsProxy = nil
				}
				b.mu.Unlock()
				if dp != nil {
					dp.Stop()
				}
			}()
			if strings.TrimSpace(dnsRuntime.systemDNSAddress) != "" {
				tunCfg.MapDNSAddress = dnsRuntime.systemDNSAddress
			}
			tunCfg.MapDNSLocalProxy = true
		}

		mapDNSAddr := ""
		if strings.TrimSpace(tunCfg.MapDNSAddress) != "" && tunCfg.MapDNSPort > 0 {
			mapDNSAddr = net.JoinHostPort(strings.TrimSpace(tunCfg.MapDNSAddress), fmt.Sprintf("%d", tunCfg.MapDNSPort))
		}
		mapDNSEnabled := tunCfg.MapDNSEnabled && mapDNSAddr != ""
		tunCfg.MapDNSEnabled = mapDNSEnabled
		if mapDNSEnabled {
			if dnsRuntime != nil && dnsRuntime.proxy != nil {
				b.addLog("info", "dns", fmt.Sprintf("system DNS override enabled: %s:53 -> localhost:%d", localLoopbackIPv4, localDNSProxyListenPort()))
			} else {
				b.addLog("info", "dns", fmt.Sprintf("HEV MapDNS enabled: %s", mapDNSAddr))
			}
		} else {
			b.addLog("warn", "dns", "HEV MapDNS disabled; TUN mode will use the system resolver")
		}

		b.addLog("info", "tun", fmt.Sprintf("starting hev with config %s", hevCfgPath))
		hevCmd := hevBin
		hevArgs := []string{hevCfgPath}
		hevWorkDir := workDir
		hevLogFile := ""
		routeAlreadySetup := false
		var preRouteCtx *routeContext
		if runtime.GOOS == "windows" {
			// Ensure Windows DLL dependencies (wintun.dll/msys-2.0.dll) can be resolved reliably.
			hevWorkDir = filepath.Dir(hevBin)
		}

		if (runtime.GOOS == "darwin" || runtime.GOOS == "linux") && os.Geteuid() != 0 && b.tunAdmin != nil {
			pidFile := filepath.Join(b.store.RuntimeDir(), "hev.pid")
			logFile := filepath.Join(b.store.LogDir(), "hev.log")
			hevLogFile = logFile
			b.addLog("warn", "tun", "starting TUN requires administrator privileges; waiting for password...")
			pid, err := b.tunAdmin.Start(hevCmd, []string{hevCfgPath}, hevWorkDir, pidFile, logFile)
			if err != nil {
				_ = b.coreProc.Stop(3 * time.Second)
				b.mu.Lock()
				b.state.CoreRunning = false
				b.state.Running = false
				b.state.NeedsAdmin = isLikelyPermissionError(err)
				b.state.RouteSetupError = err.Error()
				b.state.LastError = err.Error()
				b.emitStateLocked()
				b.mu.Unlock()
				return err
			}
			b.addLog("info", "tun", fmt.Sprintf("hev started as admin (pid=%d, log=%s)", pid, logFile))
		} else {
			if err := b.tunProc.Start(hevCmd, hevArgs, nil, hevWorkDir, b.onTunLog); err != nil {
				_ = b.coreProc.Stop(3 * time.Second)
				b.mu.Lock()
				b.state.CoreRunning = false
				b.state.Running = false
				b.state.NeedsAdmin = isLikelyPermissionError(err)
				b.state.RouteSetupError = err.Error()
				b.state.LastError = err.Error()
				b.emitStateLocked()
				b.mu.Unlock()
				return err
			}
		}

		select {
		case <-time.After(900 * time.Millisecond):
		case <-startCtx.Done():
			_ = b.stopTunLocked(2 * time.Second)
			_ = b.coreProc.Stop(2 * time.Second)
			b.mu.Lock()
			b.state.TunRunning = false
			b.state.CoreRunning = false
			b.state.Running = false
			b.state.LastError = startCtx.Err().Error()
			b.emitStateLocked()
			b.mu.Unlock()
			return startCtx.Err()
		}
		if runtime.GOOS == "darwin" && !routeAlreadySetup {
			actual := darwinWaitNewTunInterface(beforeTunIfs, 3*time.Second)
			if actual == "" {
				actual = darwinFindTunInterfaceByIPv4(tunCfg.IPv4)
			}
			if actual != "" {
				b.runningTunInterface = actual
				b.addLog("info", "tun", fmt.Sprintf("detected tunnel interface: %s", actual))
			}
		}
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
			if !b.tunRunningLocked() {
				tail := ""
				if hevLogFile != "" {
					tail = tailFile(hevLogFile, 60)
				}
				err := errors.New("hev exited early")
				if tail != "" {
					err = fmt.Errorf("hev exited early:\n%s", tail)
				}
				_ = b.stopTunLocked(2 * time.Second)
				_ = b.coreProc.Stop(2 * time.Second)
				b.mu.Lock()
				b.state.NeedsAdmin = isLikelyPermissionError(err)
				b.state.RouteSetupError = err.Error()
				b.state.TunRunning = false
				b.state.CoreRunning = false
				b.state.Running = false
				b.state.LastError = err.Error()
				b.emitStateLocked()
				b.mu.Unlock()
				return err
			}
		}
		if runtime.GOOS == "darwin" && b.runningTunInterface != "" {
			tunCfg.InterfaceName = b.runningTunInterface
		}
		routeCtx := preRouteCtx
		if !routeAlreadySetup {
			var routeErr error
			routeCtx, routeErr = setupRoutes(nodeCopy, tunCfg, func(line string) {
				b.addLog("info", "route", line)
			})
			if routeErr != nil {
				_ = b.stopTunLocked(2 * time.Second)
				_ = b.coreProc.Stop(2 * time.Second)
				b.mu.Lock()
				b.state.NeedsAdmin = isLikelyPermissionError(routeErr)
				b.state.RouteSetupError = routeErr.Error()
				b.state.TunRunning = false
				b.state.CoreRunning = false
				b.state.Running = false
				b.state.LastError = routeErr.Error()
				b.emitStateLocked()
				b.mu.Unlock()
				return routeErr
			}
		}

		// Post-start health checks (production safety):
		// If we changed system routes/DNS but the proxy isn't actually usable, revert immediately.
		socksAddr := net.JoinHostPort(localLoopbackIPv4, fmt.Sprintf("%d", localPort))
		hctx, cancelHC := context.WithTimeout(context.Background(), 10*time.Second)
		hcErr := func() error {
			if err := waitForTCPReady(hctx, socksAddr, 3*time.Second); err != nil {
				return fmt.Errorf("core socks not ready: %w", err)
			}
			proxyMode := strings.ToLower(strings.TrimSpace(routingCfg.ProxyMode))
			if proxyMode == "global" || proxyMode == "pac" {
				// 1) Verify SOCKS can CONNECT to an external IP (tests proxy path without DNS).
				if err := healthCheckSOCKS5Connect(hctx, socksAddr, "1.1.1.1:443", 3*time.Second); err != nil {
					return fmt.Errorf("socks proxy-path check failed: %w", err)
				}
			}
			if proxyMode == "direct" || proxyMode == "pac" {
				// 1.2) Verify a "domestic direct" path is usable. This catches PAC+TUN loop issues where
				// many DIRECT flows become unreachable while PROXY still works.
				if err := healthCheckSOCKS5ConnectAny(hctx, socksAddr, []string{"223.5.5.5:443", "119.29.29.29:443"}, 3*time.Second); err != nil {
					// Some networks block these specific probes even when TUN dataplane is healthy.
					// On macOS keep this check non-fatal.
					if runtime.GOOS == "darwin" {
						b.addLog("warn", "tun", fmt.Sprintf("socks direct-path check failed (non-fatal on darwin): %v", err))
					} else {
						return fmt.Errorf("socks direct-path check failed: %w", err)
					}
				}
			}
			if runtime.GOOS == "darwin" {
				// 1.5) Best-effort observability check for system egress after default-route switch.
				// Keep it non-fatal: some networks block these fixed targets even when TUN is healthy.
				if err := healthCheckSystemTCPAny(hctx, []string{"223.5.5.5:443", "1.1.1.1:443"}, 2500*time.Millisecond); err != nil {
					b.addLog("warn", "tun", fmt.Sprintf("system egress check failed (non-fatal): %v", err))
				}
			}
			dnsHealthAddr := mapDNSAddr
			if dnsRuntime != nil && strings.TrimSpace(dnsRuntime.healthAddr) != "" {
				dnsHealthAddr = strings.TrimSpace(dnsRuntime.healthAddr)
			}
			if tunCfg.MapDNSEnabled && dnsHealthAddr != "" {
				if err := healthCheckDNSUDP(hctx, dnsHealthAddr, "www.baidu.com", 2*time.Second); err != nil {
					return fmt.Errorf("mapdns check failed: %w", err)
				}
			}
			return nil
		}()
		cancelHC()
		if hcErr != nil {
			// Best-effort immediate rollback. This avoids leaving the user's machine offline.
			b.addLog("warn", "app", fmt.Sprintf("post-start health check failed; reverting: %v", hcErr))
			var rollbackErr error
			if routeCtx != nil {
				if err := teardownRoutes(routeCtx, tunCfg, func(line string) {
					b.addLog("info", "route", line)
				}); err != nil {
					rollbackErr = err
					b.addLog("warn", "route", fmt.Sprintf("rollback routes failed: %v", err))
				}
			}
			if rollbackErr != nil {
				// Safety: never stop the TUN session when we couldn't restore system routes.
				// Otherwise the machine can look completely offline.
				b.addLog("warn", "app", "rollback failed; keeping TUN running to avoid offline state")
				b.mu.Lock()
				b.routeState = routeCtx
				b.state.NeedsAdmin = isLikelyPermissionError(rollbackErr)
				b.state.RouteSetupError = hcErr.Error()
				b.state.TunRunning = b.tunRunningLocked()
				b.state.CoreRunning = b.coreProc.IsRunning()
				b.state.Running = b.state.CoreRunning
				b.state.LastError = fmt.Sprintf("post-start health check failed; rollback failed, keeping TUN running: %v", rollbackErr)
				b.emitStateLocked()
				b.mu.Unlock()
				dnsProxyOK = true
				return fmt.Errorf("post-start health check failed: %w; rollback failed: %v", hcErr, rollbackErr)
			}
			_ = b.stopTunLocked(4 * time.Second)
			if runtime.GOOS == "darwin" {
				// Production fallback on macOS: keep core running and switch to system-proxy mode
				// so users are not left offline when TUN dataplane is unhealthy.
				b.addLog("warn", "app", fmt.Sprintf("darwin fallback: keep core running without TUN: %v", hcErr))

				restore, perr := applySystemProxy(systemProxyConfig{
					LocalPort: localPort,
					Logf: func(line string) {
						b.addLog("info", "proxy", line)
					},
				})
				if perr != nil {
					b.addLog("warn", "proxy", fmt.Sprintf("fallback set system proxy failed: %v", perr))
				} else if restore != nil {
					b.mu.Lock()
					b.sysProxyRestore = restore
					b.mu.Unlock()
					b.addLog("info", "proxy", "fallback system proxy configured")
				}

				b.mu.Lock()
				b.routeState = nil
				b.state.NeedsAdmin = false
				b.state.RouteSetupError = hcErr.Error()
				b.state.TunRunning = false
				b.state.CoreRunning = b.coreProc.IsRunning()
				b.state.Running = b.state.CoreRunning
				b.state.LastError = "TUN unavailable, fallback to system-proxy mode: " + hcErr.Error()
				b.emitStateLocked()
				b.mu.Unlock()
				return nil
			}

			_ = b.coreProc.Stop(2 * time.Second)
			b.mu.Lock()
			b.state.NeedsAdmin = true
			b.state.RouteSetupError = hcErr.Error()
			b.state.TunRunning = false
			b.state.CoreRunning = false
			b.state.Running = false
			b.state.LastError = hcErr.Error()
			b.emitStateLocked()
			b.mu.Unlock()
			return hcErr
		}

		b.mu.Lock()
		b.routeState = routeCtx
		if runtime.GOOS == "windows" && routeCtx != nil && strings.TrimSpace(routeCtx.TunAlias) != "" {
			b.runningTunInterface = strings.TrimSpace(routeCtx.TunAlias)
		}
		b.state.TunRunning = true
		b.state.NeedsAdmin = false
		b.state.RouteSetupError = ""
		b.emitStateLocked()
		b.mu.Unlock()
		dnsProxyOK = true
	}

	// Apply port forwards outside b.mu to avoid self-deadlock via b.addLog.
	b.pfMgr.Apply(portForwards)

	b.mu.Lock()
	b.emitStateLocked()
	b.mu.Unlock()
	return nil
}

func (b *Backend) applySystemProxyWhenCoreReady(ctx context.Context, localPort int, readyTimeout time.Duration) error {
	if localPort <= 0 || localPort > 65535 {
		return fmt.Errorf("invalid local port: %d", localPort)
	}
	b.mu.RLock()
	if b.sysProxyRestore != nil {
		b.mu.RUnlock()
		return nil
	}
	b.mu.RUnlock()

	socksAddr := net.JoinHostPort(localLoopbackIPv4, fmt.Sprintf("%d", localPort))
	if err := waitForTCPReady(ctx, socksAddr, readyTimeout); err != nil {
		return fmt.Errorf("core proxy not ready on %s: %w", socksAddr, err)
	}
	restore, err := applySystemProxy(systemProxyConfig{
		LocalPort: localPort,
		Logf: func(line string) {
			b.addLog("info", "proxy", line)
		},
	})
	if err != nil {
		return err
	}
	if restore == nil {
		return nil
	}

	b.mu.Lock()
	if b.sysProxyRestore == nil {
		b.sysProxyRestore = restore
		b.mu.Unlock()
		b.addLog("info", "proxy", "system proxy configured")
		return nil
	}
	b.mu.Unlock()
	_ = restore()
	return nil
}

func (b *Backend) retryApplySystemProxy(ctx context.Context, localPort int, maxDuration time.Duration) {
	if ctx == nil {
		ctx = context.Background()
	}
	if maxDuration <= 0 {
		maxDuration = 20 * time.Second
	}
	deadline := time.Now().Add(maxDuration)
	for {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		if err := b.applySystemProxyWhenCoreReady(ctx, localPort, 3*time.Second); err == nil {
			return
		}
		if time.Now().After(deadline) {
			b.addLog("warn", "proxy", "system proxy background retry timed out")
			return
		}
		select {
		case <-time.After(1200 * time.Millisecond):
		case <-ctx.Done():
			return
		}
	}
}

func (b *Backend) StopProxy() error {
	// Interrupt any pending start (e.g. waiting for admin privileges/network readiness).
	b.cancelStart()
	b.opMu.Lock()
	defer b.opMu.Unlock()

	// Restore system proxy as early as possible to avoid leaving the machine offline.
	var restoreSysProxy func() error
	b.mu.Lock()
	restoreSysProxy = b.sysProxyRestore
	b.sysProxyRestore = nil
	b.mu.Unlock()
	if restoreSysProxy != nil {
		if err := restoreSysProxy(); err != nil {
			b.addLog("warn", "proxy", fmt.Sprintf("restore system proxy failed: %v", err))
		} else {
			b.addLog("info", "proxy", "restored system proxy settings")
		}
	}

	b.mu.Lock()
	routeState := b.routeState
	tunCfg := b.cfg.Tun
	if strings.TrimSpace(b.runningTunInterface) != "" {
		tunCfg.InterfaceName = strings.TrimSpace(b.runningTunInterface)
	}
	b.mu.Unlock()

	// Production safety (darwin):
	// If we lost in-memory routeState (app crash/force-quit) but the TUN interface is still present,
	// attempt an emergency teardown so StopProxy doesn't leave the machine offline.
	if runtime.GOOS == "darwin" && routeState == nil {
		if tunIf := strings.TrimSpace(darwinFindTunInterfaceByIPv4(tunCfg.IPv4)); tunIf != "" {
			tunCfg.InterfaceName = tunIf
			physIf, _ := darwinResolveOutboundBypassInterface(2 * time.Second)
			physIf = strings.TrimSpace(physIf)
			emerg := &routeContext{
				DefaultInterface: physIf,
				PFAnchor:         fmt.Sprintf("com.apple/sudoku4x4.tun.%d", os.Getuid()),
			}
			if physIf != "" {
				if svc, err := darwinNetworkServiceForDevice(physIf); err == nil && strings.TrimSpace(svc) != "" {
					// Without a snapshot, restore DNS to automatic for this service.
					emerg.DarwinDNSSnapshots = []darwinDNSSnapshot{{
						Service:      strings.TrimSpace(svc),
						Servers:      nil,
						WasAutomatic: true,
					}}
				}
			}
			routeState = emerg
			b.addLog("warn", "route", fmt.Sprintf("darwin: recovered missing route state (tunIf=%s); attempting emergency route teardown", tunIf))
		}
	}

	// Route teardown may require admin; it also logs via b.addLog.
	if routeState != nil {
		if err := teardownRoutes(routeState, tunCfg, func(line string) {
			b.addLog("info", "route", line)
		}); err != nil {
			// Safety: never stop the TUN session if we couldn't restore system routes.
			// Otherwise the machine can look completely offline.
			b.addLog("warn", "route", fmt.Sprintf("route teardown failed; abort stop to avoid offline state: %v", err))
			b.mu.Lock()
			b.state.NeedsAdmin = isLikelyPermissionError(err)
			b.state.RouteSetupError = err.Error()
			b.state.LastError = err.Error()
			b.emitStateLocked()
			b.mu.Unlock()
			return err
		}

		// Routes are no longer active at this point. Clear routeState immediately so the monitor
		// loop doesn't mis-detect an "unexpected TUN exit" during an intentional stop/restart.
		b.mu.Lock()
		if b.routeState == routeState {
			b.routeState = nil
		}
		b.emitStateLocked()
		b.mu.Unlock()
	}

	b.mu.Lock()
	dnsProxy := b.dnsProxy
	b.dnsProxy = nil
	b.mu.Unlock()
	if dnsProxy != nil {
		dnsProxy.Stop()
	}

	tunStopTimeout := 2 * time.Second
	if (runtime.GOOS == "darwin" || runtime.GOOS == "linux") && os.Geteuid() != 0 && b.tunAdmin != nil && b.tunAdmin.PID() > 0 {
		// Detached root process teardown can take longer.
		tunStopTimeout = 6 * time.Second
	}
	if err := b.stopTunLocked(tunStopTimeout); err != nil {
		b.addLog("warn", "tun", fmt.Sprintf("stop hev failed: %v", err))
		b.mu.Lock()
		b.state.LastError = err.Error()
		b.emitStateLocked()
		b.mu.Unlock()
	}
	_ = b.coreProc.Stop(3 * time.Second)
	b.pfMgr.StopAll()

	b.mu.Lock()
	b.routeState = nil
	b.state.Running = false
	b.state.TunRunning = false
	b.state.CoreRunning = false
	b.tunRecovering = false
	b.trafficCache = trafficSampleState{}
	b.state.Traffic = TrafficState{RecentBandwidth: []BandwidthSample{}}
	b.runningLocalPort = 0
	b.runningTunInterface = ""
	b.state.NeedsAdmin = false
	b.state.RouteSetupError = ""
	b.emitStateLocked()
	b.mu.Unlock()
	return nil
}

func (b *Backend) RestartProxy(req StartRequest) error {
	if err := b.StopProxy(); err != nil {
		return err
	}
	return b.StartProxy(req)
}

func (b *Backend) OpenRuntimeDir() string {
	return b.store.RuntimeDir()
}

func (b *Backend) OpenConfigPath() string {
	return b.store.ConfigPath()
}

func (b *Backend) BuildInfo() map[string]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return map[string]string{
		"runtimeDir": b.store.RuntimeDir(),
		"configPath": b.store.ConfigPath(),
		"logDir":     b.store.LogDir(),
		"sudoku":     b.cfg.Core.SudokuBinary,
		"hev":        b.cfg.Core.HevBinary,
	}
}

func (b *Backend) EnsureCoreBinaries() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ensureCoreBinariesLocked()
}
