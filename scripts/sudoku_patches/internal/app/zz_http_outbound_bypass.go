//go:build sudoku_patch

package app

import (
	"net/http"
	"time"

	"github.com/saba-futai/sudoku/pkg/dnsutil"
)

// This file is injected into the upstream sudoku core at build time.
//
// When the desktop app switches the system default route to TUN, the core's own
// HTTP requests (PAC/rule downloads, geodata refreshes, etc.) must still egress
// via the physical interface, otherwise the core may self-loop into the tunnel.

func init() {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok || tr == nil {
		return
	}
	clone := tr.Clone()
	d := dnsutil.OutboundDialer(30 * time.Second)
	clone.DialContext = d.DialContext
	http.DefaultTransport = clone
}
