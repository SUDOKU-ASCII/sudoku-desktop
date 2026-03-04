//go:build sudoku_patch && !(darwin || linux || windows)

package app

import "net"

func platformUDPWriteToBypass(conn *net.UDPConn, payload []byte, addr *net.UDPAddr) (int, error) {
	return conn.WriteToUDP(payload, addr)
}
