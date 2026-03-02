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
	if blockQUIC {
		b.WriteString("cfg=\"${cfg}block drop out proto udp to any port 443\\n\"; ")
	}
	if dnsProxyPort > 0 {
		b.WriteString("cfg=\"${cfg}rdr pass on lo0 inet proto { udp tcp } from any to " + localDNSServerIPv4 + " port 53 -> " + localDNSServerIPv4 + " port " + fmt.Sprintf("%d", dnsProxyPort) + "\\n\"; ")
	}

	b.WriteString("if [ -n \"$cfg\" ]; then printf \"%b\" \"$cfg\" | pfctl -a " + shellQuote(anchor) + " -f - >/dev/null 2>&1 || true; fi; ")

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
