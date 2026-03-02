//go:build sudoku_patch && linux

package app

import (
	"net"
	"os"
	"strings"
	"sync"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const envOutboundSrcIP = "SUDOKU_OUTBOUND_SRC_IP"

var (
	linuxUDPSrcOnce sync.Once
	linuxUDPSrc4    net.IP
	linuxUDPSrc6    net.IP
)

func linuxUDPOutboundSrcIPs() (net.IP, net.IP) {
	linuxUDPSrcOnce.Do(func() {
		raw := strings.TrimSpace(os.Getenv(envOutboundSrcIP))
		if raw == "" {
			return
		}
		ip := net.ParseIP(raw)
		if ip == nil || ip.IsLoopback() {
			return
		}
		if ip4 := ip.To4(); ip4 != nil {
			linuxUDPSrc4 = ip4
			return
		}
		if ip16 := ip.To16(); ip16 != nil {
			linuxUDPSrc6 = ip16
		}
	})
	return linuxUDPSrc4, linuxUDPSrc6
}

func platformUDPWriteToBypass(conn *net.UDPConn, payload []byte, addr *net.UDPAddr) (int, error) {
	if conn == nil {
		return 0, net.ErrClosed
	}
	if addr == nil || addr.IP == nil {
		return conn.WriteToUDP(payload, addr)
	}

	if ip4 := addr.IP.To4(); ip4 != nil {
		src4, _ := linuxUDPOutboundSrcIPs()
		if src4 == nil {
			return conn.WriteToUDP(payload, addr)
		}
		pc := ipv4.NewPacketConn(conn)
		_ = pc.SetControlMessage(ipv4.FlagSrc, true)
		return pc.WriteTo(payload, &ipv4.ControlMessage{Src: src4}, addr)
	}

	_, src6 := linuxUDPOutboundSrcIPs()
	if src6 == nil {
		return conn.WriteToUDP(payload, addr)
	}
	pc := ipv6.NewPacketConn(conn)
	_ = pc.SetControlMessage(ipv6.FlagSrc, true)
	return pc.WriteTo(payload, &ipv6.ControlMessage{Src: src6}, addr)
}

