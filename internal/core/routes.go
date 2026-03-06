package core

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type routeContext struct {
	DefaultGateway        string
	DefaultInterface      string
	ServerIP              string
	TunIndex              int
	TunAlias              string
	DNSService            string
	DNSServers            []string
	DNSWasAutomatic       bool
	DNSOverrideAddress    string
	DNSProxyRedirectPort  int
	DarwinDNSSnapshots    []darwinDNSSnapshot
	PFAnchor              string
	LinuxOutboundSrcIP    string
	LinuxDNSMode          string
	LinuxResolvConfBackup string
	WindowsFirewallRule   string
	WindowsDNSBackup      string
	WindowsDefaultIfIndex int
}

type darwinDNSSnapshot struct {
	Service      string
	Servers      []string
	WasAutomatic bool
}

func setupRoutes(activeNode NodeConfig, tun TunSettings, logf func(string)) (*routeContext, error) {
	ctx := &routeContext{}
	ctx.ServerIP = resolveServerIPFromAddress(activeNode.ServerAddress)
	switch runtime.GOOS {
	case "linux":
		return setupRoutesLinux(ctx, tun, logf)
	case "darwin":
		return setupRoutesDarwin(ctx, tun, logf)
	case "windows":
		return setupRoutesWindows(ctx, tun, logf)
	default:
		return nil, nil
	}
}

func resolveServerIPFromAddress(serverAddress string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(serverAddress))
	if err != nil {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
		return ""
	}
	ips, _ := net.LookupIP(host)
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

func teardownRoutes(ctx *routeContext, tun TunSettings, logf func(string)) error {
	if ctx == nil {
		return nil
	}
	switch runtime.GOOS {
	case "linux":
		return teardownRoutesLinux(ctx, tun, logf)
	case "darwin":
		return teardownRoutesDarwin(ctx, tun, logf)
	case "windows":
		return teardownRoutesWindows(ctx, tun, logf)
	}
	return nil
}

func setupRoutesLinux(ctx *routeContext, tun TunSettings, logf func(string)) (*routeContext, error) {
	if !linuxHasCommand("ip") {
		return nil, errors.New("required command not found on linux: ip")
	}

	uid := os.Getuid()
	hasIPTables := linuxHasCommand("iptables")

	cmdlines := make([]string, 0, 32)
	if ctx.ServerIP != "" {
		cmdlines = append(cmdlines, shellJoin("ip", "rule", "add", "to", ctx.ServerIP, "lookup", "main", "pref", "5")+" || true")
	}

	// Ensure the core process can bypass the TUN by binding to the physical source IP.
	if srcIP, err := linuxDefaultOutboundIPv4(); err == nil && strings.TrimSpace(srcIP) != "" {
		ctx.LinuxOutboundSrcIP = strings.TrimSpace(srcIP)
		cmdlines = append(cmdlines, shellJoin("ip", "rule", "add", "from", ctx.LinuxOutboundSrcIP, "lookup", "main", "pref", "8")+" || true")
	}

	// Optional: block QUIC (UDP/443).
	if tun.BlockQUIC {
		if hasIPTables {
			cmdlines = append(cmdlines, "iptables -C OUTPUT -p udp --dport 443 -j DROP >/dev/null 2>&1 || iptables -I OUTPUT 1 -p udp --dport 443 -j DROP")
		} else if logf != nil {
			logf("[route] linux: skip IPv4 QUIC block (iptables not found)")
		}
	}

	// Optional: switch system DNS to HEV MapDNS while TUN is active (FakeIP mode).
	if tun.MapDNSEnabled && strings.TrimSpace(tun.MapDNSAddress) != "" {
		dnsAddr := strings.TrimSpace(tun.MapDNSAddress)
		ctx.DNSOverrideAddress = dnsAddr
		if tun.MapDNSLocalProxy {
			ctx.DNSProxyRedirectPort = localDNSProxyRedirectPort(dnsAddr)
			if ctx.DNSProxyRedirectPort > 0 {
				if !hasIPTables {
					return nil, errors.New("iptables required for linux local dns proxy redirect")
				}
				redirectPort := strconv.Itoa(ctx.DNSProxyRedirectPort)
				cmdlines = append(cmdlines,
					"iptables -t nat -C OUTPUT -d 127.0.0.1/32 -p udp --dport 53 -j REDIRECT --to-ports "+redirectPort+" >/dev/null 2>&1 || iptables -t nat -I OUTPUT 1 -d 127.0.0.1/32 -p udp --dport 53 -j REDIRECT --to-ports "+redirectPort,
					"iptables -t nat -C OUTPUT -d 127.0.0.1/32 -p tcp --dport 53 -j REDIRECT --to-ports "+redirectPort+" >/dev/null 2>&1 || iptables -t nat -I OUTPUT 1 -d 127.0.0.1/32 -p tcp --dport 53 -j REDIRECT --to-ports "+redirectPort,
				)
			}
		}
		if _, err := exec.LookPath("resolvectl"); err == nil {
			ctx.LinuxDNSMode = "resolvectl"
			cmdlines = append(cmdlines,
				shellJoin("resolvectl", "dns", tun.InterfaceName, dnsAddr)+" || true",
				shellJoin("resolvectl", "domain", tun.InterfaceName, "~.")+" || true",
				"resolvectl flush-caches >/dev/null 2>&1 || true",
			)
		} else {
			ctx.LinuxDNSMode = "resolvconf"
			ctx.LinuxResolvConfBackup = fmt.Sprintf("/tmp/sudoku4x4-resolv.conf.%d.bak", uid)
			cmdlines = append(cmdlines,
				"cp -f /etc/resolv.conf "+shellQuote(ctx.LinuxResolvConfBackup)+" >/dev/null 2>&1 || true",
				"printf 'nameserver "+dnsAddr+"\\n' > /etc/resolv.conf",
				"resolvectl flush-caches >/dev/null 2>&1 || systemd-resolve --flush-caches >/dev/null 2>&1 || true",
			)
		}
	}

	cmdlines = append(cmdlines,
		shellJoin("sysctl", "-w", "net.ipv4.conf.all.rp_filter=0")+" || true",
		shellJoin("sysctl", "-w", fmt.Sprintf("net.ipv4.conf.%s.rp_filter=0", tun.InterfaceName))+" || true",
		shellJoin("ip", "rule", "add", "fwmark", strconv.Itoa(tun.SocksMark), "lookup", "main", "pref", "10")+" || true",
		shellJoin("ip", "route", "add", "default", "dev", tun.InterfaceName, "table", strconv.Itoa(tun.RouteTable))+" || true",
		shellJoin("ip", "rule", "add", "lookup", strconv.Itoa(tun.RouteTable), "pref", "20")+" || true",
	)

	if err := runCmdsLinuxAdmin(logf, cmdlines...); err != nil {
		// Best-effort cleanup to avoid leaving the system half-configured.
		_ = teardownRoutesLinux(ctx, tun, logf)
		return nil, err
	}

	// Verification (production safety): ensure the policy route and default route in the TUN
	// table are actually present. Many linux commands above are idempotent ("|| true") and
	// can otherwise mask a broken TUN dataplane.
	verify := func() error {
		// 1) ip rule (pref 20 lookup <table>).
		out, err := exec.Command("ip", "rule", "show").CombinedOutput()
		if err != nil {
			return fmt.Errorf("ip rule show: %w: %s", err, strings.TrimSpace(string(out)))
		}
		needle := fmt.Sprintf("lookup %d", tun.RouteTable)
		okRule := false
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "20:") && strings.Contains(line, needle) {
				okRule = true
				break
			}
		}
		if !okRule {
			return fmt.Errorf("missing ip rule: pref 20 %s", needle)
		}

		// 2) table default route (default dev <tun>).
		out2, err2 := exec.Command("ip", "route", "show", "table", fmt.Sprintf("%d", tun.RouteTable)).CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("ip route show table %d: %w: %s", tun.RouteTable, err2, strings.TrimSpace(string(out2)))
		}
		routeOut := string(out2)
		if !strings.Contains(routeOut, "default") || !strings.Contains(routeOut, "dev "+strings.TrimSpace(tun.InterfaceName)) {
			return fmt.Errorf("missing default route in table %d via %s", tun.RouteTable, strings.TrimSpace(tun.InterfaceName))
		}
		return nil
	}
	deadline := time.Now().Add(2 * time.Second)
	var verifyErr error
	for {
		verifyErr = verify()
		if verifyErr == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = teardownRoutesLinux(ctx, tun, logf)
			return nil, verifyErr
		}
		time.Sleep(120 * time.Millisecond)
	}

	return ctx, nil
}

