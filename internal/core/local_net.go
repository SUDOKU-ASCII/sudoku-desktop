package core

import "runtime"

const localLoopbackIPv4 = "127.0.0.1"

func localDNSProxyListenPort() int {
	if runtime.GOOS == "windows" {
		return 53
	}
	return 1053
}
