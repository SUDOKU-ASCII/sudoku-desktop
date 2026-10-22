package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	kernelReadyTimeout = 15 * time.Second
	kernelTailLines    = 40
)

// kernel is one running Sudoku core: the child process plus its controller client.
type kernel struct {
	proc    *ManagedProcess
	ctl     *ctlClient
	names   map[string]string // node ID -> outbound name reported by the controller
	version string

	tailMu sync.Mutex
	tail   []string
}

func (k *kernel) addTail(line string) {
	k.tailMu.Lock()
	k.tail = append(k.tail, line)
	if len(k.tail) > kernelTailLines {
		k.tail = append(k.tail[:0], k.tail[len(k.tail)-kernelTailLines:]...)
	}
	k.tailMu.Unlock()
}

func (k *kernel) tailText() string {
	k.tailMu.Lock()
	defer k.tailMu.Unlock()
	return strings.TrimSpace(stripANSI(strings.Join(k.tail, "\n")))
}

// stop terminates the process and releases the controller connections.
func (k *kernel) stop(timeout time.Duration) error {
	if k == nil {
		return nil
	}
	k.ctl.Close()
	return k.proc.Stop(timeout)
}

// mapMode converts the app's routing mode to the controller's.
func controllerMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "global":
		return "global"
	case "direct":
		return "direct"
	default:
		return "rule"
	}
}

// kernelStartOptions are the per-start inputs of startKernel.
type kernelStartOptions struct {
	Cfg           *AppConfig
	Binary        string
	CustomRuleURL string
	RuleCacheDir  string
	ConfigsDir    string
	WithTun       bool
	OnLine        func(string)
	OnSpawn       func(pid int)
}

// startKernel launches the core with one config per node and waits until the controller and
// the mixed listener answer. On any error nothing is left running.
func startKernel(ctx context.Context, opt kernelStartOptions, logf func(string)) (*kernel, error) {
	cfg := opt.Cfg
	files, err := buildKernelConfigs(cfg, opt.CustomRuleURL)
	if err != nil {
		return nil, err
	}
	paths, err := writeKernelConfigs(opt.ConfigsDir, files)
	if err != nil {
		return nil, err
	}
	if err := ensureDir(opt.RuleCacheDir); err != nil {
		logf(fmt.Sprintf("rule cache directory unavailable: %v", err))
	}

	env := []string{
		"SUDOKU_LOG_LEVEL=" + coreLogLevel(cfg.Core.LogLevel),
		"SUDOKU_RULE_CACHE_DIR=" + opt.RuleCacheDir,
	}
	if opt.WithTun {
		env = append(env, outboundBypassEnv(cfg.Tun, logf)...)
	}
	args := make([]string, 0, len(paths)*2)
	for _, p := range paths {
		args = append(args, "-c", p)
	}

	k := &kernel{
		proc:  NewManagedProcess("sudoku"),
		ctl:   newCtlClient(cfg.Controller.Port, cfg.Controller.Secret),
		names: nodeProxyNames(cfg.Nodes),
	}
	onLine := func(line string) {
		k.addTail(line)
		if opt.OnLine != nil {
			opt.OnLine(line)
		}
	}
	logf(fmt.Sprintf("starting sudoku core with %d node config(s)", len(paths)))
	if err := k.proc.Start(opt.Binary, args, env, cfg.Core.WorkingDir, onLine); err != nil {
		k.ctl.Close()
		return nil, err
	}
	if opt.OnSpawn != nil {
		opt.OnSpawn(k.proc.PID())
	}
	fail := func(err error) (*kernel, error) {
		_ = k.stop(2 * time.Second)
		return nil, err
	}

	// Race controller readiness against the process dying (config error, port in use…).
	type readyResult struct {
		version string
		err     error
	}
	readyCh := make(chan readyResult, 1)
	readyCtx, cancelReady := context.WithCancel(ctx)
	defer cancelReady()
	go func() {
		v, err := k.ctl.WaitReady(readyCtx, kernelReadyTimeout)
		readyCh <- readyResult{v, err}
	}()
	select {
	case r := <-readyCh:
		if r.err != nil {
			if ctx.Err() != nil {
				return fail(ctx.Err())
			}
			return fail(withTail(k, r.err))
		}
		k.version = r.version
	case <-k.proc.Done():
		return fail(withTail(k, errors.New("sudoku core exited during startup")))
	case <-ctx.Done():
		return fail(ctx.Err())
	}

	addr := net.JoinHostPort(localLoopbackIPv4, strconv.Itoa(ProxyPort))
	if err := waitForTCPReady(ctx, addr, 6*time.Second); err != nil {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(withTail(k, fmt.Errorf("proxy listener %s not ready: %w", addr, err)))
	}

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := k.ctl.SetMode(reqCtx, controllerMode(cfg.Routing.ProxyMode)); err != nil {
		logf(fmt.Sprintf("set routing mode failed: %v", err))
	}
	if err := k.selectNode(reqCtx, cfg, cfg.ActiveNodeID); err != nil {
		logf(fmt.Sprintf("select active node failed: %v", err))
	}
	return k, nil
}

