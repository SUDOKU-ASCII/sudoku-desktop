//go:build linux

package core

import (
	"net"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func platformOutboundBypassControl(cfg outboundBypassConfig) func(network, address string, c syscall.RawConn) error {
	mark := cfg.LinuxMark
	src := strings.TrimSpace(cfg.LinuxSourceIP)

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

	if mark <= 0 && src4 == nil && src6 == nil {
		return nil
	}

	return func(network string, address string, c syscall.RawConn) error {
		var inner error
		if err := c.Control(func(fd uintptr) {
			fdInt := int(fd)
			if src4 != nil && !strings.HasSuffix(network, "6") {
				if berr := unix.Bind(fdInt, &unix.SockaddrInet4{Addr: *src4}); berr != nil {
					inner = berr
					return
				}
			} else if src6 != nil {
				if berr := unix.Bind(fdInt, &unix.SockaddrInet6{Addr: *src6}); berr != nil {
					inner = berr
					return
				}
			}

			if mark > 0 {
				merr := unix.SetsockoptInt(fdInt, unix.SOL_SOCKET, unix.SO_MARK, mark)
				if merr == unix.EPERM || merr == unix.EACCES {
					merr = nil
				}
				if inner == nil {
					inner = merr
				}
			}
		}); err != nil {
			return err
		}
		return inner
	}
}
