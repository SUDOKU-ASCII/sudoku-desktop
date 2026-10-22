package core

import (
	"reflect"
	"testing"
)

func TestNodeProxyNamesDeduplicatesAndAvoidsReserved(t *testing.T) {
	nodes := []NodeConfig{
		{ID: "a", Name: "HKG"}, {ID: "b", Name: "HKG"}, {ID: "c", Name: "PROXY"}, {ID: "d", Name: ""},
	}
	got := nodeProxyNames(nodes)
	want := map[string]string{"a": "HKG", "b": "HKG#2", "c": "PROXY#2", "d": "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestBuildKernelConfigsPutsControllerOnFirstConfig(t *testing.T) {
	cfg := &AppConfig{
		Nodes: []NodeConfig{
			{ID: "a", Name: "one", ServerAddress: "203.0.113.1:443", Key: "k1", AEAD: "chacha20-poly1305", ASCII: "prefer_entropy"},
			{ID: "b", Name: "two", ServerAddress: "203.0.113.2:443", Key: "k2", AEAD: "chacha20-poly1305", ASCII: "prefer_entropy"},
		},
		Controller: ControllerSettings{Port: 9097, Secret: "s3cret"},
		Routing:    RoutingSettings{ProxyMode: "pac"},
	}
	files, err := buildKernelConfigs(cfg, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 configs, got %d", len(files))
	}
	if files[0].Config.ExternalController != "127.0.0.1:9097" || files[0].Config.Secret != "s3cret" {
		t.Fatalf("controller not on first config: %+v", files[0].Config)
	}
	if files[1].Config.ExternalController != "" || files[1].Config.Secret != "" {
		t.Fatalf("controller leaked into second config")
	}
	if len(files[0].Config.RuleURLs) == 0 {
		t.Fatal("expected default rule urls")
	}
	cfg.Controller.AllowLAN = true
	files, _ = buildKernelConfigs(cfg, "")
	if files[0].Config.ExternalController != ":9097" {
		t.Fatalf("LAN controller addr: %q", files[0].Config.ExternalController)
	}
	if _, err := buildKernelConfigs(&AppConfig{}, ""); err == nil {
		t.Fatal("expected error without nodes")
	}
}

func TestValidateTunPlan(t *testing.T) {
	good := func() *tunPlan {
		cfg := DefaultConfig("/tmp/x")
		return &tunPlan{Tun: cfg.Tun, ProxyPort: ProxyPort, Mode: "global", ServerAddrs: []string{"203.0.113.1:443"}}
	}
	if err := validateTunPlan(good()); err != nil {
		t.Fatalf("good plan rejected: %v", err)
	}
	cases := map[string]func(p *tunPlan){
		"port":     func(p *tunPlan) { p.ProxyPort = 1 },
		"mode":     func(p *tunPlan) { p.Mode = "x" },
		"iface":    func(p *tunPlan) { p.Tun.InterfaceName = "utun9; rm -rf /" },
		"server":   func(p *tunPlan) { p.ServerAddrs = []string{"a b;c:1"} },
		"noserver": func(p *tunPlan) { p.ServerAddrs = nil },
		"rule":     func(p *tunPlan) { p.DirectRuleURLs = []string{"file:///etc/passwd"} },
	}
	for name, mut := range cases {
		p := good()
		mut(p)
		if err := validateTunPlan(p); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}
