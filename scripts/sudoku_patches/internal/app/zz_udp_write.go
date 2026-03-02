//go:build sudoku_patch

package app

import (
	"net"
	"sync"
)

// This file is injected into the upstream sudoku core at build time.
//
// Problem: In TUN mode, the system default route is switched to the TUN interface.
// For SOCKS5 UDP ASSOCIATE, upstream reuses a single *net.UDPConn for both:
//   1) local UDP between HEV <-> core (loopback)
//   2) DIRECT UDP to the Internet
// When DIRECT UDP egresses via the TUN, it can self-loop and blackhole packets.
//
// Fix: serialize writes to the UDP socket and apply a best-effort outbound-bypass
// ONLY for DIRECT writes (platform-specific). Non-DIRECT writes keep the default
// routing so loopback delivery stays correct.

var udpWriteMu sync.Map // map[*net.UDPConn]*sync.Mutex

func udpWriteTo(conn *net.UDPConn, payload []byte, addr *net.UDPAddr, bypass bool) (int, error) {
	if conn == nil {
		return 0, net.ErrClosed
	}
	muAny, _ := udpWriteMu.LoadOrStore(conn, &sync.Mutex{})
	mu := muAny.(*sync.Mutex)

	mu.Lock()
	defer mu.Unlock()

	if !bypass {
		return conn.WriteToUDP(payload, addr)
	}
	return platformUDPWriteToBypass(conn, payload, addr)
}

