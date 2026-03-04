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
	src4, src6 := parseOutboundSourceIPs(cfg.DarwinSourceIP)

	if name == "" {
		if src4 == nil && src6 == nil {
			return nil
		}
	}
	ifIndex := 0
	if name != "" {
		ifi, err := net.InterfaceByName(name)
		if err == nil && ifi != nil && ifi.Index > 0 {
			ifIndex = ifi.Index
		}
	}

	return func(network, address string, c syscall.RawConn) error {
		var inner error
		if err := c.Control(func(fd uintptr) {
			fdInt := int(fd)
			isV6 := networkLooksIPv6(network, address)

			// Prefer binding to the physical interface when available. This avoids "can't assign requested address"
			// issues that can happen when binding a specific source IP on some macOS networks.
			if ifIndex > 0 {
				var errBound error
				if isV6 {
					errBound = unix.SetsockoptInt(fdInt, unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, ifIndex)
				} else {
					errBound = unix.SetsockoptInt(fdInt, unix.IPPROTO_IP, unix.IP_BOUND_IF, ifIndex)
				}
				if errBound == nil {
					inner = nil
					return
				}
			}

			// Fallback: bind to the physical source IP when provided.
			if !isV6 && src4 != nil {
				if berr := unix.Bind(fdInt, &unix.SockaddrInet4{Addr: *src4}); berr != nil {
					inner = berr
					return
				}
				inner = nil
				return
			}
			if isV6 && src6 != nil {
				if berr := unix.Bind(fdInt, &unix.SockaddrInet6{Addr: *src6}); berr != nil {
					inner = berr
					return
				}
				inner = nil
				return
			}

			inner = nil
		}); err != nil {
			return err
		}
		return inner
	}
}
