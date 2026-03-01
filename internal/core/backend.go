package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

var routeLineRegex = regexp.MustCompile(`(?i)\[(tcp|udp)\]\s+([^\s]+)\s+-->\s+([^\s]+).*using\s+(direct|proxy)`) //nolint:lll

type Backend struct {
	mu sync.RWMutex

	ctx        context.Context
	store      *Store
	cfg        *AppConfig
	state      RuntimeState
	coreProc   *ManagedProcess
	tunProc    *ManagedProcess
	revProc    *ManagedProcess
	routeState *routeContext
	pfMgr      *portForwardManager

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
	store, err := NewStore("sudoku-desktop")
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
		revProc:     NewManagedProcess("reverse"),
		connections: map[string]*ActiveConnection{},
		latencyByID: map[string]LatencyResult{},
		tickerStop:  make(chan struct{}),
		logs:        make([]LogEntry, 0, 512),
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

func (b *Backend) Startup(ctx context.Context) {
	b.mu.Lock()
	b.ctx = ctx
	autoStart := b.cfg.Core.AutoStart
	withTun := b.cfg.Tun.Enabled
	b.mu.Unlock()

	b.startPACServer()

	go b.monitorLoop()
	if autoStart {
		go func() {
			_ = b.StartProxy(StartRequest{WithTun: withTun})
		}()
	}
}

func (b *Backend) Shutdown() {
	close(b.tickerStop)
	_ = b.StopReverseForwarder()
	_ = b.StopProxy()
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
	defer b.mu.Unlock()
	normalizeConfig(&next, b.store.RuntimeDir())
	if err := b.store.Save(&next); err != nil {
		return err
	}
	b.cfg = &next
	b.state.ActiveNodeID = next.ActiveNodeID
	if node := b.findNode(next.ActiveNodeID); node != nil {
		b.state.ActiveNodeName = node.Name
	}
	b.pfMgr.Apply(next.PortForwards)
	b.emitStateLocked()
	return nil
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
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.Running {
		return nil
	}
	node := b.findNode(b.cfg.ActiveNodeID)
	if node == nil {
		return errors.New("no active node")
	}
	sudokuCfgPath, hevCfgPath, localPort, err := writeRuntimeConfigs(b.store, b.cfg, *node, b.pacURL)
	if err != nil {
		return err
	}
	if err := ensureDir(b.cfg.Core.WorkingDir); err != nil {
		return err
	}
	b.addLog("info", "app", fmt.Sprintf("starting sudoku core with config %s", sudokuCfgPath))
	if err := b.coreProc.Start(b.cfg.Core.SudokuBinary, []string{"-c", sudokuCfgPath}, []string{"SUDOKU_LOG_LEVEL=" + b.cfg.Core.LogLevel}, b.cfg.Core.WorkingDir, b.onCoreLog); err != nil {
		return err
	}
	b.state.CoreRunning = true
	b.runningLocalPort = localPort
	b.state.ActiveNodeID = node.ID
	b.state.ActiveNodeName = node.Name
	b.state.StartedAtUnix = time.Now().UnixMilli()
	b.state.Running = true
	b.cfg.LastStartedNode = node.ID
	_ = b.store.Save(b.cfg)

	if req.WithTun && b.cfg.Tun.Enabled {
		b.addLog("info", "tun", fmt.Sprintf("starting hev with config %s", hevCfgPath))
		hevCmd := b.cfg.Core.HevBinary
		hevArgs := []string{hevCfgPath}
		if runtime.GOOS == "linux" && os.Geteuid() != 0 {
			if _, err := exec.LookPath("pkexec"); err == nil {
				hevCmd = "pkexec"
				hevArgs = []string{b.cfg.Core.HevBinary, hevCfgPath}
			}
		}
		if err := b.tunProc.Start(hevCmd, hevArgs, nil, b.cfg.Core.WorkingDir, b.onTunLog); err != nil {
			_ = b.coreProc.Stop(3 * time.Second)
			b.state.CoreRunning = false
			b.state.Running = false
			b.state.NeedsAdmin = isLikelyPermissionError(err)
			b.state.RouteSetupError = err.Error()
			b.state.LastError = err.Error()
			b.emitStateLocked()
			return err
		}
		time.Sleep(900 * time.Millisecond)
		routeCtx, routeErr := setupRoutes(*node, b.cfg.Tun, func(line string) {
			b.addLog("info", "route", line)
		})
		if routeErr != nil {
			b.state.NeedsAdmin = true
			b.state.RouteSetupError = routeErr.Error()
			_ = b.tunProc.Stop(2 * time.Second)
			_ = b.coreProc.Stop(2 * time.Second)
			b.state.TunRunning = false
			b.state.CoreRunning = false
			b.state.Running = false
			b.state.LastError = routeErr.Error()
			b.emitStateLocked()
			return routeErr
		}
		b.routeState = routeCtx
		b.state.TunRunning = true
		b.state.NeedsAdmin = false
		b.state.RouteSetupError = ""
	}
	b.pfMgr.Apply(b.cfg.PortForwards)
	b.emitStateLocked()
	return nil
}

func (b *Backend) StopProxy() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.routeState != nil {
		teardownRoutes(b.routeState, b.cfg.Tun, func(line string) {
			b.addLog("info", "route", line)
		})
		b.routeState = nil
	}
	_ = b.tunProc.Stop(2 * time.Second)
	_ = b.coreProc.Stop(3 * time.Second)
	b.pfMgr.StopAll()
	b.state.Running = false
	b.state.TunRunning = false
	b.state.CoreRunning = false
	b.runningLocalPort = 0
	b.state.NeedsAdmin = false
	b.state.RouteSetupError = ""
	b.emitStateLocked()
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
	return topConnections(b.connections, 300)
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
		return nil
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
	b.state.TunRunning = b.tunProc.IsRunning()
	b.state.ReverseRunning = b.revProc.IsRunning()
	b.state.Running = b.state.CoreRunning

	tx, rx, ok := lookupInterfaceCounters(b.cfg.Tun.InterfaceName)
	b.state.Traffic.Interface = b.cfg.Tun.InterfaceName
	b.state.Traffic.InterfaceFound = ok
	now := time.Now()
	if ok {
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
	out.RecentLogs = append([]LogEntry(nil), b.state.RecentLogs...)
	out.Connections = append([]ActiveConnection(nil), b.state.Connections...)
	out.Latencies = append([]LatencyResult(nil), b.state.Latencies...)
	out.Traffic.RecentBandwidth = append([]BandwidthSample(nil), b.state.Traffic.RecentBandwidth...)
	return out
}

func (b *Backend) emitStateLocked() {
	if b.ctx == nil {
		return
	}
	state := b.snapshotStateLocked()
	wailsruntime.EventsEmit(b.ctx, EventStateUpdated, state)
}

func (b *Backend) emitLog(entry LogEntry) {
	if b.ctx == nil {
		return
	}
	wailsruntime.EventsEmit(b.ctx, EventLogAdded, entry)
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
	b.mu.RLock()
	sudokuBin := b.cfg.Core.SudokuBinary
	hevBin := b.cfg.Core.HevBinary
	b.mu.RUnlock()
	if _, err := os.Stat(sudokuBin); err != nil {
		return fmt.Errorf("sudoku binary not found: %s", sudokuBin)
	}
	if _, err := os.Stat(hevBin); err != nil {
		return fmt.Errorf("hev binary not found: %s", hevBin)
	}
	return nil
}