func teardownRoutesLinux(ctx *routeContext, tun TunSettings, logf func(string)) error {
	cmdlines := make([]string, 0, 32)
	if ctx != nil && ctx.ServerIP != "" {
		cmdlines = append(cmdlines, shellJoin("ip", "rule", "del", "to", ctx.ServerIP, "lookup", "main", "pref", "5")+" || true")
	}
	if ctx != nil && strings.TrimSpace(ctx.LinuxOutboundSrcIP) != "" {
		cmdlines = append(cmdlines, shellJoin("ip", "rule", "del", "from", ctx.LinuxOutboundSrcIP, "lookup", "main", "pref", "8")+" || true")
	}
	if tun.BlockQUIC {
		cmdlines = append(cmdlines,
			"iptables -D OUTPUT -p udp --dport 443 -j DROP >/dev/null 2>&1 || true",
			"ip6tables -D OUTPUT -p udp --dport 443 -j DROP >/dev/null 2>&1 || true",
		)
	}
	// Restore DNS (FakeIP mode).
	if ctx != nil && ctx.LinuxDNSMode == "resolvectl" {
		cmdlines = append(cmdlines,
			shellJoin("resolvectl", "revert", tun.InterfaceName)+" || true",
			"resolvectl flush-caches >/dev/null 2>&1 || true",
		)
	} else if ctx != nil && ctx.LinuxDNSMode == "resolvconf" && strings.TrimSpace(ctx.LinuxResolvConfBackup) != "" {
		cmdlines = append(cmdlines,
			"if [ -f "+shellQuote(ctx.LinuxResolvConfBackup)+" ]; then cp -f "+shellQuote(ctx.LinuxResolvConfBackup)+" /etc/resolv.conf >/dev/null 2>&1 || true; rm -f "+shellQuote(ctx.LinuxResolvConfBackup)+" >/dev/null 2>&1 || true; fi",
			"resolvectl flush-caches >/dev/null 2>&1 || systemd-resolve --flush-caches >/dev/null 2>&1 || true",
		)
	} else if ctx != nil && strings.TrimSpace(ctx.LinuxDNSMode) == "" {
		// Emergency/unknown mode (e.g. crash/force-quit): attempt both resolvectl revert
		// and /etc/resolv.conf restoration from the known backup path (if present).
		cmdlines = append(cmdlines,
			shellJoin("resolvectl", "revert", tun.InterfaceName)+" || true",
			"resolvectl flush-caches >/dev/null 2>&1 || systemd-resolve --flush-caches >/dev/null 2>&1 || true",
		)
		if strings.TrimSpace(ctx.LinuxResolvConfBackup) != "" {
			cmdlines = append(cmdlines,
				"if [ -f "+shellQuote(ctx.LinuxResolvConfBackup)+" ]; then cp -f "+shellQuote(ctx.LinuxResolvConfBackup)+" /etc/resolv.conf >/dev/null 2>&1 || true; rm -f "+shellQuote(ctx.LinuxResolvConfBackup)+" >/dev/null 2>&1 || true; fi",
			)
		}
	}
	if ctx != nil && ctx.DNSProxyRedirectPort > 0 {
		redirectPort := strconv.Itoa(ctx.DNSProxyRedirectPort)
		cmdlines = append(cmdlines,
			"iptables -t nat -D OUTPUT -d 127.0.0.1/32 -p udp --dport 53 -j REDIRECT --to-ports "+redirectPort+" >/dev/null 2>&1 || true",
			"iptables -t nat -D OUTPUT -d 127.0.0.1/32 -p tcp --dport 53 -j REDIRECT --to-ports "+redirectPort+" >/dev/null 2>&1 || true",
		)
	}
	cmdlines = append(cmdlines,
		shellJoin("ip", "rule", "del", "fwmark", strconv.Itoa(tun.SocksMark), "lookup", "main", "pref", "10")+" || true",
		shellJoin("ip", "-6", "rule", "del", "fwmark", strconv.Itoa(tun.SocksMark), "lookup", "main", "pref", "10")+" || true",
		shellJoin("ip", "rule", "del", "lookup", strconv.Itoa(tun.RouteTable), "pref", "20")+" || true",
		shellJoin("ip", "-6", "rule", "del", "lookup", strconv.Itoa(tun.RouteTable), "pref", "20")+" || true",
		shellJoin("ip", "route", "del", "default", "dev", tun.InterfaceName, "table", strconv.Itoa(tun.RouteTable))+" || true",
		shellJoin("ip", "-6", "route", "del", "default", "dev", tun.InterfaceName, "table", strconv.Itoa(tun.RouteTable))+" || true",
	)
	_ = runCmdsLinuxAdmin(logf, cmdlines...)
	return nil
}

