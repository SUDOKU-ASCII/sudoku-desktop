//go:build sudoku_patch && windows

package dnsutil

import (
	"encoding/binary"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const envOutboundIfIndex = "SUDOKU_OUTBOUND_IFINDEX" // default route interface index (IPv4)

const (
	// https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-ip-socket-options
	// https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-ipv6-socket-options
	ipUnicastIf   = 31
	ipv6UnicastIf = 31
)

func platformOutboundControl() func(network, address string, c syscall.RawConn) error {
	raw := strings.TrimSpace(os.Getenv(envOutboundIfIndex))
	if raw == "" {
		return nil
	}
	ifIndex, err := strconv.Atoi(raw)
	if err != nil || ifIndex <= 0 {
		return nil
	}

	return func(network, address string, c syscall.RawConn) error {
		var inner error
		if err := c.Control(func(fd uintptr) {
			handle := syscall.Handle(fd)
			// Best-effort dual-stack binding.
			err4 := bind4(handle, ifIndex)
			err6 := bind6(handle, ifIndex)
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

func bind4(handle syscall.Handle, ifaceIdx int) error {
	// IP_UNICAST_IF expects the interface index in network byte order (big-endian).
	var bytes [4]byte
	binary.BigEndian.PutUint32(bytes[:], uint32(ifaceIdx))
	idx := *(*uint32)(unsafe.Pointer(&bytes[0]))
	return syscall.SetsockoptInt(handle, syscall.IPPROTO_IP, ipUnicastIf, int(idx))
}

func bind6(handle syscall.Handle, ifaceIdx int) error {
	// IPV6_UNICAST_IF expects host byte order.
	return syscall.SetsockoptInt(handle, syscall.IPPROTO_IPV6, ipv6UnicastIf, ifaceIdx)
}
