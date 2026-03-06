//go:build darwin

package core

import (
	"fmt"
	"strings"
)

// darwinBuildPFSetCmd builds a shell snippet that enables pf if needed,
// clears the anchor, and loads the rules currently required by the desktop app.
func darwinBuildPFSetCmd(anchor string, tunIfExpr string, blockQUIC bool, dnsProxyPort int) string {
	anchor = strings.TrimSpace(anchor)
	tunIfExpr = strings.TrimSpace(tunIfExpr)

	if anchor == "" || (!blockQUIC && dnsProxyPort <= 0) {
		return ""
	}

	var b strings.Builder
	b.WriteString("pfctl -q -e >/dev/null 2>&1 || true; ")
	b.WriteString("pfctl -a " + shellQuote(anchor) + " -F all >/dev/null 2>&1 || true; ")
	b.WriteString("cfg_trans=''; cfg_flt=''; ")
	if dnsProxyPort > 0 && dnsProxyPort != 53 {
		b.WriteString("cfg_trans=\"${cfg_trans}rdr pass on lo0 inet proto { udp tcp } from any to " + localLoopbackIPv4 + " port 53 -> " + localLoopbackIPv4 + " port " + fmt.Sprintf("%d", dnsProxyPort) + "\\n\"; ")
	}
	if blockQUIC {
		if tunIfExpr != "" {
			b.WriteString("cfg_flt=\"${cfg_flt}block drop out quick on " + tunIfExpr + " proto udp to any port 443\\n\"; ")
		} else {
			b.WriteString("cfg_flt=\"${cfg_flt}block drop out proto udp to any port 443\\n\"; ")
		}
	}
	b.WriteString("cfg=\"${cfg_trans}${cfg_flt}\"; ")
	b.WriteString("if [ -n \"$cfg\" ]; then printf \"%b\" \"$cfg\" | pfctl -a " + shellQuote(anchor) + " -f - >/dev/null; fi; ")

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
