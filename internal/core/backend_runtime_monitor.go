package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var routeLineRegex = regexp.MustCompile(`(?i)\[(tcp|udp)\]\s+([^\s]+)\s+-->\s+([^\s]+).*using\s+(direct|proxy)`)                          //nolint:lll
var coreTrafficLineRegex = regexp.MustCompile(`__SUDOKU_TRAFFIC__\s+direct_tx=(\d+)\s+direct_rx=(\d+)\s+proxy_tx=(\d+)\s+proxy_rx=(\d+)`) //nolint:lll

type coreTrafficFileSnapshot struct {
	DirectTx uint64 `json:"direct_tx"`
	DirectRx uint64 `json:"direct_rx"`
	ProxyTx  uint64 `json:"proxy_tx"`
	ProxyRx  uint64 `json:"proxy_rx"`
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
	cleanRaw := strings.TrimSpace(stripANSI(raw))
	normLevel := normalizeLogLevel(level)
	normComponent := strings.TrimSpace(component)
	message := cleanRaw

	if parsed, ok := parseLogxLine(cleanRaw); ok {
		if parsed.Level != "" {
			normLevel = normalizeLogLevel(parsed.Level)
		}
		if parsed.Component != "" {
			normComponent = parsed.Component
		}
		if parsed.Message != "" {
			message = parsed.Message
		} else {
			message = ""
		}
	}

	message = trimComponentPrefix(message, normComponent)
	if message == "" {
		message = cleanRaw
	}
	if normComponent == "" {
		normComponent = componentFromLine(cleanRaw)
	}

	entry := LogEntry{
		ID:        newID("log_"),
		Timestamp: time.Now(),
		Level:     normLevel,
		Component: normComponent,
		Message:   message,
		Raw:       cleanRaw,
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logs = append(b.logs, entry)
	if len(b.logs) > 20000 {
		b.logs = b.logs[len(b.logs)-20000:]
	}
	b.state.RecentLogs = b.lastLogsLocked(120)
	b.parseRouteLineLocked(entry.Raw)
	b.parseCoreTrafficLineLocked(entry.Raw)
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
	b.applyCoreTrafficCountersLocked(directTx, directRx, proxyTx, proxyRx, time.Now())
}

func (b *Backend) applyCoreTrafficCountersLocked(directTx, directRx, proxyTx, proxyRx uint64, now time.Time) {
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

			b.recordUsageDeltaLocked(now, dTx, dRx, dDirectTx, dDirectRx, dProxyTx, dProxyRx)

			sample := BandwidthSample{
				At:      now,
				TxBps:   float64(dTx) / deltaSeconds,
				RxBps:   float64(dRx) / deltaSeconds,
				Direct:  float64(dDirectTx+dDirectRx) / deltaSeconds,
				Proxy:   float64(dProxyTx+dProxyRx) / deltaSeconds,
				TotalTx: totalTx,
				TotalRx: totalRx,
			}
			b.appendBandwidthSampleLocked(sample)
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

func (b *Backend) recordUsageDeltaLocked(now time.Time, tx, rx, directTx, directRx, proxyTx, proxyRx uint64) {
	b.usageDays = addUsageToDay(b.usageDays, usageDayKey(now), tx, rx, directTx, directRx, proxyTx, proxyRx)
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
}

func (b *Backend) appendBandwidthSampleLocked(sample BandwidthSample) {
	b.state.Traffic.RecentBandwidth = append(b.state.Traffic.RecentBandwidth, sample)
	if len(b.state.Traffic.RecentBandwidth) > 150 {
		b.state.Traffic.RecentBandwidth = b.state.Traffic.RecentBandwidth[len(b.state.Traffic.RecentBandwidth)-150:]
	}
}

func (b *Backend) refreshCoreTrafficFromFileLocked(now time.Time) {
	path := strings.TrimSpace(b.trafficCache.coreTrafficFile)
	if path == "" {
		return
	}
	s, ok, err := readCoreTrafficStatsFile(path)
	if err != nil || !ok {
		return
	}
	b.applyCoreTrafficCountersLocked(s.DirectTx, s.DirectRx, s.ProxyTx, s.ProxyRx, now)
}

func readCoreTrafficStatsFile(path string) (coreTrafficFileSnapshot, bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return coreTrafficFileSnapshot{}, false, nil
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return coreTrafficFileSnapshot{}, false, nil
		}
		return coreTrafficFileSnapshot{}, false, err
	}
	if len(strings.TrimSpace(string(buf))) == 0 {
		return coreTrafficFileSnapshot{}, false, nil
	}
	var s coreTrafficFileSnapshot
	if err := json.Unmarshal(buf, &s); err != nil {
		return coreTrafficFileSnapshot{}, false, err
	}
	return s, true, nil
}

