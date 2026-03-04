//go:build linux

package core

import (
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

func platformOutboundBypassControl(cfg outboundBypassConfig) func(network, address string, c syscall.RawConn) error {
	mark := cfg.LinuxMark
	src4, src6 := parseOutboundSourceIPs(cfg.LinuxSourceIP)

	if mark <= 0 && src4 == nil && src6 == nil {
		return nil
	}

	return func(network string, address string, c syscall.RawConn) error {
		var inner error
		if err := c.Control(func(fd uintptr) {
			fdInt := int(fd)
			// Bind source IP best-effort (no privileges required) to escape TUN policy routing.
			if src4 != nil && !strings.HasSuffix(network, "6") {
				sa := &unix.SockaddrInet4{Addr: *src4}
				if berr := unix.Bind(fdInt, sa); berr != nil {
					inner = berr
					return
				}
			} else if src6 != nil {
				sa := &unix.SockaddrInet6{Addr: *src6}
				if berr := unix.Bind(fdInt, sa); berr != nil {
					inner = berr
					return
				}
			}

			if mark > 0 {
				merr := unix.SetsockoptInt(fdInt, unix.SOL_SOCKET, unix.SO_MARK, mark)
				// SO_MARK requires CAP_NET_ADMIN. Ignore EPERM/EACCES when unprivileged.
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
