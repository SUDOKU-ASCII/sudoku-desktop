//go:build sudoku_patch

package app

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/saba-futai/sudoku/pkg/dnsutil"
)

// This file is injected into the upstream sudoku core at build time.
//
// Goal: make DIRECT dialing resilient when the system resolver returns FakeIP
// (OpenClash fakeip, HEV MapDNS fake ip, etc.). We resolve via dnsutil.ResolveWithCache,
// which is also patched to prefer DoH with bootstrap IPs.

func init() {
	// Override the package variable declared in client_target.go.
	directDial = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			if host, port, err := net.SplitHostPort(addr); err == nil && port != "" {
				hostClean := strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
				if net.ParseIP(hostClean) == nil {
					resolveTimeout := timeout
					if resolveTimeout > 2*time.Second {
						resolveTimeout = 2 * time.Second
					}
					ctx, cancel := context.WithTimeout(context.Background(), resolveTimeout)
					if resolved, rerr := dnsutil.ResolveWithCache(ctx, addr); rerr == nil && strings.TrimSpace(resolved) != "" {
						addr = resolved
					}
					cancel()
				}
			}
		}
		return dnsutil.OutboundDialer(timeout).Dial(network, addr)
	}
}
