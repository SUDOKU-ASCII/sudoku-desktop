//go:build sudoku_patch

package app

import (
	"context"
	"net"
	"time"

	"github.com/saba-futai/sudoku/pkg/dnsutil"
)

func init() {
	// Ensure all DIRECT dials from the core bypass the system TUN default route, otherwise
	// "DIRECT" traffic will self-loop into the tunnel and effectively break split routing.
	directDial = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		d := dnsutil.OutboundDialer(timeout)
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return d.DialContext(ctx, network, addr)
	}
}
