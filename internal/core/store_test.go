package core

import (
	"runtime"
	"testing"
)

func TestDefaultPACRuleURLsIncludeRejectRule(t *testing.T) {
	urls := defaultPACRuleURLs()
	if len(urls) != 4 {
		t.Fatalf("expected 4 default PAC rule URLs, got %d: %v", len(urls), urls)
	}
	last := urls[len(urls)-1]
	if last != "!"+defaultPACRejectRuleURL {
		t.Fatalf("expected default PAC reject rule %q, got %q", "!"+defaultPACRejectRuleURL, last)
	}
}

func TestDefaultTunSettingsUsePlatformSpecificBlockQUIC(t *testing.T) {
	if !defaultTunSettings("windows").BlockQUIC {
		t.Fatalf("expected BlockQUIC default to be enabled on windows")
	}
	if defaultTunSettings("darwin").BlockQUIC {
		t.Fatalf("expected BlockQUIC default to be disabled on darwin")
	}
	if defaultTunSettings("linux").BlockQUIC {
		t.Fatalf("expected BlockQUIC default to be disabled on linux")
	}
}

func TestDefaultConfigUsesCurrentPlatformTunDefaults(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	want := defaultTunSettings(runtime.GOOS)

	if cfg.Tun.Enabled {
		t.Fatalf("expected TUN default to be disabled")
	}
	if cfg.Tun.BlockQUIC != want.BlockQUIC {
		t.Fatalf("expected BlockQUIC default %v, got %v", want.BlockQUIC, cfg.Tun.BlockQUIC)
	}
	if !cfg.Tun.MapDNSEnabled {
		t.Fatalf("expected MapDNS default to be enabled")
	}
	if cfg.Tun.MapDNSNetwork != want.MapDNSNetwork || cfg.Tun.MapDNSNetmask != want.MapDNSNetmask {
		t.Fatalf("expected FakeIP range %s/%s, got %s/%s", want.MapDNSNetwork, want.MapDNSNetmask, cfg.Tun.MapDNSNetwork, cfg.Tun.MapDNSNetmask)
	}
}

func TestNormalizeConfigMigratesLegacyV3TunDefaults(t *testing.T) {
	cfg := &AppConfig{
		Version: 3,
		Tun: TunSettings{
			Enabled:       false,
			BlockQUIC:     true,
			MapDNSEnabled: false,
		},
	}

	normalizeConfigForOS(cfg, t.TempDir(), "darwin")

	if cfg.Version != configVersion {
		t.Fatalf("expected version %d, got %d", configVersion, cfg.Version)
	}
	if cfg.Tun.BlockQUIC {
		t.Fatalf("expected legacy v3 BlockQUIC default to migrate to false")
	}
	if !cfg.Tun.MapDNSEnabled {
		t.Fatalf("expected legacy v3 MapDNS default to migrate to true")
	}
}

func TestNormalizeConfigMigratesLegacyPACRulesToIncludeRejectRule(t *testing.T) {
	cfg := &AppConfig{
		Version: 5,
		Routing: RoutingSettings{
			ProxyMode: "pac",
			RuleURLs: []string{
				"https://fastly.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/BiliBili/BiliBili.list",
				"https://fastly.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/WeChat/WeChat.list",
				"https://fastly.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/ChinaMaxNoIP/ChinaMaxNoIP.list",
			},
		},
	}

	normalizeConfigForOS(cfg, t.TempDir(), "darwin")

	if cfg.Version != configVersion {
		t.Fatalf("expected version %d, got %d", configVersion, cfg.Version)
	}
	got := cfg.Routing.RuleURLs[len(cfg.Routing.RuleURLs)-1]
	if got != "!"+defaultPACRejectRuleURL {
		t.Fatalf("expected migrated PAC reject rule %q, got %q", "!"+defaultPACRejectRuleURL, got)
	}
}

