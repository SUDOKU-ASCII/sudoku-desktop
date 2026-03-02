package core

import (
	"net"
	"strings"
	"syscall"
	"time"
)

type outboundBypassConfig struct {
	DarwinInterface string
	LinuxMark       int
	LinuxSourceIP   string
	WindowsIfIndex  int
}

func newOutboundBypassDialer(timeout time.Duration, cfg outboundBypassConfig) *net.Dialer {
	d := &net.Dialer{Timeout: timeout}
	if ctrl := outboundBypassControl(cfg); ctrl != nil {
		d.Control = ctrl
	}
	return d
}

func outboundBypassControl(cfg outboundBypassConfig) func(network, address string, c syscall.RawConn) error {
	base := platformOutboundBypassControl(cfg)
	if base == nil {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		// Never bind/mark loopback/localhost. The app uses internal loopback URLs and
		// the DNS proxy should always remain reachable.
		if strings.HasPrefix(network, "unix") {
			return nil
		}
		host := address
		if h, _, err := net.SplitHostPort(address); err == nil && strings.TrimSpace(h) != "" {
			host = h
		}
		host = strings.TrimPrefix(host, "[")
		host = strings.TrimSuffix(host, "]")
		if strings.EqualFold(host, "localhost") {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return base(network, address, c)
	}
}
