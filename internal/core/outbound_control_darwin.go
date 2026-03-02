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
			// Prefer best-effort source-IP binding when provided. Unlike IP_BOUND_IF, this tends to keep
			// working across different macOS routing states. However, treat failures as non-fatal so we
			// don't break IPv6 or dual-stack sockets on some networks.
			if src4 != nil || src6 != nil {
				isV6 := false
				if sa, gerr := unix.Getsockname(fdInt); gerr == nil {
					switch sa.(type) {
					case *unix.SockaddrInet6:
						isV6 = true
					case *unix.SockaddrInet4:
						isV6 = false
					}
				} else {
					// Heuristic fallback for non-literal targets.
					isV6 = strings.HasSuffix(network, "6") || strings.Contains(strings.ToLower(address), ":")
				}
				if !isV6 && src4 != nil {
					_ = unix.Bind(fdInt, &unix.SockaddrInet4{Addr: *src4})
				} else if isV6 && src6 != nil {
					_ = unix.Bind(fdInt, &unix.SockaddrInet6{Addr: *src6})
				}
			}

			if ifIndex > 0 {
				// Best-effort dual-stack interface binding.
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
			}
			inner = nil
		}); err != nil {
			return err
		}
		return inner
	}
}