func setupRoutesDarwin(ctx *routeContext, tun TunSettings, logf func(string)) (*routeContext, error) {
	info, _ := darwinPrimaryNetworkInfo()
	gw := strings.TrimSpace(info.Router4)
	ifName := strings.TrimSpace(info.Interface4)
	if darwinIsTunLikeInterface(ifName) {
		ifName = ""
	}

	// Prefer scutil; fallback to netstat and DHCP when scutil is empty/stale (common during Wi‑Fi switches).
	if gw == "" || ifName == "" {
		if routes, err := darwinNetstatRoutesIPv4(); err == nil {
			if g, ifn := darwinPickPhysicalDefaultRouteIPv4(routes); strings.TrimSpace(g) != "" && strings.TrimSpace(ifn) != "" {
				if gw == "" {
					gw = strings.TrimSpace(g)
				}
				if ifName == "" {
					ifName = strings.TrimSpace(ifn)
				}
			}
		}
	}
	if ifName == "" {
		ifn, _ := darwinResolveOutboundBypassInterface(2 * time.Second)
		ifName = strings.TrimSpace(ifn)
		if darwinIsTunLikeInterface(ifName) {
			ifName = ""
		}
	}
	if gw == "" && ifName != "" && !darwinIsTunLikeInterface(ifName) {
		if g, err := darwinDHCPRouterForInterface(ifName); err == nil {
			gw = strings.TrimSpace(g)
		}
	}
	if gw == "" || ifName == "" {
		// Last resort: route(8) output (can point to utun while TUN is active; validate strictly).
		if g, ifn, err := darwinDefaultRoute(); err == nil {
			g = strings.TrimSpace(g)
			ifn = strings.TrimSpace(ifn)
			if ifName == "" && ifn != "" && !darwinIsTunLikeInterface(ifn) {
				ifName = ifn
			}
			if gw == "" {
				if ip := net.ParseIP(g); ip != nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
					gw = g
				}
			}
		}
	}

	if strings.TrimSpace(gw) == "" {
		return nil, errors.New("default gateway not found")
	}
	if strings.TrimSpace(ifName) == "" {
		return nil, errors.New("default interface not found")
	}
	ctx.DefaultGateway = strings.TrimSpace(gw)
	ctx.DefaultInterface = strings.TrimSpace(ifName)
	dnsFlushCmd := "dscacheutil -flushcache >/dev/null 2>&1 || true; killall -HUP mDNSResponder >/dev/null 2>&1 || true"

	// Optional: switch system DNS to HEV MapDNS while TUN is active (for correct PAC/domain routing).
	dnsSetCmd := ""
	if tun.MapDNSEnabled && strings.TrimSpace(tun.MapDNSAddress) != "" && strings.TrimSpace(ctx.DefaultInterface) != "" {
		if svc, derr := darwinNetworkServiceForDevice(ctx.DefaultInterface); derr == nil && strings.TrimSpace(svc) != "" {
			ctx.DNSService = svc
			ctx.DNSOverrideAddress = strings.TrimSpace(tun.MapDNSAddress)
			if tun.MapDNSLocalProxy {
				ctx.DNSProxyRedirectPort = localDNSProxyRedirectPort(ctx.DNSOverrideAddress)
			}
			prev, wasAuto, gerr := darwinGetDNSServers(svc)
			if gerr == nil {
				ctx.DNSServers = prev
				ctx.DNSWasAutomatic = wasAuto
			}
			ctx.DarwinDNSSnapshots = append(ctx.DarwinDNSSnapshots, darwinDNSSnapshot{
				Service:      svc,
				Servers:      append([]string(nil), ctx.DNSServers...),
				WasAutomatic: ctx.DNSWasAutomatic,
			})
			dnsSetCmd = shellJoin("networksetup", "-setdnsservers", svc, strings.TrimSpace(tun.MapDNSAddress))
		}
	}

	pfSetCmd := ""
	if runtime.GOOS == "darwin" && (tun.BlockQUIC || ctx.DNSProxyRedirectPort > 0) {
		ctx.PFAnchor = fmt.Sprintf("com.apple/sudoku4x4.tun.%d", os.Getuid())
		pfSetCmd = darwinBuildPFSetCmd(ctx.PFAnchor, tun.InterfaceName, tun.BlockQUIC, ctx.DNSProxyRedirectPort)
	}
	if runtime.GOOS == "darwin" && os.Geteuid() != 0 {
		cmds := make([]string, 0, 9)
		if ctx.ServerIP != "" {
			// Be idempotent: a host route may already exist (e.g. from a prior run or system clone).
			cmds = append(cmds, shellJoin("route", "-n", "add", "-host", ctx.ServerIP, gw)+" || "+shellJoin("route", "-n", "change", "-host", ctx.ServerIP, gw))
		}
		cmds = append(cmds,
			// Some macOS setups have multiple scoped default routes; if `change` fails, recreate it.
			shellJoin("route", "-n", "change", "default", "-interface", tun.InterfaceName)+" || ("+
				shellJoin("route", "-n", "delete", "default")+" >/dev/null 2>&1 || true; "+
				shellJoin("route", "-n", "add", "default", "-interface", tun.InterfaceName)+")",
		)
		if ctx.DefaultInterface != "" && ctx.DefaultGateway != "" {
			// Ensure a physical scoped default route exists for sockets bound to DefaultInterface (core outbound bypass).
			// NOTE: Creating this route *before* switching the global default route can fail with "File exists" (it
			// collides with the current global default). Ensure it after the default route has switched to utun.
			cmds = append(cmds, "("+shellJoin("route", "-n", "add", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1 || "+
				shellJoin("route", "-n", "change", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1) || echo '__SUDOKU_WARN__=scoped_default_route_failed'")
		}
		if pfSetCmd != "" {
			cmds = append(cmds, pfSetCmd)
		}
		if dnsSetCmd != "" {
			cmds = append(cmds, dnsSetCmd)
			cmds = append(cmds, dnsFlushCmd)
		}
		if err := runCmdsDarwinAdmin(logf, cmds...); err != nil {
			// Best-effort rollback: never leave the machine offline when route setup fails mid-way.
			_ = teardownRoutesDarwin(ctx, tun, logf)
			return nil, err
		}
		return ctx, nil
	}
	if ctx.ServerIP != "" {
		// Be idempotent: a host route may already exist (e.g. from a prior run or system clone).
		_ = runCmd(logf, "route", "-n", "add", "-host", ctx.ServerIP, gw)
		_ = runCmd(logf, "route", "-n", "change", "-host", ctx.ServerIP, gw)
	}
	if err := runCmd(logf, "route", "-n", "change", "default", "-interface", tun.InterfaceName); err != nil {
		// Some macOS setups have multiple scoped default routes; if `change` fails, recreate it.
		_ = runCmd(logf, "route", "-n", "delete", "default")
		if err2 := runCmd(logf, "route", "-n", "add", "default", "-interface", tun.InterfaceName); err2 != nil {
			_ = teardownRoutesDarwin(ctx, tun, logf)
			return nil, err2
		}
	}
	if ctx.DefaultInterface != "" && ctx.DefaultGateway != "" {
		_ = runCmd(logf, "sh", "-lc", "("+shellJoin("route", "-n", "add", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1 || "+
			shellJoin("route", "-n", "change", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1) || echo '__SUDOKU_WARN__=scoped_default_route_failed'")
	}
	if pfSetCmd != "" {
		if err := runCmd(logf, "sh", "-lc", pfSetCmd); err != nil {
			_ = teardownRoutesDarwin(ctx, tun, logf)
			return nil, err
		}
	}
	if dnsSetCmd != "" {
		if err := runCmd(logf, "networksetup", "-setdnsservers", ctx.DNSService, strings.TrimSpace(tun.MapDNSAddress)); err != nil {
			_ = teardownRoutesDarwin(ctx, tun, logf)
			return nil, err
		}
		_ = runCmd(logf, "sh", "-lc", dnsFlushCmd)
	}
	return ctx, nil
}

func teardownRoutesDarwin(ctx *routeContext, tun TunSettings, logf func(string)) error {
	tunIf := strings.TrimSpace(tun.InterfaceName)
	routes, routesErr := darwinNetstatRoutesIPv4()
	// Prefer resolving the active TUN interface by its configured IPv4 (most reliable).
	// This avoids accidentally operating on unrelated utun routes from other VPNs.
	if runtime.GOOS == "darwin" {
		if actual := strings.TrimSpace(darwinFindTunInterfaceByIPv4(tun.IPv4)); actual != "" {
			tunIf = actual
		}
	}
	if routesErr == nil {
		// Fall back to the currently-active tunnel interface from the routing table only when the
		// provided tun interface doesn't appear to have the default route.
		if tunIf == "" || !darwinIsTunLikeInterface(tunIf) || !darwinHasDefaultRouteOnInterface(routes, tunIf) {
			for _, r := range routes {
				if r.Destination != "default" {
					continue
				}
				if strings.TrimSpace(r.Netif) == "" || !darwinIsTunLikeInterface(r.Netif) {
					continue
				}
				tunIf = strings.TrimSpace(r.Netif)
				break
			}
		}
	}

	// Snapshot whether a non-tunnel default route already exists. If it does, we can safely proceed
	// even when we can't resolve the physical gateway (rare).
	hasAltDefault := false
	if routesErr == nil {
		hasAltDefault = darwinHasUnscopedDefaultRouteIPv4(routes, tunIf)
	}

	rollbackDefaultToTun := func() {
		if tunIf == "" {
			return
		}
		if err := runCmd(logf, "route", "-n", "change", "default", "-interface", tunIf); err != nil {
			_ = runCmd(logf, "route", "-n", "delete", "default")
			_ = runCmd(logf, "route", "-n", "add", "default", "-interface", tunIf)
		}
	}

	restoreGW, gwErr := darwinResolveRestoreGatewayIPv4(ctx, tunIf)
	if gwErr != nil && !hasAltDefault {
		// Safety: do NOT remove the tunnel default route if we don't know what to restore.
		// Otherwise the machine can be left with no default route and appear completely offline.
		return gwErr
	}

	scopedIf := strings.TrimSpace(ctx.DefaultInterface)
	scopedGW := strings.TrimSpace(ctx.DefaultGateway)
	if scopedGW == "" {
		scopedGW = strings.TrimSpace(restoreGW)
	}
	if (scopedIf == "" || scopedGW == "") && routesErr == nil {
		// Best-effort: infer the scoped default route we may have added while TUN was active.
		for _, r := range routes {
			if r.Destination != "default" {
				continue
			}
			if strings.TrimSpace(r.Netif) == "" || darwinIsTunLikeInterface(r.Netif) {
				continue
			}
			if !strings.Contains(r.Flags, "I") {
				continue
			}
			if scopedIf == "" {
				scopedIf = strings.TrimSpace(r.Netif)
			}
			if scopedGW == "" {
				scopedGW = strings.TrimSpace(r.Gateway)
			}
			break
		}
	}

	dnsFlushCmd := "dscacheutil -flushcache >/dev/null 2>&1 || true; killall -HUP mDNSResponder >/dev/null 2>&1 || true"

	// Collect all services we touched (to restore them all on stop).
	snaps := append([]darwinDNSSnapshot(nil), ctx.DarwinDNSSnapshots...)
	if svc := strings.TrimSpace(ctx.DNSService); svc != "" {
		found := false
		for _, s := range snaps {
			if strings.EqualFold(strings.TrimSpace(s.Service), svc) {
				found = true
				break
			}
		}
		if !found {
			snaps = append(snaps, darwinDNSSnapshot{
				Service:      svc,
				Servers:      append([]string(nil), ctx.DNSServers...),
				WasAutomatic: ctx.DNSWasAutomatic,
			})
		}
	}

	// 1) Restore default route first. If we can't restore a *global* (unscoped) non-TUN default route,
	// abort stop to avoid leaving the machine offline.
	//
	// NOTE: During TUN operation we add an interface-scoped default route on the physical interface
	// for bound sockets (core outbound bypass). That scoped route alone is not sufficient for normal
	// routing after stop (`route -n get default` can still fail with "not in table"). Always ensure an
	// unscoped default route exists again before removing the tunnel default route.
	restoreIf := strings.TrimSpace(ctx.DefaultInterface)
	if restoreIf == "" {
		restoreIf = scopedIf
	}
	if restoreIf == "" {
		if info, ierr := darwinPrimaryNetworkInfo(); ierr == nil {
			ifName := strings.TrimSpace(info.Interface4)
			if ifName != "" && !darwinIsTunLikeInterface(ifName) {
				restoreIf = ifName
			}
		}
	}
	if restoreIf == "" {
		ifName, _ := darwinResolveOutboundBypassInterface(1200 * time.Millisecond)
		ifName = strings.TrimSpace(ifName)
		if ifName != "" && !darwinIsTunLikeInterface(ifName) {
			restoreIf = ifName
		}
	}

	restoreScopedRoute := func() {
		if scopedIf == "" || scopedGW == "" {
			return
		}
		_ = runCmd(logf, "route", "-n", "add", "-ifscope", scopedIf, "default", scopedGW)
		_ = runCmd(logf, "route", "-n", "change", "-ifscope", scopedIf, "default", scopedGW)
	}

	// Remove the auxiliary physical scoped default routes early to avoid `route change default` ambiguity
	// when multiple default routes exist. If stop fails, we restore them before returning.
	if routesErr == nil {
		for _, r := range routes {
			if r.Destination != "default" {
				continue
			}
			if strings.TrimSpace(r.Netif) == "" || darwinIsTunLikeInterface(r.Netif) {
				continue
			}
			if !strings.Contains(r.Flags, "I") {
				continue
			}
			if strings.TrimSpace(r.Gateway) == "" {
				continue
			}
			_ = runCmd(logf, "route", "-n", "delete", "-ifscope", strings.TrimSpace(r.Netif), "default", strings.TrimSpace(r.Gateway))
		}
	}
	if scopedIf != "" && scopedGW != "" {
		_ = runCmd(logf, "route", "-n", "delete", "-ifscope", scopedIf, "default", scopedGW)
	}

	physicalDefaultOK := func() bool {
		gwNow, ifNow, err := darwinDefaultRoute()
		if err != nil {
			return false
		}
		ifNow = strings.TrimSpace(ifNow)
		if ifNow == "" || darwinIsTunLikeInterface(ifNow) {
			return false
		}
		if tunIf != "" && strings.EqualFold(ifNow, tunIf) {
			return false
		}
		ip := net.ParseIP(strings.TrimSpace(gwNow))
		return ip != nil && ip.To4() != nil && !ip.IsLoopback() && !ip.IsUnspecified()
	}

	// Attempt to restore the global default route to the physical gateway.
	if strings.TrimSpace(restoreGW) != "" {
		if logf != nil {
			logf(fmt.Sprintf("[route] restoring default route via %s (tunIf=%s)", restoreGW, tunIf))
		}
		if err := runCmd(logf, "route", "-n", "change", "default", restoreGW); err != nil {
			// Fallback: recreate the global default route. Roll back to TUN on failure.
			_ = runCmd(logf, "route", "-n", "delete", "default")
			if err2 := runCmd(logf, "route", "-n", "add", "default", restoreGW); err2 != nil {
				rollbackDefaultToTun()
				restoreScopedRoute()
				return fmt.Errorf("restore default route via %s failed: %v; %w", restoreGW, err, err2)
			}
		}
	} else if !hasAltDefault {
		// Safety: do not proceed if we have neither a gateway nor an alternate default route.
		rollbackDefaultToTun()
		restoreScopedRoute()
		return errors.New("restore default route: gateway not found")
	}

	// If the global default route still points to the tunnel (or is missing), force it to the physical gateway.
	if strings.TrimSpace(restoreGW) != "" && !physicalDefaultOK() {
		if tunIf != "" {
			_ = runCmd(logf, "route", "-n", "delete", "default", "-interface", tunIf)
		}
		_ = runCmd(logf, "route", "-n", "delete", "default")
		if err := runCmd(logf, "route", "-n", "add", "default", restoreGW); err != nil {
			rollbackDefaultToTun()
			restoreScopedRoute()
			return fmt.Errorf("restore default route via %s failed: %w", restoreGW, err)
		}
	}

	// Remove the tunnel default routes only after we have a working physical global default.
	if tunIf != "" && physicalDefaultOK() {
		_ = runCmd(logf, "route", "-n", "delete", "default", "-interface", tunIf)
	}

	// Validation: ensure we are not leaving the global default route on the tunnel interface.
	// Use netstat-based checks (plus route get) because route output can be transient during Wi-Fi switches.
	waitErr := darwinWaitDefaultRouteNotOnTun(tunIf, 5*time.Second)
	if waitErr != nil && restoreIf != "" && !darwinIsTunLikeInterface(restoreIf) {
		// Kick DHCP once (best-effort) to recover from transient Wi‑Fi transitions where the system
		// hasn't reinstalled an unscoped default route yet.
		_ = runCmd(logf, "ipconfig", "set", restoreIf, "DHCP")
		waitErr = darwinWaitDefaultRouteNotOnTun(tunIf, 7*time.Second)
	}
	if waitErr != nil {
		rollbackDefaultToTun()
		restoreScopedRoute()
		return waitErr
	}

	// 2) Always attempt to remove the host route (if we added one).
	if strings.TrimSpace(ctx.ServerIP) != "" {
		_ = runCmd(logf, "route", "-n", "delete", "-host", strings.TrimSpace(ctx.ServerIP))
	}

	// 3) Restore DNS on all captured services. If DNS can't be restored, abort stop so the local DNS proxy
	// remains running (avoids "DNS looks down" offline symptoms).
	if len(snaps) > 0 {
		seen := map[string]struct{}{}
		for _, snap := range snaps {
			svc := strings.TrimSpace(snap.Service)
			if svc == "" {
				continue
			}
			key := strings.ToLower(svc)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if snap.WasAutomatic || len(snap.Servers) == 0 {
				if err := runCmd(logf, "networksetup", "-setdnsservers", svc, "Empty"); err != nil {
					return fmt.Errorf("restore dns servers for %s: %w", svc, err)
				}
			} else {
				args := append([]string{"-setdnsservers", svc}, snap.Servers...)
				if err := runCmd(logf, "networksetup", args...); err != nil {
					return fmt.Errorf("restore dns servers for %s: %w", svc, err)
				}
			}
		}
		_ = runCmd(logf, "sh", "-lc", dnsFlushCmd)
	}

	// 4) Flush pf anchor (best-effort). If this fails due to privileges, StopProxy will surface it
	// and avoid stopping the TUN.
	if strings.TrimSpace(ctx.PFAnchor) != "" {
		if err := runCmd(logf, "pfctl", "-a", strings.TrimSpace(ctx.PFAnchor), "-F", "all"); err != nil {
			if isLikelyPermissionError(err) {
				return err
			}
			if logf != nil {
				logf(fmt.Sprintf("[route] warn: pfctl restore failed (ignored): %v", err))
			}
		}
	}

	return nil
}

func setupRoutesWindows(ctx *routeContext, tun TunSettings, logf func(string)) (*routeContext, error) {
	idx, alias, err := windowsResolveTunInterfaceIndex(tun, 10*time.Second)
	if err != nil {
		return nil, err
	}
	ctx.TunIndex = idx
	ctx.TunAlias = strings.TrimSpace(alias)
	if logf != nil {
		if strings.TrimSpace(alias) != "" {
			logf(fmt.Sprintf("[route] windows tun interface: %s (ifindex=%d)", alias, idx))
		} else {
			logf(fmt.Sprintf("[route] windows tun ifindex=%d", idx))
		}
	}
	gw, if4, err := windowsPreferredDefaultRouteIPv4(idx)
	if err != nil {
		return nil, err
	}
	ctx.DefaultGateway = gw
	ctx.WindowsDefaultIfIndex = if4
	firewallRule := "4x4-sudoku Block QUIC (UDP/443)"
	if tun.BlockQUIC {
		ctx.WindowsFirewallRule = firewallRule
	}

	dnsBackupName := ""
	if tun.MapDNSEnabled && strings.TrimSpace(tun.MapDNSAddress) != "" {
		ctx.DNSOverrideAddress = strings.TrimSpace(tun.MapDNSAddress)
		// Use PID to avoid collisions (os.Getuid is not meaningful on Windows).
		dnsBackupName = fmt.Sprintf("sudoku4x4-dns-%d.json", os.Getpid())
		ctx.WindowsDNSBackup = dnsBackupName
	}
	ps := buildWindowsRouteScript(
		true,
		ctx.ServerIP,
		firewallRule,
		tun.BlockQUIC,
		idx,
		ctx.DefaultGateway,
		ctx.WindowsDefaultIfIndex,
		tun.MapDNSEnabled,
		strings.TrimSpace(tun.MapDNSAddress),
		dnsBackupName,
	)
	if err := runCmdsWindowsAdmin(logf, ps, 5*time.Minute); err != nil {
		_ = teardownRoutesWindows(ctx, tun, logf)
		return nil, err
	}
	return ctx, nil
}

func teardownRoutesWindows(ctx *routeContext, tun TunSettings, logf func(string)) error {
	if ctx == nil {
		return nil
	}
	firewallRule := ctx.WindowsFirewallRule
	if firewallRule == "" {
		firewallRule = "4x4-sudoku Block QUIC (UDP/443)"
	}
	// Restore DNS whenever we backed it up during start (we always do so in TUN mode).
	mapDNSEnabled := strings.TrimSpace(ctx.WindowsDNSBackup) != ""
	ps := buildWindowsRouteScript(
		false,
		ctx.ServerIP,
		firewallRule,
		tun.BlockQUIC,
		ctx.TunIndex,
		ctx.DefaultGateway,
		ctx.WindowsDefaultIfIndex,
		mapDNSEnabled,
		strings.TrimSpace(tun.MapDNSAddress),
		ctx.WindowsDNSBackup,
	)
	return runCmdsWindowsAdmin(logf, ps, 5*time.Minute)
}

func runCmdsLinuxAdmin(logf func(string), cmdlines ...string) error {
	if len(cmdlines) == 0 {
		return nil
	}
	shell := "set -e; PATH=/usr/sbin:/sbin:/usr/bin:/bin:$PATH; " + strings.Join(cmdlines, "; ")
	if os.Geteuid() == 0 {
		return runCmdExec(logf, "sh", "-lc", shell)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	output, err := linuxAdminRunShLC(ctx, shell)
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("linux admin batch: timeout")
	}
	clean := strings.TrimSpace(output)
	if logf != nil {
		if clean != "" {
			logf(fmt.Sprintf("[route] sudo (batch) => %s", clean))
		} else {
			logf("[route] sudo (batch)")
		}
	}
	if err != nil {
		if clean != "" {
			return fmt.Errorf("linux admin batch: %w: %s", err, clean)
		}
		return fmt.Errorf("linux admin batch: %w", err)
	}
	return nil
}

func runCmdsWindowsAdmin(logf func(string), scriptBody string, timeout time.Duration) error {
	script := windowsAdminWrapper(scriptBody)
	f, err := os.CreateTemp("", "sudoku-admin-*.ps1")
	if err != nil {
		return err
	}
	path := f.Name()
	if _, werr := f.WriteString(script); werr != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return werr
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(path)
		return cerr
	}
	defer os.Remove(path)

	// PowerShell script self-elevates if needed (UAC prompt).
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", path)
	applyManagedProcessSysProcAttr(cmd)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		if clean := strings.TrimSpace(string(output)); clean != "" {
			return fmt.Errorf("windows admin: timeout: %s", clean)
		}
		return errors.New("windows admin: timeout")
	}
	clean := strings.TrimSpace(string(output))
	if logf != nil {
		if clean != "" {
			logf(fmt.Sprintf("[route] windows admin => %s", clean))
		} else {
			logf("[route] windows admin")
		}
	}
	if err != nil {
		if clean != "" {
			return fmt.Errorf("windows admin: %w: %s", err, clean)
		}
		return fmt.Errorf("windows admin: %w", err)
	}
	return nil
}

func windowsAdminWrapper(body string) string {
	// `$PSCommandPath` points to this script file.
	return strings.Join([]string{
		"param([switch]$Elevated)",
		"$ErrorActionPreference = 'Stop'",
		"function Test-Admin {",
		"  $id = [Security.Principal.WindowsIdentity]::GetCurrent()",
		"  $p = New-Object Security.Principal.WindowsPrincipal($id)",
		"  return $p.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)",
		"}",
		"if (-not $Elevated -and -not (Test-Admin)) {",
		"  $args = @('-NoProfile','-NonInteractive','-WindowStyle','Hidden','-ExecutionPolicy','Bypass','-File', $PSCommandPath, '-Elevated')",
		"  $proc = Start-Process -FilePath 'powershell.exe' -Verb RunAs -WindowStyle Hidden -ArgumentList $args -Wait -PassThru",
		"  exit $proc.ExitCode",
		"}",
		"",
		body,
		"",
	}, "\r\n")
}

func buildWindowsRouteScript(
	start bool,
	serverIP string,
	firewallRule string,
	blockQUIC bool,
	tunIfIndex int,
	defaultGw4 string,
	defaultIf4 int,
	mapDNSEnabled bool,
	mapDNSAddress string,
	dnsBackupName string,
) string {
	op := "start"
	if !start {
		op = "stop"
	}
	serverIP = strings.TrimSpace(serverIP)
	firewallRule = strings.TrimSpace(firewallRule)
	defaultGw4 = strings.TrimSpace(defaultGw4)
	mapDNSAddress = strings.TrimSpace(mapDNSAddress)
	dnsBackupName = strings.TrimSpace(dnsBackupName)
	if firewallRule == "" {
		firewallRule = "4x4-sudoku Block QUIC (UDP/443)"
	}

	// Use ActiveStore so routes/rules are not persisted across reboot.
	lines := []string{
		fmt.Sprintf("$op = '%s'", op),
		fmt.Sprintf("$tunIf = %d", tunIfIndex),
		fmt.Sprintf("$gw4 = '%s'", strings.ReplaceAll(defaultGw4, "'", "''")),
		fmt.Sprintf("$if4 = %d", defaultIf4),
		"if (-not $gw4 -or -not $if4 -or $if4 -le 0) {",
		"  $default4 = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object { $_.InterfaceIndex -ne $tunIf -and $_.NextHop -and $_.NextHop -ne '0.0.0.0' } | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1",
		"  if ($default4 -eq $null) { $default4 = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object { $_.InterfaceIndex -ne $tunIf } | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1 }",
		"  if ($default4 -ne $null) { $gw4 = $default4.NextHop; $if4 = [int]$default4.InterfaceIndex }",
		"}",
	}
	if serverIP != "" {
		lines = append(lines, fmt.Sprintf("$serverIP = '%s'", strings.ReplaceAll(serverIP, "'", "''")))
	} else {
		lines = append(lines, "$serverIP = ''")
	}
	lines = append(lines,
		fmt.Sprintf("$fwRule = '%s'", strings.ReplaceAll(firewallRule, "'", "''")),
		fmt.Sprintf("$blockQUIC = %s", map[bool]string{true: "$true", false: "$false"}[blockQUIC]),
		fmt.Sprintf("$mapDNSEnabled = %s", map[bool]string{true: "$true", false: "$false"}[mapDNSEnabled]),
		fmt.Sprintf("$mapDNS = '%s'", strings.ReplaceAll(mapDNSAddress, "'", "''")),
		fmt.Sprintf("$dnsBackupName = '%s'", strings.ReplaceAll(dnsBackupName, "'", "''")),
		"$physMetric = 5000",
		"",
		"function Add-RoutePrefix($prefix, $ifIndex, $gw) {",
		"  try {",
		"    if (-not $prefix -or -not $ifIndex -or $ifIndex -le 0 -or -not $gw) { return }",
		"    New-NetRoute -DestinationPrefix $prefix -InterfaceIndex $ifIndex -NextHop $gw -PolicyStore ActiveStore -ErrorAction Stop | Out-Null",
		"  } catch { }",
		"}",
		"function Remove-RoutePrefix($prefix, $ifIndex, $gw) {",
		"  try {",
		"    if (-not $prefix -or -not $ifIndex -or $ifIndex -le 0 -or -not $gw) { return }",
		"    Remove-NetRoute -DestinationPrefix $prefix -InterfaceIndex $ifIndex -NextHop $gw -PolicyStore ActiveStore -Confirm:$false -ErrorAction Stop",
		"  } catch { }",
		"}",
		"",
		"$dnsBackup = ''",
		"if ($dnsBackupName) { $dnsBackup = Join-Path $env:TEMP $dnsBackupName }",
		"",
		"if ($op -eq 'start') {",
		"  if ($serverIP) {",
		"    Add-RoutePrefix ($serverIP + '/32') $if4 $gw4",
		"  }",
		"  if ($mapDNSEnabled -and $mapDNS) {",
		"    $prev4 = @((Get-DnsClientServerAddress -InterfaceIndex $tunIf -AddressFamily IPv4 -ErrorAction SilentlyContinue).ServerAddresses)",
		"    if ($dnsBackup) {",
		"      $tunAuto = $null; $tunMetric = $null",
		"      $physAuto = $null; $physMetric0 = $null",
		"      $tunIfInfo = Get-NetIPInterface -InterfaceIndex $tunIf -AddressFamily IPv4 -ErrorAction SilentlyContinue | Select-Object -First 1",
		"      if ($tunIfInfo -ne $null) { $tunAuto = $tunIfInfo.AutomaticMetric; $tunMetric = [int]$tunIfInfo.InterfaceMetric }",
		"      $physIfInfo = $null",
		"      if ($if4 -gt 0) { $physIfInfo = Get-NetIPInterface -InterfaceIndex $if4 -AddressFamily IPv4 -ErrorAction SilentlyContinue | Select-Object -First 1 }",
		"      if ($physIfInfo -ne $null) { $physAuto = $physIfInfo.AutomaticMetric; $physMetric0 = [int]$physIfInfo.InterfaceMetric }",
		"      $backupOk = $false",
		"      try {",
		"        @{ v4 = $prev4; metrics = @{ tun = @{ auto = $tunAuto; metric = $tunMetric }; phys = @{ auto = $physAuto; metric = $physMetric0 } } } | ConvertTo-Json -Compress | Set-Content -Path $dnsBackup -Encoding ASCII",
		"        $backupOk = $true",
		"      } catch { $backupOk = $false }",
		"    }",
		"    # Set-DnsClientServerAddress has no -AddressFamily parameter on Windows PowerShell 5.1.",
		"    Set-DnsClientServerAddress -InterfaceIndex $tunIf -ServerAddresses @($mapDNS) -ErrorAction SilentlyContinue | Out-Null",
		"    try { Clear-DnsClientCache | Out-Null } catch { }",
		"    # Ensure Windows prefers the tunnel for the default route (metrics are restored on stop).",
		"    if ($backupOk) {",
		"      try { Set-NetIPInterface -InterfaceIndex $tunIf -AutomaticMetric Disabled -InterfaceMetric 1 -ErrorAction SilentlyContinue | Out-Null } catch { }",
		"      try { if ($if4 -gt 0) { Set-NetIPInterface -InterfaceIndex $if4 -AutomaticMetric Disabled -InterfaceMetric $physMetric -ErrorAction SilentlyContinue | Out-Null } } catch { }",
		"    }",
		"  }",
		"  # Add a low-metric default route to the tunnel interface (ActiveStore only).",
		"  try { Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $tunIf -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue } catch { }",
		"  try { New-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $tunIf -NextHop '0.0.0.0' -RouteMetric 1 -PolicyStore ActiveStore -ErrorAction Stop | Out-Null } catch {",
		"    $out4 = & route.exe add 0.0.0.0 mask 0.0.0.0 0.0.0.0 metric 1 if $tunIf 2>&1",
		"    if ($LASTEXITCODE -ne 0) { throw ('route.exe add default route failed: ' + ($out4 | Out-String).Trim()) }",
		"  }",
		"  $best4 = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Sort-Object @{Expression={ [int]$_.RouteMetric + [int]$_.InterfaceMetric }},RouteMetric,InterfaceMetric | Select-Object -First 1",
		"  if ($best4 -eq $null) { throw 'windows default route not found after tun switch' }",
		"  if ([int]$best4.InterfaceIndex -ne $tunIf) { throw ('windows default route still not on tun interface: expected=' + $tunIf + ' got=' + [int]$best4.InterfaceIndex) }",
		"  # Keep a physical default route for core-bypass sockets (IP_UNICAST_IF).",
		"  try { if ($if4 -gt 0 -and $gw4) { New-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $if4 -NextHop $gw4 -RouteMetric $physMetric -PolicyStore ActiveStore -ErrorAction Stop | Out-Null } } catch { }",
		"  if ($blockQUIC) {",
		"    if (-not (Get-NetFirewallRule -DisplayName $fwRule -ErrorAction SilentlyContinue)) {",
		"      New-NetFirewallRule -DisplayName $fwRule -Direction Outbound -Action Block -Protocol UDP -RemotePort 443 -Profile Any | Out-Null",
		"    }",
		"  }",
		"} else {",
		"  if ($dnsBackup -and (Test-Path $dnsBackup)) {",
		"    $json = $null",
		"    try { $json = (Get-Content $dnsBackup -Raw | ConvertFrom-Json) } catch { $json = $null }",
		"    if ($json -ne $null) {",
		"      $p4 = @($json.v4)",
		"      $all = @($p4 | Where-Object { $_ } | Select-Object -Unique)",
		"      if ($all.Count -eq 0) { Set-DnsClientServerAddress -InterfaceIndex $tunIf -ResetServerAddresses -ErrorAction SilentlyContinue | Out-Null } else { Set-DnsClientServerAddress -InterfaceIndex $tunIf -ServerAddresses $all -ErrorAction SilentlyContinue | Out-Null }",
		"      # Restore interface metrics if we changed them during start.",
		"      try {",
		"        $m = $json.metrics",
		"        if ($m -ne $null) {",
		"          if ($m.tun -ne $null) {",
		"            if ($m.tun.auto -ne $null -and [bool]$m.tun.auto) {",
		"              Set-NetIPInterface -InterfaceIndex $tunIf -AutomaticMetric Enabled -ErrorAction SilentlyContinue | Out-Null",
		"            } elseif ($m.tun.metric -ne $null) {",
		"              Set-NetIPInterface -InterfaceIndex $tunIf -AutomaticMetric Disabled -InterfaceMetric ([int]$m.tun.metric) -ErrorAction SilentlyContinue | Out-Null",
		"            }",
		"          }",
		"          if ($m.phys -ne $null -and $if4 -gt 0) {",
		"            if ($m.phys.auto -ne $null -and [bool]$m.phys.auto) {",
		"              Set-NetIPInterface -InterfaceIndex $if4 -AutomaticMetric Enabled -ErrorAction SilentlyContinue | Out-Null",
		"            } elseif ($m.phys.metric -ne $null) {",
		"              Set-NetIPInterface -InterfaceIndex $if4 -AutomaticMetric Disabled -InterfaceMetric ([int]$m.phys.metric) -ErrorAction SilentlyContinue | Out-Null",
		"            }",
		"          }",
		"        }",
		"      } catch { }",
		"    } else {",
		"      Set-DnsClientServerAddress -InterfaceIndex $tunIf -ResetServerAddresses -ErrorAction SilentlyContinue | Out-Null",
		"    }",
		"    Remove-Item $dnsBackup -Force -ErrorAction SilentlyContinue | Out-Null",
		"  } elseif ($mapDNSEnabled) {",
		"    Set-DnsClientServerAddress -InterfaceIndex $tunIf -ResetServerAddresses -ErrorAction SilentlyContinue | Out-Null",
		"    # Best-effort metric restore when we don't have a backup.",
		"    try { Set-NetIPInterface -InterfaceIndex $tunIf -AutomaticMetric Enabled -ErrorAction SilentlyContinue | Out-Null } catch { }",
		"    try { if ($if4 -gt 0) { Set-NetIPInterface -InterfaceIndex $if4 -AutomaticMetric Enabled -ErrorAction SilentlyContinue | Out-Null } } catch { }",
		"  }",
		"  # Always attempt to restore interface auto-metric on stop.",
		"  try { Set-NetIPInterface -InterfaceIndex $tunIf -AutomaticMetric Enabled -ErrorAction SilentlyContinue | Out-Null } catch { }",
		"  try { if ($if4 -gt 0) { Set-NetIPInterface -InterfaceIndex $if4 -AutomaticMetric Enabled -ErrorAction SilentlyContinue | Out-Null } } catch { }",
		"  try { Clear-DnsClientCache | Out-Null } catch { }",
		"  # Remove the tunnel default route (ActiveStore only).",
		"  try { Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $tunIf -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue } catch { }",
		"  # Safety: ensure a non-tunnel IPv4 default route exists after stop.",
		"  $best4After = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object { [int]$_.InterfaceIndex -ne $tunIf } | Sort-Object @{Expression={ [int]$_.RouteMetric + [int]$_.InterfaceMetric }},RouteMetric,InterfaceMetric | Select-Object -First 1",
		"  if ($best4After -eq $null -and $if4 -gt 0 -and $gw4) {",
		"    try { New-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $if4 -NextHop $gw4 -RouteMetric 25 -PolicyStore ActiveStore -ErrorAction Stop | Out-Null } catch { }",
		"    $best4After = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object { [int]$_.InterfaceIndex -ne $tunIf } | Sort-Object @{Expression={ [int]$_.RouteMetric + [int]$_.InterfaceMetric }},RouteMetric,InterfaceMetric | Select-Object -First 1",
		"  }",
		"  if ($best4After -eq $null) { throw 'windows restore default route failed after tun stop' }",
		"  if ($serverIP) {",
		"    Remove-RoutePrefix ($serverIP + '/32') $if4 $gw4",
		"  }",
		"  if (Get-NetFirewallRule -DisplayName $fwRule -ErrorAction SilentlyContinue) {",
		"    Remove-NetFirewallRule -DisplayName $fwRule | Out-Null",
		"  }",
		"}",
	)
	return strings.Join(lines, "\r\n")
}

func runCmd(logf func(string), name string, args ...string) error {
	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmdline := shellJoin(append([]string{name}, args...)...)
		output, err := linuxAdminRunShLC(ctx, cmdline)
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("run %s %s (admin): timeout", name, strings.Join(args, " "))
		}
		clean := strings.TrimSpace(output)
		if logf != nil {
			if clean != "" {
				logf(fmt.Sprintf("[route] sudo %s %s => %s", name, strings.Join(args, " "), clean))
			} else {
				logf(fmt.Sprintf("[route] sudo %s %s", name, strings.Join(args, " ")))
			}
		}
		if err != nil {
			if clean != "" {
				return fmt.Errorf("run %s %s (admin): %w: %s", name, strings.Join(args, " "), err, clean)
			}
			return fmt.Errorf("run %s %s (admin): %w", name, strings.Join(args, " "), err)
		}
		return nil
	}
	if runtime.GOOS == "darwin" && os.Geteuid() != 0 {
		return runCmdDarwinAdmin(logf, name, args...)
	}
	return runCmdExec(logf, name, args...)
}

