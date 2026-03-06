//go:build windows

package core

import (
	"encoding/binary"
	"strings"
	"syscall"
	"unsafe"
)

const (
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
				if strings.HasSuffix(network, "6") || strings.Contains(address, ":") {
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
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], uint32(ifaceIdx))
	idx := *(*uint32)(unsafe.Pointer(&bytes[0]))
	return syscall.SetsockoptInt(handle, syscall.IPPROTO_IP, winIPUnicastIf, int(idx))
}

func winBind6(handle syscall.Handle, ifaceIdx int) error {
	return syscall.SetsockoptInt(handle, syscall.IPPROTO_IPV6, winIPV6UnicastIf, ifaceIdx)
}
