package core

import (
	"net"
	"strings"
)

func parseOutboundSourceIPs(raw string) (src4 *[4]byte, src6 *[16]byte) {
	src := strings.TrimSpace(raw)
	if src == "" {
		return nil, nil
	}
	ip := net.ParseIP(src)
	if ip == nil || ip.IsLoopback() {
		return nil, nil
	}
	if ip4 := ip.To4(); ip4 != nil {
		var b [4]byte
		copy(b[:], ip4)
		return &b, nil
	}
	if ip16 := ip.To16(); ip16 != nil {
		var b [16]byte
		copy(b[:], ip16)
		return nil, &b
	}
	return nil, nil
}

func networkLooksIPv6(network, address string) bool {
	if strings.HasSuffix(network, "6") {
		return true
	}
	host := address
	if h, _, err := net.SplitHostPort(address); err == nil && strings.TrimSpace(h) != "" {
		host = h
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return true
	}
	return strings.Count(host, ":") > 1
}
