package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	localDNSServerIPv4      = "127.0.0.1"
	localDNSProxyPortNonWin = 1053
)

func localDNSProxyListenPort() int {
	if runtime.GOOS == "windows" {
		return 53
	}
	return localDNSProxyPortNonWin
}

type dnsProxyConfig struct {
	ProxyMode     string
	CNRules       *cnRuleSet
	MapDNSEnabled bool
	MapDNSAddr    string
	AlwaysDirect  []string
	DirectDial    func(ctx context.Context, network, addr string) (net.Conn, error)
	Logf          func(string)
}

type dnsProxyServer struct {
	cfg dnsProxyConfig

	udpConn net.PacketConn
	tcpLn   net.Listener

	stopCh chan struct{}
	wg     sync.WaitGroup

	cacheMu sync.Mutex
	cache   map[string]dnsCacheEntry

	dohDirect []*dohClient
	dohGlobal []*dohClient
}

type dnsCacheEntry struct {
	expires time.Time
	resp    []byte // response with ID bytes zeroed
}

func newDNSProxyServer(cfg dnsProxyConfig) *dnsProxyServer {
	s := &dnsProxyServer{
		cfg:    cfg,
		stopCh: make(chan struct{}),
		cache:  map[string]dnsCacheEntry{},
	}
	s.dohDirect = []*dohClient{
		newDoHClient("dns.alidns.com", "/dns-query", []string{"223.5.5.5", "223.6.6.6"}, cfg.DirectDial),
		newDoHClient("doh.pub", "/dns-query", []string{"119.29.29.29", "119.28.28.28"}, cfg.DirectDial),
	}
	s.dohGlobal = []*dohClient{
		newDoHClient("cloudflare-dns.com", "/dns-query", []string{"1.1.1.1", "1.0.0.1"}, cfg.DirectDial),
		newDoHClient("dns.google", "/dns-query", []string{"8.8.8.8", "8.8.4.4"}, cfg.DirectDial),
		newDoHClient("dns.quad9.net", "/dns-query", []string{"9.9.9.9", "149.112.112.112"}, cfg.DirectDial),
	}
	return s
}

func (s *dnsProxyServer) Start() error {
	if s == nil {
		return errors.New("nil dns proxy")
	}
	port := localDNSProxyListenPort()
	addr := net.JoinHostPort(localDNSServerIPv4, fmt.Sprintf("%d", port))

	udpConn, err := net.ListenPacket("udp4", addr)
	if err != nil {
		return err
	}
	tcpLn, err := net.Listen("tcp4", addr)
	if err != nil {
		_ = udpConn.Close()
		return err
	}

	s.udpConn = udpConn
	s.tcpLn = tcpLn

	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.serveUDP()
	}()
	go func() {
		defer s.wg.Done()
		s.serveTCP()
	}()

	if s.cfg.Logf != nil {
		s.cfg.Logf(fmt.Sprintf("dns proxy listening on %s (udp/tcp)", addr))
	}
	return nil
}

func (s *dnsProxyServer) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stopCh:
		// already stopped
		return
	default:
		close(s.stopCh)
	}
	if s.tcpLn != nil {
		_ = s.tcpLn.Close()
	}
	if s.udpConn != nil {
		_ = s.udpConn.Close()
	}
	s.wg.Wait()
}

func (s *dnsProxyServer) serveUDP() {
	buf := make([]byte, 4096)
	for {
		n, addr, err := s.udpConn.ReadFrom(buf)
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
			}
			continue
		}
		req := append([]byte(nil), buf[:n]...)
		s.wg.Add(1)
		go func(peer net.Addr, payload []byte) {
			defer s.wg.Done()
			resp := s.handleQuery(payload)
			if len(resp) == 0 {
				return
			}
			_, _ = s.udpConn.WriteTo(resp, peer)
		}(addr, req)
	}
}

func (s *dnsProxyServer) serveTCP() {
	for {
		c, err := s.tcpLn.Accept()
		if err != nil {
			select {
			case <-s.stopCh:
				return
			default:
			}
			continue
		}
		s.wg.Add(1)
		go func(conn net.Conn) {
			defer s.wg.Done()
			defer conn.Close()

			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			for {
				req, err := readDNSOverTCP(conn)
				if err != nil {
					return
				}
				resp := s.handleQuery(req)
				if len(resp) == 0 {
					resp = s.buildSERVFAIL(req)
				}
				if err := writeDNSOverTCP(conn, resp); err != nil {
					return
				}
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			}
		}(c)
	}
}

