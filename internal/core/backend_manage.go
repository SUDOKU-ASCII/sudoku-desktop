package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func contextTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func (b *Backend) ValidateYAML(content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	var v any
	return yaml.Unmarshal([]byte(content), &v)
}

// ---------------------------------------------------------------------------------------
// reverse forwarder

func (b *Backend) StartReverseForwarder() error {
	if b.rev.IsRunning() {
		return nil
	}
	if err := b.EnsureCoreBinaries(); err != nil {
		b.update(func(s *RuntimeState) { s.LastError = err.Error() })
		return err
	}
	cfg := b.cfgClone()
	rf := cfg.ReverseForward
	if strings.TrimSpace(rf.DialURL) == "" {
		return errors.New("reverse dial URL is empty")
	}
	if strings.TrimSpace(rf.ListenAddr) == "" {
		return errors.New("reverse listen address is empty")
	}
	args := []string{"-rev-dial", rf.DialURL, "-rev-listen", rf.ListenAddr}
	if rf.Insecure {
		args = append(args, "-rev-insecure")
	}
	env := []string{"SUDOKU_LOG_LEVEL=" + coreLogLevel(cfg.Core.LogLevel)}
	err := b.rev.Start(cfg.Core.SudokuBinary, args, env, cfg.Core.WorkingDir, func(l string) { b.addCoreLine("reverse", l) })
	if err != nil {
		return err
	}
	_ = b.journal.SetChild("-rev-dial", b.rev.PID())
	b.update(func(s *RuntimeState) { s.ReverseRunning = true })
	done := b.rev.Done()
	go func() {
		<-done
		_ = b.journal.SetChild("-rev-dial", 0)
		b.update(func(s *RuntimeState) { s.ReverseRunning = false })
	}()
	return nil
}

func (b *Backend) StopReverseForwarder() error {
	err := b.rev.Stop(2 * time.Second)
	_ = b.journal.SetChild("-rev-dial", 0)
	b.update(func(s *RuntimeState) { s.ReverseRunning = false })
	return err
}

// ---------------------------------------------------------------------------------------
// diagnostics

func (b *Backend) DetectIPDirect() IPDetectResult { return b.detect(false) }
func (b *Backend) DetectIPProxy() IPDetectResult  { return b.detect(true) }

func (b *Backend) detect(useProxy bool) IPDetectResult {
	var dial func(ctx context.Context, network, addr string) (net.Conn, error)
	if !useProxy && b.GetState().TunRunning {
		// Go around the tunnel so the real uplink address is reported.
		if d, err := newBypassDialer(b.cfgClone().Tun); err == nil {
			dial = d.DialContext
		}
	}
	return detectIP(useProxy, ProxyPort, dial)
}

func (b *Backend) GetLANProxyInfo() LANProxyInfo {
	info := LANProxyInfo{Port: ProxyPort, IPs: []string{}, Ready: b.GetState().CoreRunning}
	if b.cfgClone().Core.AllowLAN {
		info.IPs = localLANIPv4s()
	}
	return info
}

func (b *Backend) GetUsageHistory(limit int) []UsageDay { return b.usage.list(limit) }

// ---------------------------------------------------------------------------------------
// connections (live view through the controller)

func (b *Backend) GetConnections() []ActiveConnection {
	k := b.kernelRef()
	if k == nil {
		return []ActiveConnection{}
	}
	ctx, cancel := contextTimeout(3 * time.Second)
	defer cancel()
	snap, err := k.ctl.Connections(ctx)
	if err != nil {
		return []ActiveConnection{}
	}
	out := make([]ActiveConnection, 0, len(snap.Connections))
	for _, c := range snap.Connections {
		out = append(out, toActiveConnection(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if len(out) > 300 {
		out = out[:300]
	}
	return out
}

func (b *Backend) CloseConnection(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("connection id is empty")
	}
	k := b.kernelRef()
	if k == nil {
		return fmt.Errorf("proxy is not running")
	}
	ctx, cancel := contextTimeout(3 * time.Second)
	defer cancel()
	return k.ctl.CloseConnection(ctx, id)
}

func (b *Backend) CloseAllConnections() error {
	k := b.kernelRef()
	if k == nil {
		return nil
	}
	ctx, cancel := contextTimeout(3 * time.Second)
	defer cancel()
	return k.ctl.CloseAllConnections(ctx)
}
