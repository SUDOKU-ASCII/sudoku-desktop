//go:build darwin

package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"
)

func darwinIsTunLikeInterface(name string) bool {
	name = strings.TrimSpace(name)
	return strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "tun")
}

func darwinPickPhysicalDefaultRouteIPv4(routes []darwinNetstatRoute) (gateway string, iface string) {
	for _, r := range routes {
		if r.Destination != "default" {
			continue
		}
		if darwinIsTunLikeInterface(r.Netif) {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(r.Gateway))
		if ip == nil || ip.To4() == nil {
			continue
		}
		return strings.TrimSpace(r.Gateway), strings.TrimSpace(r.Netif)
	}
	return "", ""
}

func darwinPickPhysicalDefaultInterface(routes []darwinNetstatRoute) string {
	for _, r := range routes {
		if r.Destination != "default" {
			continue
		}
		if darwinIsTunLikeInterface(r.Netif) {
			continue
		}
		if strings.TrimSpace(r.Netif) == "" {
			continue
		}
		return strings.TrimSpace(r.Netif)
	}
	return ""
}

func darwinHasUnscopedDefaultRouteIPv4(routes []darwinNetstatRoute, excludedIf string) bool {
	excludedIf = strings.TrimSpace(excludedIf)
	for _, r := range routes {
		if r.Destination != "default" {
			continue
		}
		if strings.TrimSpace(r.Netif) == "" {
			continue
		}
		if excludedIf != "" && strings.EqualFold(strings.TrimSpace(r.Netif), excludedIf) {
			continue
		}
		// I = RTF_IFSCOPE. A scoped default route alone is not sufficient to restore normal routing;
		// `route -n get default` can still fail ("not in table") and the machine appears offline.
		if strings.Contains(r.Flags, "I") {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(r.Gateway))
		if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		return true
	}
	return false
}

func darwinHasDefaultRouteNotOnInterface(routes []darwinNetstatRoute, excludedIf string) bool {
	excludedIf = strings.TrimSpace(excludedIf)
	for _, r := range routes {
		if r.Destination != "default" {
			continue
		}
		if strings.TrimSpace(r.Netif) == "" {
			continue
		}
		if excludedIf != "" && strings.EqualFold(strings.TrimSpace(r.Netif), excludedIf) {
			continue
		}
		return true
	}
	return false
}

func darwinHasDefaultRouteOnInterface(routes []darwinNetstatRoute, ifName string) bool {
	ifName = strings.TrimSpace(ifName)
	if ifName == "" {
		return false
	}
	for _, r := range routes {
		if r.Destination != "default" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(r.Netif), ifName) {
			return true
		}
	}
	return false
}

func darwinHasDefaultRouteOnTunLikeInterface(routes []darwinNetstatRoute) bool {
	for _, r := range routes {
		if r.Destination != "default" {
			continue
		}
		if darwinIsTunLikeInterface(r.Netif) {
			return true
		}
	}
	return false
}

func darwinWaitDefaultRouteNotOnTun(tunIf string, timeout time.Duration) error {
	tunIf = strings.TrimSpace(tunIf)
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		routes, err := darwinNetstatRoutesIPv4()
		if err != nil {
			lastErr = err
		} else if tunIf != "" {
			// We consider restore successful only when:
			// - there is at least one *unscoped* default route not on tunIf (so the machine isn't left offline)
			// - there is no default route on tunIf
			if darwinHasUnscopedDefaultRouteIPv4(routes, tunIf) && !darwinHasDefaultRouteOnInterface(routes, tunIf) {
				// Extra safety: route(8) must be able to resolve the global default route.
				// A scoped default route alone can still make `route -n get default` fail ("not in table").
				_, _, rerr := darwinDefaultRoute()
				if rerr == nil {
					return nil
				}
				lastErr = fmt.Errorf("route get default failed: %w", rerr)
			} else if darwinHasDefaultRouteOnInterface(routes, tunIf) {
				lastErr = fmt.Errorf("default route still on %s", tunIf)
			} else {
				lastErr = errors.New("unscoped default route not found")
			}
		} else {
			// Fall back to "any utun/tun" only when we don't know the specific tunnel interface.
			if darwinHasUnscopedDefaultRouteIPv4(routes, "") && !darwinHasDefaultRouteOnTunLikeInterface(routes) {
				_, _, rerr := darwinDefaultRoute()
				if rerr == nil {
					return nil
				}
				lastErr = fmt.Errorf("route get default failed: %w", rerr)
			} else if darwinHasDefaultRouteOnTunLikeInterface(routes) {
				lastErr = errors.New("default route still on a tunnel interface")
			} else {
				lastErr = errors.New("unscoped default route not found")
			}
		}

		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("restore default route validation failed: %w", lastErr)
			}
			return errors.New("restore default route validation failed")
		}
		time.Sleep(160 * time.Millisecond)
	}
}

