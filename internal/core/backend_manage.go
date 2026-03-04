package core

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

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
	return b.detectIPStateful(false)
}

func (b *Backend) DetectIPProxy() IPDetectResult {
	return b.detectIPStateful(true)
}

func (b *Backend) detectIPStateful(useProxy bool) IPDetectResult {
	b.mu.RLock()
	port := b.effectiveLocalPortLocked()
	b.mu.RUnlock()
	result := detectIP(useProxy, port)
	b.mu.Lock()
	defer b.mu.Unlock()
	if result.Error != "" {
		b.state.LastError = result.Error
	}
	return result
}

func (b *Backend) effectiveLocalPortLocked() int {
	if b.runningLocalPort > 0 {
		return b.runningLocalPort
	}
	return b.cfg.Core.LocalPort
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
	if limit <= 0 || limit > 20000 {
		limit = 5000
	}
	out := make([]LogEntry, 0, limit)
	for i := len(b.logs) - 1; i >= 0 && len(out) < limit; i-- {
		l := b.logs[i]
		if level != "" && level != "all" && l.Level != level {
			continue
		}
		out = append(out, l)
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

func (b *Backend) CloseConnection(connectionID string) error {
	connectionID = strings.TrimSpace(connectionID)
	if connectionID == "" {
		return errors.New("connection id is empty")
	}

	b.mu.RLock()
	conn, ok := b.connections[connectionID]
	var snapshot ActiveConnection
	if ok && conn != nil {
		snapshot = *conn
	}
	b.mu.RUnlock()
	if !ok {
		return fmt.Errorf("connection not found: %s", connectionID)
	}

	if err := terminateActiveConnection(snapshot.Network, snapshot.Source, snapshot.Destination); err != nil {
		return err
	}

	b.mu.Lock()
	delete(b.connections, connectionID)
	b.state.Connections = topConnections(b.connections, 200)
	b.emitStateLocked()
	b.mu.Unlock()
	return nil
}

func (b *Backend) CloseAllConnections() error {
	b.mu.RLock()
	running := b.state.Running
	withTun := b.state.TunRunning
	b.mu.RUnlock()

	if running {
		if err := b.RestartProxy(StartRequest{WithTun: withTun}); err != nil {
			return err
		}
	}

	b.mu.Lock()
	b.connections = map[string]*ActiveConnection{}
	b.state.Connections = []ActiveConnection{}
	b.emitStateLocked()
	b.mu.Unlock()
	return nil
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