func runCmdExec(logf func(string), name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	clean := strings.TrimSpace(string(output))
	if logf != nil {
		if clean != "" {
			logf(fmt.Sprintf("[route] %s %s => %s", name, strings.Join(args, " "), clean))
		} else {
			logf(fmt.Sprintf("[route] %s %s", name, strings.Join(args, " ")))
		}
	}
	if err != nil {
		if clean != "" {
			return fmt.Errorf("run %s %s: %w: %s", name, strings.Join(args, " "), err, clean)
		}
		return fmt.Errorf("run %s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runCmdDarwinAdmin(logf func(string), name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmdline := shellJoin(append([]string{name}, args...)...)
	output, err := darwinAdminRunShLC(ctx, cmdline)
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("run %s %s (admin): timeout", name, strings.Join(args, " "))
	}
	clean := strings.TrimSpace(output)
	if logf != nil {
		if clean != "" {
			logf(fmt.Sprintf("[route] sudo %s %s => %s", name, strings.Join(args, " "), clean))
		} else {
			logf(fmt.Sprintf("[route] sudo %s %s", name, strings.Join(args, " ")))
		}
	}
	if err != nil {
		if clean != "" {
			return fmt.Errorf("run %s %s (admin): %w: %s", name, strings.Join(args, " "), err, clean)
		}
		return fmt.Errorf("run %s %s (admin): %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func runCmdsDarwinAdmin(logf func(string), cmdlines ...string) error {
	if len(cmdlines) == 0 {
		return nil
	}
	shell := "set -e; " + strings.Join(cmdlines, "; ")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	output, err := darwinAdminRunShLC(ctx, shell)
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("run (admin batch): timeout")
	}
	clean := strings.TrimSpace(output)
	if logf != nil {
		if clean != "" {
			logf(fmt.Sprintf("[route] sudo (batch) => %s", clean))
		} else {
			logf("[route] sudo (batch)")
		}
	}
	if err != nil {
		if clean != "" {
			return fmt.Errorf("run (admin batch): %w: %s", err, clean)
		}
		return fmt.Errorf("run (admin batch): %w", err)
	}
	return nil
}

func shellJoin(args ...string) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$&;|<>*?()[]{}!") {
		return s
	}
	// Single-quote with proper escaping: ' -> '"'"'
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func darwinDefaultRoute() (gateway string, iface string, err error) {
	cmd := exec.Command("route", "-n", "get", "default")
	output, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	s := bufio.NewScanner(strings.NewReader(string(output)))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "gateway:") {
			gateway = strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
		}
		if strings.HasPrefix(line, "interface:") {
			iface = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	if iface == "" {
		return "", "", errors.New("default interface not found")
	}
	return gateway, iface, nil
}

func linuxDefaultOutboundIPv4() (string, error) {
	cmd := exec.Command("ip", "-4", "route", "show", "default")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ip route default: %w: %s", err, strings.TrimSpace(string(output)))
	}
	line := strings.TrimSpace(string(output))
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	fields := strings.Fields(line)
	ifName := ""
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "dev" {
			ifName = strings.TrimSpace(fields[i+1])
			break
		}
	}
	if ifName == "" {
		return "", errors.New("default route interface not found")
	}
	ifi, err := net.InterfaceByName(ifName)
	if err != nil {
		return "", err
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok || ipNet == nil || ipNet.IP == nil {
			continue
		}
		ip4 := ipNet.IP.To4()
		if ip4 == nil || ip4.IsLoopback() {
			continue
		}
		return ip4.String(), nil
	}
	return "", errors.New("no ipv4 address found on default route interface")
}