func darwinDHCPRouterForInterface(ifName string) (string, error) {
	ifName = strings.TrimSpace(ifName)
	if ifName == "" {
		return "", errors.New("empty interface")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ipconfig", "getoption", ifName, "router")
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", errors.New("ipconfig getoption router: timeout")
	}
	clean := strings.TrimSpace(string(out))
	if err != nil {
		if clean != "" {
			return "", fmt.Errorf("ipconfig getoption %s router: %w: %s", ifName, err, clean)
		}
		return "", fmt.Errorf("ipconfig getoption %s router: %w", ifName, err)
	}
	// ipconfig can output multiple lines; pick the first IPv4.
	s := bufio.NewScanner(strings.NewReader(clean))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		ip := net.ParseIP(line)
		if ip != nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
			return ip.String(), nil
		}
	}
	return "", errors.New("router not found")
}

func darwinResolveOutboundBypassInterface(timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ifName, err := darwinResolveOutboundBypassInterfaceOnce()
		if strings.TrimSpace(ifName) != "" {
			return strings.TrimSpace(ifName), nil
		}
		if err != nil {
			lastErr = err
		}
		if time.Now().After(deadline) {
			if lastErr != nil {
				return "", lastErr
			}
			return "", errors.New("outbound bypass interface not found")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func darwinResolveOutboundBypassInterfaceOnce() (string, error) {
	if info, err := darwinPrimaryNetworkInfo(); err == nil {
		ifName := strings.TrimSpace(info.Interface4)
		if ifName != "" && !darwinIsTunLikeInterface(ifName) {
			return ifName, nil
		}
	}
	if routes, err := darwinNetstatRoutesIPv4(); err == nil {
		if ifName := darwinPickPhysicalDefaultInterface(routes); ifName != "" {
			return ifName, nil
		}
	}
	_, ifName, err := darwinDefaultRoute()
	if err != nil {
		return "", err
	}
	ifName = strings.TrimSpace(ifName)
	if ifName != "" && !darwinIsTunLikeInterface(ifName) {
		return ifName, nil
	}
	return "", nil
}

func darwinResolveRestoreGatewayIPv4(ctx *routeContext, tunIf string) (string, error) {
	restoreGW := ""
	restoreIf := ""
	if ctx != nil {
		restoreGW = strings.TrimSpace(ctx.DefaultGateway)
		restoreIf = strings.TrimSpace(ctx.DefaultInterface)
	}

	if info, err := darwinPrimaryNetworkInfo(); err == nil {
		if r := strings.TrimSpace(info.Router4); r != "" {
			restoreGW = r
		}
		if i := strings.TrimSpace(info.Interface4); i != "" && !darwinIsTunLikeInterface(i) {
			restoreIf = i
		}
	}

	// Ensure we have a physical interface to query DHCP from.
	if strings.TrimSpace(restoreIf) == "" {
		if routes, err := darwinNetstatRoutesIPv4(); err == nil {
			if ifName := strings.TrimSpace(darwinPickPhysicalDefaultInterface(routes)); ifName != "" {
				restoreIf = ifName
			}
		}
	}
	if strings.TrimSpace(restoreIf) == "" {
		if ifName, _ := darwinResolveOutboundBypassInterface(1200 * time.Millisecond); strings.TrimSpace(ifName) != "" && !darwinIsTunLikeInterface(ifName) {
			restoreIf = strings.TrimSpace(ifName)
		}
	}

	// Prefer the current DHCP router for the physical interface if available. This avoids restoring
	// a stale gateway after the user switched Wi‑Fi while TUN was active.
	if strings.TrimSpace(restoreIf) != "" {
		if gw, err := darwinDHCPRouterForInterface(restoreIf); err == nil && strings.TrimSpace(gw) != "" {
			restoreGW = strings.TrimSpace(gw)
		}
	}

	// If scutil router is empty while TUN is active, netstat often still contains a scoped default
	// route for the physical interface (added during setup).
	if strings.TrimSpace(restoreGW) == "" {
		if routes, err := darwinNetstatRoutesIPv4(); err == nil {
			if gw, ifName := darwinPickPhysicalDefaultRouteIPv4(routes); gw != "" {
				restoreGW = gw
				if restoreIf == "" {
					restoreIf = ifName
				}
			}
		}
	}

	ip := net.ParseIP(strings.TrimSpace(restoreGW))
	if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
		restoreGW = ""
	}
	if restoreGW == "" {
		if tunIf != "" {
			return "", fmt.Errorf("restore default route: gateway not found (tunIf=%s)", tunIf)
		}
		return "", errors.New("restore default route: gateway not found")
	}
	return restoreGW, nil
}