func TestNormalizeConfigDoesNotDuplicateExistingRejectRule(t *testing.T) {
	cfg := &AppConfig{
		Version: 5,
		Routing: RoutingSettings{
			ProxyMode: "pac",
			RuleURLs: []string{
				"https://fastly.jsdelivr.net/gh/blackmatrix7/ios_rule_script@master/rule/Clash/ChinaMaxNoIP/ChinaMaxNoIP.list",
				"!https://example.com/reject.yaml",
			},
		},
	}

	normalizeConfigForOS(cfg, t.TempDir(), "linux")

	if len(cfg.Routing.RuleURLs) != 2 {
		t.Fatalf("expected existing reject rule to be preserved without duplication, got %v", cfg.Routing.RuleURLs)
	}
}

func TestNormalizeConfigMigratesWindowsTunDefaultsAndFakeIPRange(t *testing.T) {
	cfg := &AppConfig{
		Version: 4,
		Tun: TunSettings{
			Enabled:       false,
			BlockQUIC:     false,
			MapDNSEnabled: true,
			MapDNSAddress: "198.18.0.2",
			MapDNSNetwork: "100.64.0.0",
			MapDNSNetmask: "255.192.0.0",
		},
	}

	normalizeConfigForOS(cfg, t.TempDir(), "windows")

	if !cfg.Tun.BlockQUIC {
		t.Fatalf("expected windows legacy default BlockQUIC to migrate to true")
	}
	if cfg.Tun.MapDNSNetwork != "198.18.0.0" || cfg.Tun.MapDNSNetmask != "255.254.0.0" {
		t.Fatalf("expected FakeIP range to migrate to 198.18.0.0/255.254.0.0, got %s/%s", cfg.Tun.MapDNSNetwork, cfg.Tun.MapDNSNetmask)
	}
}

func TestNormalizeConfigKeepsExplicitTunFlagsAndCustomFakeIPRange(t *testing.T) {
	cfg := &AppConfig{
		Version: 4,
		Tun: TunSettings{
			Enabled:       true,
			BlockQUIC:     false,
			MapDNSEnabled: true,
			MapDNSAddress: "198.18.0.2",
			MapDNSNetwork: "172.20.0.0",
			MapDNSNetmask: "255.255.0.0",
		},
	}

	normalizeConfigForOS(cfg, t.TempDir(), "windows")

	if !cfg.Tun.Enabled {
		t.Fatalf("expected explicit TUN enable to be preserved")
	}
	if cfg.Tun.BlockQUIC {
		t.Fatalf("expected explicit BlockQUIC disable to be preserved")
	}
	if cfg.Tun.MapDNSNetwork != "172.20.0.0" || cfg.Tun.MapDNSNetmask != "255.255.0.0" {
		t.Fatalf("expected custom FakeIP range to be preserved, got %s/%s", cfg.Tun.MapDNSNetwork, cfg.Tun.MapDNSNetmask)
	}
}

func TestNormalizeConfigKeepsKnownThemeAndResetsUnknownTheme(t *testing.T) {
	cfg := &AppConfig{
		UI: UISettings{
			Theme: "qingshanlan",
		},
	}

	normalizeConfigForOS(cfg, t.TempDir(), "darwin")

	if cfg.UI.Theme != "qingshanlan" {
		t.Fatalf("expected known theme to be preserved, got %q", cfg.UI.Theme)
	}

	cfg.UI.Theme = "unexpected-theme"
	normalizeConfigForOS(cfg, t.TempDir(), "darwin")

	if cfg.UI.Theme != "auto" {
		t.Fatalf("expected unknown theme to reset to auto, got %q", cfg.UI.Theme)
	}
}

func TestNormalizeNodeCanonicalizesDirectionalASCIIMode(t *testing.T) {
	node := &NodeConfig{
		ASCII: "up_ascii_down_ascii",
	}

	normalizeNode(node, 1080)

	if node.ASCII != "prefer_ascii" {
		t.Fatalf("expected ascii mode to canonicalize to prefer_ascii, got %q", node.ASCII)
	}
}
