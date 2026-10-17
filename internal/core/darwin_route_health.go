package core

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"time"
)

func parseRouteGetOutput(out string) (gateway string, iface string) {
	s := bufio.NewScanner(strings.NewReader(out))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if v, ok := strings.CutPrefix(line, "gateway:"); ok {
			gateway = strings.TrimSpace(v)
		}
		if v, ok := strings.CutPrefix(line, "interface:"); ok {
			iface = strings.TrimSpace(v)
		}
	}
	return gateway, iface
}

// darwinHostRouteLeaksIntoTun reports whether traffic to host would be routed
// through a tun interface (or no interface at all), i.e. the direct host route
// installed at TUN setup has been dropped by the system. Lookup failures are
// treated as "unknown" and never as a leak.
func darwinHostRouteLeaksIntoTun(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "route", "-n", "get", host).Output()
	if err != nil {
		return false
	}
	_, iface := parseRouteGetOutput(string(out))
	return iface == "" || darwinIsTunLikeInterface(iface)
}
