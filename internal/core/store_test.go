package core

import "testing"

func TestDefaultConfigUsesUpdatedTunDefaults(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())

	if cfg.Tun.Enabled {
		t.Fatalf("expected TUN default to be disabled")
	}
	if cfg.Tun.BlockQUIC {
		t.Fatalf("expected BlockQUIC default to be disabled")
	}
	if !cfg.Tun.MapDNSEnabled {
		t.Fatalf("expected MapDNS default to be enabled")
	}
}

func TestNormalizeConfigMigratesLegacyTunDefaults(t *testing.T) {
	cfg := &AppConfig{
		Version: 3,
		Tun: TunSettings{
			Enabled:       false,
			BlockQUIC:     true,
			MapDNSEnabled: false,
		},
	}

	normalizeConfig(cfg, t.TempDir())

	if cfg.Version != configVersion {
		t.Fatalf("expected version %d, got %d", configVersion, cfg.Version)
	}
	if cfg.Tun.BlockQUIC {
		t.Fatalf("expected legacy default BlockQUIC to migrate to false")
	}
	if !cfg.Tun.MapDNSEnabled {
		t.Fatalf("expected legacy default MapDNS to migrate to true")
	}
}

func TestNormalizeConfigKeepsExplicitTunFlags(t *testing.T) {
	cfg := &AppConfig{
		Version: 3,
		Tun: TunSettings{
			Enabled:       true,
			BlockQUIC:     true,
			MapDNSEnabled: false,
		},
	}

	normalizeConfig(cfg, t.TempDir())

	if !cfg.Tun.Enabled {
		t.Fatalf("expected explicit TUN enable to be preserved")
	}
	if !cfg.Tun.BlockQUIC {
		t.Fatalf("expected explicit BlockQUIC enable to be preserved")
	}
	if cfg.Tun.MapDNSEnabled {
		t.Fatalf("expected explicit MapDNS disable to be preserved")
	}
}