func windowsDefaultGateway() (string, error) {
	gw, _, err := windowsPreferredDefaultRouteIPv4(0)
	return gw, err
}

func windowsDefaultInterfaceIndex() (int, error) {
	_, idx, err := windowsPreferredDefaultRouteIPv4(0)
	return idx, err
}

func windowsPreferredDefaultRouteIPv4(excludeIf int) (string, int, error) {
	script := strings.Join([]string{
		fmt.Sprintf("$exclude = %d", excludeIf),
		"$routes = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue",
		"if ($exclude -gt 0) { $routes = $routes | Where-Object { $_.InterfaceIndex -ne $exclude } }",
		"$sel = $routes | Where-Object { $_.NextHop -and $_.NextHop -ne '0.0.0.0' } | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1",
		"if ($sel -eq $null) { $sel = $routes | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1 }",
		"if ($sel -eq $null) { '' } else { \"$($sel.NextHop)`t$([int]$sel.InterfaceIndex)\" }",
	}, "; ")
	output, err := windowsPowerShellOutput(script)
	if err != nil {
		return "", 0, err
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return "", 0, errors.New("windows default route not found")
	}
	parts := strings.SplitN(raw, "\t", 2)
	gw := strings.TrimSpace(parts[0])
	if gw == "" {
		return "", 0, errors.New("windows default gateway not found")
	}
	idxRaw := raw
	if len(parts) == 2 {
		idxRaw = parts[1]
	}
	idx, err := parseFirstInt(idxRaw)
	if err != nil {
		return "", 0, err
	}
	return gw, idx, nil
}

