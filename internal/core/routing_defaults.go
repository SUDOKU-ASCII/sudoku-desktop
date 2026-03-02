package core

// defaultPACRuleURLs returns the recommended PAC rule sources.
//
// Keep this list CDN-friendly and aligned with upstream defaults so PAC mode works
// even if a user clears their rule list by accident.
func defaultPACRuleURLs() []string {
	return []string{
		"https://gh-proxy.org/https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Clash/BiliBili/BiliBili.list",
		"https://gh-proxy.org/https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Clash/WeChat/WeChat.list",
		"https://gh-proxy.org/https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Clash/ChinaMaxNoIP/ChinaMaxNoIP.list",
		"https://gh-proxy.org/https://raw.githubusercontent.com/fernvenue/chn-cidr-list/master/ipv4.yaml",
		"https://gh-proxy.org/https://raw.githubusercontent.com/fernvenue/chn-cidr-list/master/ipv6.yaml",
	}
}