func writeCoreTrafficStatsFile(path string, s coreTrafficFileSnapshot) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	buf, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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
			b.maybeRepairDarwinTunNetwork()
		}
	}
}

func (b *Backend) startInProgress() bool {
	b.startMu.Lock()
	inProgress := b.startCancel != nil
	b.startMu.Unlock()
	return inProgress
}

func (b *Backend) maybeRepairDarwinTunNetwork() {
	if runtime.GOOS != "darwin" {
		return
	}
	if b.startInProgress() {
		return
	}

	now := time.Now()
	b.mu.Lock()
	if !b.darwinNetLastCheck.IsZero() && now.Sub(b.darwinNetLastCheck) < 2*time.Second {
		b.mu.Unlock()
		return
	}
	b.darwinNetLastCheck = now
	if b.darwinNetRepairing || b.tunRecovering {
		b.mu.Unlock()
		return
	}
	routeCtx := b.routeState
	tunRunning := b.state.TunRunning
	if routeCtx == nil || !tunRunning {
		b.mu.Unlock()
		return
	}
	prevIf := strings.TrimSpace(routeCtx.DefaultInterface)
	prevGW := strings.TrimSpace(routeCtx.DefaultGateway)
	tunCfg := b.cfg.Tun
	if strings.TrimSpace(b.runningTunInterface) != "" {
		tunCfg.InterfaceName = strings.TrimSpace(b.runningTunInterface)
	}
	b.mu.Unlock()

	info, infoErr := darwinPrimaryNetworkInfo()
	if infoErr != nil {
		b.mu.Lock()
		b.darwinNetLastErr = infoErr.Error()
		b.darwinNetLastErrAt = now
		b.mu.Unlock()
	}

	newIf := strings.TrimSpace(info.Interface4)
	newGW := strings.TrimSpace(info.Router4)

	// scutil can report utun as the "primary" interface while TUN is active.
	// Always prefer a physical interface for repair actions.
	if newIf == "" || darwinIsTunLikeInterface(newIf) {
		ifName, _ := darwinResolveOutboundBypassInterface(900 * time.Millisecond)
		if strings.TrimSpace(ifName) != "" && !darwinIsTunLikeInterface(ifName) {
			newIf = strings.TrimSpace(ifName)
		} else if strings.TrimSpace(prevIf) != "" && !darwinIsTunLikeInterface(prevIf) {
			newIf = strings.TrimSpace(prevIf)
		} else {
			newIf = ""
		}
	}

	// Router can be empty during transitions; fall back to DHCP on the physical interface.
	if strings.TrimSpace(newGW) == "" && newIf != "" && !darwinIsTunLikeInterface(newIf) {
		if gw, _ := darwinDHCPRouterForInterface(newIf); strings.TrimSpace(gw) != "" {
			newGW = strings.TrimSpace(gw)
		}
	}

	if newIf == "" || newGW == "" {
		return
	}

	changed := prevIf != newIf || prevGW != newGW
	if !changed {
		return
	}

	sig := strings.Join([]string{newIf, newGW}, "|")
	b.mu.Lock()
	lastSig := b.darwinNetLastSig
	lastErr := b.darwinNetLastErr
	lastErrAt := b.darwinNetLastErrAt
	if sig == lastSig && lastErr != "" && now.Sub(lastErrAt) < 30*time.Second {
		b.mu.Unlock()
		return
	}
	b.darwinNetLastSig = sig
	b.darwinNetRepairing = true
	b.mu.Unlock()

	go b.repairDarwinTunNetwork(routeCtx, tunCfg, prevIf, prevGW, newIf, newGW, sig)
}

