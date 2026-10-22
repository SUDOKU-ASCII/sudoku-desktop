package core

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// The TUN helper is a second instance of this executable running with administrator
// rights. It owns everything privileged (the tunnel device, routes, system DNS, firewall
// rules) so the GUI process never runs as root. The two talk over one loopback TCP
// connection that the *app* listens on and the helper dials, using newline-delimited JSON:
//
//	helper → app   hello{token}                       (first line, authenticates the helper)
//	app    → helper ping | up{plan} | down | status | quit   (each answered with {id, ok, error, data})
//	helper → app   events: log | exit | degraded
//
// The helper tears everything down when the connection drops, when the app stops pinging
// for helperIdleTimeout, or when the app process disappears, so a crashed GUI can never
// leave the machine routed into a dead tunnel.

const (
	ipcVersion        = 1
	maxIPCLine        = 1 << 20
	helperIdleTimeout = 10
)

const (
	opHello  = "hello"
	opPing   = "ping"
	opUp     = "up"
	opDown   = "down"
	opStatus = "status"
	opQuit   = "quit"

	evLog      = "log"
	evExit     = "exit"
	evDegraded = "degraded"
)

// tunPlan is everything the helper needs to bring the tunnel up. It is validated by the
// helper: the app is not trusted to hand a root process arbitrary strings.
type tunPlan struct {
	Tun         TunSettings `json:"tun"`
	ProxyPort   int         `json:"proxyPort"`
	ServerAddrs []string    `json:"serverAddrs"`
	// Mode is the routing mode (pac|global|direct); it selects the health checks and
	// whether split DNS is wanted.
	Mode string `json:"mode"`
	// DirectRuleURLs are the plain (direct) rule sources used to split DNS in pac mode.
	DirectRuleURLs []string `json:"directRuleUrls,omitempty"`
	// RuleSeedDir is the app's rule cache; its files seed the helper's own root-owned cache.
	RuleSeedDir string `json:"ruleSeedDir,omitempty"`
}

type tunStatus struct {
	Up         bool   `json:"up"`
	Interface  string `json:"interface,omitempty"`
	SplitDNS   bool   `json:"splitDns,omitempty"`
	HevPID     int    `json:"hevPid,omitempty"`
	StaleState bool   `json:"staleState,omitempty"`
}

type ipcMsg struct {
	ID    uint64     `json:"id,omitempty"`
	Op    string     `json:"op,omitempty"`
	Token string     `json:"token,omitempty"`
	Ver   int        `json:"ver,omitempty"`
	Plan  *tunPlan   `json:"plan,omitempty"`
	OK    bool       `json:"ok,omitempty"`
	Error string     `json:"error,omitempty"`
	Event string     `json:"event,omitempty"`
	Line  string     `json:"line,omitempty"`
	Data  *tunStatus `json:"data,omitempty"`
}

// ipcConn is a line-oriented JSON connection with serialized writes.
type ipcConn struct {
	conn net.Conn
	rd   *bufio.Reader
	wmu  sync.Mutex
}

func newIPCConn(c net.Conn) *ipcConn {
	return &ipcConn{conn: c, rd: bufio.NewReaderSize(c, 64*1024)}
}

func (c *ipcConn) send(m *ipcMsg) error {
	buf, err := json.Marshal(m)
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.conn.Write(buf)
	return err
}

func (c *ipcConn) recv() (*ipcMsg, error) {
	var line []byte
	for {
		chunk, isPrefix, err := c.rd.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, chunk...)
		if len(line) > maxIPCLine {
			return nil, errors.New("ipc message too large")
		}
		if !isPrefix {
			break
		}
	}
	var m ipcMsg
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (c *ipcConn) close() { _ = c.conn.Close() }

var (
	ifaceNameRe = regexp.MustCompile(`^[A-Za-z0-9_.\- ]{1,32}$`)
	hostNameRe  = regexp.MustCompile(`^[A-Za-z0-9._\-]{1,253}$`)
)

