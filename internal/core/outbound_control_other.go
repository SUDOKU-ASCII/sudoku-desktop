//go:build !darwin && !linux && !windows

package core

import "syscall"

func platformOutboundBypassControl(cfg outboundBypassConfig) func(network, address string, c syscall.RawConn) error {
	return nil
}