func readDNSOverTCP(r io.Reader) ([]byte, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := int(lenBuf[0])<<8 | int(lenBuf[1])
	if n <= 0 || n > 65535 {
		return nil, fmt.Errorf("invalid dns tcp length: %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeDNSOverTCP(w io.Writer, msg []byte) error {
	if len(msg) == 0 || len(msg) > 65535 {
		return fmt.Errorf("invalid dns msg length: %d", len(msg))
	}
	lenBuf := []byte{byte(len(msg) >> 8), byte(len(msg))}
	if _, err := w.Write(lenBuf); err != nil {
		return err
	}
	_, err := w.Write(msg)
	return err
}

func (s *dnsProxyServer) handleQuery(req []byte) []byte {
	id := dnsID(req)
	qname, qtype, ok := parseDNSQuestion(req)
	if !ok {
		return s.buildSERVFAILWithID(id)
	}

	key := s.cacheKey(qname, qtype)
	if cached := s.loadCache(key, id); len(cached) > 0 {
		return cached
	}

	shouldDirect := s.shouldDirect(qname)
	var resp []byte
	if shouldDirect {
		resp = s.forwardDirect(req)
	} else if !s.cfg.MapDNSEnabled || strings.TrimSpace(s.cfg.MapDNSAddr) == "" {
		resp = s.forwardNonDirect(req)
	} else {
		resp = s.forwardMapDNS(req)
		if len(resp) == 0 {
			// Fallback: keep the system working even if MapDNS isn't reachable yet.
			resp = s.forwardNonDirect(req)
		}
	}
	if len(resp) == 0 {
		resp = s.buildSERVFAILWithID(id)
	}
	s.storeCache(key, resp, 25*time.Second)
	return resp
}

func (s *dnsProxyServer) shouldDirect(host string) bool {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.ProxyMode))
	if mode == "direct" {
		return true
	}
	if mode == "global" {
		return false
	}

	host = normalizeLookupHost(host)
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		// IP literals should always be resolved via "direct" (no mapping).
		return true
	}
	if isLikelyLocalHostname(host) {
		return true
	}
	for _, h := range s.cfg.AlwaysDirect {
		if normalizeLookupHost(h) == host {
			return true
		}
	}
	if mode == "pac" && s.cfg.CNRules != nil && s.cfg.CNRules.matchDomain(host) {
		return true
	}
	return false
}

func isLikelyLocalHostname(host string) bool {
	if host == "" {
		return false
	}
	if !strings.Contains(host, ".") {
		return true
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".lan") {
		return true
	}
	return false
}

func (s *dnsProxyServer) forwardMapDNS(req []byte) []byte {
	resp, _ := dnsExchangeUDP(s.cfg.MapDNSAddr, req, 800*time.Millisecond)
	return resp
}

func (s *dnsProxyServer) forwardDirect(req []byte) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 1400*time.Millisecond)
	defer cancel()
	for _, c := range s.dohDirect {
		if c == nil {
			continue
		}
		resp, err := c.Exchange(ctx, req)
		if err == nil && len(resp) > 0 {
			return resp
		}
	}
	// Last resort: try plaintext UDP to AliDNS/Tencent DNS.
	// Note: in FakeIP router environments (e.g. OpenClash), port-53 may be hijacked and return
	// bogus answers (198.18.0.0/15 etc). Detect and ignore those to avoid breaking DIRECT dials.
	for _, server := range []string{"223.5.5.5:53", "119.29.29.29:53"} {
		resp, _ := dnsExchangeUDPWithDial(s.cfg.DirectDial, server, req, 800*time.Millisecond)
		if len(resp) > 0 && !dnsResponseLooksHijacked(resp) {
			return resp
		}
	}
	return nil
}

func (s *dnsProxyServer) forwardNonDirect(req []byte) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 1600*time.Millisecond)
	defer cancel()
	for _, c := range s.dohGlobal {
		if c == nil {
			continue
		}
		resp, err := c.Exchange(ctx, req)
		if err == nil && len(resp) > 0 {
			return resp
		}
	}
	for _, server := range []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"} {
		resp, _ := dnsExchangeUDPWithDial(s.cfg.DirectDial, server, req, 800*time.Millisecond)
		if len(resp) > 0 && !dnsResponseLooksHijacked(resp) {
			return resp
		}
	}
	return s.forwardDirect(req)
}

func (s *dnsProxyServer) cacheKey(name string, qtype dnsmessage.Type) string {
	name = strings.ToLower(strings.TrimSpace(name))
	return fmt.Sprintf("%s|%d|%t", name, qtype, s.shouldDirect(name))
}

func (s *dnsProxyServer) loadCache(key string, id uint16) []byte {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	ent, ok := s.cache[key]
	if !ok || time.Now().After(ent.expires) || len(ent.resp) < 2 {
		if ok {
			delete(s.cache, key)
		}
		return nil
	}
	out := append([]byte(nil), ent.resp...)
	out[0] = byte(id >> 8)
	out[1] = byte(id)
	return out
}

func (s *dnsProxyServer) storeCache(key string, resp []byte, ttl time.Duration) {
	if len(resp) < 2 {
		return
	}
	cp := append([]byte(nil), resp...)
	cp[0] = 0
	cp[1] = 0
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if len(s.cache) > 1024 {
		// Simple bound: reset cache under extreme churn.
		s.cache = map[string]dnsCacheEntry{}
	}
	s.cache[key] = dnsCacheEntry{expires: time.Now().Add(ttl), resp: cp}
}