func windowsInterfaceIndex(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("empty interface name")
	}
	// Use Get-NetAdapter first because a freshly-created Wintun adapter may not have
	// a NetIPInterface/IPv4 object immediately.
	safe := strings.ReplaceAll(name, "'", "''")
	script := strings.Join([]string{
		fmt.Sprintf("$name = '%s'", safe),
		"$a = Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object { $_.Name -eq $name -and $_.Status -eq 'Up' } | Select-Object -First 1",
		"if ($a -ne $null -and ($a.InterfaceDescription -match '(?i)wintun|wireguard|hev')) { [int]$a.ifIndex } else {",
		"  $ipif = Get-NetIPInterface -AddressFamily IPv4 -InterfaceAlias $name -ErrorAction SilentlyContinue | Select-Object -First 1",
		"  if ($ipif -eq $null) { '' } else { [int]$ipif.InterfaceIndex }",
		"}",
	}, "; ")
	output, err := windowsPowerShellOutput(script)
	if err != nil {
		return 0, err
	}
	return parseFirstInt(string(output))
}

func windowsResolveTunInterfaceIndex(tun TunSettings, timeout time.Duration) (int, string, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	tunIPv4 := strings.TrimSpace(tun.IPv4)
	for {
		// Prefer resolving the actual TUN interface by its configured IPv4. This avoids
		// accidentally picking an unrelated Wintun adapter (e.g. from other apps).
		if tunIPv4 != "" {
			if idx, alias, err := windowsInterfaceIndexByIPv4(tunIPv4); err == nil && idx > 0 {
				return idx, alias, nil
			} else if err != nil {
				lastErr = err
			}
		}

		if idx, err := windowsInterfaceIndex(tun.InterfaceName); err == nil && idx > 0 {
			alias := strings.TrimSpace(tun.InterfaceName)
			if tunIPv4 == "" {
				return idx, alias, nil
			}
			// Only accept the name match once the expected IPv4 shows up, otherwise we may
			// be racing adapter initialization.
			if idx2, alias2, err2 := windowsInterfaceIndexByIPv4(tunIPv4); err2 == nil && idx2 == idx {
				if strings.TrimSpace(alias2) != "" {
					alias = strings.TrimSpace(alias2)
				}
				return idx, alias, nil
			} else if err2 != nil {
				lastErr = err2
			}
		} else if err != nil {
			lastErr = err
		}

		// No reliable IPv4 configured; fall back to heuristics.
		if tunIPv4 == "" {
			if idx, alias, err := windowsLikelyTunInterfaceIndex(tun.InterfaceName); err == nil && idx > 0 {
				return idx, alias, nil
			} else if err != nil {
				lastErr = err
			}
		}

		if time.Now().After(deadline) {
			if lastErr != nil {
				return 0, "", fmt.Errorf("resolve windows tun interface index failed: %w", lastErr)
			}
			return 0, "", errors.New("resolve windows tun interface index failed")
		}
		time.Sleep(350 * time.Millisecond)
	}
}

