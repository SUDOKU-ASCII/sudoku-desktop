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
	DefaultGatewayV6      string
	DefaultInterface      string
	ServerIP              string
	TunIndex              int
	DNSService            string
	DNSServers            []string
	DNSWasAutomatic       bool
	PFAnchor              string
	BypassV4Path          string
	BypassV6Path          string
	LinuxOutboundSrcIP    string
	LinuxBypassMark       int
	LinuxBypassSet4       string
	LinuxBypassSet6       string
	LinuxDNSMode          string
	LinuxResolvConfBackup string
	LinuxDNSRedirectPort  int
	WindowsFirewallRule   string
	WindowsDNSBackup      string
}

func darwinTunIPv6Enabled() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return strings.TrimSpace(os.Getenv("SUDOKU_DARWIN_TUN_IPV6")) == "1"
}

func setupRoutes(activeNode NodeConfig, tun TunSettings, routing RoutingSettings, bypass tunBypass, logf func(string)) (*routeContext, error) {
	ctx := &routeContext{}
	ctx.ServerIP = resolveServerIPFromAddress(activeNode.ServerAddress)
	ctx.BypassV4Path = strings.TrimSpace(bypass.V4Path)
	ctx.BypassV6Path = strings.TrimSpace(bypass.V6Path)
	switch runtime.GOOS {
	case "linux":
		return setupRoutesLinux(ctx, tun, logf)
	case "darwin":
		_ = routing
		return setupRoutesDarwin(ctx, tun, logf)
	case "windows":
		_ = routing
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
		return ip.String()
	}
	ips, _ := net.LookupIP(host)
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	if len(ips) > 0 {
		return ips[0].String()
	}
	return ""
}

func teardownRoutes(ctx *routeContext, tun TunSettings, logf func(string)) {
	if ctx == nil {
		return
	}
	switch runtime.GOOS {
	case "linux":
		teardownRoutesLinux(ctx, tun, logf)
	case "darwin":
		teardownRoutesDarwin(ctx, tun, logf)
	case "windows":
		teardownRoutesWindows(ctx, tun, logf)
	}
}

