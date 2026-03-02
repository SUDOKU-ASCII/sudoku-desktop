//go:build !darwin && !linux && !windows

package core

import "syscall"

func platformOutboundBypassControl(_ outboundBypassConfig) func(network, address string, c syscall.RawConn) error {
	return nil
}
