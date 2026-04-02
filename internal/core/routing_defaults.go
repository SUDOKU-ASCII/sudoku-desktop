package core

import "strings"

const defaultPACRejectRuleURL = "https://gcore.jsdelivr.net/gh/TG-Twilight/AWAvenue-Ads-Rule@main/Filters/AWAvenue-Ads-Rule-Clash.yaml"

// defaultPACRuleURLs returns the recommended PAC rule sources.
//
// Keep this list CDN-friendly and aligned with upstream defaults so PAC mode works
// even if a user clears their rule list by accident.
func defaultPACRuleURLs() []string {
	return []string{
		"https://fastly.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/ChinaMaxNoIP/ChinaMaxNoIP.list",
		"https://fastly.jsdelivr.net/gh/fernvenue/chn-cidr-list@master/ipv4.yaml",
		"https://fastly.jsdelivr.net/gh/fernvenue/chn-cidr-list@master/ipv6.yaml",
		"!" + defaultPACRejectRuleURL,
	}
}

func hasRejectRulePrefix(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "!") || strings.HasPrefix(raw, "！")
}
