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
	src := strings.TrimSpace(cfg.DarwinSourceIP)
	var src4 *[4]byte
	var src6 *[16]byte
	if src != "" {
		if ip := net.ParseIP(src); ip != nil && !ip.IsLoopback() {
			if ip4 := ip.To4(); ip4 != nil {
				var b [4]byte
				copy(b[:], ip4)
				src4 = &b
			} else if ip16 := ip.To16(); ip16 != nil {
				var b [16]byte
				copy(b[:], ip16)
				src6 = &b
			}
		}
	}

	if name == "" && src4 == nil && src6 == nil {
		return nil
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
			isV6 := strings.HasSuffix(network, "6")
			if !isV6 {
				host := address
				if h, _, err := net.SplitHostPort(address); err == nil && strings.TrimSpace(h) != "" {
					host = h
				}
				host = strings.TrimPrefix(host, "[")
				host = strings.TrimSuffix(host, "]")
				if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
					isV6 = true
				} else if strings.Count(host, ":") > 1 {
					isV6 = true
				}
			}

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
