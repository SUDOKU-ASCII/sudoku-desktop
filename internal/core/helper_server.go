package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/SUDOKU-ASCII/sudoku/pkg/geodata"
)

// RunTunHelper is the entry point of `--tun-helper`. It returns the process exit code.
//
//	--tun-helper --addr 127.0.0.1:PORT --token-file FILE --parent PID   serve the app
//	--tun-helper --recover                                               undo a dead helper's changes
func RunTunHelper(args []string, runtimeFS fs.FS, runtimeRoot string) int {
	fl := flag.NewFlagSet("tun-helper", flag.ContinueOnError)
	addr := fl.String("addr", "", "app IPC address")
	tokenFile := fl.String("token-file", "", "file holding the IPC token")
	parent := fl.Int("parent", 0, "app pid")
	recoverOnly := fl.Bool("recover", false, "only recover stale tunnel state")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	h := &helperServer{fsys: runtimeFS, fsRoot: runtimeRoot}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	defer h.cancel()

	if *recoverOnly {
		h.logf = func(line string) { fmt.Fprintln(os.Stderr, line) }
		if err := h.recoverStale(); err != nil {
			fmt.Fprintln(os.Stderr, "recover failed:", err)
			return 1
		}
		return 0
	}

	if *addr == "" || *tokenFile == "" {
		fmt.Fprintln(os.Stderr, "missing --addr/--token-file")
		return 2
	}
	tokenRaw, err := os.ReadFile(*tokenFile)
	_ = os.Remove(*tokenFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read token:", err)
		return 2
	}
	if host, _, err := net.SplitHostPort(*addr); err != nil || host != "127.0.0.1" {
		fmt.Fprintln(os.Stderr, "refusing non-loopback app address")
		return 2
	}
	conn, err := net.DialTimeout("tcp", *addr, 5*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect to app:", err)
		return 1
	}
	h.conn = newIPCConn(conn)
	h.logf = func(line string) {
		_ = h.conn.send(&ipcMsg{Event: evLog, Line: line})
	}
	if err := h.conn.send(&ipcMsg{Op: opHello, Token: strings.TrimSpace(string(tokenRaw)), Ver: ipcVersion}); err != nil {
		return 1
	}
	ack, err := h.conn.recv()
	if err != nil || !ack.OK {
		return 1
	}
	return h.serve(*parent)
}

type helperServer struct {
	fsys   fs.FS
	fsRoot string
	conn   *ipcConn
	logf   func(string)
	ctx    context.Context
	cancel context.CancelFunc

	mu   sync.Mutex // serializes up/down/teardown
	sess *tunSession

	lastSeen atomic.Int64 // unix nanos of the last message from the app
}

func (h *helperServer) serve(parentPID int) int {
	h.lastSeen.Store(time.Now().UnixNano())
	jobs := make(chan *ipcMsg, 8)
	var workerWG sync.WaitGroup
	workerWG.Add(1)
	go func() { // runs the slow ops (up/down) one at a time
		defer workerWG.Done()
		for m := range jobs {
			h.handle(m)
		}
	}()

	quit := make(chan string, 1)
	notify := func(reason string) {
		select {
		case quit <- reason:
		default:
		}
	}

	go func() { // reader
		for {
			m, err := h.conn.recv()
			if err != nil {
				notify("connection to app lost")
				return
			}
			h.lastSeen.Store(time.Now().UnixNano())
			switch m.Op {
			case opPing:
				_ = h.conn.send(&ipcMsg{ID: m.ID, OK: true})
			case opQuit:
				_ = h.conn.send(&ipcMsg{ID: m.ID, OK: true})
				notify("quit requested")
				return
			default:
				select {
				case jobs <- m:
				default:
					_ = h.conn.send(&ipcMsg{ID: m.ID, Error: "helper busy"})
				}
			}
		}
	}()

	go func() { // watchdog: silent or vanished app
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-h.ctx.Done():
				return
			case <-t.C:
				if time.Since(time.Unix(0, h.lastSeen.Load())) > helperIdleTimeout*time.Second {
					notify("app stopped responding")
					return
				}
				if parentPID > 0 && !processAlive(parentPID) {
					notify("app process is gone")
					return
				}
			}
		}
	}()

	reason := <-quit
	h.logf("helper stopping: " + reason)
	h.cancel() // abort an in-flight up
	close(jobs)
	workerWG.Wait()
	code := 0
	h.mu.Lock()
	if h.sess != nil {
		if err := h.teardownLocked(); err != nil {
			h.logf(fmt.Sprintf("teardown on exit failed: %v (state kept for repair)", err))
			code = 1
		}
	}
	h.mu.Unlock()
	_ = h.conn.send(&ipcMsg{Event: evExit, Line: reason})
	h.conn.close()
	return code
}

