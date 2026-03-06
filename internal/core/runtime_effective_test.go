package core

import "testing"

func TestEffectiveRuntimeConfigForcesMapDNSInTunPACMode(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Tun.Enabled = true
	cfg.Routing.ProxyMode = "pac"
	cfg.Tun.MapDNSEnabled = false

	effective, warnings, err := effectiveRuntimeConfig(cfg, true)
	if err != nil {
		t.Fatalf("effectiveRuntimeConfig error = %v", err)
	}
	if !effective.Tun.MapDNSEnabled {
		t.Fatalf("expected runtime config to force-enable MapDNS")
	}
	if cfg.Tun.MapDNSEnabled {
		t.Fatalf("expected original config to remain unchanged")
	}
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %d", len(warnings))
	}
}

func TestEffectiveRuntimeConfigKeepsMapDNSDisabledOutsideTunPACMode(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Tun.Enabled = true
	cfg.Routing.ProxyMode = "global"
	cfg.Tun.MapDNSEnabled = false

	effective, warnings, err := effectiveRuntimeConfig(cfg, true)
	if err != nil {
		t.Fatalf("effectiveRuntimeConfig error = %v", err)
	}
	if effective.Tun.MapDNSEnabled {
		t.Fatalf("expected runtime config to keep MapDNS disabled in global mode")
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %d", len(warnings))
	}
}

func TestEffectiveRuntimeConfigRejectsTunPACWithoutMapDNSEndpoint(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	cfg.Tun.Enabled = true
	cfg.Routing.ProxyMode = "pac"
	cfg.Tun.MapDNSEnabled = false
	cfg.Tun.MapDNSAddress = ""
	cfg.Tun.MapDNSPort = 0

	if _, _, err := effectiveRuntimeConfig(cfg, true); err == nil {
		t.Fatalf("expected error when TUN PAC mode has no MapDNS endpoint")
	}
}