func windowsInterfaceIndexByIPv4(ipv4 string) (int, string, error) {
	ipv4 = strings.TrimSpace(ipv4)
	if ipv4 == "" {
		return 0, "", errors.New("empty tun ipv4")
	}
	script := strings.Join([]string{
		"$ip = '" + strings.ReplaceAll(ipv4, "'", "''") + "'",
		"$addr = Get-NetIPAddress -AddressFamily IPv4 -IPAddress $ip -ErrorAction SilentlyContinue | Select-Object -First 1",
		"if ($addr -eq $null) { '' } else {",
		"  $ifx = [int]$addr.InterfaceIndex",
		"  $ad = (Get-NetAdapter -InterfaceIndex $ifx -ErrorAction SilentlyContinue | Select-Object -First 1)",
		"  if ($ad -eq $null -or $ad.Status -ne 'Up') { '' } else {",
		"  $alias = $ad.Name",
		"  if (-not $alias) { $alias = (Get-NetIPInterface -AddressFamily IPv4 -InterfaceIndex $ifx -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty InterfaceAlias) }",
		"  \"${ifx}`t${alias}\"",
		"  }",
		"}",
	}, "; ")
	output, err := windowsPowerShellOutput(script)
	if err != nil {
		return 0, "", err
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return 0, "", errors.New("tun interface by ipv4 not found")
	}
	parts := strings.SplitN(raw, "\t", 2)
	idx, err := parseFirstInt(parts[0])
	if err != nil {
		return 0, "", err
	}
	alias := ""
	if len(parts) == 2 {
		alias = strings.TrimSpace(parts[1])
	}
	return idx, alias, nil
}

