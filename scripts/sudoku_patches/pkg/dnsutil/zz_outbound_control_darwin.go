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
const envOutboundSrcIP = "SUDOKU_OUTBOUND_SRC_IP"

var (
	darwinOutboundOnce sync.Once
	darwinOutboundIf   int
	darwinOutboundSrc4 *[4]byte
	darwinOutboundSrc6 *[16]byte
)

func darwinOutboundInterfaceIndex() int {
	darwinOutboundOnce.Do(func() {
		src := strings.TrimSpace(os.Getenv(envOutboundSrcIP))
		if src != "" {
			if ip := net.ParseIP(src); ip != nil && !ip.IsLoopback() {
				if ip4 := ip.To4(); ip4 != nil {
					var b [4]byte
					copy(b[:], ip4)
					darwinOutboundSrc4 = &b
				} else if ip16 := ip.To16(); ip16 != nil {
					var b [16]byte
					copy(b[:], ip16)
					darwinOutboundSrc6 = &b
				}
			}
		}

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
	src4 := darwinOutboundSrc4
	src6 := darwinOutboundSrc6
	if ifIndex <= 0 && src4 == nil && src6 == nil {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		var inner error
		if err := c.Control(func(fd uintptr) {
			fdInt := int(fd)
			isV6 := strings.HasSuffix(network, "6") || strings.Contains(strings.ToLower(address), ":")

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
