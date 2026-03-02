package core

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

type sudokuHTTPMask struct {
	Disable   bool   `json:"disable"`
	Mode      string `json:"mode"`
	TLS       bool   `json:"tls"`
	Host      string `json:"host"`
	PathRoot  string `json:"path_root"`
	Multiplex string `json:"multiplex"`
}

type sudokuReverseRoute struct {
	Path        string `json:"path"`
	Target      string `json:"target"`
	StripPrefix *bool  `json:"strip_prefix,omitempty"`
	HostHeader  string `json:"host_header,omitempty"`
}

type sudokuReverse struct {
	ClientID string               `json:"client_id,omitempty"`
	Routes   []sudokuReverseRoute `json:"routes,omitempty"`
}

type sudokuClientConfig struct {
	Mode               string         `json:"mode"`
	Transport          string         `json:"transport"`
	LocalPort          int            `json:"local_port"`
	ServerAddress      string         `json:"server_address"`
	Key                string         `json:"key"`
	AEAD               string         `json:"aead"`
	PaddingMin         int            `json:"padding_min"`
	PaddingMax         int            `json:"padding_max"`
	CustomTable        string         `json:"custom_table,omitempty"`
	CustomTables       []string       `json:"custom_tables,omitempty"`
	ASCII              string         `json:"ascii"`
	EnablePureDownlink bool           `json:"enable_pure_downlink"`
	HTTPMask           sudokuHTTPMask `json:"httpmask"`
	RuleURLs           []string       `json:"rule_urls"`
	Reverse            *sudokuReverse `json:"reverse,omitempty"`
}

func buildSudokuClientConfig(cfg *AppConfig, node NodeConfig, customPACURL string, forceGlobal bool) (*sudokuClientConfig, error) {
	if strings.TrimSpace(node.ServerAddress) == "" {
		return nil, fmt.Errorf("node server address is empty")
	}
	if strings.TrimSpace(node.Key) == "" {
		return nil, fmt.Errorf("node key is empty")
	}
	localPort := node.LocalPort
	if localPort <= 0 {
		localPort = cfg.Core.LocalPort
	}
	ruleURLs := []string{"global"}
	if !forceGlobal {
		switch strings.ToLower(strings.TrimSpace(cfg.Routing.ProxyMode)) {
		case "direct":
			ruleURLs = []string{"direct"}
		case "global":
			ruleURLs = []string{"global"}
		case "pac":
			ruleURLs = append([]string(nil), cfg.Routing.RuleURLs...)
			if cfg.Routing.CustomRulesEnabled && strings.TrimSpace(cfg.Routing.CustomRules) != "" && strings.TrimSpace(customPACURL) != "" {
				ruleURLs = append([]string{strings.TrimSpace(customPACURL)}, ruleURLs...)
			}
			if len(ruleURLs) == 0 {
				ruleURLs = defaultPACRuleURLs()
			}
		}
	}

	clientCfg := &sudokuClientConfig{
		Mode:               "client",
		Transport:          "tcp",
		LocalPort:          localPort,
		ServerAddress:      strings.TrimSpace(node.ServerAddress),
		Key:                strings.TrimSpace(node.Key),
		AEAD:               strings.TrimSpace(node.AEAD),
		PaddingMin:         node.PaddingMin,
		PaddingMax:         node.PaddingMax,
		CustomTable:        strings.TrimSpace(node.CustomTable),
		CustomTables:       append([]string(nil), node.CustomTables...),
		ASCII:              normalizeASCII(node.ASCII),
		EnablePureDownlink: node.EnablePureDownlink,
		HTTPMask: sudokuHTTPMask{
			Disable:   node.HTTPMask.Disable,
			Mode:      strings.TrimSpace(node.HTTPMask.Mode),
			TLS:       node.HTTPMask.TLS,
			Host:      strings.TrimSpace(node.HTTPMask.Host),
			PathRoot:  strings.TrimSpace(node.HTTPMask.PathRoot),
			Multiplex: strings.TrimSpace(node.HTTPMask.Multiplex),
		},
		RuleURLs: ruleURLs,
	}
	if clientCfg.AEAD == "" {
		clientCfg.AEAD = "chacha20-poly1305"
	}
	if clientCfg.HTTPMask.Mode == "" {
		clientCfg.HTTPMask.Mode = "legacy"
	}
	if clientCfg.HTTPMask.Multiplex == "" {
		clientCfg.HTTPMask.Multiplex = "off"
	}
	if clientCfg.PaddingMax <= 0 {
		clientCfg.PaddingMax = 15
	}
	if clientCfg.PaddingMin > clientCfg.PaddingMax {
		clientCfg.PaddingMin = clientCfg.PaddingMax
	}
	if cfg.ReverseClient.ClientID != "" || len(cfg.ReverseClient.Routes) > 0 {
		rev := &sudokuReverse{
			ClientID: strings.TrimSpace(cfg.ReverseClient.ClientID),
			Routes:   make([]sudokuReverseRoute, 0, len(cfg.ReverseClient.Routes)),
		}
		for _, r := range cfg.ReverseClient.Routes {
			rev.Routes = append(rev.Routes, sudokuReverseRoute{
				Path:        strings.TrimSpace(r.Path),
				Target:      strings.TrimSpace(r.Target),
				StripPrefix: r.StripPrefix,
				HostHeader:  strings.TrimSpace(r.HostHeader),
			})
		}
		clientCfg.Reverse = rev
	}
	return clientCfg, nil
}

func normalizeASCII(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "ascii" {
		return "prefer_ascii"
	}
	if s == "prefer_ascii" {
		return s
	}
	return "prefer_entropy"
}