func (b *Backend) repairDarwinTunNetwork(routeCtx *routeContext, tunCfg TunSettings, prevIf string, prevGW string, newIf string, newGW string, sig string) {
	defer func() {
		b.mu.Lock()
		b.darwinNetRepairing = false
		b.mu.Unlock()
	}()

	newIf = strings.TrimSpace(newIf)
	newGW = strings.TrimSpace(newGW)
	if newIf == "" || darwinIsTunLikeInterface(newIf) || newGW == "" {
		return
	}

	// Avoid concurrent StopProxy and ensure a consistent route state snapshot.
	b.opMu.Lock()
	defer b.opMu.Unlock()

	b.mu.Lock()
	// Re-check: we may have stopped/restarted since we queued this repair.
	if b.routeState != routeCtx || !b.state.TunRunning || b.tunRecovering {
		b.mu.Unlock()
		return
	}
	if strings.TrimSpace(b.runningTunInterface) != "" {
		tunCfg.InterfaceName = strings.TrimSpace(b.runningTunInterface)
	}
	serverIP := strings.TrimSpace(routeCtx.ServerIP)
	dnsAddr := strings.TrimSpace(routeCtx.DNSOverrideAddress)
	dnsServiceCurrent := strings.TrimSpace(routeCtx.DNSService)
	pfAnchor := strings.TrimSpace(routeCtx.PFAnchor)
	b.mu.Unlock()

	b.addLog("warn", "tun", fmt.Sprintf("darwin network changed: %s/%s -> %s/%s; repairing TUN routes", prevIf, prevGW, newIf, newGW))

	newDNSService := ""
	var newDNSSnap darwinDNSSnapshot
	if dnsAddr != "" {
		if svc, derr := darwinNetworkServiceForDevice(newIf); derr == nil {
			newDNSService = strings.TrimSpace(svc)
		}
		if newDNSService != "" {
			needSnap := true
			b.mu.RLock()
			for _, s := range routeCtx.DarwinDNSSnapshots {
				if strings.EqualFold(strings.TrimSpace(s.Service), newDNSService) {
					needSnap = false
					break
				}
			}
			b.mu.RUnlock()
			if needSnap {
				prev, wasAuto, _ := darwinGetDNSServers(newDNSService)
				newDNSSnap = darwinDNSSnapshot{
					Service:      newDNSService,
					Servers:      prev,
					WasAutomatic: wasAuto,
				}
			}
		}
	}

	cmds := make([]string, 0, 10)
	if serverIP != "" {
		cmds = append(cmds,
			shellJoin("route", "-n", "add", "-host", serverIP, newGW)+" || "+shellJoin("route", "-n", "change", "-host", serverIP, newGW)+" || true",
		)
	}
	if tunIf := strings.TrimSpace(tunCfg.InterfaceName); tunIf != "" {
		cmds = append(cmds,
			shellJoin("route", "-n", "change", "default", "-interface", tunIf)+" || ("+
				shellJoin("route", "-n", "delete", "default")+" >/dev/null 2>&1 || true; "+
				shellJoin("route", "-n", "add", "default", "-interface", tunIf)+") || true",
		)
	}
	cmds = append(cmds,
		"("+shellJoin("route", "-n", "add", "-ifscope", newIf, "default", newGW)+" >/dev/null 2>&1 || "+
			shellJoin("route", "-n", "change", "-ifscope", newIf, "default", newGW)+" >/dev/null 2>&1) || true",
	)
	if pfAnchor != "" {
		if pfCmd := strings.TrimSpace(darwinBuildPFSetCmd(pfAnchor, strings.TrimSpace(tunCfg.InterfaceName), tunCfg.BlockQUIC, routeCtx.DNSProxyRedirectPort)); pfCmd != "" {
			cmds = append(cmds, pfCmd+" || true")
		}
	}
	if dnsAddr != "" && newDNSService != "" {
		dnsFlushCmd := "dscacheutil -flushcache >/dev/null 2>&1 || true; killall -HUP mDNSResponder >/dev/null 2>&1 || true"
		cmds = append(cmds,
			shellJoin("networksetup", "-setdnsservers", newDNSService, dnsAddr)+" || true",
			dnsFlushCmd,
		)
	}

	var runErr error
	if os.Geteuid() != 0 {
		runErr = runCmdsDarwinAdmin(func(line string) {
			b.addLog("info", "route", line)
		}, cmds...)
	} else {
		shell := "set -e; " + strings.Join(cmds, "; ")
		runErr = runCmdExec(func(line string) {
			b.addLog("info", "route", line)
		}, "sh", "-lc", shell)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if runErr != nil {
		b.darwinNetLastErr = runErr.Error()
		b.darwinNetLastErrAt = time.Now()
		b.state.LastError = runErr.Error()
		b.emitStateLocked()
		return
	}
	b.darwinNetLastErr = ""
	b.darwinNetLastErrAt = time.Time{}
	b.darwinNetLastSig = sig

	// Update the in-memory route context so future stop/repair uses the new physical gateway/interface.
	routeCtx.DefaultInterface = newIf
	routeCtx.DefaultGateway = newGW

	if newDNSService != "" && dnsAddr != "" {
		routeCtx.DNSOverrideAddress = dnsAddr
		if dnsServiceCurrent == "" || !strings.EqualFold(dnsServiceCurrent, newDNSService) {
			routeCtx.DNSService = newDNSService
		}
		if strings.TrimSpace(newDNSSnap.Service) != "" {
			routeCtx.DarwinDNSSnapshots = append(routeCtx.DarwinDNSSnapshots, newDNSSnap)
		}
	}
	b.addLog("info", "tun", "darwin network repair applied")
	b.emitStateLocked()
}

func (b *Backend) tick() {
	b.mu.Lock()
	defer b.mu.Unlock()
	needTunRecover := false
	b.state.CoreRunning = b.coreProc.IsRunning()
	b.state.TunRunning = b.tunRunningLocked()
	b.state.ReverseRunning = b.revProc.IsRunning()
	b.state.Running = b.state.CoreRunning
	if b.routeState != nil && !b.state.TunRunning && !b.tunRecovering {
		b.tunRecovering = true
		needTunRecover = true
		b.state.LastError = "tun process exited unexpectedly; auto-recovering routes"
	}

	ifName := b.cfg.Tun.InterfaceName
	if b.runningTunInterface != "" {
		ifName = b.runningTunInterface
	}
	tx, rx, ok := lookupInterfaceCounters(ifName)
	b.state.Traffic.Interface = ifName
	b.state.Traffic.InterfaceFound = ok
	now := time.Now()
	b.refreshCoreTrafficFromFileLocked(now)
	if ok && !b.trafficCache.coreTrafficActive {
		b.state.Traffic.TotalTx = tx
		b.state.Traffic.TotalRx = rx
		if !b.trafficCache.lastAt.IsZero() {
			deltaSeconds := now.Sub(b.trafficCache.lastAt).Seconds()
			if deltaSeconds > 0 {
				prevTx := b.trafficCache.lastTx
				prevRx := b.trafficCache.lastRx
				if tx < prevTx || rx < prevRx {
					prevTx = tx
					prevRx = rx
				}
				dTx := tx - prevTx
				dRx := rx - prevRx
				b.recordUsageDeltaLocked(now, dTx, dRx, 0, 0, 0, 0)
				sample := BandwidthSample{
					At:      now,
					TxBps:   float64(dTx) / deltaSeconds,
					RxBps:   float64(dRx) / deltaSeconds,
					Direct:  0,
					Proxy:   0,
					TotalTx: tx,
					TotalRx: rx,
				}
				b.appendBandwidthSampleLocked(sample)
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
	if needTunRecover {
		go b.recoverFromTunFailure()
	}
}

func (b *Backend) recoverFromTunFailure() {
	msg := "tun process is not running while routes are active; auto-stopping proxy to restore network"
	if b.store != nil {
		if tail := tailFile(filepath.Join(b.store.LogDir(), "hev.log"), 40); strings.TrimSpace(tail) != "" {
			msg = msg + "; hev tail:\n" + tail
		}
	}
	b.addLog("warn", "tun", msg)
	_ = b.StopProxy()
	b.mu.Lock()
	b.tunRecovering = false
	if strings.TrimSpace(b.state.LastError) == "" {
		b.state.LastError = "tun process exited unexpectedly"
	}
	b.emitStateLocked()
	b.mu.Unlock()
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
	b.eventMu.RLock()
	hasEmitter := b.eventEmitter != nil
	b.eventMu.RUnlock()
	if !hasEmitter {
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
	b.eventMu.RLock()
	hasEmitter := b.eventEmitter != nil
	b.eventMu.RUnlock()
	if !hasEmitter {
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
