//go:build !darwin && !windows && !linux

package core

import (
	"fmt"
	"runtime"
	"syscall"
)

func setLaunchAtLogin(enabled bool) error {
	_ = enabled
	return fmt.Errorf("launch at login is not supported on %s", runtime.GOOS)
}

func platformApplySystemProxy(_ systemProxyConfig) (func() error, error) {
	return nil, nil
}

func platformOutboundBypassControl(_ outboundBypassConfig) func(network, address string, c syscall.RawConn) error {
	return nil
}
