//go:build darwin

package core

import (
	"fmt"
	"strings"
)

const (
	darwinPFTableCN4 = "cn4"
	darwinPFTableCN6 = "cn6"
)

// darwinBuildPFSetCmd builds a shell snippet that:
// - enables pf if needed
// - loads anchor rules for CN bypass (PAC direct) and/or QUIC blocking
// - loads CN CIDR tables from files (if provided)
//
// tunIfExpr may be a literal interface name (e.g. "utun2") or a shell expression (e.g. "${tun_if}").
func darwinBuildPFSetCmd(anchor string, tunIfExpr string, defaultIf string, gw4 string, gw6 string, bypassV4File string, bypassV6File string, blockQUIC bool, dnsProxyPort int) string {
	anchor = strings.TrimSpace(anchor)
	tunIfExpr = strings.TrimSpace(tunIfExpr)
	defaultIf = strings.TrimSpace(defaultIf)
	gw4 = strings.TrimSpace(gw4)
	gw6 = strings.TrimSpace(gw6)
	bypassV4File = strings.TrimSpace(bypassV4File)
	bypassV6File = strings.TrimSpace(bypassV6File)
	if dnsProxyPort <= 0 || dnsProxyPort == 53 {
		dnsProxyPort = 0
	}

	if anchor == "" {
		return ""
	}
	if tunIfExpr == "" || defaultIf == "" || gw4 == "" {
		// Without routing context, only QUIC blocking can be applied.
		if !blockQUIC && dnsProxyPort == 0 {
			return ""
		}
	}

	// Build pf.conf content in a shell variable (cfg) and feed it to pfctl.
	var b strings.Builder
	b.WriteString("pfctl -q -e >/dev/null 2>&1 || true; ")
	b.WriteString("pfctl -a " + shellQuote(anchor) + " -F all >/dev/null 2>&1 || true; ")
	b.WriteString("cfg=''; ")

	if bypassV4File != "" && tunIfExpr != "" && defaultIf != "" && gw4 != "" {
		b.WriteString("if [ -f " + shellQuote(bypassV4File) + " ]; then ")
		b.WriteString("cfg=\"${cfg}table <" + darwinPFTableCN4 + "> persist\\n\"; ")
		b.WriteString("cfg=\"${cfg}pass out quick on " + tunIfExpr + " route-to (" + defaultIf + " " + gw4 + ") inet to <" + darwinPFTableCN4 + "> keep state\\n\"; ")
		b.WriteString("fi; ")
	}
	if bypassV6File != "" && tunIfExpr != "" && defaultIf != "" && gw6 != "" {
		b.WriteString("if [ -f " + shellQuote(bypassV6File) + " ]; then ")
		b.WriteString("cfg=\"${cfg}table <" + darwinPFTableCN6 + "> persist\\n\"; ")
		b.WriteString("cfg=\"${cfg}pass out quick on " + tunIfExpr + " route-to (" + defaultIf + " " + gw6 + ") inet6 to <" + darwinPFTableCN6 + "> keep state\\n\"; ")
		b.WriteString("fi; ")
	}
	if dnsProxyPort > 0 && tunIfExpr != "" && defaultIf != "" && gw4 != "" {
		// Ensure DNS proxy upstream IPs can always reach the physical gateway even after the global
		// default route switches to the TUN. This is especially important for DoH bootstrap IPs.
		b.WriteString("cfg=\"${cfg}pass out quick on " + tunIfExpr + " route-to (" + defaultIf + " " + gw4 + ") inet proto tcp to { 223.5.5.5, 223.6.6.6, 119.29.29.29, 119.28.28.28 } port 443 keep state\\n\"; ")
		b.WriteString("cfg=\"${cfg}pass out quick on " + tunIfExpr + " route-to (" + defaultIf + " " + gw4 + ") inet proto { udp tcp } to { 223.5.5.5, 223.6.6.6, 119.29.29.29, 119.28.28.28 } port 53 keep state\\n\"; ")
	}
	if blockQUIC {
		b.WriteString("cfg=\"${cfg}block drop out proto udp to any port 443\\n\"; ")
	}
	if dnsProxyPort > 0 {
		b.WriteString("cfg=\"${cfg}rdr pass on lo0 inet proto { udp tcp } from any to " + localDNSServerIPv4 + " port 53 -> " + localDNSServerIPv4 + " port " + fmt.Sprintf("%d", dnsProxyPort) + "\\n\"; ")
	}

	// Keep stdout quiet but allow pfctl stderr through (useful when startup fails).
	b.WriteString("if [ -n \"$cfg\" ]; then printf \"%b\" \"$cfg\" | pfctl -a " + shellQuote(anchor) + " -f - >/dev/null; fi; ")
	if dnsProxyPort > 0 {
		// Validate that the local DNS proxy works, and that pf rdr makes 127.0.0.1:53 reach it.
		// If this fails, we prefer to abort startup before the caller points system DNS at 127.0.0.1.
		//
		// Note: dig isn't guaranteed, but ships on macOS by default. nslookup can't specify a custom port,
		// so when dig isn't available we only do a best-effort 127.0.0.1:53 check.
		ipv4Re := "'^[0-9]+\\.[0-9]+\\.[0-9]+\\.[0-9]+$'"
		b.WriteString("if command -v dig >/dev/null 2>&1; then ")
		b.WriteString("echo '__SUDOKU_STEP__=dns_proxy_selftest'; ")
		b.WriteString("ok=0; for i in $(seq 1 10); do ")
		b.WriteString("dig +time=1 +tries=1 @" + localDNSServerIPv4 + " -p " + fmt.Sprintf("%d", dnsProxyPort) + " www.baidu.com A +short 2>/dev/null | grep -E " + ipv4Re + " >/dev/null 2>&1 && ok=1 && break; ")
		b.WriteString("sleep 0.2; ")
		b.WriteString("done; ")
		b.WriteString("if [ \"$ok\" -ne 1 ]; then echo '__SUDOKU_ERR__=dns_proxy_unreachable'; exit 23; fi; ")
		b.WriteString("echo '__SUDOKU_STEP__=dns_rdr_selftest'; ")
		b.WriteString("ok=0; for i in $(seq 1 10); do ")
		b.WriteString("dig +time=1 +tries=1 @" + localDNSServerIPv4 + " -p 53 www.baidu.com A +short 2>/dev/null | grep -E " + ipv4Re + " >/dev/null 2>&1 && ok=1 && break; ")
		b.WriteString("sleep 0.2; ")
		b.WriteString("done; ")
		b.WriteString("if [ \"$ok\" -ne 1 ]; then echo '__SUDOKU_ERR__=dns_rdr_failed'; exit 24; fi; ")
		b.WriteString("elif command -v nslookup >/dev/null 2>&1; then ")
		b.WriteString("echo '__SUDOKU_STEP__=dns_rdr_selftest'; ")
		b.WriteString("ok=0; for i in $(seq 1 10); do nslookup www.baidu.com " + localDNSServerIPv4 + " >/dev/null 2>&1 && ok=1 && break; sleep 0.2; done; ")
		b.WriteString("if [ \"$ok\" -ne 1 ]; then echo '__SUDOKU_ERR__=dns_rdr_failed'; exit 24; fi; ")
		b.WriteString("fi; ")
	}

	if bypassV4File != "" {
		b.WriteString("if [ -f " + shellQuote(bypassV4File) + " ]; then pfctl -a " + shellQuote(anchor) + " -t " + darwinPFTableCN4 + " -T replace -f " + shellQuote(bypassV4File) + " >/dev/null 2>&1 || true; fi; ")
	}
	if bypassV6File != "" {
		b.WriteString("if [ -f " + shellQuote(bypassV6File) + " ]; then pfctl -a " + shellQuote(anchor) + " -t " + darwinPFTableCN6 + " -T replace -f " + shellQuote(bypassV6File) + " >/dev/null 2>&1 || true; fi; ")
	}

	out := strings.TrimSpace(b.String())
	for strings.HasSuffix(out, ";") {
		out = strings.TrimSpace(strings.TrimSuffix(out, ";"))
	}
	return out
}

func darwinBuildPFRestoreCmd(anchor string) string {
	anchor = strings.TrimSpace(anchor)
	if anchor == "" {
		return ""
	}
	return fmt.Sprintf("pfctl -a %s -F all", shellQuote(anchor))
}
