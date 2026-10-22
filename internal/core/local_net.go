package core

import (
	"runtime"
	"strings"
)

const localLoopbackIPv4 = "127.0.0.1"

func localDNSProxyListenPort() int {
	if runtime.GOOS == "windows" {
		return 53
	}
	return 1053
}

func localDNSProxyRedirectPort(systemDNSAddress string) int {
	if strings.TrimSpace(systemDNSAddress) != localLoopbackIPv4 {
		return 0
	}
	port := localDNSProxyListenPort()
	if port == 53 {
		return 0
	}
	return port
}

// localDNSProxyActive reports whether the system resolver points at our own split-DNS proxy
// (the 127.0.0.1 address) rather than straight at the tunnel's MapDNS.
func localDNSProxyActive(tun TunSettings) bool {
	return tun.MapDNSEnabled && strings.TrimSpace(tun.MapDNSAddress) == localLoopbackIPv4
}