type hevTunnelConfig struct {
	Tunnel hevTunnelSection  `yaml:"tunnel"`
	Socks5 hevSocks5Section  `yaml:"socks5"`
	MapDNS *hevMapDNSSection `yaml:"mapdns,omitempty"`
	Misc   *hevMiscSection   `yaml:"misc,omitempty"`
}

type hevTunnelSection struct {
	Name       string `yaml:"name"`
	MTU        int    `yaml:"mtu"`
	MultiQueue bool   `yaml:"multi-queue"`
	IPv4       string `yaml:"ipv4"`
	IPv6       string `yaml:"ipv6"`
}

type hevSocks5Section struct {
	Port    int    `yaml:"port"`
	Address string `yaml:"address"`
	UDP     string `yaml:"udp"`
	Mark    int    `yaml:"mark"`
}

type hevMapDNSSection struct {
	Address   string `yaml:"address"`
	Port      int    `yaml:"port"`
	Network   string `yaml:"network"`
	Netmask   string `yaml:"netmask"`
	CacheSize int    `yaml:"cache-size"`
}

type hevMiscSection struct {
	TaskStackSize   int    `yaml:"task-stack-size,omitempty"`
	TCPBufferSize   int    `yaml:"tcp-buffer-size,omitempty"`
	MaxSessionCount int    `yaml:"max-session-count,omitempty"`
	ConnectTimeout  int    `yaml:"connect-timeout,omitempty"`
	LogFile         string `yaml:"log-file,omitempty"`
	LogLevel        string `yaml:"log-level,omitempty"`
}

func buildHevConfig(cfg *AppConfig, localPort int) *hevTunnelConfig {
	c := &hevTunnelConfig{
		Tunnel: hevTunnelSection{
			Name:       cfg.Tun.InterfaceName,
			MTU:        cfg.Tun.MTU,
			MultiQueue: false,
			IPv4:       cfg.Tun.IPv4,
			IPv6:       cfg.Tun.IPv6,
		},
		Socks5: hevSocks5Section{
			Port:    localPort,
			Address: "127.0.0.1",
			UDP:     strings.TrimSpace(cfg.Tun.SocksUDP),
			Mark:    cfg.Tun.SocksMark,
		},
		Misc: &hevMiscSection{
			TaskStackSize:   cfg.Tun.TaskStackSize,
			TCPBufferSize:   cfg.Tun.TCPBufferSize,
			MaxSessionCount: cfg.Tun.MaxSession,
			ConnectTimeout:  cfg.Tun.ConnectTimeout,
			LogFile:         "stderr",
			LogLevel:        strings.TrimSpace(cfg.Tun.LogLevel),
		},
	}
	if c.Socks5.UDP == "" {
		c.Socks5.UDP = "udp"
	}
	enableMapDNS := cfg.Tun.MapDNSEnabled
	if runtime.GOOS == "darwin" && strings.TrimSpace(os.Getenv("SUDOKU_DARWIN_ENABLE_HEV_MAPDNS")) != "1" {
		// Production default on macOS: local DNS proxy already handles split DNS, while HEV MapDNS
		// may destabilize data-plane forwarding on some networks.
		enableMapDNS = false
	}
	if enableMapDNS {
		c.MapDNS = &hevMapDNSSection{
			Address:   cfg.Tun.MapDNSAddress,
			Port:      cfg.Tun.MapDNSPort,
			Network:   cfg.Tun.MapDNSNetwork,
			Netmask:   cfg.Tun.MapDNSNetmask,
			CacheSize: 10000,
		}
	}
	if c.Misc.LogLevel == "" {
		c.Misc.LogLevel = "warn"
	}
	return c
}

func writeRuntimeConfigs(store *Store, cfg *AppConfig, node NodeConfig, customPACURL string, withTun bool) (string, string, int, error) {
	// Routing behavior should follow user settings. TUN-mode loop avoidance is handled by
	// platform route setup (e.g. CN bypass routes for PAC-mode direct decisions).
	//
	// In FakeIP mode (MapDNS), system DNS may be switched to HEV's MapDNS while TUN is active.
	// To avoid the core accidentally resolving the server address into a FakeIP later, resolve
	// it once here and pin the server address to an IP:port if it was a hostname.
	if withTun && cfg != nil && cfg.Tun.MapDNSEnabled {
		node.ServerAddress = pinHostPortToIP(node.ServerAddress)
	}
	sCfg, err := buildSudokuClientConfig(cfg, node, customPACURL, false)
	if err != nil {
		return "", "", 0, err
	}
	localPort := sCfg.LocalPort
	runtimeCfgDir := filepath.Join(store.RuntimeDir(), "configs")
	sudokuPath := filepath.Join(runtimeCfgDir, "client.config.json")
	if err := writeJSONFile(sudokuPath, sCfg); err != nil {
		return "", "", 0, fmt.Errorf("write sudoku config: %w", err)
	}
	hevPath := filepath.Join(runtimeCfgDir, "hev.yml")
	hevCfg := buildHevConfig(cfg, localPort)
	if err := ensureDir(filepath.Dir(hevPath)); err != nil {
		return "", "", 0, err
	}
	buf, err := yaml.Marshal(hevCfg)
	if err != nil {
		return "", "", 0, fmt.Errorf("marshal hev config: %w", err)
	}
	if err := os.WriteFile(hevPath, buf, 0o644); err != nil {
		return "", "", 0, fmt.Errorf("write hev config: %w", err)
	}
	return sudokuPath, hevPath, localPort, nil
}

func pinHostPortToIP(addr string) string {
	addr = strings.TrimSpace(addr)
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if net.ParseIP(host) != nil {
		return addr
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return addr
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			return net.JoinHostPort(v4.String(), port)
		}
	}
	return net.JoinHostPort(ips[0].String(), port)
}
