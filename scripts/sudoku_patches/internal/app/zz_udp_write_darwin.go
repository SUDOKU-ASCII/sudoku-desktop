//go:build sudoku_patch && darwin

package app

import (
	"net"
	"os"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const envOutboundIface = "SUDOKU_OUTBOUND_IFACE" // e.g. "en0"

var (
	darwinUDPOnce sync.Once
	darwinUDPIf   int
)

func darwinUDPOutboundInterfaceIndex() int {
	darwinUDPOnce.Do(func() {
		name := strings.TrimSpace(os.Getenv(envOutboundIface))
		if name == "" {
			return
		}
		ifi, err := net.InterfaceByName(name)
		if err != nil || ifi == nil || ifi.Index <= 0 {
			return
		}
		darwinUDPIf = ifi.Index
	})
	return darwinUDPIf
}

func platformUDPWriteToBypass(conn *net.UDPConn, payload []byte, addr *net.UDPAddr) (int, error) {
	if conn == nil {
		return 0, net.ErrClosed
	}
	if addr == nil {
		return 0, unix.EINVAL
	}

	ifIndex := darwinUDPOutboundInterfaceIndex()
	if ifIndex <= 0 {
		return conn.WriteToUDP(payload, addr)
	}

	raw, err := conn.SyscallConn()
	if err != nil {
		return conn.WriteToUDP(payload, addr)
	}

	set := func(v int) {
		_ = raw.Control(func(fd uintptr) {
			fdInt := int(fd)
			// Best-effort dual-stack binding.
			_ = unix.SetsockoptInt(fdInt, unix.IPPROTO_IP, unix.IP_BOUND_IF, v)
			_ = unix.SetsockoptInt(fdInt, unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, v)
		})
	}

	set(ifIndex)
	defer set(0)
	return conn.WriteToUDP(payload, addr)
}

