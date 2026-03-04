package core

import (
	"bufio"
	"net"
	"strings"
)

type darwinPrimaryRouteInfo struct {
	Interface4 string
	Router4    string
	Interface6 string
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
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		switch {
		case strings.HasPrefix(line, "IPv4 network interface:"):
			info.Interface4 = strings.TrimSpace(strings.TrimPrefix(line, "IPv4 network interface:"))
		case strings.HasPrefix(line, "IPv4 router:"):
			info.Router4 = strings.TrimSpace(strings.TrimPrefix(line, "IPv4 router:"))
		case strings.HasPrefix(line, "IPv6 network interface:"):
			info.Interface6 = strings.TrimSpace(strings.TrimPrefix(line, "IPv6 network interface:"))
		case strings.HasPrefix(line, "IPv6 router:"):
			info.Router6 = strings.TrimSpace(strings.TrimPrefix(line, "IPv6 router:"))
		}
	}

	info.Interface4 = strings.TrimSpace(info.Interface4)
	info.Router4 = strings.TrimSpace(stripIPZone(info.Router4))
	info.Interface6 = strings.TrimSpace(info.Interface6)
	info.Router6 = strings.TrimSpace(stripIPZone(info.Router6))

	if ip := net.ParseIP(info.Router4); ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
		info.Router4 = ""
	}
	if ip := net.ParseIP(info.Router6); ip == nil || ip.To4() != nil || ip.IsLoopback() || ip.IsUnspecified() {
		info.Router6 = ""
	}
	return info
}
