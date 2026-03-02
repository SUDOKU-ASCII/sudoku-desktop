//go:build darwin

package core

import (
	"net"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func platformOutboundBypassControl(cfg outboundBypassConfig) func(network, address string, c syscall.RawConn) error {
	name := strings.TrimSpace(cfg.DarwinInterface)
	if name == "" {
		return nil
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil || ifi == nil || ifi.Index <= 0 {
		return nil
	}
	ifIndex := ifi.Index

	return func(network, address string, c syscall.RawConn) error {
		var inner error
		if err := c.Control(func(fd uintptr) {
			fdInt := int(fd)
			// Best-effort dual-stack binding.
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
