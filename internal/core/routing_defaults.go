package core

import "strings"

const defaultPACRejectRuleURL = "https://gcore.jsdelivr.net/gh/TG-Twilight/AWAvenue-Ads-Rule@main/Filters/AWAvenue-Ads-Rule-Clash.yaml"

type ruleSourceAction uint8

const (
	ruleSourceDirect ruleSourceAction = iota
	ruleSourceProxy
	ruleSourceReject
	ruleSourceReserved
)

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

func parseRuleSource(raw string) (ruleSourceAction, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ruleSourceReserved, ""
	}
	switch {
	case strings.HasPrefix(raw, "!"):
		return ruleSourceReject, strings.TrimSpace(strings.TrimPrefix(raw, "!"))
	case strings.HasPrefix(raw, "！"):
		return ruleSourceReject, strings.TrimSpace(strings.TrimPrefix(raw, "！"))
	case strings.HasPrefix(raw, "-"), strings.HasPrefix(raw, "_"):
		return ruleSourceProxy, strings.TrimSpace(raw[1:])
	case strings.HasPrefix(raw, "?"):
		return ruleSourceReserved, strings.TrimSpace(raw[1:])
	default:
		return ruleSourceDirect, raw
	}
}
