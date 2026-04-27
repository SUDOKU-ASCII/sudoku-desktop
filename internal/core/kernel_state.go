package core

import (
	"context"
	"debug/buildinfo"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	gprocess "github.com/shirou/gopsutil/v4/process"
	"golang.org/x/net/proxy"
)

const (
	bundledSudokuVersion      = "v0.4.3"
	kernelLatencyProbeWindow  = 10 * time.Minute
	kernelLatencyProbeTimeout = 12 * time.Second
)

func (b *Backend) refreshKernelStateLocked(now time.Time) {
	b.refreshKernelVersionLocked()
	b.refreshKernelMemoryLocked()
	b.scheduleKernelLatencyProbeLocked(now)
}

func (b *Backend) refreshKernelVersionLocked() {
	path := strings.TrimSpace(b.cfg.Core.SudokuBinary)
	if path == b.kernelVersionBin && strings.TrimSpace(b.state.Kernel.Version) != "" {
		return
	}
	b.kernelVersionBin = path
	b.state.Kernel.Version = resolveSudokuBinaryVersion(path, b.store)
}

func (b *Backend) refreshKernelMemoryLocked() {
	pid := b.coreProc.PID()
	if pid <= 0 {
		b.state.Kernel.MemoryBytes = 0
		return
	}
	proc, err := gprocess.NewProcess(int32(pid))
	if err != nil {
		b.state.Kernel.MemoryBytes = 0
		return
	}
	mem, err := proc.MemoryInfo()
	if err != nil || mem == nil {
		b.state.Kernel.MemoryBytes = 0
		return
	}
	b.state.Kernel.MemoryBytes = mem.RSS
}

func (b *Backend) scheduleKernelLatencyProbeLocked(now time.Time) {
	if !b.state.CoreRunning || b.kernelProbing {
		return
	}
	checkedAt := b.state.Kernel.LatencyCheckedAt
	if checkedAt > 0 && now.Sub(time.UnixMilli(checkedAt)) < kernelLatencyProbeWindow {
		return
	}
	localPort := b.effectiveLocalPortLocked()
	if localPort <= 0 || localPort > 65535 {
		return
	}
	b.kernelProbing = true
	b.kernelProbeID++
	probeID := b.kernelProbeID
	go b.runKernelLatencyProbe(probeID, localPort)
}

func (b *Backend) resetKernelLatencyLocked() {
	b.kernelProbeID++
	b.kernelProbing = false
	b.state.Kernel.LatencyMs = -1
	b.state.Kernel.LatencyStatusCode = 0
	b.state.Kernel.LatencyCheckedAt = 0
	b.state.Kernel.LatencyError = ""
}

func (b *Backend) runKernelLatencyProbe(probeID uint64, localPort int) {
	latencyMs, statusCode, err := measureGenerate204Latency(localPort)
	checkedAt := time.Now().UnixMilli()

	b.mu.Lock()
	defer b.mu.Unlock()
	if probeID != b.kernelProbeID {
		return
	}
	b.kernelProbing = false
	b.state.Kernel.LatencyCheckedAt = checkedAt
	b.state.Kernel.LatencyStatusCode = statusCode
	if err != nil {
		b.state.Kernel.LatencyMs = -1
		b.state.Kernel.LatencyError = err.Error()
	} else {
		b.state.Kernel.LatencyMs = latencyMs
		b.state.Kernel.LatencyError = ""
	}
	b.emitStateLocked()
}

func resolveSudokuBinaryVersion(path string, store *Store) string {
	runtimeDir := ""
	if store != nil {
		runtimeDir = store.RuntimeDir()
	}
	if strings.TrimSpace(path) == "" {
		return bundledSudokuVersion
	}
	if runtimeDir != "" && isWithinDir(path, runtimeDir) {
		return bundledSudokuVersion
	}
	if version, ok := readGoBinaryVersion(path); ok {
		return version
	}
	return "unknown"
}

func readGoBinaryVersion(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	info, err := buildinfo.ReadFile(path)
	if err != nil {
		return "", false
	}
	if version := strings.TrimSpace(info.Main.Version); version != "" && version != "(devel)" {
		return version, true
	}
	for _, dep := range info.Deps {
		if dep == nil || dep.Path != "github.com/SUDOKU-ASCII/sudoku" {
			continue
		}
		if version := strings.TrimSpace(dep.Version); version != "" && version != "(devel)" {
			return version, true
		}
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.tag" && strings.TrimSpace(setting.Value) != "" {
			return strings.TrimSpace(setting.Value), true
		}
	}
	return "", false
}

func measureGenerate204Latency(localPort int) (int64, int, error) {
	if localPort <= 0 || localPort > 65535 {
		return -1, 0, fmt.Errorf("invalid local port: %d", localPort)
	}
	ctx, cancel := context.WithTimeout(context.Background(), kernelLatencyProbeTimeout)
	defer cancel()

	proxyAddr := net.JoinHostPort(localLoopbackIPv4, strconv.Itoa(localPort))
	dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
	if err != nil {
		return -1, 0, err
	}

	transport := &http.Transport{
		DialContext: func(_ context.Context, network string, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		},
		DisableCompression:    true,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: 8 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   kernelLatencyProbeTimeout,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://i.ytimg.com/generate_204", nil)
	if err != nil {
		return -1, 0, err
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return -1, 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	latencyMs := time.Since(start).Milliseconds()
	if resp.StatusCode != http.StatusNoContent {
		return latencyMs, resp.StatusCode, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	return latencyMs, resp.StatusCode, nil
}