func setupRoutesLinux(ctx *routeContext, tun TunSettings, logf func(string)) (*routeContext, error) {
	uid := os.Getuid()
	bypassMark := tun.SocksMark + 1
	if bypassMark <= 0 {
		bypassMark = 439
	}
	ctx.LinuxBypassMark = bypassMark
	ctx.LinuxBypassSet4 = fmt.Sprintf("sudoku4x4_cn4_%d", uid)
	ctx.LinuxBypassSet6 = fmt.Sprintf("sudoku4x4_cn6_%d", uid)

	cmdlines := make([]string, 0, 32)
	if ctx.ServerIP != "" {
		if ip := net.ParseIP(ctx.ServerIP); ip != nil && ip.To4() != nil {
			cmdlines = append(cmdlines, shellJoin("ip", "rule", "add", "to", ctx.ServerIP, "lookup", "main", "pref", "5")+" || true")
		} else {
			cmdlines = append(cmdlines, shellJoin("ip", "-6", "rule", "add", "to", ctx.ServerIP, "lookup", "main", "pref", "5")+" || true")
		}
	}

	// Ensure the core process can bypass the TUN by binding to the physical source IP.
	if srcIP, err := linuxDefaultOutboundIPv4(); err == nil && strings.TrimSpace(srcIP) != "" {
		ctx.LinuxOutboundSrcIP = strings.TrimSpace(srcIP)
		cmdlines = append(cmdlines, shellJoin("ip", "rule", "add", "from", ctx.LinuxOutboundSrcIP, "lookup", "main", "pref", "8")+" || true")
	}

	// PAC-mode loop avoidance: bypass CN CIDRs to the main routing table.
	if strings.TrimSpace(ctx.BypassV4Path) != "" || strings.TrimSpace(ctx.BypassV6Path) != "" {
		if strings.TrimSpace(ctx.BypassV4Path) != "" {
			cmdlines = append(cmdlines,
				shellJoin("ipset", "create", ctx.LinuxBypassSet4, "hash:net", "family", "inet", "-exist"),
				shellJoin("ipset", "flush", ctx.LinuxBypassSet4)+" || true",
				"if [ -f "+shellQuote(ctx.BypassV4Path)+" ]; then while IFS= read -r cidr; do [ -z \"$cidr\" ] && continue; ipset add "+shellQuote(ctx.LinuxBypassSet4)+" \"$cidr\" -exist || true; done < "+shellQuote(ctx.BypassV4Path)+"; fi",
				"iptables -t mangle -C OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet4)+" dst -j MARK --set-mark "+strconv.Itoa(bypassMark)+" >/dev/null 2>&1 || "+
					"iptables -t mangle -A OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet4)+" dst -j MARK --set-mark "+strconv.Itoa(bypassMark),
			)
		}
		if strings.TrimSpace(ctx.BypassV6Path) != "" {
			cmdlines = append(cmdlines,
				shellJoin("ipset", "create", ctx.LinuxBypassSet6, "hash:net", "family", "inet6", "-exist"),
				shellJoin("ipset", "flush", ctx.LinuxBypassSet6)+" || true",
				"if [ -f "+shellQuote(ctx.BypassV6Path)+" ]; then while IFS= read -r cidr; do [ -z \"$cidr\" ] && continue; ipset add "+shellQuote(ctx.LinuxBypassSet6)+" \"$cidr\" -exist || true; done < "+shellQuote(ctx.BypassV6Path)+"; fi",
				"ip6tables -t mangle -C OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet6)+" dst -j MARK --set-mark "+strconv.Itoa(bypassMark)+" >/dev/null 2>&1 || "+
					"ip6tables -t mangle -A OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet6)+" dst -j MARK --set-mark "+strconv.Itoa(bypassMark),
			)
		}
		cmdlines = append(cmdlines,
			shellJoin("ip", "rule", "add", "fwmark", strconv.Itoa(bypassMark), "lookup", "main", "pref", "15")+" || true",
			shellJoin("ip", "-6", "rule", "add", "fwmark", strconv.Itoa(bypassMark), "lookup", "main", "pref", "15")+" || true",
		)
	}

	// Optional: block QUIC (UDP/443).
	if tun.BlockQUIC {
		cmdlines = append(cmdlines,
			"iptables -C OUTPUT -p udp --dport 443 -j DROP >/dev/null 2>&1 || iptables -I OUTPUT 1 -p udp --dport 443 -j DROP",
			"ip6tables -C OUTPUT -p udp --dport 443 -j DROP >/dev/null 2>&1 || ip6tables -I OUTPUT 1 -p udp --dport 443 -j DROP",
		)
	}

	// Optional: switch system DNS to HEV MapDNS while TUN is active (FakeIP mode).
	if tun.MapDNSEnabled && strings.TrimSpace(tun.MapDNSAddress) != "" {
		dnsAddr := strings.TrimSpace(tun.MapDNSAddress)
		if dnsAddr == localDNSServerIPv4 && localDNSProxyListenPort() != 53 {
			ctx.LinuxDNSRedirectPort = localDNSProxyListenPort()
			cmdlines = append(cmdlines,
				"iptables -t nat -C OUTPUT -p udp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort)+" >/dev/null 2>&1 || "+
					"iptables -t nat -I OUTPUT 1 -p udp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort),
				"iptables -t nat -C OUTPUT -p tcp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort)+" >/dev/null 2>&1 || "+
					"iptables -t nat -I OUTPUT 1 -p tcp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort),
			)
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
		shellJoin("ip", "-6", "rule", "add", "fwmark", strconv.Itoa(tun.SocksMark), "lookup", "main", "pref", "10")+" || true",
		shellJoin("ip", "route", "add", "default", "dev", tun.InterfaceName, "table", strconv.Itoa(tun.RouteTable))+" || true",
		shellJoin("ip", "rule", "add", "lookup", strconv.Itoa(tun.RouteTable), "pref", "20")+" || true",
		shellJoin("ip", "-6", "route", "add", "default", "dev", tun.InterfaceName, "table", strconv.Itoa(tun.RouteTable))+" || true",
		shellJoin("ip", "-6", "rule", "add", "lookup", strconv.Itoa(tun.RouteTable), "pref", "20")+" || true",
	)

	if err := runCmdsLinuxAdmin(logf, cmdlines...); err != nil {
		// Best-effort cleanup to avoid leaving the system half-configured.
		teardownRoutesLinux(ctx, tun, logf)
		return nil, err
	}
	return ctx, nil
}

func teardownRoutesLinux(ctx *routeContext, tun TunSettings, logf func(string)) {
	cmdlines := make([]string, 0, 32)
	if ctx != nil && ctx.ServerIP != "" {
		if ip := net.ParseIP(ctx.ServerIP); ip != nil && ip.To4() != nil {
			cmdlines = append(cmdlines, shellJoin("ip", "rule", "del", "to", ctx.ServerIP, "lookup", "main", "pref", "5")+" || true")
		} else {
			cmdlines = append(cmdlines, shellJoin("ip", "-6", "rule", "del", "to", ctx.ServerIP, "lookup", "main", "pref", "5")+" || true")
		}
	}
	if ctx != nil && strings.TrimSpace(ctx.LinuxOutboundSrcIP) != "" {
		cmdlines = append(cmdlines, shellJoin("ip", "rule", "del", "from", ctx.LinuxOutboundSrcIP, "lookup", "main", "pref", "8")+" || true")
	}
	if ctx != nil && ctx.LinuxBypassMark > 0 {
		cmdlines = append(cmdlines,
			shellJoin("ip", "rule", "del", "fwmark", strconv.Itoa(ctx.LinuxBypassMark), "lookup", "main", "pref", "15")+" || true",
			shellJoin("ip", "-6", "rule", "del", "fwmark", strconv.Itoa(ctx.LinuxBypassMark), "lookup", "main", "pref", "15")+" || true",
		)
		if strings.TrimSpace(ctx.LinuxBypassSet4) != "" {
			cmdlines = append(cmdlines,
				"iptables -t mangle -D OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet4)+" dst -j MARK --set-mark "+strconv.Itoa(ctx.LinuxBypassMark)+" >/dev/null 2>&1 || true",
				shellJoin("ipset", "destroy", ctx.LinuxBypassSet4)+" || true",
			)
		}
		if strings.TrimSpace(ctx.LinuxBypassSet6) != "" {
			cmdlines = append(cmdlines,
				"ip6tables -t mangle -D OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet6)+" dst -j MARK --set-mark "+strconv.Itoa(ctx.LinuxBypassMark)+" >/dev/null 2>&1 || true",
				shellJoin("ipset", "destroy", ctx.LinuxBypassSet6)+" || true",
			)
		}
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
	}
	if ctx != nil && ctx.LinuxDNSRedirectPort > 0 {
		cmdlines = append(cmdlines,
			"iptables -t nat -D OUTPUT -p udp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort)+" >/dev/null 2>&1 || true",
			"iptables -t nat -D OUTPUT -p tcp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort)+" >/dev/null 2>&1 || true",
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
}

func setupRoutesDarwin(ctx *routeContext, tun TunSettings, logf func(string)) (*routeContext, error) {
	gw, ifName, err := darwinDefaultRoute()
	if err != nil {
		return nil, err
	}
	ctx.DefaultGateway = gw
	ctx.DefaultInterface = ifName
	if darwinTunIPv6Enabled() {
		gw6, ifName6, _ := darwinDefaultRouteIPv6()
		ctx.DefaultGatewayV6 = strings.TrimSpace(gw6)
		if strings.TrimSpace(ctx.DefaultInterface) == "" && strings.TrimSpace(ifName6) != "" {
			ctx.DefaultInterface = strings.TrimSpace(ifName6)
		}
	}
	dnsFlushCmd := "dscacheutil -flushcache >/dev/null 2>&1 || true; killall -HUP mDNSResponder >/dev/null 2>&1 || true"

	// Optional: switch system DNS to HEV MapDNS while TUN is active (for correct PAC/domain routing).
	dnsSetCmd := ""
	if tun.MapDNSEnabled && strings.TrimSpace(tun.MapDNSAddress) != "" && strings.TrimSpace(ctx.DefaultInterface) != "" {
		if svc, derr := darwinNetworkServiceForDevice(ctx.DefaultInterface); derr == nil && strings.TrimSpace(svc) != "" {
			ctx.DNSService = svc
			prev, wasAuto, gerr := darwinGetDNSServers(svc)
			if gerr == nil {
				ctx.DNSServers = prev
				ctx.DNSWasAutomatic = wasAuto
			}
			dnsSetCmd = shellJoin("networksetup", "-setdnsservers", svc, strings.TrimSpace(tun.MapDNSAddress))
		}
	}

	pfSetCmd := ""
	dnsProxyPort := 0
	if strings.TrimSpace(tun.MapDNSAddress) == localDNSServerIPv4 {
		dnsProxyPort = localDNSProxyListenPort()
	}
	if runtime.GOOS == "darwin" && (tun.BlockQUIC || strings.TrimSpace(ctx.BypassV4Path) != "" || strings.TrimSpace(ctx.BypassV6Path) != "" || dnsProxyPort > 0) {
		ctx.PFAnchor = fmt.Sprintf("com.apple/sudoku4x4.tun.%d", os.Getuid())
		pfSetCmd = darwinBuildPFSetCmd(ctx.PFAnchor, tun.InterfaceName, ctx.DefaultInterface, gw, ctx.DefaultGatewayV6, ctx.BypassV4Path, ctx.BypassV6Path, tun.BlockQUIC, dnsProxyPort)
	}
	if runtime.GOOS == "darwin" && os.Geteuid() != 0 {
		cmds := make([]string, 0, 7)
		if ctx.DefaultInterface != "" && ctx.DefaultGateway != "" {
			// Keep a physical scoped default route for the core process (it binds sockets to DefaultInterface).
			cmds = append(cmds, shellJoin("route", "-n", "add", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1 || true")
		}
		if ctx.DefaultInterface != "" && ctx.DefaultGatewayV6 != "" {
			// Keep a physical scoped IPv6 default route for direct sockets bound to DefaultInterface.
			cmds = append(cmds, shellJoin("route", "-n", "add", "-inet6", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGatewayV6)+" >/dev/null 2>&1 || true")
		}
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
		if ctx.DefaultGatewayV6 != "" {
			// IPv6 default route may fail depending on system state; ignore.
			cmds = append(cmds, shellJoin("route", "-n", "change", "-inet6", "default", "-interface", tun.InterfaceName)+" || true")
		}
		if pfSetCmd != "" {
			cmds = append(cmds, pfSetCmd)
		}
		if dnsSetCmd != "" {
			cmds = append(cmds, dnsSetCmd)
			cmds = append(cmds, dnsFlushCmd)
		}
		if err := runCmdsDarwinAdmin(logf, cmds...); err != nil {
			return nil, err
		}
		return ctx, nil
	}
	if ctx.ServerIP != "" {
		// Be idempotent: a host route may already exist (e.g. from a prior run or system clone).
		_ = runCmd(logf, "route", "-n", "add", "-host", ctx.ServerIP, gw)
		_ = runCmd(logf, "route", "-n", "change", "-host", ctx.ServerIP, gw)
	}
	if ctx.DefaultInterface != "" && ctx.DefaultGateway != "" {
		_ = runCmd(logf, "sh", "-lc", shellJoin("route", "-n", "add", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1 || true")
	}
	if ctx.DefaultInterface != "" && ctx.DefaultGatewayV6 != "" {
		_ = runCmd(logf, "sh", "-lc", shellJoin("route", "-n", "add", "-inet6", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGatewayV6)+" >/dev/null 2>&1 || true")
	}
	if err := runCmd(logf, "route", "-n", "change", "default", "-interface", tun.InterfaceName); err != nil {
		// Some macOS setups have multiple scoped default routes; if `change` fails, recreate it.
		_ = runCmd(logf, "route", "-n", "delete", "default")
		if err2 := runCmd(logf, "route", "-n", "add", "default", "-interface", tun.InterfaceName); err2 != nil {
			return nil, err
		}
	}
	if ctx.DefaultGatewayV6 != "" {
		_ = runCmd(logf, "route", "-n", "change", "-inet6", "default", "-interface", tun.InterfaceName)
	}
	if pfSetCmd != "" {
		if err := runCmd(logf, "sh", "-lc", pfSetCmd); err != nil {
			return nil, err
		}
	}
	if dnsSetCmd != "" {
		if err := runCmd(logf, "networksetup", "-setdnsservers", ctx.DNSService, strings.TrimSpace(tun.MapDNSAddress)); err != nil {
			return nil, err
		}
		_ = runCmd(logf, "sh", "-lc", dnsFlushCmd)
	}
	return ctx, nil
}

func teardownRoutesDarwin(ctx *routeContext, _ TunSettings, logf func(string)) {
	if runtime.GOOS == "darwin" && os.Geteuid() != 0 {
		dnsFlushCmd := "dscacheutil -flushcache >/dev/null 2>&1 || true; killall -HUP mDNSResponder >/dev/null 2>&1 || true"
		cmds := make([]string, 0, 6)
		if ctx.DefaultGateway != "" {
			cmds = append(cmds, shellJoin("route", "-n", "change", "default", ctx.DefaultGateway)+" || ("+
				shellJoin("route", "-n", "delete", "default")+" >/dev/null 2>&1 || true; "+
				shellJoin("route", "-n", "add", "default", ctx.DefaultGateway)+")")
		}
		if strings.TrimSpace(ctx.DefaultGatewayV6) != "" {
			cmds = append(cmds, shellJoin("route", "-n", "change", "-inet6", "default", ctx.DefaultGatewayV6)+" || true")
		}
		// Always attempt to remove the host route (if we added one), regardless of gateway changes.
		if ctx.ServerIP != "" {
			cmds = append(cmds, shellJoin("route", "-n", "delete", "-host", ctx.ServerIP)+" || true")
		}
		if strings.TrimSpace(ctx.DNSService) != "" {
			if ctx.DNSWasAutomatic || len(ctx.DNSServers) == 0 {
				cmds = append(cmds, shellJoin("networksetup", "-setdnsservers", ctx.DNSService, "Empty")+" || true")
			} else {
				args := append([]string{"networksetup", "-setdnsservers", ctx.DNSService}, ctx.DNSServers...)
				cmds = append(cmds, shellJoin(args...)+" || true")
			}
			cmds = append(cmds, dnsFlushCmd)
		}
		if strings.TrimSpace(ctx.PFAnchor) != "" {
			cmds = append(cmds, shellJoin("pfctl", "-a", ctx.PFAnchor, "-F", "all")+" || true")
		}
		if len(cmds) > 0 {
			_ = runCmdsDarwinAdmin(logf, cmds...)
		}
		return
	}
	if ctx.DefaultGateway != "" {
		if err := runCmd(logf, "route", "-n", "change", "default", ctx.DefaultGateway); err != nil {
			_ = runCmd(logf, "route", "-n", "delete", "default")
			_ = runCmd(logf, "route", "-n", "add", "default", ctx.DefaultGateway)
		}
	}
	if strings.TrimSpace(ctx.DefaultGatewayV6) != "" {
		_ = runCmd(logf, "route", "-n", "change", "-inet6", "default", ctx.DefaultGatewayV6)
	}
	if ctx.ServerIP != "" {
		_ = runCmd(logf, "route", "-n", "delete", "-host", ctx.ServerIP)
	}
	if strings.TrimSpace(ctx.DNSService) != "" {
		if ctx.DNSWasAutomatic || len(ctx.DNSServers) == 0 {
			_ = runCmd(logf, "networksetup", "-setdnsservers", ctx.DNSService, "Empty")
		} else {
			args := append([]string{"-setdnsservers", ctx.DNSService}, ctx.DNSServers...)
			_ = runCmd(logf, "networksetup", args...)
		}
		_ = runCmd(logf, "sh", "-lc", "dscacheutil -flushcache >/dev/null 2>&1 || true; killall -HUP mDNSResponder >/dev/null 2>&1 || true")
	}
	if strings.TrimSpace(ctx.PFAnchor) != "" {
		_ = runCmd(logf, "pfctl", "-a", ctx.PFAnchor, "-F", "all")
	}
}

func setupRoutesWindows(ctx *routeContext, tun TunSettings, logf func(string)) (*routeContext, error) {
	gw, err := windowsDefaultGateway()
	if err != nil {
		return nil, err
	}
	ctx.DefaultGateway = gw
	idx, alias, err := windowsResolveTunInterfaceIndex(tun, 10*time.Second)
	if err != nil {
		return nil, err
	}
	ctx.TunIndex = idx
	if logf != nil {
		if strings.TrimSpace(alias) != "" {
			logf(fmt.Sprintf("[route] windows tun interface: %s (ifindex=%d)", alias, idx))
		} else {
			logf(fmt.Sprintf("[route] windows tun ifindex=%d", idx))
		}
	}
	firewallRule := "4x4-sudoku Block QUIC (UDP/443)"
	if tun.BlockQUIC {
		ctx.WindowsFirewallRule = firewallRule
	}

	dnsBackupName := ""
	if tun.MapDNSEnabled && strings.TrimSpace(tun.MapDNSAddress) != "" {
		dnsBackupName = fmt.Sprintf("sudoku4x4-dns-%d.json", os.Getuid())
		ctx.WindowsDNSBackup = dnsBackupName
	}
	ps := buildWindowsRouteScript(true, ctx.ServerIP, ctx.BypassV4Path, ctx.BypassV6Path, firewallRule, tun.BlockQUIC, idx, tun.MapDNSEnabled, strings.TrimSpace(tun.MapDNSAddress), dnsBackupName)
	if err := runCmdsWindowsAdmin(logf, ps); err != nil {
		return nil, err
	}
	return ctx, nil
}

func teardownRoutesWindows(ctx *routeContext, tun TunSettings, logf func(string)) {
	if ctx == nil {
		return
	}
	firewallRule := ctx.WindowsFirewallRule
	if firewallRule == "" {
		firewallRule = "4x4-sudoku Block QUIC (UDP/443)"
	}
	// Restore DNS whenever we backed it up during start (we always do so in TUN mode).
	mapDNSEnabled := strings.TrimSpace(ctx.WindowsDNSBackup) != ""
	ps := buildWindowsRouteScript(false, ctx.ServerIP, ctx.BypassV4Path, ctx.BypassV6Path, firewallRule, tun.BlockQUIC, ctx.TunIndex, mapDNSEnabled, localDNSServerIPv4, ctx.WindowsDNSBackup)
	_ = runCmdsWindowsAdmin(logf, ps)
}

func runCmdsLinuxAdmin(logf func(string), cmdlines ...string) error {
	if len(cmdlines) == 0 {
		return nil
	}
	shell := "set -e; PATH=/usr/sbin:/sbin:/usr/bin:/bin:$PATH; " + strings.Join(cmdlines, "; ")
	if os.Geteuid() == 0 {
		return runCmdExec(logf, "sh", "-lc", shell)
	}
	if _, err := exec.LookPath("pkexec"); err == nil {
		return runCmdExec(logf, "pkexec", "sh", "-lc", shell)
	}
	// No elevation helper available; run directly (will likely fail).
	return runCmdExec(logf, "sh", "-lc", shell)
}

func runCmdsWindowsAdmin(logf func(string), scriptBody string) error {
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
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", path)
	output, err := cmd.CombinedOutput()
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

func buildWindowsRouteScript(start bool, serverIP string, bypassV4 string, bypassV6 string, firewallRule string, blockQUIC bool, tunIfIndex int, mapDNSEnabled bool, mapDNSAddress string, dnsBackupName string) string {
	op := "start"
	if !start {
		op = "stop"
	}
	bypassV4 = strings.TrimSpace(bypassV4)
	bypassV6 = strings.TrimSpace(bypassV6)
	serverIP = strings.TrimSpace(serverIP)
	firewallRule = strings.TrimSpace(firewallRule)
	mapDNSAddress = strings.TrimSpace(mapDNSAddress)
	dnsBackupName = strings.TrimSpace(dnsBackupName)
	if firewallRule == "" {
		firewallRule = "4x4-sudoku Block QUIC (UDP/443)"
	}

	// Use ActiveStore so routes/rules are not persisted across reboot.
	lines := []string{
		fmt.Sprintf("$op = '%s'", op),
		"$default4 = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Sort-Object RouteMetric | Select-Object -First 1",
		"$gw4 = $default4.NextHop",
		"$if4 = $default4.InterfaceIndex",
		"$default6 = Get-NetRoute -AddressFamily IPv6 -DestinationPrefix '::/0' -ErrorAction SilentlyContinue | Sort-Object RouteMetric | Select-Object -First 1",
		"$gw6 = $default6.NextHop",
		"$if6 = $default6.InterfaceIndex",
		fmt.Sprintf("$tunIf = %d", tunIfIndex),
	}
	if serverIP != "" {
		lines = append(lines, fmt.Sprintf("$serverIP = '%s'", strings.ReplaceAll(serverIP, "'", "''")))
	} else {
		lines = append(lines, "$serverIP = ''")
	}
	lines = append(lines,
		fmt.Sprintf("$bypassV4 = '%s'", strings.ReplaceAll(bypassV4, "'", "''")),
		fmt.Sprintf("$bypassV6 = '%s'", strings.ReplaceAll(bypassV6, "'", "''")),
		fmt.Sprintf("$fwRule = '%s'", strings.ReplaceAll(firewallRule, "'", "''")),
		fmt.Sprintf("$blockQUIC = %s", map[bool]string{true: "$true", false: "$false"}[blockQUIC]),
		fmt.Sprintf("$mapDNSEnabled = %s", map[bool]string{true: "$true", false: "$false"}[mapDNSEnabled]),
		fmt.Sprintf("$mapDNS = '%s'", strings.ReplaceAll(mapDNSAddress, "'", "''")),
		fmt.Sprintf("$dnsBackupName = '%s'", strings.ReplaceAll(dnsBackupName, "'", "''")),
		"$physMetric = 5000",
		"",
		"function Add-RoutePrefix($prefix, $ifIndex, $gw) {",
		"  try {",
		"    if (-not $prefix) { return }",
		"    New-NetRoute -DestinationPrefix $prefix -InterfaceIndex $ifIndex -NextHop $gw -PolicyStore ActiveStore -ErrorAction Stop | Out-Null",
		"  } catch { }",
		"}",
		"function Remove-RoutePrefix($prefix, $ifIndex, $gw) {",
		"  try {",
		"    if (-not $prefix) { return }",
		"    Remove-NetRoute -DestinationPrefix $prefix -InterfaceIndex $ifIndex -NextHop $gw -PolicyStore ActiveStore -Confirm:$false -ErrorAction Stop",
		"  } catch { }",
		"}",
		"",
		"$dnsBackup = ''",
		"if ($dnsBackupName) { $dnsBackup = Join-Path $env:TEMP $dnsBackupName }",
		"",
		"if ($op -eq 'start') {",
		"  if ($serverIP) {",
		"    if ($serverIP -match ':') { Add-RoutePrefix ($serverIP + '/128') $if6 $gw6 } else { Add-RoutePrefix ($serverIP + '/32') $if4 $gw4 }",
		"  }",
		"  if ($bypassV4 -and (Test-Path $bypassV4)) {",
		"    Get-Content $bypassV4 | ForEach-Object { $p = $_.Trim(); if ($p) { Add-RoutePrefix $p $if4 $gw4 } }",
		"  }",
		"  if ($bypassV6 -and (Test-Path $bypassV6)) {",
		"    Get-Content $bypassV6 | ForEach-Object { $p = $_.Trim(); if ($p) { Add-RoutePrefix $p $if6 $gw6 } }",
		"  }",
		"  if ($mapDNSEnabled -and $mapDNS) {",
		"    $prev4 = @((Get-DnsClientServerAddress -InterfaceIndex $tunIf -AddressFamily IPv4 -ErrorAction SilentlyContinue).ServerAddresses)",
		"    $prev6 = @((Get-DnsClientServerAddress -InterfaceIndex $tunIf -AddressFamily IPv6 -ErrorAction SilentlyContinue).ServerAddresses)",
		"    if ($dnsBackup) {",
		"      @{ v4 = $prev4; v6 = $prev6 } | ConvertTo-Json -Compress | Set-Content -Path $dnsBackup -Encoding ASCII",
		"    }",
		"    Set-DnsClientServerAddress -InterfaceIndex $tunIf -AddressFamily IPv4 -ServerAddresses @($mapDNS) -ErrorAction SilentlyContinue | Out-Null",
		"    try { Clear-DnsClientCache | Out-Null } catch { }",
		"  }",
		"  & route.exe change 0.0.0.0 mask 0.0.0.0 0.0.0.0 if $tunIf | Out-Null",
		"  # Keep a physical default route for core-bypass sockets (IP_UNICAST_IF).",
		"  try { New-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $if4 -NextHop $gw4 -RouteMetric $physMetric -PolicyStore ActiveStore -ErrorAction Stop | Out-Null } catch { }",
		"  try { if ($gw6) { New-NetRoute -DestinationPrefix '::/0' -InterfaceIndex $if6 -NextHop $gw6 -RouteMetric $physMetric -PolicyStore ActiveStore -ErrorAction Stop | Out-Null } } catch { }",
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
		"      if ($p4.Count -eq 0) { Set-DnsClientServerAddress -InterfaceIndex $tunIf -AddressFamily IPv4 -ResetServerAddresses -ErrorAction SilentlyContinue | Out-Null } else { Set-DnsClientServerAddress -InterfaceIndex $tunIf -AddressFamily IPv4 -ServerAddresses $p4 -ErrorAction SilentlyContinue | Out-Null }",
		"      $p6 = @($json.v6)",
		"      if ($p6.Count -eq 0) { Set-DnsClientServerAddress -InterfaceIndex $tunIf -AddressFamily IPv6 -ResetServerAddresses -ErrorAction SilentlyContinue | Out-Null } else { Set-DnsClientServerAddress -InterfaceIndex $tunIf -AddressFamily IPv6 -ServerAddresses $p6 -ErrorAction SilentlyContinue | Out-Null }",
		"    } else {",
		"      Set-DnsClientServerAddress -InterfaceIndex $tunIf -ResetServerAddresses -ErrorAction SilentlyContinue | Out-Null",
		"    }",
		"    Remove-Item $dnsBackup -Force -ErrorAction SilentlyContinue | Out-Null",
		"  } elseif ($mapDNSEnabled) {",
		"    Set-DnsClientServerAddress -InterfaceIndex $tunIf -ResetServerAddresses -ErrorAction SilentlyContinue | Out-Null",
		"  }",
		"  try { Clear-DnsClientCache | Out-Null } catch { }",
		"  # Remove the auxiliary physical default route (if we added it).",
		"  try { Get-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $if4 -NextHop $gw4 -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object { $_.RouteMetric -eq $physMetric } | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue } catch { }",
		"  try { if ($gw6) { Get-NetRoute -DestinationPrefix '::/0' -InterfaceIndex $if6 -NextHop $gw6 -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object { $_.RouteMetric -eq $physMetric } | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue } } catch { }",
		"  & route.exe change 0.0.0.0 mask 0.0.0.0 $gw4 | Out-Null",
		"  if ($serverIP) {",
		"    if ($serverIP -match ':') { Remove-RoutePrefix ($serverIP + '/128') $if6 $gw6 } else { Remove-RoutePrefix ($serverIP + '/32') $if4 $gw4 }",
		"  }",
		"  if ($bypassV4 -and (Test-Path $bypassV4)) {",
		"    Get-Content $bypassV4 | ForEach-Object { $p = $_.Trim(); if ($p) { Remove-RoutePrefix $p $if4 $gw4 } }",
		"  }",
		"  if ($bypassV6 -and (Test-Path $bypassV6)) {",
		"    Get-Content $bypassV6 | ForEach-Object { $p = $_.Trim(); if ($p) { Remove-RoutePrefix $p $if6 $gw6 } }",
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
		if _, err := exec.LookPath("pkexec"); err == nil {
			return runCmdExec(logf, "pkexec", append([]string{name}, args...)...)
		}
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
	cmdline := shellJoin(append([]string{name}, args...)...)
	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, appleScriptEscape(cmdline))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("run %s %s (admin): timeout", name, strings.Join(args, " "))
	}
	clean := strings.TrimSpace(string(output))
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
	cmdline := shellJoin("sh", "-lc", shell)
	script := fmt.Sprintf(`do shell script "%s" with administrator privileges`, appleScriptEscape(cmdline))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New("run (admin batch): timeout")
	}
	clean := strings.TrimSpace(string(output))
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

func appleScriptEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
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
	if gateway == "" {
		return "", "", errors.New("gateway not found")
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
	output, err := windowsPowerShellOutput("(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1).NextHop")
	if err != nil {
		return "", err
	}
	gw := strings.TrimSpace(string(output))
	if gw == "" {
		return "", errors.New("windows default gateway not found")
	}
	return gw, nil
}

func windowsDefaultInterfaceIndex() (int, error) {
	output, err := windowsPowerShellOutput("(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1).InterfaceIndex")
	if err != nil {
		return 0, err
	}
	return parseFirstInt(string(output))
}

func windowsInterfaceIndex(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("empty interface name")
	}
	script := fmt.Sprintf("(Get-NetIPInterface -AddressFamily IPv4 -InterfaceAlias '%s' -ErrorAction SilentlyContinue | Select-Object -First 1).InterfaceIndex", strings.ReplaceAll(name, "'", "''"))
	output, err := windowsPowerShellOutput(script)
	if err != nil {
		return 0, err
	}
	return parseFirstInt(string(output))
}

func windowsResolveTunInterfaceIndex(tun TunSettings, timeout time.Duration) (int, string, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		if idx, err := windowsInterfaceIndex(tun.InterfaceName); err == nil && idx > 0 {
			return idx, strings.TrimSpace(tun.InterfaceName), nil
		} else if err != nil {
			lastErr = err
		}

		if idx, alias, err := windowsInterfaceIndexByIPv4(tun.IPv4); err == nil && idx > 0 {
			return idx, alias, nil
		} else if err != nil {
			lastErr = err
		}

		if idx, alias, err := windowsLikelyTunInterfaceIndex(tun.InterfaceName); err == nil && idx > 0 {
			return idx, alias, nil
		} else if err != nil {
			lastErr = err
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
		"  $alias = (Get-NetAdapter -InterfaceIndex $ifx -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty Name)",
		"  if (-not $alias) { $alias = (Get-NetIPInterface -AddressFamily IPv4 -InterfaceIndex $ifx -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty InterfaceAlias) }",
		"  \"${ifx}`t${alias}\"",
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
		"$cands = Get-NetAdapter -ErrorAction SilentlyContinue | Where-Object { $_.Status -ne 'Disabled' -and (($_.Name -match '(?i)wintun|sudoku|hev') -or ($_.InterfaceDescription -match '(?i)wintun|wireguard|hev') -or ($pref -and $_.Name -eq $pref)) }",
		"$sel = $cands | Sort-Object ifIndex | Select-Object -First 1",
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
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	return cmd.CombinedOutput()
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
