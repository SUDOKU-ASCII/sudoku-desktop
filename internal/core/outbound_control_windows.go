//go:build windows

package core

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

const (
	// https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-ip-socket-options
	// https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-ipv6-socket-options
	winIPUnicastIf   = 31
	winIPV6UnicastIf = 31
)

func platformOutboundBypassControl(cfg outboundBypassConfig) func(network, address string, c syscall.RawConn) error {
	ifIndex := cfg.WindowsIfIndex
	if ifIndex <= 0 {
		return nil
	}

	return func(network, address string, c syscall.RawConn) error {
		var inner error
		if err := c.Control(func(fd uintptr) {
			handle := syscall.Handle(fd)
			err4 := winBind4(handle, ifIndex)
			err6 := winBind6(handle, ifIndex)
			if err4 != nil && err6 != nil {
				if networkLooksIPv6(network, address) {
					inner = err6
				} else {
					inner = err4
				}
				return
			}
			inner = nil
		}); err != nil {
			return err
		}
		return inner
	}
}

func winBind4(handle syscall.Handle, ifaceIdx int) error {
	// IP_UNICAST_IF expects the interface index in network byte order (big-endian).
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], uint32(ifaceIdx))
	idx := *(*uint32)(unsafe.Pointer(&bytes[0]))
	return syscall.SetsockoptInt(handle, syscall.IPPROTO_IP, winIPUnicastIf, int(idx))
}

func winBind6(handle syscall.Handle, ifaceIdx int) error {
	// IPV6_UNICAST_IF expects host byte order.
	return syscall.SetsockoptInt(handle, syscall.IPPROTO_IPV6, winIPV6UnicastIf, ifaceIdx)
}
