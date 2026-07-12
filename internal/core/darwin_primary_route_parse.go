package core

import (
	"bufio"
	"net"
	"strings"
)

type darwinPrimaryRouteInfo struct {
	Interface4 string
	Address4   string
	Router4    string
	Interface6 string
	Address6   string
	Router6    string
}

func stripIPZone(s string) string {
	if i := strings.IndexByte(s, '%'); i >= 0 {
		return s[:i]
	}
	return s
}

func parseDarwinScutilNWIOutput(out string) darwinPrimaryRouteInfo {
	var info darwinPrimaryRouteInfo
	addressFamily := 0
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		switch {
		case line == "IPv4 network interface information":
			addressFamily = 4
		case line == "IPv6 network interface information":
			addressFamily = 6
		case strings.HasPrefix(line, "Network interfaces:"):
			addressFamily = 0
		case strings.HasPrefix(line, "IPv4 network interface:"):
			info.Interface4 = strings.TrimSpace(strings.TrimPrefix(line, "IPv4 network interface:"))
		case strings.HasPrefix(line, "IPv4 primary address:"):
			info.Address4 = strings.TrimSpace(strings.TrimPrefix(line, "IPv4 primary address:"))
		case strings.HasPrefix(line, "IPv4 router:"):
			info.Router4 = strings.TrimSpace(strings.TrimPrefix(line, "IPv4 router:"))
		case strings.HasPrefix(line, "IPv6 network interface:"):
			info.Interface6 = strings.TrimSpace(strings.TrimPrefix(line, "IPv6 network interface:"))
		case strings.HasPrefix(line, "IPv6 primary address:"):
			info.Address6 = strings.TrimSpace(strings.TrimPrefix(line, "IPv6 primary address:"))
		case strings.HasPrefix(line, "IPv6 router:"):
			info.Router6 = strings.TrimSpace(strings.TrimPrefix(line, "IPv6 router:"))
		case strings.Contains(line, " : flags"):
			ifName := strings.TrimSpace(strings.SplitN(line, " : flags", 2)[0])
			if addressFamily == 4 && info.Interface4 == "" {
				info.Interface4 = ifName
			}
			if addressFamily == 6 && info.Interface6 == "" {
				info.Interface6 = ifName
			}
		case strings.HasPrefix(line, "address"):
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			address := strings.TrimSpace(parts[1])
			if addressFamily == 4 && info.Address4 == "" {
				info.Address4 = address
			}
			if addressFamily == 6 && info.Address6 == "" {
				info.Address6 = address
			}
		}
	}

	info.Interface4 = strings.TrimSpace(info.Interface4)
	info.Address4 = strings.TrimSpace(stripIPZone(info.Address4))
	info.Router4 = strings.TrimSpace(stripIPZone(info.Router4))
	info.Interface6 = strings.TrimSpace(info.Interface6)
	info.Address6 = strings.TrimSpace(stripIPZone(info.Address6))
	info.Router6 = strings.TrimSpace(stripIPZone(info.Router6))

	if ip := net.ParseIP(info.Address4); ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
		info.Address4 = ""
	}
	if ip := net.ParseIP(info.Router4); ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
		info.Router4 = ""
	}
	if ip := net.ParseIP(info.Address6); ip == nil || ip.To4() != nil || ip.IsLoopback() || ip.IsUnspecified() {
		info.Address6 = ""
	}
	if ip := net.ParseIP(info.Router6); ip == nil || ip.To4() != nil || ip.IsLoopback() || ip.IsUnspecified() {
		info.Router6 = ""
	}
	return info
}
