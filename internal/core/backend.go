package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var routeLineRegex = regexp.MustCompile(`(?i)\[(tcp|udp)\]\s+([^\s]+)\s+-->\s+([^\s]+).*using\s+(direct|proxy)`)                          //nolint:lll
var coreTrafficLineRegex = regexp.MustCompile(`__SUDOKU_TRAFFIC__\s+direct_tx=(\d+)\s+direct_rx=(\d+)\s+proxy_tx=(\d+)\s+proxy_rx=(\d+)`) //nolint:lll

type Backend struct {
	mu           sync.RWMutex
	opMu         sync.Mutex
	startMu      sync.Mutex
	startOpID    uint64
	startCancel  context.CancelFunc
	shutdownOnce sync.Once

	ctx                 context.Context
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

	tickerStop chan struct{}
}

func NewBackend() (*Backend, error) {
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
		store:       store,
		cfg:         cfg,
		coreProc:    NewManagedProcess("sudoku"),
		tunProc:     NewManagedProcess("hev"),
		tunAdmin:    newAdminDetachedProcess(),
		revProc:     NewManagedProcess("reverse"),
		connections: map[string]*ActiveConnection{},
		latencyByID: map[string]LatencyResult{},
		tickerStop:  make(chan struct{}),
		logs:        make([]LogEntry, 0, 512),
		emitStateCh: make(chan RuntimeState, 8),
		emitLogCh:   make(chan LogEntry, 512),
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
			if b.ctx != nil {
				wailsruntime.EventsEmit(b.ctx, EventStateUpdated, state)
			}
		case entry := <-b.emitLogCh:
			if b.ctx != nil {
				wailsruntime.EventsEmit(b.ctx, EventLogAdded, entry)
			}
		}
	}
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

func (b *Backend) Startup(ctx context.Context) {
	b.mu.Lock()
	b.ctx = ctx
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
	b.shutdownOnce.Do(func() {
		close(b.tickerStop)
	})

	done := make(chan struct{})
	go func() {
		_ = b.StopReverseForwarder()
		_ = b.StopProxy()
		close(done)
	}()

	// Never hang the app on quit (e.g. if an admin prompt is pending).
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		// Best-effort cleanup without blocking shutdown.
		_ = b.revProc.Stop(800 * time.Millisecond)
		_ = b.stopTunLocked(800 * time.Millisecond)
		_ = b.coreProc.Stop(800 * time.Millisecond)
		b.pfMgr.StopAll()
	}
	b.stopPACServer()
}

func (b *Backend) GetConfig() AppConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cfg := cloneConfig(b.cfg)
	if cfg == nil {
		return AppConfig{}
	}
	return *cfg
}

func (b *Backend) SaveConfig(next AppConfig) error {
	b.mu.Lock()
	prev := cloneConfig(b.cfg)
	wasRunning := b.state.Running
	wasTun := b.state.TunRunning
	normalizeConfig(&next, b.store.RuntimeDir())
	if err := b.store.Save(&next); err != nil {
		b.mu.Unlock()
		return err
	}
	b.cfg = &next
	b.state.ActiveNodeID = next.ActiveNodeID
	if node := b.findNode(next.ActiveNodeID); node != nil {
		b.state.ActiveNodeName = node.Name
	}
	b.emitStateLocked()
	portForwards := append([]PortForwardRule(nil), next.PortForwards...)
	b.mu.Unlock()

	// Apply may emit logs; do it outside b.mu to avoid self-deadlock via b.addLog.
	b.pfMgr.Apply(portForwards)

	if wasRunning && prev != nil && configChangeRequiresRestart(*prev, next) {
		b.addLog("info", "app", "config changed; restarting proxy to apply runtime settings")
		go func(withTun bool) {
			_ = b.RestartProxy(StartRequest{WithTun: withTun})
		}(wasTun)
	}
	return nil
}

func configChangeRequiresRestart(prev AppConfig, next AppConfig) bool {
	if strings.TrimSpace(prev.ActiveNodeID) != strings.TrimSpace(next.ActiveNodeID) {
		return true
	}
	if strings.ToLower(strings.TrimSpace(prev.Routing.ProxyMode)) != strings.ToLower(strings.TrimSpace(next.Routing.ProxyMode)) {
		return true
	}
	if prev.Routing.CustomRulesEnabled != next.Routing.CustomRulesEnabled ||
		strings.TrimSpace(prev.Routing.CustomRules) != strings.TrimSpace(next.Routing.CustomRules) {
		return true
	}
	if strings.Join(prev.Routing.RuleURLs, "\n") != strings.Join(next.Routing.RuleURLs, "\n") {
		return true
	}

	// TUN/runtime behavior.
	if prev.Tun.MapDNSEnabled != next.Tun.MapDNSEnabled ||
		strings.TrimSpace(prev.Tun.MapDNSAddress) != strings.TrimSpace(next.Tun.MapDNSAddress) ||
		prev.Tun.MapDNSPort != next.Tun.MapDNSPort ||
		prev.Tun.BlockQUIC != next.Tun.BlockQUIC {
		return true
	}
	if strings.TrimSpace(prev.Tun.InterfaceName) != strings.TrimSpace(next.Tun.InterfaceName) ||
		strings.TrimSpace(prev.Tun.IPv4) != strings.TrimSpace(next.Tun.IPv4) {
		return true
	}

	// Core settings that affect listeners/logging.
	if prev.Core.LocalPort != next.Core.LocalPort ||
		strings.TrimSpace(prev.Core.LogLevel) != strings.TrimSpace(next.Core.LogLevel) ||
		strings.TrimSpace(prev.Core.WorkingDir) != strings.TrimSpace(next.Core.WorkingDir) {
		return true
	}
	return false
}

