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
	cmds := make([]string, 0, 8)
	if ctx.ServerIP != "" {
		cmds = append(cmds,
			shellJoin("route", "-n", "add", "-host", ctx.ServerIP, gw)+" || "+
				shellJoin("route", "-n", "change", "-host", ctx.ServerIP, gw),
		)
	}
	cmds = append(cmds, darwinAddTunnelRouteCommands(strings.TrimSpace(tun.InterfaceName))...)
	cmds = append(cmds, darwinAddPhysicalBypassRouteCommands(ctx.DefaultInterface, ctx.DefaultGateway)...)
	if pfSetCmd != "" {
		cmds = append(cmds, pfSetCmd)
	}
	if dnsSetCmd != "" {
		cmds = append(cmds, dnsSetCmd, dnsFlushCmd)
	}
	if err := runDarwinBatch(logf, cmds...); err != nil {
		_ = teardownRoutesDarwin(ctx, tun, logf)
		return nil, err
	}
	return ctx, nil
}

func teardownRoutesDarwin(ctx *routeContext, tun TunSettings, logf func(string)) error {
	tunIf := strings.TrimSpace(tun.InterfaceName)
	if runtime.GOOS == "darwin" {
		if actual := strings.TrimSpace(darwinFindTunInterfaceByIPv4(tun.IPv4)); actual != "" {
			tunIf = actual
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

	cmds := darwinDeleteTunnelRouteCommands(tunIf)
	cmds = append(cmds, darwinDeletePhysicalBypassRouteCommands(ctx.DefaultInterface, ctx.DefaultGateway)...)
	if strings.TrimSpace(ctx.ServerIP) != "" {
		cmds = append(cmds, shellJoin("route", "-n", "delete", "-host", strings.TrimSpace(ctx.ServerIP))+" >/dev/null 2>&1 || true")
	}

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
				cmds = append(cmds, shellJoin("networksetup", "-setdnsservers", svc, "Empty"))
			} else {
				args := append([]string{"-setdnsservers", svc}, snap.Servers...)
				cmds = append(cmds, shellJoin(append([]string{"networksetup"}, args...)...))
			}
		}
		cmds = append(cmds, dnsFlushCmd)
	}

	if strings.TrimSpace(ctx.PFAnchor) != "" {
		cmds = append(cmds, shellJoin("pfctl", "-a", strings.TrimSpace(ctx.PFAnchor), "-F", "all")+" >/dev/null 2>&1 || true")
	}
	return runDarwinBatch(logf, cmds...)
}

func darwinAddTunnelRouteCommands(tunIf string) []string {
	tunIf = strings.TrimSpace(tunIf)
	if tunIf == "" {
		return nil
	}
	return []string{
		shellJoin("route", "-n", "delete", "-net", "0.0.0.0/1", "-interface", tunIf) + " >/dev/null 2>&1 || true",
		shellJoin("route", "-n", "delete", "-net", "128.0.0.0/1", "-interface", tunIf) + " >/dev/null 2>&1 || true",
		shellJoin("route", "-n", "add", "-net", "0.0.0.0/1", "-interface", tunIf),
		shellJoin("route", "-n", "add", "-net", "128.0.0.0/1", "-interface", tunIf),
	}
}

func darwinDeleteTunnelRouteCommands(tunIf string) []string {
	tunIf = strings.TrimSpace(tunIf)
	if tunIf == "" {
		return nil
	}
	return []string{
		shellJoin("route", "-n", "delete", "-net", "0.0.0.0/1", "-interface", tunIf) + " >/dev/null 2>&1 || true",
		shellJoin("route", "-n", "delete", "-net", "128.0.0.0/1", "-interface", tunIf) + " >/dev/null 2>&1 || true",
	}
}

func darwinAddPhysicalBypassRouteCommands(ifName string, gateway string) []string {
	ifName = strings.TrimSpace(ifName)
	gateway = strings.TrimSpace(gateway)
	if ifName == "" || gateway == "" {
		return nil
	}
	cmds := make([]string, 0, 4)
	for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		cmds = append(cmds,
			shellJoin("route", "-n", "delete", "-net", "-ifscope", ifName, prefix, gateway)+" >/dev/null 2>&1 || true",
			shellJoin("route", "-n", "add", "-net", "-ifscope", ifName, prefix, gateway),
		)
	}
	return cmds
}

func darwinDeletePhysicalBypassRouteCommands(ifName string, gateway string) []string {
	ifName = strings.TrimSpace(ifName)
	gateway = strings.TrimSpace(gateway)
	if ifName == "" || gateway == "" {
		return nil
	}
	cmds := make([]string, 0, 2)
	for _, prefix := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		cmds = append(cmds,
			shellJoin("route", "-n", "delete", "-net", "-ifscope", ifName, prefix, gateway)+" >/dev/null 2>&1 || true",
		)
	}
	return cmds
}

func runDarwinBatch(logf func(string), cmds ...string) error {
	if len(cmds) == 0 {
		return nil
	}
	if os.Geteuid() != 0 {
		return runCmdsDarwinAdmin(logf, cmds...)
	}
	return runCmdExec(logf, "sh", "-lc", "set -e; "+strings.Join(cmds, "; "))
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
