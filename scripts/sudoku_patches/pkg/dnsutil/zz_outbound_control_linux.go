//go:build sudoku_patch && linux

package dnsutil

import (
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const envOutboundMark = "SUDOKU_OUTBOUND_MARK" // SO_MARK value used to bypass TUN routing policy
const envOutboundSrcIP = "SUDOKU_OUTBOUND_SRC_IP"

func platformOutboundControl() func(network, address string, c syscall.RawConn) error {
	raw := strings.TrimSpace(os.Getenv(envOutboundMark))
	mark := 0
	if raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			mark = v
		}
	}
	src := strings.TrimSpace(os.Getenv(envOutboundSrcIP))
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