func (b *Backend) UpsertNode(node NodeConfig) (NodeConfig, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if node.ID == "" {
		node.ID = newID("node_")
	}
	if strings.TrimSpace(node.Name) == "" {
		node.Name = "Node " + strconvBase36(time.Now().Unix())
	}
	normalizeNode(&node, b.cfg.Core.LocalPort)
	updated := false
	for i := range b.cfg.Nodes {
		if b.cfg.Nodes[i].ID == node.ID {
			b.cfg.Nodes[i] = node
			updated = true
			break
		}
	}
	if !updated {
		b.cfg.Nodes = append(b.cfg.Nodes, node)
	}
	if b.cfg.ActiveNodeID == "" {
		b.cfg.ActiveNodeID = node.ID
	}
	if err := b.store.Save(b.cfg); err != nil {
		return NodeConfig{}, err
	}
	b.emitStateLocked()
	return node, nil
}

func (b *Backend) DeleteNode(nodeID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	idx := -1
	for i := range b.cfg.Nodes {
		if b.cfg.Nodes[i].ID == nodeID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	b.cfg.Nodes = append(b.cfg.Nodes[:idx], b.cfg.Nodes[idx+1:]...)
	delete(b.latencyByID, nodeID)
	if b.cfg.ActiveNodeID == nodeID {
		b.cfg.ActiveNodeID = ""
		if len(b.cfg.Nodes) > 0 {
			b.cfg.ActiveNodeID = b.cfg.Nodes[0].ID
		}
	}
	if err := b.store.Save(b.cfg); err != nil {
		return err
	}
	b.emitStateLocked()
	return nil
}

func (b *Backend) SetActiveNode(nodeID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	node := b.findNode(nodeID)
	if node == nil {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	b.cfg.ActiveNodeID = nodeID
	if err := b.store.Save(b.cfg); err != nil {
		return err
	}
	b.state.ActiveNodeID = nodeID
	b.state.ActiveNodeName = node.Name
	b.emitStateLocked()
	return nil
}

func (b *Backend) ImportShortLink(name string, link string) (NodeConfig, error) {
	node, err := ParseShortLink(strings.TrimSpace(link))
	if err != nil {
		return NodeConfig{}, err
	}
	if strings.TrimSpace(name) != "" {
		node.Name = strings.TrimSpace(name)
	}
	return b.UpsertNode(*node)
}

func (b *Backend) ExportShortLink(nodeID string) (string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	node := b.findNode(nodeID)
	if node == nil {
		return "", fmt.Errorf("node not found")
	}
	return BuildShortLink(*node)
}

func (b *Backend) StartProxy(req StartRequest) error {
	b.opMu.Lock()

	b.mu.Lock()
	if b.state.Running {
		b.mu.Unlock()
		b.opMu.Unlock()
		return nil
	}
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
	sudokuCfgPath, hevCfgPath, localPort, err := writeRuntimeConfigs(b.store, b.cfg, nodeCopy, b.pacURL, withTun)
	if err != nil {
		b.state.LastError = err.Error()
		b.emitStateLocked()
		b.mu.Unlock()
		b.opMu.Unlock()
		return err
	}
	workDir := b.cfg.Core.WorkingDir
	sudokuBin := b.cfg.Core.SudokuBinary
	hevBin := b.cfg.Core.HevBinary
	coreLogLevel := b.cfg.Core.LogLevel
	routingCfg := b.cfg.Routing
	tunCfg := b.cfg.Tun
	portForwards := append([]PortForwardRule(nil), b.cfg.PortForwards...)

	b.mu.Unlock()

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
			gw, ifName, derr := darwinDefaultRoute()
			if derr != nil || strings.TrimSpace(gw) == "" || strings.TrimSpace(ifName) == "" {
				b.mu.Lock()
				b.state.LastError = fmt.Sprintf("unable to resolve default route for outbound bypass: %v", derr)
				b.emitStateLocked()
				b.mu.Unlock()
				b.opMu.Unlock()
				if derr != nil {
					return derr
				}
				return errors.New("unable to resolve default route for outbound bypass")
			}
			ifName = strings.TrimSpace(ifName)
			coreEnv = append(coreEnv, "SUDOKU_OUTBOUND_IFACE="+ifName)
			if ip4 := darwinInterfaceIPv4(ifName); strings.TrimSpace(ip4) != "" {
				coreEnv = append(coreEnv, "SUDOKU_OUTBOUND_SRC_IP="+strings.TrimSpace(ip4))
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

	if !withTun && strings.ToLower(strings.TrimSpace(routingCfg.ProxyMode)) != "direct" {
		socksAddr := net.JoinHostPort(localDNSServerIPv4, fmt.Sprintf("%d", localPort))
		if err := waitForTCPReady(startCtx, socksAddr, 5*time.Second); err != nil {
			b.addLog("warn", "proxy", fmt.Sprintf("core proxy not ready on %s; skip setting system proxy: %v", socksAddr, err))
		} else {
			b.mu.RLock()
			pacURL := b.pacURL
			b.mu.RUnlock()
			restore, err := applySystemProxy(systemProxyConfig{
				ProxyMode: routingCfg.ProxyMode,
				LocalPort: localPort,
				PACURL:    pacURL,
				Logf: func(line string) {
					b.addLog("info", "proxy", line)
				},
			})
			if err != nil {
				b.addLog("warn", "proxy", fmt.Sprintf("set system proxy failed: %v", err))
			} else if restore != nil {
				b.mu.Lock()
				b.sysProxyRestore = restore
				b.mu.Unlock()
				b.addLog("info", "proxy", "system proxy configured")
			}
		}
	}

	b.mu.Lock()
	b.trafficCache = trafficSampleState{}
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
		serverIP := resolveServerIPFromAddress(nodeCopy.ServerAddress)
		beforeTunIfs := map[string]struct{}{}
		if runtime.GOOS == "darwin" {
			beforeTunIfs = darwinListTunInterfaces()
		}

		bypass := tunBypass{}

		// Always serve DNS locally in TUN mode to avoid:
		// - DNS poisoning when MapDNS is disabled (foreign sites unreachable)
		// - FakeIP -> forced PROXY for CN domains when MapDNS is enabled (domestic sites unreachable)
		//
		// The local DNS proxy uses DoH for "direct" domains, and (optionally) forwards other
		// queries to HEV MapDNS for FakeIP mapping.
		effectiveDNS := localDNSServerIPv4

		// Ensure DNS proxy upstream (DoH/plain DNS) bypasses the TUN on all platforms.
		// Otherwise, "direct" domains may resolve from the proxy egress IP and return unreachable/incorrect
		// answers (breaking split routing and sometimes making the whole network look down).
		bypassCfg := outboundBypassConfig{}
		switch runtime.GOOS {
		case "linux":
			if tunCfg.SocksMark > 0 {
				bypassCfg.LinuxMark = tunCfg.SocksMark
			}
			if srcIP, err := linuxDefaultOutboundIPv4(); err == nil && strings.TrimSpace(srcIP) != "" {
				bypassCfg.LinuxSourceIP = strings.TrimSpace(srcIP)
			}
		case "darwin":
			if _, ifName, err := darwinDefaultRoute(); err == nil && strings.TrimSpace(ifName) != "" {
				bypassCfg.DarwinInterface = strings.TrimSpace(ifName)
				if ip4 := darwinInterfaceIPv4(ifName); strings.TrimSpace(ip4) != "" {
					bypassCfg.DarwinSourceIP = strings.TrimSpace(ip4)
				}
			}
		case "windows":
			if ifIndex, err := windowsDefaultInterfaceIndex(); err == nil && ifIndex > 0 {
				bypassCfg.WindowsIfIndex = ifIndex
			}
		}
		directDialer := newOutboundBypassDialer(3*time.Second, bypassCfg)

		// Prepare CN domain rules via the running core SOCKS5 (so fetching works even on restrictive networks).
		var cnRules *cnRuleSet
		if strings.ToLower(strings.TrimSpace(routingCfg.ProxyMode)) == "pac" {
			socksAddr := net.JoinHostPort(localDNSServerIPv4, fmt.Sprintf("%d", localPort))
			if werr := waitForTCPReady(startCtx, socksAddr, 6*time.Second); werr != nil {
				b.addLog("warn", "rule", fmt.Sprintf("core SOCKS5 not ready on %s; skipping PAC rules fetch: %v", socksAddr, werr))
			} else {
				if httpc, herr := newHTTPClientViaSOCKS5(socksAddr, 20*time.Second); herr == nil {
					cnRules, _ = prepareCNRules(startCtx, b.store, b.cfg, httpc, func(line string) {
						b.addLog("info", "rule", line)
					})
				} else {
					b.addLog("warn", "rule", fmt.Sprintf("init SOCKS5 http client failed: %v", herr))
				}
			}
			// Fallback: attempt direct fetch (pre-TUN) to avoid starting with zero rules.
			if cnRules == nil || (len(cnRules.domainExact) == 0 && len(cnRules.domainSuffix) == 0) {
				tr := &http.Transport{DialContext: directDialer.DialContext}
				cnRules, _ = prepareCNRules(startCtx, b.store, b.cfg, &http.Client{Timeout: 20 * time.Second, Transport: tr}, func(line string) {
					b.addLog("info", "rule", line)
				})
			}
			if cnRules != nil && len(cnRules.domainExact) == 0 && len(cnRules.domainSuffix) == 0 {
				cnRules = nil
			}
		}

		mapDNSAddr := ""
		if strings.TrimSpace(tunCfg.MapDNSAddress) != "" && tunCfg.MapDNSPort > 0 {
			mapDNSAddr = net.JoinHostPort(strings.TrimSpace(tunCfg.MapDNSAddress), fmt.Sprintf("%d", tunCfg.MapDNSPort))
		}

		mapDNSEnabled := tunCfg.MapDNSEnabled && mapDNSAddr != ""
		if mapDNSEnabled && strings.ToLower(strings.TrimSpace(routingCfg.ProxyMode)) == "pac" && cnRules == nil {
			// In PAC mode, MapDNS without any CN-domain rules effectively forces everything into FakeIP,
			// which tends to break domestic access hard. Keep the network usable and log a clear warning.
			mapDNSEnabled = false
			b.addLog("warn", "dns", "PAC rules are empty; disabling MapDNS for this run to avoid FakeIP breaking DIRECT traffic")
		}

		dnsProxy := newDNSProxyServer(dnsProxyConfig{
			ProxyMode:     routingCfg.ProxyMode,
			CNRules:       cnRules,
			MapDNSEnabled: mapDNSEnabled,
			MapDNSAddr:    mapDNSAddr,
			DirectDial:    directDialer.DialContext,
			Logf: func(line string) {
				b.addLog("info", "dns", line)
			},
		})
		if err := dnsProxy.Start(); err != nil {
			_ = b.coreProc.Stop(3 * time.Second)
			b.mu.Lock()
			b.state.CoreRunning = false
			b.state.Running = false
			b.state.LastError = err.Error()
			b.emitStateLocked()
			b.mu.Unlock()
			return fmt.Errorf("start dns proxy: %w", err)
		}
		b.mu.Lock()
		b.dnsProxy = dnsProxy
		b.mu.Unlock()
		dnsProxyOK := false
		defer func() {
			if dnsProxyOK {
				return
			}
			b.mu.Lock()
			dp := b.dnsProxy
			if dp == dnsProxy {
				b.dnsProxy = nil
			}
			b.mu.Unlock()
			dnsProxy.Stop()
		}()

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

		if runtime.GOOS == "darwin" && os.Geteuid() != 0 && b.tunAdmin != nil {
			pidFile := filepath.Join(b.store.RuntimeDir(), "hev.pid")
			logFile := filepath.Join(b.store.LogDir(), "hev.log")
			hevLogFile = logFile
			b.addLog("warn", "tun", "starting TUN requires administrator privileges; requesting approval...")
			type startWithRoutesAdmin interface {
				StartWithRoutes(ctx context.Context, command string, args []string, workdir string, pidFile string, logFile string, tunIPv4 string, serverIP string, defaultGateway string, defaultInterface string, dnsSetCmd string, dnsRestoreCmd string, pfSetCmd string, pfRestoreCmd string) (int, string, string, error)
			}
			if dp, ok := b.tunAdmin.(startWithRoutesAdmin); ok {
				gw, ifName, gwErr := darwinDefaultRoute()
				if gwErr != nil {
					_ = b.coreProc.Stop(3 * time.Second)
					b.mu.Lock()
					b.state.CoreRunning = false
					b.state.Running = false
					b.state.NeedsAdmin = true
					b.state.RouteSetupError = gwErr.Error()
					b.state.LastError = gwErr.Error()
					b.emitStateLocked()
					b.mu.Unlock()
					return gwErr
				}

				dnsSetCmd := ""
				dnsRestoreCmd := ""
				dnsService := ""
				var dnsPrev []string
				dnsWasAuto := false
				if effectiveDNS != "" && strings.TrimSpace(ifName) != "" {
					if svc, derr := darwinNetworkServiceForDevice(ifName); derr == nil && strings.TrimSpace(svc) != "" {
						dnsService = svc
						if prev, wasAuto, gerr := darwinGetDNSServers(svc); gerr == nil {
							dnsPrev = prev
							dnsWasAuto = wasAuto
						}
						dnsFlushCmd := "dscacheutil -flushcache >/dev/null 2>&1 || true; killall -HUP mDNSResponder >/dev/null 2>&1 || true"
						dnsSetCmd = shellJoin("networksetup", "-setdnsservers", svc, effectiveDNS) + "; " + dnsFlushCmd
						if dnsWasAuto || len(dnsPrev) == 0 {
							dnsRestoreCmd = shellJoin("networksetup", "-setdnsservers", svc, "Empty") + "; " + dnsFlushCmd
						} else {
							args := append([]string{"networksetup", "-setdnsservers", svc}, dnsPrev...)
							dnsRestoreCmd = shellJoin(args...) + "; " + dnsFlushCmd
						}
						b.addLog("info", "dns", fmt.Sprintf("set system DNS for %s to %s (restore on stop)", dnsService, effectiveDNS))
					} else if derr != nil {
						b.addLog("warn", "dns", fmt.Sprintf("unable to resolve network service for %s: %v", ifName, derr))
					}
				}

				pfSetCmd := ""
				pfRestoreCmd := ""
				pfAnchor := ""
				dnsProxyPort := 0
				if effectiveDNS == localDNSServerIPv4 {
					dnsProxyPort = localDNSProxyListenPort()
				}
				if tunCfg.BlockQUIC || dnsProxyPort > 0 {
					pfAnchor = fmt.Sprintf("com.apple/sudoku4x4.tun.%d", os.Getuid())
					gw6, _, _ := darwinDefaultRouteIPv6()
					pfSetCmd = darwinBuildPFSetCmd(pfAnchor, "${tun_if}", ifName, gw, gw6, "", "", tunCfg.BlockQUIC, dnsProxyPort)
					pfRestoreCmd = darwinBuildPFRestoreCmd(pfAnchor)
					if tunCfg.BlockQUIC {
						b.addLog("info", "pf", "blocking QUIC: drop outbound UDP/443 via pf")
					}
				}

				pid, tunIf, scriptOut, err := dp.StartWithRoutes(startCtx, hevCmd, []string{hevCfgPath}, hevWorkDir, pidFile, logFile, tunCfg.IPv4, serverIP, gw, ifName, dnsSetCmd, dnsRestoreCmd, pfSetCmd, pfRestoreCmd)
				if err != nil {
					_ = b.coreProc.Stop(3 * time.Second)
					b.mu.Lock()
					b.state.CoreRunning = false
					b.state.Running = false
					b.state.NeedsAdmin = true
					b.state.RouteSetupError = err.Error()
					b.state.LastError = err.Error()
					b.emitStateLocked()
					b.mu.Unlock()
					return err
				}
				if strings.TrimSpace(scriptOut) != "" {
					for _, ln := range strings.Split(strings.ReplaceAll(scriptOut, "\r", "\n"), "\n") {
						ln = strings.TrimSpace(ln)
						if ln == "" {
							continue
						}
						switch {
						case strings.HasPrefix(ln, "__SUDOKU_STEP__="):
							b.addLog("info", "tun", "admin "+strings.TrimPrefix(ln, "__SUDOKU_STEP__="))
						case strings.HasPrefix(ln, "__SUDOKU_WARN__="):
							b.addLog("warn", "tun", "admin "+strings.TrimPrefix(ln, "__SUDOKU_WARN__="))
						case strings.HasPrefix(ln, "__SUDOKU_GUARD__="):
							b.addLog("warn", "tun", "admin "+strings.TrimPrefix(ln, "__SUDOKU_GUARD__="))
						default:
							b.addLog("info", "tun", "admin "+ln)
						}
					}
				}
				b.runningTunInterface = strings.TrimSpace(tunIf)
				if b.runningTunInterface != "" {
					b.addLog("info", "tun", fmt.Sprintf("detected tunnel interface: %s", b.runningTunInterface))
				}
				preRouteCtx = &routeContext{
					DefaultGateway:   gw,
					DefaultInterface: ifName,
					ServerIP:         serverIP,
					DNSService:       dnsService,
					DNSServers:       dnsPrev,
					DNSWasAutomatic:  dnsWasAuto,
					PFAnchor:         pfAnchor,
					BypassV4Path:     "",
					BypassV6Path:     "",
				}
				routeAlreadySetup = true
				b.addLog("info", "tun", fmt.Sprintf("hev started as admin (pid=%d, log=%s)", pid, logFile))
			} else {
				pid, err := b.tunAdmin.Start(hevCmd, []string{hevCfgPath}, hevWorkDir, pidFile, logFile)
				if err != nil {
					_ = b.coreProc.Stop(3 * time.Second)
					b.mu.Lock()
					b.state.CoreRunning = false
					b.state.Running = false
					b.state.NeedsAdmin = true
					b.state.RouteSetupError = err.Error()
					b.state.LastError = err.Error()
					b.emitStateLocked()
					b.mu.Unlock()
					return err
				}
				b.addLog("info", "tun", fmt.Sprintf("hev started as admin (pid=%d, log=%s)", pid, logFile))
			}
		} else {
			if runtime.GOOS == "linux" && os.Geteuid() != 0 {
				if _, err := exec.LookPath("pkexec"); err == nil {
					hevCmd = "pkexec"
					hevArgs = []string{hevBin, hevCfgPath}
				}
			}
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
		if runtime.GOOS == "darwin" {
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
				b.state.NeedsAdmin = true
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
			routeTunCfg := tunCfg
			routeTunCfg.MapDNSEnabled = true
			routeTunCfg.MapDNSAddress = effectiveDNS
			routeCtx, routeErr = setupRoutes(nodeCopy, routeTunCfg, routingCfg, bypass, func(line string) {
				b.addLog("info", "route", line)
			})
			if routeErr != nil {
				_ = b.stopTunLocked(2 * time.Second)
				_ = b.coreProc.Stop(2 * time.Second)
				b.mu.Lock()
				b.state.NeedsAdmin = true
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
		socksAddr := net.JoinHostPort(localDNSServerIPv4, fmt.Sprintf("%d", localPort))
		hctx, cancelHC := context.WithTimeout(context.Background(), 10*time.Second)
		hcErr := func() error {
			if err := waitForTCPReady(hctx, socksAddr, 3*time.Second); err != nil {
				return fmt.Errorf("core socks not ready: %w", err)
			}
			// 1) Verify SOCKS can CONNECT to an external IP (tests proxy path without DNS).
			if err := healthCheckSOCKS5Connect(hctx, socksAddr, "1.1.1.1:443", 3*time.Second); err != nil {
				return fmt.Errorf("socks connect check failed: %w", err)
			}
			// 2) Verify local DNS (via pf rdr 127.0.0.1:53 -> dns proxy) returns answers.
			if err := healthCheckDNSUDP(hctx, net.JoinHostPort(localDNSServerIPv4, "53"), "www.baidu.com", 2*time.Second); err != nil {
				return fmt.Errorf("dns check failed: %w", err)
			}
			return nil
		}()
		cancelHC()
		if hcErr != nil {
			// Best-effort immediate rollback. This avoids leaving the user's machine offline.
			b.addLog("warn", "app", fmt.Sprintf("post-start health check failed; reverting: %v", hcErr))
			if routeCtx != nil {
				teardownRoutes(routeCtx, tunCfg, func(line string) {
					b.addLog("info", "route", line)
				})
			}
			_ = b.stopTunLocked(4 * time.Second)
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

func (b *Backend) StopProxy() error {
	// Interrupt any pending start (e.g. waiting for admin prompt).
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
	b.routeState = nil
	b.mu.Unlock()

	// Route teardown may require admin; it also logs via b.addLog.
	if routeState != nil {
		teardownRoutes(routeState, tunCfg, func(line string) {
			b.addLog("info", "route", line)
		})
	}

	b.mu.Lock()
	dnsProxy := b.dnsProxy
	b.dnsProxy = nil
	b.mu.Unlock()
	if dnsProxy != nil {
		dnsProxy.Stop()
	}

	tunStopTimeout := 2 * time.Second
	if runtime.GOOS == "darwin" && os.Geteuid() != 0 && b.tunAdmin != nil && b.tunAdmin.PID() > 0 {
		// Allow time for the admin password prompt.
		tunStopTimeout = 90 * time.Second
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
	b.state.Running = false
	b.state.TunRunning = false
	b.state.CoreRunning = false
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

func (b *Backend) SwitchNode(nodeID string) error {
	if err := b.SetActiveNode(nodeID); err != nil {
		return err
	}
	b.mu.RLock()
	running := b.state.Running
	withTun := b.state.TunRunning
	b.mu.RUnlock()
	if running {
		return b.RestartProxy(StartRequest{WithTun: withTun})
	}
	return nil
}

func (b *Backend) StartReverseForwarder() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.revProc.IsRunning() {
		return nil
	}
	if err := b.ensureCoreBinariesLocked(); err != nil {
		b.state.LastError = err.Error()
		b.emitStateLocked()
		return err
	}
	if strings.TrimSpace(b.cfg.ReverseForward.DialURL) == "" {
		return errors.New("reverse dial URL is empty")
	}
	if strings.TrimSpace(b.cfg.ReverseForward.ListenAddr) == "" {
		return errors.New("reverse listen address is empty")
	}
	args := []string{"-rev-dial", b.cfg.ReverseForward.DialURL, "-rev-listen", b.cfg.ReverseForward.ListenAddr}
	if b.cfg.ReverseForward.Insecure {
		args = append(args, "-rev-insecure")
	}
	if err := b.revProc.Start(b.cfg.Core.SudokuBinary, args, []string{"SUDOKU_LOG_LEVEL=" + b.cfg.Core.LogLevel}, b.cfg.Core.WorkingDir, b.onReverseLog); err != nil {
		return err
	}
	b.state.ReverseRunning = true
	b.emitStateLocked()
	return nil
}

func (b *Backend) StopReverseForwarder() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.revProc.Stop(2 * time.Second); err != nil {
		b.state.LastError = err.Error()
	}
	b.state.ReverseRunning = false
	b.emitStateLocked()
	return nil
}

func (b *Backend) ProbeNode(nodeID string) (LatencyResult, error) {
	b.mu.RLock()
	node := b.findNode(nodeID)
	b.mu.RUnlock()
	if node == nil {
		return LatencyResult{}, fmt.Errorf("node not found")
	}
	result := probeNodeLatency(*node)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.latencyByID[nodeID] = result
	b.refreshLatencySliceLocked()
	b.emitStateLocked()
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	return result, nil
}

func (b *Backend) ProbeAllNodes() []LatencyResult {
	b.mu.RLock()
	nodes := append([]NodeConfig(nil), b.cfg.Nodes...)
	b.mu.RUnlock()
	results := make([]LatencyResult, 0, len(nodes))
	for _, node := range nodes {
		results = append(results, probeNodeLatency(node))
	}
	sortLatencyResults(results)
	b.mu.Lock()
	for _, r := range results {
		b.latencyByID[r.NodeID] = r
	}
	b.refreshLatencySliceLocked()
	b.emitStateLocked()
	b.mu.Unlock()
	return results
}

func (b *Backend) AutoSelectLowestLatency() (LatencyResult, error) {
	results := b.ProbeAllNodes()
	if len(results) == 0 {
		return LatencyResult{}, errors.New("no nodes")
	}
	best := results[0]
	if !best.ConnectOK || best.LatencyMs < 0 {
		return best, errors.New("no reachable node")
	}
	if err := b.SwitchNode(best.NodeID); err != nil {
		return best, err
	}
	return best, nil
}

func (b *Backend) SortNodesByName() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	sort.SliceStable(b.cfg.Nodes, func(i, j int) bool {
		return strings.ToLower(b.cfg.Nodes[i].Name) < strings.ToLower(b.cfg.Nodes[j].Name)
	})
	if err := b.store.Save(b.cfg); err != nil {
		return err
	}
	b.emitStateLocked()
	return nil
}

func (b *Backend) SortNodesByLatency() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	sort.SliceStable(b.cfg.Nodes, func(i, j int) bool {
		li, lok := b.latencyByID[b.cfg.Nodes[i].ID]
		lj, jok := b.latencyByID[b.cfg.Nodes[j].ID]
		if lok != jok {
			return lok
		}
		if !lok {
			return strings.ToLower(b.cfg.Nodes[i].Name) < strings.ToLower(b.cfg.Nodes[j].Name)
		}
		if li.ConnectOK != lj.ConnectOK {
			return li.ConnectOK
		}
		if li.LatencyMs < 0 {
			return false
		}
		if lj.LatencyMs < 0 {
			return true
		}
		return li.LatencyMs < lj.LatencyMs
	})
	if err := b.store.Save(b.cfg); err != nil {
		return err
	}
	b.emitStateLocked()
	return nil
}

func (b *Backend) DetectIPDirect() IPDetectResult {
	b.mu.RLock()
	port := b.cfg.Core.LocalPort
	if b.runningLocalPort > 0 {
		port = b.runningLocalPort
	}
	b.mu.RUnlock()
	result := detectIP(false, port)
	b.mu.Lock()
	defer b.mu.Unlock()
	if result.Error != "" {
		b.state.LastError = result.Error
	}
	return result
}

func (b *Backend) DetectIPProxy() IPDetectResult {
	b.mu.RLock()
	port := b.cfg.Core.LocalPort
	if b.runningLocalPort > 0 {
		port = b.runningLocalPort
	}
	b.mu.RUnlock()
	result := detectIP(true, port)
	b.mu.Lock()
	defer b.mu.Unlock()
	if result.Error != "" {
		b.state.LastError = result.Error
	}
	return result
}

func (b *Backend) GetState() RuntimeState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.snapshotStateLocked()
}

func (b *Backend) GetLogs(level string, limit int) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	level = strings.ToLower(strings.TrimSpace(level))
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	out := make([]LogEntry, 0, limit)
	for i := len(b.logs) - 1; i >= 0 && len(out) < limit; i-- {
		l := b.logs[i]
		if level != "" && level != "all" && l.Level != level {
			continue
		}
		out = append(out, l)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func (b *Backend) GetConnections() []ActiveConnection {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := topConnections(b.connections, 300)
	if out == nil {
		return []ActiveConnection{}
	}
	return out
}

func (b *Backend) GetUsageHistory(limit int) []UsageDay {
	b.mu.RLock()
	defer b.mu.RUnlock()
	days := append([]UsageDay(nil), b.usageDays...)
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	if limit <= 0 || limit > len(days) {
		limit = len(days)
	}
	if limit == 0 {
		return []UsageDay{}
	}
	return append([]UsageDay(nil), days[len(days)-limit:]...)
}

func (b *Backend) ImportNodeShare(name string, link string) (NodeConfig, error) {
	return b.ImportShortLink(name, link)
}

func (b *Backend) ExportNodeShare(nodeID string) (string, error) {
	return b.ExportShortLink(nodeID)
}

func (b *Backend) onCoreLog(line string) {
	b.addLog(levelFromLine(line), componentFromLine(line), line)
}

func (b *Backend) onTunLog(line string) {
	b.addLog(levelFromLine(line), "hev", line)
}

func (b *Backend) onReverseLog(line string) {
	b.addLog(levelFromLine(line), "reverse", line)
}

func (b *Backend) addLog(level, component, raw string) {
	entry := LogEntry{
		ID:        newID("log_"),
		Timestamp: time.Now(),
		Level:     normalizeLogLevel(level),
		Component: strings.TrimSpace(component),
		Message:   strings.TrimSpace(stripANSI(raw)),
		Raw:       strings.TrimSpace(raw),
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logs = append(b.logs, entry)
	if len(b.logs) > 2000 {
		b.logs = b.logs[len(b.logs)-2000:]
	}
	b.state.RecentLogs = b.lastLogsLocked(120)
	b.parseRouteLineLocked(entry.Message)
	b.parseCoreTrafficLineLocked(entry.Message)
	b.emitLog(entry)
}

func (b *Backend) parseRouteLineLocked(line string) {
	m := routeLineRegex.FindStringSubmatch(stripANSI(line))
	if len(m) != 5 {
		return
	}
	network := strings.ToUpper(m[1])
	src := m[2]
	dst := m[3]
	dir := strings.ToLower(m[4])
	id := network + "|" + src + "|" + dst + "|" + dir
	now := time.Now()
	conn, ok := b.connections[id]
	if !ok {
		conn = &ActiveConnection{
			ID:          id,
			Network:     network,
			Source:      src,
			Destination: dst,
			Direction:   dir,
			LastSeen:    now,
			Hits:        1,
		}
		b.connections[id] = conn
	} else {
		conn.LastSeen = now
		conn.Hits++
	}
	if dir == "direct" {
		b.state.Traffic.DirectConnDecisions++
	} else {
		b.state.Traffic.ProxyConnDecisions++
	}
}

func (b *Backend) parseCoreTrafficLineLocked(line string) {
	m := coreTrafficLineRegex.FindStringSubmatch(stripANSI(line))
	if len(m) != 5 {
		return
	}
	parse := func(s string) (uint64, bool) {
		v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
		return v, err == nil
	}
	directTx, ok1 := parse(m[1])
	directRx, ok2 := parse(m[2])
	proxyTx, ok3 := parse(m[3])
	proxyRx, ok4 := parse(m[4])
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return
	}

	now := time.Now()
	totalTx := directTx + proxyTx
	totalRx := directRx + proxyRx

	b.trafficCache.coreTrafficActive = true
	b.state.Traffic.TotalTx = totalTx
	b.state.Traffic.TotalRx = totalRx
	b.state.Traffic.EstimatedDirectTx = directTx
	b.state.Traffic.EstimatedDirectRx = directRx
	b.state.Traffic.EstimatedProxyTx = proxyTx
	b.state.Traffic.EstimatedProxyRx = proxyRx

	if b.trafficCache.coreLastTrafficSeen && !b.trafficCache.coreLastTrafficAt.IsZero() {
		deltaSeconds := now.Sub(b.trafficCache.coreLastTrafficAt).Seconds()
		if deltaSeconds > 0 {
			prevDirectTx := b.trafficCache.coreLastDirectTx
			prevDirectRx := b.trafficCache.coreLastDirectRx
			prevProxyTx := b.trafficCache.coreLastProxyTx
			prevProxyRx := b.trafficCache.coreLastProxyRx

			// Handle counter resets (core restart) gracefully.
			if directTx < prevDirectTx || directRx < prevDirectRx || proxyTx < prevProxyTx || proxyRx < prevProxyRx {
				prevDirectTx, prevDirectRx, prevProxyTx, prevProxyRx = directTx, directRx, proxyTx, proxyRx
			}

			dDirectTx := directTx - prevDirectTx
			dDirectRx := directRx - prevDirectRx
			dProxyTx := proxyTx - prevProxyTx
			dProxyRx := proxyRx - prevProxyRx
			dTx := dDirectTx + dProxyTx
			dRx := dDirectRx + dProxyRx

			b.usageDays = addUsageToDay(b.usageDays, usageDayKey(now), dTx, dRx, dDirectTx, dDirectRx, dProxyTx, dProxyRx)
			b.usageDays = trimUsageDays(b.usageDays, 120)
			b.usageDirty = true
			if b.usageDirty && (b.lastUsageFlush.IsZero() || now.Sub(b.lastUsageFlush) > 15*time.Second) {
				if err := saveUsageHistory(b.store.UsageHistoryPath(), b.usageDays); err != nil {
					b.state.LastError = fmt.Sprintf("save usage history: %v", err)
				} else {
					b.usageDirty = false
					b.lastUsageFlush = now
				}
			}

			sample := BandwidthSample{
				At:      now,
				TxBps:   float64(dTx) / deltaSeconds,
				RxBps:   float64(dRx) / deltaSeconds,
				Direct:  float64(dDirectTx+dDirectRx) / deltaSeconds,
				Proxy:   float64(dProxyTx+dProxyRx) / deltaSeconds,
				TotalTx: totalTx,
				TotalRx: totalRx,
			}
			b.state.Traffic.RecentBandwidth = append(b.state.Traffic.RecentBandwidth, sample)
			if len(b.state.Traffic.RecentBandwidth) > 300 {
				b.state.Traffic.RecentBandwidth = b.state.Traffic.RecentBandwidth[len(b.state.Traffic.RecentBandwidth)-300:]
			}
		}
	}

	b.trafficCache.coreLastDirectTx = directTx
	b.trafficCache.coreLastDirectRx = directRx
	b.trafficCache.coreLastProxyTx = proxyTx
	b.trafficCache.coreLastProxyRx = proxyRx
	b.trafficCache.coreLastTrafficAt = now
	b.trafficCache.coreLastTrafficSeen = true
	b.state.Traffic.LastSampleUnixMillis = now.UnixMilli()
}

func (b *Backend) monitorLoop() {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-b.tickerStop:
			return
		case <-t.C:
			b.tick()
		}
	}
}

func (b *Backend) tick() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state.CoreRunning = b.coreProc.IsRunning()
	b.state.TunRunning = b.tunRunningLocked()
	b.state.ReverseRunning = b.revProc.IsRunning()
	b.state.Running = b.state.CoreRunning

	ifName := b.cfg.Tun.InterfaceName
	if b.runningTunInterface != "" {
		ifName = b.runningTunInterface
	}
	tx, rx, ok := lookupInterfaceCounters(ifName)
	b.state.Traffic.Interface = ifName
	b.state.Traffic.InterfaceFound = ok
	now := time.Now()
	if ok && !b.trafficCache.coreTrafficActive {
		b.state.Traffic.TotalTx = tx
		b.state.Traffic.TotalRx = rx
		if !b.trafficCache.lastAt.IsZero() {
			deltaSeconds := now.Sub(b.trafficCache.lastAt).Seconds()
			if deltaSeconds > 0 {
				dTx := tx - b.trafficCache.lastTx
				dRx := rx - b.trafficCache.lastRx
				dDirect := b.state.Traffic.DirectConnDecisions - b.trafficCache.lastDirectDec
				dProxy := b.state.Traffic.ProxyConnDecisions - b.trafficCache.lastProxyDec
				totalDec := dDirect + dProxy
				directRatio := 0.0
				if totalDec > 0 {
					directRatio = float64(dDirect) / float64(totalDec)
				}
				proxyRatio := 1.0 - directRatio
				directTx := uint64(float64(dTx) * directRatio)
				if directTx > dTx {
					directTx = dTx
				}
				directRx := uint64(float64(dRx) * directRatio)
				if directRx > dRx {
					directRx = dRx
				}
				proxyTx := dTx - directTx
				proxyRx := dRx - directRx
				b.state.Traffic.EstimatedDirectTx += directTx
				b.state.Traffic.EstimatedDirectRx += directRx
				b.state.Traffic.EstimatedProxyTx += proxyTx
				b.state.Traffic.EstimatedProxyRx += proxyRx
				b.usageDays = addUsageToDay(b.usageDays, usageDayKey(now), dTx, dRx, directTx, directRx, proxyTx, proxyRx)
				b.usageDays = trimUsageDays(b.usageDays, 120)
				b.usageDirty = true
				if b.usageDirty && (b.lastUsageFlush.IsZero() || now.Sub(b.lastUsageFlush) > 15*time.Second) {
					if err := saveUsageHistory(b.store.UsageHistoryPath(), b.usageDays); err != nil {
						b.state.LastError = fmt.Sprintf("save usage history: %v", err)
					} else {
						b.usageDirty = false
						b.lastUsageFlush = now
					}
				}
				sample := BandwidthSample{
					At:      now,
					TxBps:   float64(dTx) / deltaSeconds,
					RxBps:   float64(dRx) / deltaSeconds,
					Direct:  (float64(dTx) + float64(dRx)) * directRatio / deltaSeconds,
					Proxy:   (float64(dTx) + float64(dRx)) * proxyRatio / deltaSeconds,
					TotalTx: tx,
					TotalRx: rx,
				}
				b.state.Traffic.RecentBandwidth = append(b.state.Traffic.RecentBandwidth, sample)
				if len(b.state.Traffic.RecentBandwidth) > 300 {
					b.state.Traffic.RecentBandwidth = b.state.Traffic.RecentBandwidth[len(b.state.Traffic.RecentBandwidth)-300:]
				}
			}
		}
		b.trafficCache.lastTx = tx
		b.trafficCache.lastRx = rx
		b.trafficCache.lastAt = now
		b.trafficCache.lastDirectDec = b.state.Traffic.DirectConnDecisions
		b.trafficCache.lastProxyDec = b.state.Traffic.ProxyConnDecisions
		b.state.Traffic.LastSampleUnixMillis = now.UnixMilli()
	}

	cutoff := now.Add(-45 * time.Second)
	for id, conn := range b.connections {
		if conn.LastSeen.Before(cutoff) {
			delete(b.connections, id)
		}
	}
	b.state.Connections = topConnections(b.connections, 200)
	b.state.RecentLogs = b.lastLogsLocked(120)
	b.emitStateLocked()
}

func (b *Backend) refreshLatencySliceLocked() {
	list := make([]LatencyResult, 0, len(b.latencyByID))
	for _, v := range b.latencyByID {
		list = append(list, v)
	}
	sortLatencyResults(list)
	b.state.Latencies = list
}

func (b *Backend) snapshotStateLocked() RuntimeState {
	out := b.state
	// Always return non-nil slices so the frontend never receives `null` arrays.
	out.RecentLogs = append([]LogEntry{}, b.state.RecentLogs...)
	out.Connections = append([]ActiveConnection{}, b.state.Connections...)
	out.Latencies = append([]LatencyResult{}, b.state.Latencies...)
	out.Traffic.RecentBandwidth = append([]BandwidthSample{}, b.state.Traffic.RecentBandwidth...)
	return out
}

func (b *Backend) emitStateLocked() {
	if b.ctx == nil {
		return
	}
	state := b.snapshotStateLocked()
	select {
	case b.emitStateCh <- state:
	default:
		select {
		case <-b.emitStateCh:
		default:
		}
		select {
		case b.emitStateCh <- state:
		default:
		}
	}
}

func (b *Backend) emitLog(entry LogEntry) {
	if b.ctx == nil {
		return
	}
	select {
	case b.emitLogCh <- entry:
	default:
		// Drop logs under backpressure to avoid UI deadlocks.
	}
}

func (b *Backend) lastLogsLocked(max int) []LogEntry {
	if max <= 0 {
		max = 100
	}
	if len(b.logs) <= max {
		return append([]LogEntry(nil), b.logs...)
	}
	return append([]LogEntry(nil), b.logs[len(b.logs)-max:]...)
}

func (b *Backend) findNode(id string) *NodeConfig {
	for i := range b.cfg.Nodes {
		if b.cfg.Nodes[i].ID == id {
			return &b.cfg.Nodes[i]
		}
	}
	return nil
}

func normalizeLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return "debug"
	case "warn", "warning":
		return "warn"
	case "error":
		return "error"
	default:
		return "info"
	}
}

func strconvBase36(v int64) string {
	return strings.ToUpper(strconv.FormatInt(v, 36))
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