func (h *helperServer) handle(m *ipcMsg) {
	reply := &ipcMsg{ID: m.ID}
	switch m.Op {
	case opUp:
		st, err := h.up(m.Plan)
		if err != nil {
			reply.Error = err.Error()
		} else {
			reply.OK, reply.Data = true, st
		}
	case opDown:
		h.mu.Lock()
		err := h.teardownLocked()
		h.mu.Unlock()
		if err != nil {
			reply.Error = err.Error()
		} else {
			reply.OK = true
		}
	case opStatus:
		h.mu.Lock()
		reply.OK, reply.Data = true, h.statusLocked()
		h.mu.Unlock()
	default:
		reply.Error = "unknown op " + m.Op
	}
	_ = h.conn.send(reply)
}

// ---------------------------------------------------------------------------------------
// session

type tunSession struct {
	plan     tunPlan
	tun      TunSettings // plan.Tun with the interface name actually in use
	hev      *ManagedProcess
	hevTail  *lineRing
	routes   *routeContext
	dns      *dnsProxyServer
	feeder   *ruleFeeder
	dnsPort  int
	serverIP []string
	iface    string
	stopCh   chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex // guards routes against the wake-repair loop
}

func (s *tunSession) stopBackground() { s.stopOnce.Do(func() { close(s.stopCh) }) }

type lineRing struct {
	mu    sync.Mutex
	lines []string
}

func (r *lineRing) add(l string) {
	r.mu.Lock()
	r.lines = append(r.lines, l)
	if len(r.lines) > 30 {
		r.lines = r.lines[len(r.lines)-30:]
	}
	r.mu.Unlock()
}

func (r *lineRing) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.TrimSpace(stripANSI(strings.Join(r.lines, "\n")))
}

func (h *helperServer) statusLocked() *tunStatus {
	st := &tunStatus{StaleState: tunStateStale()}
	if h.sess != nil {
		st.Up = true
		st.Interface = h.sess.iface
		st.SplitDNS = h.sess.dns != nil
		st.HevPID = h.sess.hev.PID()
	}
	return st
}

func (h *helperServer) up(plan *tunPlan) (*tunStatus, error) {
	if err := validateTunPlan(plan); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sess != nil {
		return nil, errors.New("tunnel is already up")
	}
	if err := h.recoverStale(); err != nil {
		return nil, fmt.Errorf("recover previous tunnel state: %w", err)
	}
	s := &tunSession{
		plan:    *plan,
		tun:     plan.Tun,
		hev:     NewManagedProcess("hev"),
		hevTail: &lineRing{},
		stopCh:  make(chan struct{}),
	}
	h.sess = s
	if err := h.bringUp(s); err != nil {
		if terr := h.teardownLocked(); terr != nil {
			err = fmt.Errorf("%w (cleanup also failed: %v)", err, terr)
		}
		return nil, err
	}
	go h.watchHev(s)
	if runtime.GOOS == "darwin" {
		go h.wakeLoop(s)
	}
	return h.statusLocked(), nil
}