func withTail(k *kernel, err error) error {
	if tail := k.tailText(); tail != "" {
		return fmt.Errorf("%w\n%s", err, tail)
	}
	return err
}

// selectNode points the PROXY group at a node (or at the Balance group).
func (k *kernel) selectNode(ctx context.Context, cfg *AppConfig, nodeID string) error {
	if len(cfg.Nodes) < 2 {
		return nil // the only node is already the default
	}
	name := ""
	if nodeID == BalanceNodeID {
		name = "Balance"
	} else {
		name = k.names[nodeID]
	}
	if name == "" {
		return fmt.Errorf("node %q is not known to the core", nodeID)
	}
	return k.ctl.Select(ctx, "PROXY", name)
}

func coreLogLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info", "warn", "error":
		return strings.ToLower(strings.TrimSpace(level))
	}
	return "info"
}

// outboundBypassEnv tells the core how to dial its upstream outside the tunnel so its own
// traffic can never loop back into the TUN it feeds.
func outboundBypassEnv(tun TunSettings, logf func(string)) []string {
	var env []string
	switch runtime.GOOS {
	case "linux":
		if tun.SocksMark > 0 {
			env = append(env, "SUDOKU_OUTBOUND_MARK="+strconv.Itoa(tun.SocksMark))
		}
		if src, err := linuxDefaultOutboundIPv4(); err == nil && strings.TrimSpace(src) != "" {
			env = append(env, "SUDOKU_OUTBOUND_SRC_IP="+strings.TrimSpace(src))
		}
	case "darwin":
		// Best effort: resolving the physical interface can transiently fail while Wi-Fi
		// roams. Host routes and the helper's route repair keep the upstream reachable.
		ifName, src, err := darwinResolveOutboundBypass(4 * time.Second)
		ifName = strings.TrimSpace(ifName)
		if err != nil || ifName == "" {
			logf(fmt.Sprintf("darwin: outbound bypass interface unavailable, continuing without interface bind: %v", err))
			break
		}
		env = append(env, "SUDOKU_OUTBOUND_IFACE="+ifName)
		if src = strings.TrimSpace(src); src != "" {
			env = append(env, "SUDOKU_OUTBOUND_SRC_IP="+src)
		}
	case "windows":
		if idx, err := windowsDefaultInterfaceIndex(); err == nil && idx > 0 {
			env = append(env, "SUDOKU_OUTBOUND_IFINDEX="+strconv.Itoa(idx))
		}
	}
	return env
}

// portBusy reports whether something already listens on the TCP port (any interface).
func portBusy(port int) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(localLoopbackIPv4, strconv.Itoa(port)), 300*time.Millisecond)
	if err == nil {
		_ = c.Close()
		return true
	}
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		return true
	}
	_ = ln.Close()
	return false
}

// describePortOwner names the process holding a TCP port (best effort, unix only).
func describePortOwner(port int) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lsof", "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN", "-Fcp").Output()
	if err != nil {
		return ""
	}
	var pid, cmd string
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "p") && pid == "":
			pid = line[1:]
		case strings.HasPrefix(line, "c") && cmd == "":
			cmd = line[1:]
		}
	}
	if pid == "" {
		return ""
	}
	return fmt.Sprintf("%s (pid %s)", cmd, pid)
}

// checkPorts fails early with a readable message when a fixed port is taken.
func checkPorts(cfg *AppConfig) error {
	check := func(port int, what string) error {
		if !portBusy(port) {
			return nil
		}
		if owner := describePortOwner(port); owner != "" {
			return fmt.Errorf("%s port %d is already in use by %s", what, port, owner)
		}
		return fmt.Errorf("%s port %d is already in use", what, port)
	}
	if err := check(ProxyPort, "proxy"); err != nil {
		return err
	}
	return check(cfg.Controller.Port, "controller")
}
