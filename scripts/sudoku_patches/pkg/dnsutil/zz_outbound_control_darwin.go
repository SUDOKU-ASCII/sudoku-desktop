//go:build sudoku_patch && darwin

package dnsutil

import (
	"net"
	"os"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

const envOutboundIface = "SUDOKU_OUTBOUND_IFACE" // e.g. "en0"

var (
	darwinOutboundOnce sync.Once
	darwinOutboundIf   int
)

func darwinOutboundInterfaceIndex() int {
	darwinOutboundOnce.Do(func() {
		name := strings.TrimSpace(os.Getenv(envOutboundIface))
		if name == "" {
			return
		}
		ifi, err := net.InterfaceByName(name)
		if err != nil || ifi == nil || ifi.Index <= 0 {
			return
		}
		darwinOutboundIf = ifi.Index
	})
	return darwinOutboundIf
}

func platformOutboundControl() func(network, address string, c syscall.RawConn) error {
	ifIndex := darwinOutboundInterfaceIndex()
	if ifIndex <= 0 {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		var inner error
		if err := c.Control(func(fd uintptr) {
			fdInt := int(fd)
			// Apply both options best-effort; only one will match the actual socket family.
			err4 := unix.SetsockoptInt(fdInt, unix.IPPROTO_IP, unix.IP_BOUND_IF, ifIndex)
			err6 := unix.SetsockoptInt(fdInt, unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, ifIndex)
			if err4 != nil && err6 != nil {
				if strings.HasSuffix(network, "6") || strings.Contains(strings.ToLower(address), ":") {
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