func (h *helperServer) bringUp(s *tunSession) error {
	ctx := h.ctx
	plan := &s.plan
	for _, dir := range []string{helperStateDir(), helperBinDir(), helperConfigDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	hevPath, err := h.installHev()
	if err != nil {
		return err
	}
	s.serverIP = resolveServerIPv4s(plan.ServerAddrs)
	if len(s.serverIP) == 0 {
		return errors.New("could not resolve any server address to IPv4")
	}

	systemDNS, redirect := "", 0
	if wantSplitDNS(plan) {
		if err := h.startSplitDNS(s); err != nil {
			h.logf(fmt.Sprintf("split DNS unavailable, using FakeIP DNS for everything: %v", err))
		}
	}
	switch {
	case s.dns != nil:
		systemDNS, redirect = localLoopbackIPv4, localDNSProxyRedirectPort(localLoopbackIPv4)
	case s.tun.MapDNSEnabled:
		systemDNS = strings.TrimSpace(s.tun.MapDNSAddress)
	default:
		h.logf("MapDNS disabled; the tunnel uses the system resolver")
	}

	hevCfg := filepath.Join(helperConfigDir(), "hev.yaml")
	if err := writeHevConfig(hevCfg, s.tun); err != nil {
		return err
	}
	var before map[string]struct{}
	if runtime.GOOS == "darwin" {
		before = darwinListTunInterfaces()
	}
	workdir := helperConfigDir()
	if runtime.GOOS == "windows" {
		workdir = filepath.Dir(hevPath) // wintun.dll / msys-2.0.dll resolve from here
	}
	h.logf("starting hev-socks5-tunnel")
	if err := s.hev.Start(hevPath, []string{hevCfg}, nil, workdir, func(l string) {
		s.hevTail.add(l)
		h.logf("hev: " + l)
	}); err != nil {
		return err
	}
	h.saveState(s)

	select {
	case <-time.After(900 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.hev.Done():
		return withTailText("hev exited early", s.hevTail.text())
	default:
	}
	s.iface = s.tun.InterfaceName
	if runtime.GOOS == "darwin" {
		actual := darwinWaitNewTunInterface(before, 3*time.Second)
		if actual == "" {
			actual = darwinFindTunInterfaceByIPv4(s.tun.IPv4)
		}
		if actual != "" {
			s.tun.InterfaceName, s.iface = actual, actual
			h.logf("tunnel interface: " + actual)
		}
	}

	routes, err := setupRoutes(ctx, routePlan{Tun: s.tun, ServerIPs: s.serverIP, SystemDNS: systemDNS, DNSRedirectPort: redirect},
		func(rc *routeContext) error {
			s.mu.Lock()
			s.routes = rc
			s.mu.Unlock()
			if rc.TunAlias != "" {
				s.iface = rc.TunAlias
			}
			h.saveState(s)
			return nil
		}, h.logf)
	if routes != nil {
		s.mu.Lock()
		s.routes = routes
		s.mu.Unlock()
	}
	if err != nil {
		return err
	}
	h.saveState(s)

	hctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return h.healthCheck(hctx, s, systemDNS)
}

func (h *helperServer) healthCheck(ctx context.Context, s *tunSession, systemDNS string) error {
	socks := net.JoinHostPort(localLoopbackIPv4, strconv.Itoa(ProxyPort))
	if err := waitForTCPReady(ctx, socks, 3*time.Second); err != nil {
		return fmt.Errorf("core proxy not reachable: %w", err)
	}
	mode := s.plan.Mode
	if mode == "global" || mode == "pac" {
		if err := healthCheckSOCKS5Connect(ctx, socks, "1.1.1.1:443", 3*time.Second); err != nil {
			return fmt.Errorf("proxy path check failed: %w", err)
		}
	}
	if mode == "direct" || mode == "pac" {
		if err := healthCheckSOCKS5ConnectAny(ctx, socks, []string{"223.5.5.5:443", "119.29.29.29:443"}, 3*time.Second); err != nil {
			// Some networks block these fixed probes while the dataplane is healthy.
			h.logf(fmt.Sprintf("direct path probe failed (non-fatal): %v", err))
		}
	}
	if s.tun.MapDNSEnabled {
		target := net.JoinHostPort(strings.TrimSpace(s.tun.MapDNSAddress), strconv.Itoa(s.tun.MapDNSPort))
		if s.dns != nil {
			target = net.JoinHostPort(localLoopbackIPv4, "53")
		}
		if err := healthCheckDNSUDP(ctx, target, "www.baidu.com", 2*time.Second); err != nil {
			return fmt.Errorf("DNS check failed: %w", err)
		}
	}
	return nil
}

func withTailText(msg, tail string) error {
	if tail == "" {
		return errors.New(msg)
	}
	return fmt.Errorf("%s:\n%s", msg, tail)
}

// watchHev tears the tunnel down if hev dies by itself, since routes would otherwise point
// into a dead device.
func (h *helperServer) watchHev(s *tunSession) {
	select {
	case <-s.hev.Done():
	case <-s.stopCh:
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sess != s {
		return
	}
	reason := withTailText("tunnel process exited unexpectedly", s.hevTail.text()).Error()
	h.logf(reason)
	if err := h.teardownLocked(); err != nil {
		reason += fmt.Sprintf("\nroute cleanup failed: %v", err)
	}
	_ = h.conn.send(&ipcMsg{Event: evDegraded, Line: reason})
}

// teardownLocked undoes the session (h.mu held). Routes and DNS go first so traffic returns
// to the physical network before the tunnel disappears; if that fails the tunnel is kept
// alive (a dead tunnel with live routes would look like a dead network) and the journal stays.
func (h *helperServer) teardownLocked() error {
	s := h.sess
	if s == nil {
		return nil
	}
	s.stopBackground()
	s.mu.Lock()
	routes := s.routes
	s.mu.Unlock()
	if routes != nil {
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			if err = teardownRoutes(routes, s.tun, h.logf); err == nil {
				break
			}
			time.Sleep(700 * time.Millisecond)
		}
		if err != nil {
			return fmt.Errorf("restore routes/DNS: %w", err)
		}
		s.mu.Lock()
		s.routes = nil
		s.mu.Unlock()
	}
	if s.dns != nil {
		s.dns.Stop()
	}
	if s.feeder != nil {
		s.feeder.close()
	}
	var stopErr error
	if err := s.hev.Stop(3 * time.Second); err != nil {
		stopErr = err
		if pid := s.hev.PID(); pid > 0 {
			killPID(pid)
		}
	}
	_ = os.Remove(helperStatePath())
	h.sess = nil
	return stopErr
}

func (h *helperServer) saveState(s *tunSession) {
	s.mu.Lock()
	st := tunState{
		Version:   1,
		HelperPID: os.Getpid(),
		HevPID:    s.hev.PID(),
		Tun:       s.tun,
		Routes:    s.routes,
		Updated:   time.Now().Unix(),
		Interface: s.iface,
	}
	s.mu.Unlock()
	buf, err := json.MarshalIndent(&st, "", "  ")
	if err == nil {
		err = writeFileAtomic(helperStatePath(), buf, 0o644)
	}
	if err != nil {
		h.logf(fmt.Sprintf("save tunnel state failed: %v", err))
	}
}

// recoverStale undoes what a previous (dead) helper left behind.
func (h *helperServer) recoverStale() error {
	st, err := readTunState()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		// An unreadable journal cannot be replayed; do not block the tunnel forever.
		h.logf(fmt.Sprintf("discarding unreadable tunnel state: %v", err))
		_ = os.Remove(helperStatePath())
		return nil
	}
	if st.HelperPID > 0 && st.HelperPID != os.Getpid() && processAlive(st.HelperPID) &&
		strings.Contains(processCommand(st.HelperPID), "tun-helper") {
		return errors.New("another tunnel helper is still running")
	}
	h.logf("recovering network state left by a previous session")
	if st.Routes != nil {
		var terr error
		for attempt := 0; attempt < 3; attempt++ {
			if terr = teardownRoutes(st.Routes, st.Tun, h.logf); terr == nil {
				break
			}
			time.Sleep(700 * time.Millisecond)
		}
		if terr != nil {
			return terr
		}
	}
	if st.HevPID > 0 && processAlive(st.HevPID) && strings.Contains(processCommand(st.HevPID), "hev-socks5-tunnel") {
		killPID(st.HevPID)
	}
	return os.Remove(helperStatePath())
}

// installHev extracts the bundled tunnel binary (plus Windows DLLs) into the root-owned
// helper directory so the elevated process never executes a user-writable file.
func (h *helperServer) installHev() (string, error) {
	keep := func(name string) bool {
		l := strings.ToLower(name)
		return strings.HasPrefix(l, "hev-socks5-tunnel") || strings.HasSuffix(l, ".dll")
	}
	if err := installBundledRuntime(h.fsys, h.fsRoot, helperBinDir(), keep); err != nil {
		return "", fmt.Errorf("install tunnel runtime: %w", err)
	}
	p := filepath.Join(helperBinDir(), runtimeBinaryName("hev-socks5-tunnel"))
	if fileMissing(p) {
		return "", fmt.Errorf("tunnel binary missing: %s", p)
	}
	if missing := missingHevRuntimeDeps(p); len(missing) > 0 {
		return "", fmt.Errorf("tunnel runtime dependencies missing: %s", strings.Join(missing, ", "))
	}
	return p, nil
}

// ---------------------------------------------------------------------------------------
// split DNS

func wantSplitDNS(p *tunPlan) bool {
	if p.Mode != "pac" || !p.Tun.MapDNSEnabled || len(p.DirectRuleURLs) == 0 {
		return false
	}
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	case "linux":
		return localDNSProxyRedirectPort(localLoopbackIPv4) == 0 || linuxHasCommand("iptables")
	}
	return false
}

