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
