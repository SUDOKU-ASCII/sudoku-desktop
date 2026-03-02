//go:build sudoku_patch

package dnsutil

import (
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

// This file is injected into the upstream sudoku core at build time.
//
// It provides a best-effort "outbound bypass" dialer used by the core itself
// (rule downloads, DoH, DIRECT dials, etc.) so that once the system default
// route is switched to TUN, the core's own sockets can still egress via the
// physical interface (avoiding self-loop).

const (
	envOutboundDisable = "SUDOKU_OUTBOUND_DISABLE" // if "1", do not apply any outbound bypass
)

var (
	outboundOnce    sync.Once
	outboundControl func(network, address string, c syscall.RawConn) error
)

// OutboundDialer returns a net.Dialer that applies OS-specific bypass options when configured.
// When bypass isn't supported or configured, it returns a plain net.Dialer.
func OutboundDialer(timeout time.Duration) *net.Dialer {
	d := &net.Dialer{Timeout: timeout}
	if ctrl := outboundDialerControl(); ctrl != nil {
		d.Control = ctrl
	}
	return d
}

func outboundDialerControl() func(network, address string, c syscall.RawConn) error {
	outboundOnce.Do(func() {
		if strings.TrimSpace(os.Getenv(envOutboundDisable)) == "1" {
			outboundControl = nil
			return
		}
		base := platformOutboundControl()
		if base == nil {
			outboundControl = nil
			return
		}
		outboundControl = func(network, address string, c syscall.RawConn) error {
			// Never bind/mark loopback/localhost. The core uses local URLs (PAC server) and
			// internal connections that must remain routable regardless of TUN state.
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
	})
	return outboundControl
}