// newBypassDialer dials outside the tunnel (physical interface / marked socket).
func newBypassDialer(tun TunSettings) (*net.Dialer, error) {
	cfg := outboundBypassConfig{}
	switch runtime.GOOS {
	case "darwin":
		ifName, src, err := darwinResolveOutboundBypass(2 * time.Second)
		if err != nil {
			return nil, err
		}
		if ifName = strings.TrimSpace(ifName); ifName == "" {
			return nil, errors.New("default physical interface not found")
		}
		cfg.DarwinInterface, cfg.DarwinSourceIP = ifName, strings.TrimSpace(src)
	case "linux":
		cfg.LinuxMark = tun.SocksMark
		if src, err := linuxDefaultOutboundIPv4(); err == nil {
			cfg.LinuxSourceIP = strings.TrimSpace(src)
		}
	case "windows":
		if idx, err := windowsDefaultInterfaceIndex(); err == nil && idx > 0 {
			cfg.WindowsIfIndex = idx
		}
	}
	return newOutboundBypassDialer(3*time.Second, cfg), nil
}

func (h *helperServer) startSplitDNS(s *tunSession) error {
	dialer, err := newBypassDialer(s.tun)
	if err != nil {
		return fmt.Errorf("outbound bypass unavailable: %w", err)
	}
	feeder, err := newRuleFeeder(s.plan.DirectRuleURLs, s.plan.RuleSeedDir)
	if err != nil {
		return err
	}
	proxy := newDNSProxyServer(dnsProxyConfig{
		DirectMatch: feeder.Match,
		ListenPort:  localDNSProxyListenPort(),
		MapDNSAddr:  net.JoinHostPort(strings.TrimSpace(s.tun.MapDNSAddress), strconv.Itoa(s.tun.MapDNSPort)),
		PreferIPv4:  true,
		DirectDial:  dialer.DialContext,
		Logf:        h.logf,
	})
	if err := proxy.Start(); err != nil {
		feeder.close()
		return err
	}
	s.dns, s.feeder = proxy, feeder
	h.logf("split DNS enabled: direct rules resolve through DoH, everything else through FakeIP")
	return nil
}

