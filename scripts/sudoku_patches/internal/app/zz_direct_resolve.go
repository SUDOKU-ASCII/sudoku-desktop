//go:build sudoku_patch

package app

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/saba-futai/sudoku/pkg/dnsutil"
)

var (
	directResolveBogus198 = mustParseCIDR("198.18.0.0/15")
	directResolveBogus100 = mustParseCIDR("100.64.0.0/10")
	directResolveServers  = []string{
		"223.5.5.5:53",
		"119.29.29.29:53",
	}
)

func init() {
	prevResolveWithCache := resolveWithCache
	resolveWithCache = func(ctx context.Context, resolver *dnsutil.Resolver, addr string) (string, error) {
		if resolved, ok := resolveDirectViaDomesticDNS(ctx, addr); ok {
			return resolved, nil
		}
		return prevResolveWithCache(ctx, resolver, addr)
	}
}

func resolveDirectViaDomesticDNS(ctx context.Context, addr string) (string, bool) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", false
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", false
	}
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if host == "" || port == "" {
		return "", false
	}
	if ip := net.ParseIP(host); ip != nil {
		return addr, true
	}
	if isLikelyLocalHostname(host) {
		return "", false
	}

	timeout := 1500 * time.Millisecond
	if ctx == nil {
		ctx = context.Background()
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	resolveCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for _, server := range directResolveServers {
		if ip := lookupDirectIPv4(resolveCtx, server, host, timeout); ip != nil {
			return net.JoinHostPort(ip.String(), port), true
		}
	}
	return "", false
}

func lookupDirectIPv4(ctx context.Context, server string, host string, timeout time.Duration) net.IP {
	dialer := dnsutil.OutboundDialer(timeout)
	for _, network := range []string{"udp4", "tcp4"} {
		r := &net.Resolver{
			PreferGo: true,
			Dial: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(dialCtx, network, server)
			},
		}
		ips, err := r.LookupIP(ctx, "ip4", host)
		if err != nil {
			continue
		}
		for _, ip := range ips {
			if usableDirectIPv4(ip) {
				return ip.To4()
			}
		}
	}
	return nil
}

func usableDirectIPv4(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil || ip4.IsLoopback() || ip4.IsUnspecified() {
		return false
	}
	if directResolveBogus198.Contains(ip4) || directResolveBogus100.Contains(ip4) {
		return false
	}
	return true
}

func mustParseCIDR(cidr string) *net.IPNet {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return ipNet
}

func isLikelyLocalHostname(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if !strings.Contains(host, ".") {
		return true
	}
	return strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".lan")
}