func dnsID(msg []byte) uint16 {
	if len(msg) < 2 {
		return 0
	}
	return uint16(msg[0])<<8 | uint16(msg[1])
}

func parseDNSQuestion(msg []byte) (name string, qtype dnsmessage.Type, ok bool) {
	var p dnsmessage.Parser
	_, err := p.Start(msg)
	if err != nil {
		return "", 0, false
	}
	q, err := p.Question()
	if err != nil {
		return "", 0, false
	}
	name = strings.TrimSuffix(q.Name.String(), ".")
	name = strings.ToLower(name)
	return name, q.Type, true
}

func (s *dnsProxyServer) buildSERVFAIL(req []byte) []byte {
	id := dnsID(req)
	return s.buildSERVFAILWithID(id)
}

func (s *dnsProxyServer) buildSERVFAILWithID(id uint16) []byte {
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID:                 id,
		Response:           true,
		RecursionAvailable: true,
		RCode:              dnsmessage.RCodeServerFailure,
	})
	msg, _ := b.Finish()
	return msg
}

func dnsExchangeUDP(server string, req []byte, timeout time.Duration) ([]byte, error) {
	return dnsExchangeUDPWithDial(nil, server, req, timeout)
}

func dnsExchangeUDPWithDial(dial func(ctx context.Context, network, addr string) (net.Conn, error), server string, req []byte, timeout time.Duration) ([]byte, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return nil, errors.New("empty dns server")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	conn, err := dial(ctx, "udp", server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

type dohClient struct {
	host      string
	path      string
	bootstrap []string
	next      uint32
	client    *http.Client
	dial      func(ctx context.Context, network, addr string) (net.Conn, error)
}

func newDoHClient(host string, path string, bootstrap []string, dial func(ctx context.Context, network, addr string) (net.Conn, error)) *dohClient {
	host = strings.TrimSpace(host)
	path = strings.TrimSpace(path)
	bootstrap = filterIPs(bootstrap)
	if host == "" || path == "" || len(bootstrap) == 0 {
		return nil
	}

	c := &dohClient{
		host:      host,
		path:      path,
		bootstrap: bootstrap,
		dial:      dial,
	}

	tr := &http.Transport{
		// Ignore environment proxy vars (HTTP_PROXY/HTTPS_PROXY/ALL_PROXY).
		// Those are frequently set on developer machines and would unintentionally route DoH
		// through some other local proxy (often dead), making the whole system DNS break.
		Proxy: nil,
		TLSClientConfig: &tls.Config{
			ServerName: host,
		},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := c.dial
			if dialer == nil {
				dialer = (&net.Dialer{}).DialContext
			}
			h, port, err := net.SplitHostPort(addr)
			if err != nil {
				return dialer(ctx, network, addr)
			}
			if strings.EqualFold(h, host) {
				idx := atomic.AddUint32(&c.next, 1)
				ip := c.bootstrap[int(idx)%len(c.bootstrap)]
				return dialer(ctx, network, net.JoinHostPort(ip, port))
			}
			return dialer(ctx, network, addr)
		},
	}
	c.client = &http.Client{
		Timeout:   2 * time.Second,
		Transport: tr,
	}
	return c
}

func (c *dohClient) Exchange(ctx context.Context, query []byte) ([]byte, error) {
	if c == nil {
		return nil, errors.New("nil doh client")
	}
	url := "https://" + c.host + c.path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("doh http %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func filterIPs(ips []string) []string {
	out := make([]string, 0, len(ips))
	for _, s := range ips {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if net.ParseIP(s) == nil {
			continue
		}
		out = append(out, s)
	}
	return out
}

func dnsResponseLooksHijacked(resp []byte) bool {
	// Heuristic: public DNS shouldn't return IPs from these reserved FakeIP ranges for normal domains.
	benchmarkFake := &net.IPNet{IP: net.IPv4(198, 18, 0, 0), Mask: net.CIDRMask(15, 32)} // 198.18.0.0/15
	mapdnsFake := &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}    // 100.64.0.0/10

	var p dnsmessage.Parser
	if _, err := p.Start(resp); err != nil {
		return false
	}
	// Skip questions.
	for {
		_, err := p.Question()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return false
		}
	}
	for {
		h, err := p.AnswerHeader()
		if err == dnsmessage.ErrSectionDone {
			return false
		}
		if err != nil {
			return false
		}
		switch h.Type {
		case dnsmessage.TypeA:
			a, err := p.AResource()
			if err != nil {
				return false
			}
			ip := net.IPv4(a.A[0], a.A[1], a.A[2], a.A[3])
			if benchmarkFake.Contains(ip) || mapdnsFake.Contains(ip) {
				return true
			}
		default:
			_ = p.SkipAnswer()
		}
	}
}