// validateTunPlan rejects anything that is not a plausible, shell-inert tunnel plan.
func validateTunPlan(p *tunPlan) error {
	if p == nil {
		return errors.New("missing plan")
	}
	if p.ProxyPort != ProxyPort {
		return fmt.Errorf("unexpected proxy port %d", p.ProxyPort)
	}
	switch p.Mode {
	case "pac", "global", "direct":
	default:
		return fmt.Errorf("invalid mode %q", p.Mode)
	}
	t := &p.Tun
	if !ifaceNameRe.MatchString(t.InterfaceName) {
		return fmt.Errorf("invalid interface name %q", t.InterfaceName)
	}
	if t.MTU < 576 || t.MTU > 65535 {
		return fmt.Errorf("invalid mtu %d", t.MTU)
	}
	if ip := net.ParseIP(t.IPv4); ip == nil || ip.To4() == nil {
		return fmt.Errorf("invalid tun ipv4 %q", t.IPv4)
	}
	if t.IPv6 != "" && net.ParseIP(t.IPv6) == nil {
		return fmt.Errorf("invalid tun ipv6 %q", t.IPv6)
	}
	switch strings.TrimSpace(t.SocksUDP) {
	case "", "tcp", "udp":
	default:
		return fmt.Errorf("invalid socks udp mode %q", t.SocksUDP)
	}
	if t.SocksMark < 0 || t.SocksMark > 1<<30 || t.RouteTable < 0 || t.RouteTable > 1<<30 {
		return errors.New("invalid mark/table")
	}
	switch strings.ToLower(strings.TrimSpace(t.LogLevel)) {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid hev log level %q", t.LogLevel)
	}
	if t.MapDNSEnabled {
		if ip := net.ParseIP(t.MapDNSAddress); ip == nil || ip.To4() == nil {
			return fmt.Errorf("invalid mapdns address %q", t.MapDNSAddress)
		}
		if t.MapDNSPort < 1 || t.MapDNSPort > 65535 {
			return fmt.Errorf("invalid mapdns port %d", t.MapDNSPort)
		}
		if ip := net.ParseIP(t.MapDNSNetwork); ip == nil || ip.To4() == nil {
			return fmt.Errorf("invalid mapdns network %q", t.MapDNSNetwork)
		}
		if ip := net.ParseIP(t.MapDNSNetmask); ip == nil || ip.To4() == nil {
			return fmt.Errorf("invalid mapdns netmask %q", t.MapDNSNetmask)
		}
	}
	for _, v := range []int{t.TaskStackSize, t.TCPBufferSize, t.MaxSession, t.ConnectTimeout} {
		if v < 0 || v > 1<<30 {
			return errors.New("invalid hev tuning value")
		}
	}
	if len(p.ServerAddrs) == 0 || len(p.ServerAddrs) > 256 {
		return errors.New("no server addresses")
	}
	for _, a := range p.ServerAddrs {
		host, _, err := net.SplitHostPort(strings.TrimSpace(a))
		if err != nil {
			host = strings.TrimSpace(a)
		}
		host = strings.Trim(host, "[]")
		if net.ParseIP(host) == nil && !hostNameRe.MatchString(host) {
			return fmt.Errorf("invalid server address %q", a)
		}
	}
	if len(p.DirectRuleURLs) > 64 {
		return errors.New("too many rule urls")
	}
	for _, raw := range p.DirectRuleURLs {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("invalid rule url %q", raw)
		}
	}
	return nil
}

// helperStateDir is the root-owned, world-readable directory of the helper's journal and
// extracted binaries. World-readable so the unprivileged app can detect stale tunnel state.
func helperStateDir() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/4x4-sudoku"
	case "windows":
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "4x4-sudoku")
	default:
		return "/var/lib/4x4-sudoku"
	}
}

func helperStatePath() string { return filepath.Join(helperStateDir(), "tun-state.json") }
func helperBinDir() string    { return filepath.Join(helperStateDir(), "bin") }
func helperConfigDir() string { return filepath.Join(helperStateDir(), "run") }
func helperRuleDir() string   { return filepath.Join(helperStateDir(), "rules") }

// tunState is the helper's crash journal: enough to undo the routes/DNS/hev of a helper
// that was killed.
type tunState struct {
	Version     int           `json:"version"`
	HelperPID   int           `json:"helperPid"`
	HevPID      int           `json:"hevPid,omitempty"`
	Tun         TunSettings   `json:"tun"`
	Routes      *routeContext `json:"routes,omitempty"`
	Updated     int64         `json:"updated"`
	Interface   string        `json:"interface,omitempty"`
	DNSRedirect int           `json:"dnsRedirect,omitempty"`
}

func readTunState() (*tunState, error) {
	buf, err := os.ReadFile(helperStatePath())
	if err != nil {
		return nil, err
	}
	var s tunState
	if err := json.Unmarshal(buf, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// tunStateStale reports whether a helper journal exists whose helper is gone, i.e. the
// system may still carry tunnel routes / DNS overrides.
func tunStateStale() bool {
	s, err := readTunState()
	if err != nil {
		return false
	}
	return s.HelperPID <= 0 || !processAlive(s.HelperPID)
}