func windowsLikelyTunInterfaceIndex(preferredName string) (int, string, error) {
	name := strings.ToLower(strings.TrimSpace(preferredName))
	script := strings.Join([]string{
		"$pref = '" + strings.ReplaceAll(name, "'", "''") + "'",
		"$cands = Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object { $_.Status -eq 'Up' -and (($_.Name -match '(?i)wintun|sudoku|hev') -or ($_.InterfaceDescription -match '(?i)wintun|wireguard|hev') -or ($pref -and $_.Name -eq $pref)) }",
		"$sel = $null",
		"if ($pref) { $sel = $cands | Where-Object { $_.Name -eq $pref } | Select-Object -First 1 }",
		"if ($sel -eq $null) { $sel = $cands | Sort-Object ifIndex -Descending | Select-Object -First 1 }",
		"if ($sel -eq $null) { '' } else { \"$($sel.ifIndex)`t$($sel.Name)\" }",
	}, "; ")
	output, err := windowsPowerShellOutput(script)
	if err != nil {
		return 0, "", err
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return 0, "", errors.New("likely tun interface not found")
	}
	parts := strings.SplitN(raw, "\t", 2)
	idx, err := parseFirstInt(parts[0])
	if err != nil {
		return 0, "", err
	}
	alias := ""
	if len(parts) == 2 {
		alias = strings.TrimSpace(parts[1])
	}
	return idx, alias, nil
}

func windowsPowerShellOutput(script string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	applyManagedProcessSysProcAttr(cmd)
	return cmd.CombinedOutput()
}

func linuxHasCommand(name string) bool {
	_, err := exec.LookPath(strings.TrimSpace(name))
	return err == nil
}

func parseFirstInt(raw string) (int, error) {
	re := regexp.MustCompile(`\d+`)
	m := re.FindString(raw)
	if m == "" {
		return 0, errors.New("integer not found")
	}
	idx, err := strconv.Atoi(m)
	if err != nil {
		return 0, err
	}
	return idx, nil
}
