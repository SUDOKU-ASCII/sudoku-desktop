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
	DefaultGateway         string
	DefaultGatewayV6       string
	DefaultInterface       string
	ServerIP               string
	TunIndex               int
	DNSService             string
	DNSServers             []string
	DNSWasAutomatic        bool
	PFAnchor               string
	BypassV4Path           string
	BypassV6Path           string
	LinuxOutboundSrcIP     string
	LinuxBypassMark        int
	LinuxBypassSet4        string
	LinuxBypassSet6        string
	LinuxDNSMode           string
	LinuxResolvConfBackup  string
	LinuxDNSRedirectPort   int
	WindowsFirewallRule    string
	WindowsDNSBackup       string
	WindowsDefaultIfIndex  int
	WindowsDefaultIfIndex6 int
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
	if !linuxHasCommand("ip") {
		return nil, errors.New("required command not found on linux: ip")
	}

	uid := os.Getuid()
	bypassMark := tun.SocksMark + 1
	if bypassMark <= 0 {
		bypassMark = 439
	}

	hasIPSet := linuxHasCommand("ipset")
	hasIPTables := linuxHasCommand("iptables")
	hasIP6Tables := linuxHasCommand("ip6tables")

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
		enableBypass4 := strings.TrimSpace(ctx.BypassV4Path) != "" && hasIPSet && hasIPTables
		enableBypass6 := strings.TrimSpace(ctx.BypassV6Path) != "" && hasIPSet && hasIP6Tables
		if strings.TrimSpace(ctx.BypassV4Path) != "" && !enableBypass4 && logf != nil {
			logf("[route] linux: skip ipv4 CN-bypass rules (missing ipset/iptables)")
		}
		if strings.TrimSpace(ctx.BypassV6Path) != "" && !enableBypass6 && logf != nil {
			logf("[route] linux: skip ipv6 CN-bypass rules (missing ipset/ip6tables)")
		}
		if enableBypass4 || enableBypass6 {
			ctx.LinuxBypassMark = bypassMark
		}
		if enableBypass4 {
			ctx.LinuxBypassSet4 = fmt.Sprintf("sudoku4x4_cn4_%d", uid)
			cmdlines = append(cmdlines,
				shellJoin("ipset", "create", ctx.LinuxBypassSet4, "hash:net", "family", "inet", "-exist"),
				shellJoin("ipset", "flush", ctx.LinuxBypassSet4)+" || true",
				"if [ -f "+shellQuote(ctx.BypassV4Path)+" ]; then while IFS= read -r cidr; do [ -z \"$cidr\" ] && continue; ipset add "+shellQuote(ctx.LinuxBypassSet4)+" \"$cidr\" -exist || true; done < "+shellQuote(ctx.BypassV4Path)+"; fi",
				"iptables -t mangle -C OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet4)+" dst -j MARK --set-mark "+strconv.Itoa(bypassMark)+" >/dev/null 2>&1 || "+
					"iptables -t mangle -A OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet4)+" dst -j MARK --set-mark "+strconv.Itoa(bypassMark),
			)
		}
		if enableBypass6 {
			ctx.LinuxBypassSet6 = fmt.Sprintf("sudoku4x4_cn6_%d", uid)
			cmdlines = append(cmdlines,
				shellJoin("ipset", "create", ctx.LinuxBypassSet6, "hash:net", "family", "inet6", "-exist"),
				shellJoin("ipset", "flush", ctx.LinuxBypassSet6)+" || true",
				"if [ -f "+shellQuote(ctx.BypassV6Path)+" ]; then while IFS= read -r cidr; do [ -z \"$cidr\" ] && continue; ipset add "+shellQuote(ctx.LinuxBypassSet6)+" \"$cidr\" -exist || true; done < "+shellQuote(ctx.BypassV6Path)+"; fi",
				"ip6tables -t mangle -C OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet6)+" dst -j MARK --set-mark "+strconv.Itoa(bypassMark)+" >/dev/null 2>&1 || "+
					"ip6tables -t mangle -A OUTPUT -m set --match-set "+shellQuote(ctx.LinuxBypassSet6)+" dst -j MARK --set-mark "+strconv.Itoa(bypassMark),
			)
		}
		if enableBypass4 || enableBypass6 {
			cmdlines = append(cmdlines,
				shellJoin("ip", "rule", "add", "fwmark", strconv.Itoa(bypassMark), "lookup", "main", "pref", "15")+" || true",
				shellJoin("ip", "-6", "rule", "add", "fwmark", strconv.Itoa(bypassMark), "lookup", "main", "pref", "15")+" || true",
			)
		}
	}

	// Optional: block QUIC (UDP/443).
	if tun.BlockQUIC {
		if hasIPTables {
			cmdlines = append(cmdlines, "iptables -C OUTPUT -p udp --dport 443 -j DROP >/dev/null 2>&1 || iptables -I OUTPUT 1 -p udp --dport 443 -j DROP")
		} else if logf != nil {
			logf("[route] linux: skip IPv4 QUIC block (iptables not found)")
		}
		if hasIP6Tables {
			cmdlines = append(cmdlines, "ip6tables -C OUTPUT -p udp --dport 443 -j DROP >/dev/null 2>&1 || ip6tables -I OUTPUT 1 -p udp --dport 443 -j DROP")
		} else if logf != nil {
			logf("[route] linux: skip IPv6 QUIC block (ip6tables not found)")
		}
	}

	// Optional: switch system DNS to HEV MapDNS while TUN is active (FakeIP mode).
	if tun.MapDNSEnabled && strings.TrimSpace(tun.MapDNSAddress) != "" {
		dnsAddr := strings.TrimSpace(tun.MapDNSAddress)
		canApplyDNS := true
		if dnsAddr == localDNSServerIPv4 && localDNSProxyListenPort() != 53 {
			if hasIPTables {
				ctx.LinuxDNSRedirectPort = localDNSProxyListenPort()
				cmdlines = append(cmdlines,
					"iptables -t nat -C OUTPUT -p udp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort)+" >/dev/null 2>&1 || "+
						"iptables -t nat -I OUTPUT 1 -p udp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort),
					"iptables -t nat -C OUTPUT -p tcp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort)+" >/dev/null 2>&1 || "+
						"iptables -t nat -I OUTPUT 1 -p tcp -d "+localDNSServerIPv4+" --dport 53 -j REDIRECT --to-ports "+strconv.Itoa(ctx.LinuxDNSRedirectPort),
				)
			} else {
				canApplyDNS = false
				if logf != nil {
					logf("[route] linux: skip DNS override to localhost (iptables nat redirect unavailable)")
				}
			}
		}
		if canApplyDNS {
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
		pfSetCmd = darwinBuildPFSetCmd(ctx.PFAnchor, tun.InterfaceName, ctx.DefaultInterface, gw, ctx.DefaultGatewayV6, tun.IPv4, ctx.BypassV4Path, ctx.BypassV6Path, tun.BlockQUIC, dnsProxyPort)
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
		if ctx.DefaultGatewayV6 != "" {
			// IPv6 default route may fail depending on system state; ignore.
			cmds = append(cmds, shellJoin("route", "-n", "change", "-inet6", "default", "-interface", tun.InterfaceName)+" || true")
		}
		if ctx.DefaultInterface != "" && ctx.DefaultGateway != "" {
			// Ensure a physical scoped default route exists for sockets bound to DefaultInterface (core outbound bypass).
			// NOTE: Creating this route *before* switching the global default route can fail with "File exists" (it
			// collides with the current global default). Ensure it after the default route has switched to utun.
			cmds = append(cmds, "("+shellJoin("route", "-n", "add", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1 || "+
				shellJoin("route", "-n", "change", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1) || echo '__SUDOKU_WARN__=scoped_default_route_failed'")
		}
		if ctx.DefaultInterface != "" && ctx.DefaultGatewayV6 != "" {
			// Keep a physical scoped IPv6 default route for direct sockets bound to DefaultInterface.
			cmds = append(cmds, "("+shellJoin("route", "-n", "add", "-inet6", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGatewayV6)+" >/dev/null 2>&1 || "+
				shellJoin("route", "-n", "change", "-inet6", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGatewayV6)+" >/dev/null 2>&1) || echo '__SUDOKU_WARN__=scoped_default_route6_failed'")
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
	if ctx.DefaultInterface != "" && ctx.DefaultGateway != "" {
		_ = runCmd(logf, "sh", "-lc", "("+shellJoin("route", "-n", "add", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1 || "+
			shellJoin("route", "-n", "change", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGateway)+" >/dev/null 2>&1) || echo '__SUDOKU_WARN__=scoped_default_route_failed'")
	}
	if ctx.DefaultInterface != "" && ctx.DefaultGatewayV6 != "" {
		_ = runCmd(logf, "sh", "-lc", "("+shellJoin("route", "-n", "add", "-inet6", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGatewayV6)+" >/dev/null 2>&1 || "+
			shellJoin("route", "-n", "change", "-inet6", "-ifscope", ctx.DefaultInterface, "default", ctx.DefaultGatewayV6)+" >/dev/null 2>&1) || echo '__SUDOKU_WARN__=scoped_default_route6_failed'")
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
	gw, if4, err := windowsPreferredDefaultRouteIPv4(idx)
	if err != nil {
		return nil, err
	}
	ctx.DefaultGateway = gw
	ctx.WindowsDefaultIfIndex = if4
	if gw6, if6, err6 := windowsPreferredDefaultRouteIPv6(idx); err6 == nil {
		ctx.DefaultGatewayV6 = strings.TrimSpace(gw6)
		ctx.WindowsDefaultIfIndex6 = if6
	}
	firewallRule := "4x4-sudoku Block QUIC (UDP/443)"
	if tun.BlockQUIC {
		ctx.WindowsFirewallRule = firewallRule
	}

	dnsBackupName := ""
	if tun.MapDNSEnabled && strings.TrimSpace(tun.MapDNSAddress) != "" {
		// Use PID to avoid collisions (os.Getuid is not meaningful on Windows).
		dnsBackupName = fmt.Sprintf("sudoku4x4-dns-%d.json", os.Getpid())
		ctx.WindowsDNSBackup = dnsBackupName
	}
	ps := buildWindowsRouteScript(
		true,
		ctx.ServerIP,
		ctx.BypassV4Path,
		ctx.BypassV6Path,
		firewallRule,
		tun.BlockQUIC,
		idx,
		ctx.DefaultGateway,
		ctx.WindowsDefaultIfIndex,
		ctx.DefaultGatewayV6,
		ctx.WindowsDefaultIfIndex6,
		tun.MapDNSEnabled,
		strings.TrimSpace(tun.MapDNSAddress),
		dnsBackupName,
	)
	if err := runCmdsWindowsAdmin(logf, ps); err != nil {
		teardownRoutesWindows(ctx, tun, logf)
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
	ps := buildWindowsRouteScript(
		false,
		ctx.ServerIP,
		ctx.BypassV4Path,
		ctx.BypassV6Path,
		firewallRule,
		tun.BlockQUIC,
		ctx.TunIndex,
		ctx.DefaultGateway,
		ctx.WindowsDefaultIfIndex,
		ctx.DefaultGatewayV6,
		ctx.WindowsDefaultIfIndex6,
		mapDNSEnabled,
		localDNSServerIPv4,
		ctx.WindowsDNSBackup,
	)
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
	applyManagedProcessSysProcAttr(cmd)
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

func buildWindowsRouteScript(
	start bool,
	serverIP string,
	bypassV4 string,
	bypassV6 string,
	firewallRule string,
	blockQUIC bool,
	tunIfIndex int,
	defaultGw4 string,
	defaultIf4 int,
	defaultGw6 string,
	defaultIf6 int,
	mapDNSEnabled bool,
	mapDNSAddress string,
	dnsBackupName string,
) string {
	op := "start"
	if !start {
		op = "stop"
	}
	bypassV4 = strings.TrimSpace(bypassV4)
	bypassV6 = strings.TrimSpace(bypassV6)
	serverIP = strings.TrimSpace(serverIP)
	firewallRule = strings.TrimSpace(firewallRule)
	defaultGw4 = strings.TrimSpace(defaultGw4)
	defaultGw6 = strings.TrimSpace(defaultGw6)
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
		fmt.Sprintf("$gw6 = '%s'", strings.ReplaceAll(defaultGw6, "'", "''")),
		fmt.Sprintf("$if6 = %d", defaultIf6),
		"if (-not $gw4 -or -not $if4 -or $if4 -le 0) {",
		"  $default4 = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object { $_.InterfaceIndex -ne $tunIf -and $_.NextHop -and $_.NextHop -ne '0.0.0.0' } | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1",
		"  if ($default4 -eq $null) { $default4 = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object { $_.InterfaceIndex -ne $tunIf } | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1 }",
		"  if ($default4 -ne $null) { $gw4 = $default4.NextHop; $if4 = [int]$default4.InterfaceIndex }",
		"}",
		"if (-not $if6 -or $if6 -le 0) {",
		"  $default6 = Get-NetRoute -AddressFamily IPv6 -DestinationPrefix '::/0' -ErrorAction SilentlyContinue | Where-Object { $_.InterfaceIndex -ne $tunIf -and $_.NextHop -and $_.NextHop -ne '::' } | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1",
		"  if ($default6 -eq $null) { $default6 = Get-NetRoute -AddressFamily IPv6 -DestinationPrefix '::/0' -ErrorAction SilentlyContinue | Where-Object { $_.InterfaceIndex -ne $tunIf } | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1 }",
		"  if ($default6 -ne $null) { $gw6 = $default6.NextHop; $if6 = [int]$default6.InterfaceIndex }",
		"}",
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
		"  $out4 = & route.exe change 0.0.0.0 mask 0.0.0.0 0.0.0.0 if $tunIf 2>&1",
		"  if ($LASTEXITCODE -ne 0) { throw ('route.exe change default route failed: ' + ($out4 | Out-String).Trim()) }",
		"  if ($if6 -gt 0) {",
		"    $null = & netsh interface ipv6 delete route prefix=::/0 interface=$tunIf store=active 2>$null",
		"    $out6 = & netsh interface ipv6 add route prefix=::/0 interface=$tunIf metric=1 store=active 2>&1",
		"    if ($LASTEXITCODE -ne 0) { Write-Output ('[warn] netsh add ipv6 default route failed: ' + ($out6 | Out-String).Trim()) }",
		"  }",
		"  $best4 = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Sort-Object @{Expression={ [int]$_.RouteMetric + [int]$_.InterfaceMetric }},RouteMetric,InterfaceMetric | Select-Object -First 1",
		"  if ($best4 -eq $null) { throw 'windows default route not found after tun switch' }",
		"  if ([int]$best4.InterfaceIndex -ne $tunIf) { throw ('windows default route still not on tun interface: expected=' + $tunIf + ' got=' + [int]$best4.InterfaceIndex) }",
		"  if ($if6 -gt 0) {",
		"    $best6 = Get-NetRoute -AddressFamily IPv6 -DestinationPrefix '::/0' -ErrorAction SilentlyContinue | Sort-Object @{Expression={ [int]$_.RouteMetric + [int]$_.InterfaceMetric }},RouteMetric,InterfaceMetric | Select-Object -First 1",
		"    if ($best6 -ne $null -and [int]$best6.InterfaceIndex -ne $tunIf) { Write-Output ('[warn] ipv6 default route not on tun interface: expected=' + $tunIf + ' got=' + [int]$best6.InterfaceIndex) }",
		"  }",
		"  # Keep a physical default route for core-bypass sockets (IP_UNICAST_IF).",
		"  try { if ($if4 -gt 0 -and $gw4) { New-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $if4 -NextHop $gw4 -RouteMetric $physMetric -PolicyStore ActiveStore -ErrorAction Stop | Out-Null } } catch { }",
		"  try { if ($if6 -gt 0 -and $gw6) { New-NetRoute -DestinationPrefix '::/0' -InterfaceIndex $if6 -NextHop $gw6 -RouteMetric $physMetric -PolicyStore ActiveStore -ErrorAction Stop | Out-Null } } catch { }",
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
		"  try { if ($if4 -gt 0 -and $gw4) { Get-NetRoute -DestinationPrefix '0.0.0.0/0' -InterfaceIndex $if4 -NextHop $gw4 -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object { $_.RouteMetric -eq $physMetric } | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue } } catch { }",
		"  try { if ($if6 -gt 0 -and $gw6) { Get-NetRoute -DestinationPrefix '::/0' -InterfaceIndex $if6 -NextHop $gw6 -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Where-Object { $_.RouteMetric -eq $physMetric } | Remove-NetRoute -Confirm:$false -ErrorAction SilentlyContinue } } catch { }",
		"  $null = & netsh interface ipv6 delete route prefix=::/0 interface=$tunIf store=active 2>$null",
		"  if ($if4 -gt 0 -and $gw4) {",
		"    $out = & route.exe change 0.0.0.0 mask 0.0.0.0 $gw4 if $if4 2>&1",
		"    if ($LASTEXITCODE -ne 0) { Write-Output ('[warn] route.exe restore default route failed: ' + ($out | Out-String).Trim()) }",
		"  }",
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

func windowsPreferredDefaultRouteIPv6(excludeIf int) (string, int, error) {
	script := strings.Join([]string{
		fmt.Sprintf("$exclude = %d", excludeIf),
		"$routes = Get-NetRoute -AddressFamily IPv6 -DestinationPrefix '::/0' -ErrorAction SilentlyContinue",
		"if ($exclude -gt 0) { $routes = $routes | Where-Object { $_.InterfaceIndex -ne $exclude } }",
		"$sel = $routes | Where-Object { $_.NextHop -and $_.NextHop -ne '::' } | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1",
		"if ($sel -eq $null) { $sel = $routes | Sort-Object RouteMetric,InterfaceMetric | Select-Object -First 1 }",
		"if ($sel -eq $null) { '' } else { \"$($sel.NextHop)`t$([int]$sel.InterfaceIndex)\" }",
	}, "; ")
	output, err := windowsPowerShellOutput(script)
	if err != nil {
		return "", 0, err
	}
	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return "", 0, nil
	}
	parts := strings.SplitN(raw, "\t", 2)
	gw := strings.TrimSpace(parts[0])
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
