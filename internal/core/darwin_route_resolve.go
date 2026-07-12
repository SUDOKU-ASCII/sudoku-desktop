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

func darwinResolveOutboundBypass(timeout time.Duration) (ifName string, sourceIPv4 string, err error) {
	ifName, err = darwinResolveOutboundBypassInterface(timeout)
	if err != nil {
		return "", "", err
	}
	ifName = strings.TrimSpace(ifName)
	if ifName == "" {
		return "", "", errors.New("outbound bypass interface not found")
	}

	if info, infoErr := darwinPrimaryNetworkInfo(); infoErr == nil &&
		strings.EqualFold(strings.TrimSpace(info.Interface4), ifName) {
		if ip := net.ParseIP(strings.TrimSpace(info.Address4)); ip != nil && ip.To4() != nil {
			return ifName, ip.String(), nil
		}
	}

	iface, ifaceErr := net.InterfaceByName(ifName)
	if ifaceErr != nil {
		return ifName, "", nil
	}
	addrs, addrsErr := iface.Addrs()
	if addrsErr != nil {
		return ifName, "", nil
	}
	for _, addr := range addrs {
		raw := strings.TrimSpace(addr.String())
		if host, _, splitErr := net.ParseCIDR(raw); splitErr == nil {
			if ip4 := host.To4(); ip4 != nil && !host.IsLoopback() && !host.IsUnspecified() {
				return ifName, ip4.String(), nil
			}
		}
	}
	return ifName, "", nil
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
