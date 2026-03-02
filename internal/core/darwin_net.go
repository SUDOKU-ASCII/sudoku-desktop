//go:build darwin

package core

import (
	"net"
	"net/netip"
	"strings"
	"time"
)

func darwinListTunInterfaces() map[string]struct{} {
	out := map[string]struct{}{}
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, it := range ifs {
		name := strings.TrimSpace(it.Name)
		if strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "tun") {
			out[name] = struct{}{}
		}
	}
	return out
}

func darwinWaitNewTunInterface(before map[string]struct{}, timeout time.Duration) string {
	if before == nil {
		before = map[string]struct{}{}
	}
	deadline := time.Now().Add(timeout)
	for {
		after := darwinListTunInterfaces()
		for name := range after {
			if _, ok := before[name]; !ok {
				return name
			}
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func darwinFindTunInterfaceByIPv4(ip string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil || !addr.Is4() {
		return ""
	}
	ifs, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, it := range ifs {
		name := strings.TrimSpace(it.Name)
		if !(strings.HasPrefix(name, "utun") || strings.HasPrefix(name, "tun")) {
			continue
		}
		addrs, err := it.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			if prefix.Addr() == addr {
				return name
			}
		}
	}
	return ""
}
