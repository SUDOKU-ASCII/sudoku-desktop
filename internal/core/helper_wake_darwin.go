//go:build darwin

package core

import (
	"fmt"
	"strings"
	"time"
)

const (
	systemWakeGap            = 10 * time.Second
	darwinForcedRepairPeriod = 10 * time.Second
)

// wakeLoop re-applies the tunnel routes after sleep/wake or a network change. macOS drops
// every route tied to an interface when it goes away, which would otherwise leave the
// server host route leaking into the tunnel (a routing loop) until the user restarts.
func (h *helperServer) wakeLoop(s *tunSession) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	lastWall := time.Now().Round(0) // Round(0) strips the monotonic clock, which pauses in sleep
	var (
		wakePending bool
		lastCheck   time.Time
		lastRepair  time.Time
		lastSig     string
		lastErrAt   time.Time
	)
	for {
		select {
		case <-s.stopCh:
			return
		case <-h.ctx.Done():
			return
		case <-t.C:
		}
		wall := time.Now().Round(0)
		if wall.Sub(lastWall) > systemWakeGap {
			wakePending = true
		}
		lastWall = wall
		now := time.Now()
		if now.Sub(lastCheck) < 2*time.Second {
			continue
		}
		lastCheck = now

		s.mu.Lock()
		rc := s.routes
		var prevIf, prevGW string
		var servers []string
		if rc != nil {
			prevIf, prevGW, servers = rc.DefaultInterface, rc.DefaultGateway, append([]string(nil), rc.ServerIPs...)
		}
		s.mu.Unlock()
		if rc == nil {
			wakePending = false
			continue
		}

		newIf, newGW := darwinPhysicalUplink(prevIf)
		if newIf == "" || newGW == "" {
			continue
		}
		reason := ""
		switch {
		case newIf != prevIf || newGW != prevGW:
			reason = "network changed"
		case now.Sub(lastRepair) < darwinForcedRepairPeriod:
			continue
		case wakePending:
			reason = "system woke from sleep"
		default:
			for _, ip := range servers {
				if darwinHostRouteLeaksIntoTun(ip) {
					reason = "server route lost"
					break
				}
			}
		}
		if reason == "" {
			continue
		}
		sig := newIf + "|" + newGW
		if sig == lastSig && !lastErrAt.IsZero() && now.Sub(lastErrAt) < 30*time.Second {
			continue
		}
		lastSig, lastRepair, wakePending = sig, now, false
		h.logf(fmt.Sprintf("darwin %s: %s/%s -> %s/%s; repairing tunnel routes", reason, prevIf, prevGW, newIf, newGW))
		if err := h.repairDarwin(s, newIf, newGW); err != nil {
			lastErrAt = time.Now()
			h.logf(fmt.Sprintf("route repair failed: %v", err))
		} else {
			lastErrAt = time.Time{}
			h.logf("route repair applied")
		}
	}
}

// darwinPhysicalUplink returns the physical default interface and its gateway ("" if unknown).
func darwinPhysicalUplink(prevIf string) (string, string) {
	info, _ := darwinPrimaryNetworkInfo()
	ifName, gw := strings.TrimSpace(info.Interface4), strings.TrimSpace(info.Router4)
	if ifName == "" || darwinIsTunLikeInterface(ifName) {
		if n, _ := darwinResolveOutboundBypassInterface(900 * time.Millisecond); n != "" && !darwinIsTunLikeInterface(n) {
			ifName = strings.TrimSpace(n)
		} else if prevIf != "" && !darwinIsTunLikeInterface(prevIf) {
			ifName = prevIf
		} else {
			return "", ""
		}
	}
	if gw == "" {
		if g, _ := darwinDHCPRouterForInterface(ifName); g != "" {
			gw = strings.TrimSpace(g)
		}
	}
	return ifName, gw
}

func (h *helperServer) repairDarwin(s *tunSession, newIf, newGW string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sess != s {
		return nil
	}
	s.mu.Lock()
	rc := s.routes
	s.mu.Unlock()
	if rc == nil {
		return nil
	}
	tun := s.tun
	cmds := make([]string, 0, 12)
	for _, ip := range rc.ServerIPs {
		cmds = append(cmds, shellJoin("route", "-n", "add", "-host", ip, newGW)+" || "+shellJoin("route", "-n", "change", "-host", ip, newGW)+" || true")
	}
	cmds = append(cmds, darwinAddTunnelRouteCommands(tun.InterfaceName)...)
	cmds = append(cmds, darwinDeletePhysicalBypassRouteCommands(rc.DefaultInterface, rc.DefaultGateway)...)
	cmds = append(cmds, darwinAddPhysicalBypassRouteCommands(newIf, newGW)...)
	if rc.PFAnchor != "" {
		if c := darwinBuildPFSetCmd(rc.PFAnchor, tun.InterfaceName, tun.BlockQUIC, rc.DNSProxyRedirectPort); strings.TrimSpace(c) != "" {
			cmds = append(cmds, c+" || true")
		}
	}

	newSvc, snap := "", darwinDNSSnapshot{}
	if rc.DNSOverrideAddress != "" {
		if svc, err := darwinNetworkServiceForDevice(newIf); err == nil && strings.TrimSpace(svc) != "" {
			newSvc = strings.TrimSpace(svc)
			known := false
			for _, e := range rc.DarwinDNSSnapshots {
				if strings.EqualFold(e.Service, newSvc) {
					known = true
				}
			}
			if !known {
				servers, auto, _ := darwinGetDNSServers(newSvc)
				snap = darwinDNSSnapshot{Service: newSvc, Servers: servers, WasAutomatic: auto}
			}
			cmds = append(cmds,
				shellJoin("networksetup", "-setdnsservers", newSvc, rc.DNSOverrideAddress)+" || true",
				"dscacheutil -flushcache >/dev/null 2>&1 || true; killall -HUP mDNSResponder >/dev/null 2>&1 || true")
		}
	}
	if err := runShellBatch(h.logf, cmds...); err != nil {
		return err
	}
	s.mu.Lock()
	rc.DefaultInterface, rc.DefaultGateway = newIf, newGW
	if newSvc != "" {
		rc.DNSService = newSvc
		if snap.Service != "" {
			rc.DarwinDNSSnapshots = append(rc.DarwinDNSSnapshots, snap)
		}
	}
	s.mu.Unlock()
	h.saveState(s)
	return nil
}
