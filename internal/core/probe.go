package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

const (
	probeURL     = "https://i.ytimg.com/generate_204"
	probeTimeout = 8 * time.Second
	probeWorkers = 4
)

func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp4", net.JoinHostPort(localLoopbackIPv4, "0"))
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func probeResult(n NodeConfig, ms int, err error) LatencyResult {
	r := LatencyResult{NodeID: n.ID, NodeName: n.Name, LatencyMs: -1, CheckedAtUnix: time.Now().UnixMilli()}
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.LatencyMs, r.ConnectOK = int64(ms), ms > 0
	return r
}

// measureDelays times every node through the given controller, a few at a time.
func measureDelays(ctx context.Context, ctl *ctlClient, nodes []NodeConfig, names map[string]string) []LatencyResult {
	out := make([]LatencyResult, len(nodes))
	sem := make(chan struct{}, probeWorkers)
	var wg sync.WaitGroup
	for i := range nodes {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			n := nodes[i]
			name := names[n.ID]
			if name == "" {
				out[i] = probeResult(n, 0, errors.New("node is not loaded by the core yet"))
				return
			}
			ms, err := ctl.Delay(ctx, name, probeURL, probeTimeout)
			out[i] = probeResult(n, ms, err)
		}(i)
	}
	wg.Wait()
	return out
}

// probeStopped measures nodes while the proxy is off using a throw-away core instance on
// random loopback ports.
func (b *Backend) probeStopped(ctx context.Context, nodes []NodeConfig) ([]LatencyResult, error) {
	if err := b.EnsureCoreBinaries(); err != nil {
		return nil, err
	}
	cfg := b.cfgClone()
	ctlPort, err := freeLoopbackPort()
	if err != nil {
		return nil, err
	}
	localPort, err := freeLoopbackPort()
	if err != nil {
		return nil, err
	}
	probeCfg := &AppConfig{
		Nodes:      nodes,
		Controller: ControllerSettings{Port: ctlPort, Secret: randomSecret()},
		Routing:    RoutingSettings{ProxyMode: "global"},
	}
	files, err := buildKernelConfigs(probeCfg, "")
	if err != nil {
		return nil, err
	}
	for i := range files {
		files[i].Config.LocalPort = localPort
		files[i].Config.LocalAddress = localLoopbackIPv4
	}
	files[0].Config.RuleURLs = []string{"global"}
	dir, err := os.MkdirTemp("", "sudoku-probe-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	paths, err := writeKernelConfigs(dir, files)
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, 2*len(paths))
	for _, p := range paths {
		args = append(args, "-c", p)
	}
	ctl := newCtlClient(ctlPort, probeCfg.Controller.Secret)
	defer ctl.Close()
	proc := NewManagedProcess("sudoku-probe")
	if err := proc.Start(cfg.Core.SudokuBinary, args, []string{"SUDOKU_LOG_LEVEL=error"}, dir, nil); err != nil {
		return nil, err
	}
	defer proc.Stop(1500 * time.Millisecond)
	if _, err := ctl.WaitReady(ctx, 8*time.Second); err != nil {
		return nil, fmt.Errorf("probe core did not start: %w", err)
	}
	return measureDelays(ctx, ctl, nodes, nodeProxyNames(nodes)), nil
}

func (b *Backend) probe(nodes []NodeConfig) ([]LatencyResult, error) {
	if len(nodes) == 0 {
		return []LatencyResult{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var res []LatencyResult
	if k := b.kernelRef(); k != nil {
		res = measureDelays(ctx, k.ctl, nodes, k.names)
	} else {
		var err error
		if res, err = b.probeStopped(ctx, nodes); err != nil {
			return nil, err
		}
	}
	b.mu.Lock()
	for _, r := range res {
		b.latency[r.NodeID] = r
	}
	b.mu.Unlock()
	b.markDirty()
	return res, nil
}

func (b *Backend) ProbeNode(nodeID string) (LatencyResult, error) {
	cfg := b.cfgClone()
	for _, n := range cfg.Nodes {
		if n.ID == nodeID {
			res, err := b.probe([]NodeConfig{n})
			if err != nil {
				return LatencyResult{}, err
			}
			if res[0].Error != "" {
				return res[0], errors.New(res[0].Error)
			}
			return res[0], nil
		}
	}
	return LatencyResult{}, errors.New("node not found")
}

func (b *Backend) ProbeAllNodes() []LatencyResult {
	cfg := b.cfgClone()
	res, err := b.probe(cfg.Nodes)
	if err != nil {
		b.update(func(s *RuntimeState) { s.LastError = err.Error() })
		return []LatencyResult{}
	}
	sortLatencyResults(res)
	return res
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
	return best, b.SetActiveNode(best.NodeID)
}
