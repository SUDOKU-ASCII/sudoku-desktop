//go:build sudoku_patch && !linux && !darwin && !windows

package dnsutil

import "syscall"

func platformOutboundControl() func(network, address string, c syscall.RawConn) error {
	return nil
}