// ruleFeeder gives the helper the direct-rule sets without letting a root process fetch or
// write files in a user-controlled place: the app's core already downloaded the rule
// sources into its cache; the feeder serves those cached files on loopback to a geodata
// Manager (which then parses them exactly like the core does) and keeps it fresh.
type ruleFeeder struct {
	srv    *http.Server
	ln     net.Listener
	mgr    *geodata.Manager
	paths  []string
	mu     sync.Mutex
	mtimes map[string]int64
	stop   chan struct{}
}

func ruleCacheFile(dir, rawURL string) string {
	return filepath.Join(dir, fmt.Sprintf("%x.rules", sha256.Sum256([]byte(rawURL))))
}

func newRuleFeeder(urls []string, seedDir string) (*ruleFeeder, error) {
	if strings.TrimSpace(seedDir) == "" {
		return nil, errors.New("no rule cache to seed from")
	}
	f := &ruleFeeder{mtimes: map[string]int64{}, stop: make(chan struct{})}
	local := make([]string, 0, len(urls))
	for _, u := range urls {
		f.paths = append(f.paths, ruleCacheFile(seedDir, u))
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f.ln = ln
	mux := http.NewServeMux()
	for i := range urls {
		i := i
		mux.HandleFunc("/r/"+strconv.Itoa(i), func(w http.ResponseWriter, r *http.Request) {
			body, err := readRegularFile(f.paths[i], 32<<20)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		})
		local = append(local, fmt.Sprintf("http://127.0.0.1:%d/r/%d", ln.Addr().(*net.TCPAddr).Port, i))
	}
	f.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second}
	go func() { _ = f.srv.Serve(ln) }()

	// The Manager caches into a root-owned directory the helper controls.
	_ = os.MkdirAll(helperRuleDir(), 0o700)
	_ = os.Setenv("SUDOKU_RULE_CACHE_DIR", helperRuleDir())
	f.mgr = geodata.NewManager(local)
	go f.refresh()
	go f.watch()
	return f, nil
}

func readRegularFile(path string, max int64) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > max {
		return nil, errors.New("not a regular rule file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
}

func (f *ruleFeeder) refresh() {
	f.mgr.Update()
	f.mu.Lock()
	for _, p := range f.paths {
		if fi, err := os.Stat(p); err == nil {
			f.mtimes[p] = fi.ModTime().UnixNano()
		}
	}
	f.mu.Unlock()
}

// watch re-parses when the core refreshes its cache (first run: files appear a few seconds
// after the core started).
func (f *ruleFeeder) watch() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-f.stop:
			return
		case <-t.C:
			changed := false
			f.mu.Lock()
			for _, p := range f.paths {
				fi, err := os.Stat(p)
				if err != nil {
					continue
				}
				if f.mtimes[p] != fi.ModTime().UnixNano() {
					changed = true
				}
			}
			f.mu.Unlock()
			if changed {
				f.refresh()
			}
		}
	}
}

func (f *ruleFeeder) Match(host string) bool {
	ok, _ := f.mgr.MatchRuleOrLocal(host, nil)
	return ok
}

func (f *ruleFeeder) close() {
	select {
	case <-f.stop:
		return
	default:
		close(f.stop)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = f.srv.Shutdown(ctx)
}
