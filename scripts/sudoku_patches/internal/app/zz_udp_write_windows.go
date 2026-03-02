//go:build sudoku_patch && windows

package app

import (
	"encoding/binary"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
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

var (
	windowsUDPOnce   sync.Once
	windowsOutboundI int
)

func windowsUDPOutboundIfIndex() int {
	windowsUDPOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv(envOutboundIfIndex))
		if raw == "" {
			return
		}
		ifIndex, err := strconv.Atoi(raw)
		if err != nil || ifIndex <= 0 {
			return
		}
		windowsOutboundI = ifIndex
	})
	return windowsOutboundI
}

func platformUDPWriteToBypass(conn *net.UDPConn, payload []byte, addr *net.UDPAddr) (int, error) {
	if conn == nil {
		return 0, net.ErrClosed
	}
	if addr == nil {
		return 0, syscall.EINVAL
	}

	ifIndex := windowsUDPOutboundIfIndex()
	if ifIndex <= 0 {
		return conn.WriteToUDP(payload, addr)
	}

	raw, err := conn.SyscallConn()
	if err != nil {
		return conn.WriteToUDP(payload, addr)
	}

	set := func(v int) {
		_ = raw.Control(func(fd uintptr) {
			handle := syscall.Handle(fd)
			_ = bind4(handle, v)
			_ = bind6(handle, v)
		})
	}

	set(ifIndex)
	defer set(0)
	return conn.WriteToUDP(payload, addr)
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

