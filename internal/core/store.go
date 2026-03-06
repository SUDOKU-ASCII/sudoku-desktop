package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const configVersion = 5

type Store struct {
	rootDir    string
	configPath string
	runtimeDir string
	logDir     string
}

func NewStore(appName string) (*Store, error) {
	cfgRoot, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user config dir: %w", err)
	}
	root := filepath.Join(cfgRoot, appName)
	runtimeDir := filepath.Join(root, "runtime")
	logDir := filepath.Join(root, "logs")
	st := &Store{
		rootDir:    root,
		configPath: filepath.Join(root, "config.json"),
		runtimeDir: runtimeDir,
		logDir:     logDir,
	}
	if err := st.EnsureDirs(); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) EnsureDirs() error {
	for _, dir := range []string{s.rootDir, s.runtimeDir, s.logDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}

func (s *Store) RootDir() string {
	return s.rootDir
}

func (s *Store) RuntimeDir() string {
	return s.runtimeDir
}

func (s *Store) LogDir() string {
	return s.logDir
}

func (s *Store) UsageHistoryPath() string {
	return filepath.Join(s.runtimeDir, "usage_history.json")
}

func (s *Store) ConfigPath() string {
	return s.configPath
}

func (s *Store) Load() (*AppConfig, error) {
	if err := s.EnsureDirs(); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultConfig(s.runtimeDir)
			if err := s.Save(cfg); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg AppConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	normalizeConfig(&cfg, s.runtimeDir)
	return &cfg, nil
}

func (s *Store) Save(cfg *AppConfig) error {
	if cfg == nil {
		return errors.New("nil config")
	}
	normalizeConfig(cfg, s.runtimeDir)
	if err := s.EnsureDirs(); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(s.configPath, buf, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func DefaultConfig(runtimeDir string) *AppConfig {
	cfg := &AppConfig{
		Version:      configVersion,
		ActiveNodeID: "",
		Nodes:        []NodeConfig{},
		Routing: RoutingSettings{
			ProxyMode:          "pac",
			RuleURLs:           defaultPACRuleURLs(),
			CustomRulesEnabled: false,
			CustomRules:        "",
		},
		Tun: defaultTunSettings(runtime.GOOS),
		Core: CoreSettings{
			SudokuBinary: defaultBinaryPath(runtimeDir, "sudoku"),
			HevBinary:    defaultBinaryPath(runtimeDir, "hev-socks5-tunnel"),
			WorkingDir:   filepath.Join(runtimeDir, "workspace"),
			LocalPort:    1080,
			AllowLAN:     false,
			LogLevel:     "info",
			AutoStart:    false,
		},
		ReverseClient: ReverseClientSettings{
			ClientID: "",
			Routes:   []ReverseRoute{},
		},
		ReverseForward: ReverseForwarderSettings{
			DialURL:    "",
			ListenAddr: "127.0.0.1:2222",
			Insecure:   false,
		},
		PortForwards: []PortForwardRule{},
		UI: UISettings{
			Language: "auto",
			Theme:    "auto",
			// launchAtLogin defaults to false and is only used by the desktop host app.
			LaunchAtLogin: false,
		},
	}
	normalizeConfig(cfg, runtimeDir)
	return cfg
}

func normalizeConfig(cfg *AppConfig, runtimeDir string) {
	normalizeConfigForOS(cfg, runtimeDir, runtime.GOOS)
}

func normalizeConfigForOS(cfg *AppConfig, runtimeDir string, goos string) {
	prevVersion := cfg.Version
	defaultTun := defaultTunSettings(goos)
	if cfg.Version < configVersion {
		cfg.Version = configVersion
	}
	if cfg.Nodes == nil {
		cfg.Nodes = []NodeConfig{}
	}
	if cfg.PortForwards == nil {
		cfg.PortForwards = []PortForwardRule{}
	}
	if cfg.ReverseClient.Routes == nil {
		cfg.ReverseClient.Routes = []ReverseRoute{}
	}
	if cfg.Routing.ProxyMode == "" {
		cfg.Routing.ProxyMode = "pac"
	}
	if cfg.Routing.ProxyMode != "global" && cfg.Routing.ProxyMode != "direct" && cfg.Routing.ProxyMode != "pac" {
		cfg.Routing.ProxyMode = "pac"
	}
	if cfg.Routing.RuleURLs == nil {
		cfg.Routing.RuleURLs = []string{}
	}
	// PAC mode requires rule URLs. If a user accidentally cleared the list (and has no custom rules),
	// fall back to the recommended defaults so routing doesn't silently become global.
	if cfg.Routing.ProxyMode == "pac" && len(cfg.Routing.RuleURLs) == 0 {
		if !cfg.Routing.CustomRulesEnabled || strings.TrimSpace(cfg.Routing.CustomRules) == "" {
			cfg.Routing.RuleURLs = defaultPACRuleURLs()
		}
	}
	if cfg.Tun.InterfaceName == "" {
		cfg.Tun.InterfaceName = defaultTun.InterfaceName
	} else if goos == "darwin" && cfg.Tun.InterfaceName == "sudoku0" {
		// Migrate old default to macOS-friendly default.
		cfg.Tun.InterfaceName = defaultTun.InterfaceName
	}
	if cfg.Tun.MTU <= 0 {
		cfg.Tun.MTU = defaultTun.MTU
	}
	if cfg.Tun.IPv4 == "" {
		cfg.Tun.IPv4 = defaultTun.IPv4
	}
	if cfg.Tun.IPv6 == "" {
		cfg.Tun.IPv6 = defaultTun.IPv6
	}
	// v4 changes the default TUN posture to:
	// - TUN disabled
	// - QUIC blocking disabled
	// - MapDNS enabled
	// Only migrate when the old default tuple is still intact, so explicit user choices survive.
	if prevVersion < 4 && !cfg.Tun.Enabled && cfg.Tun.BlockQUIC && !cfg.Tun.MapDNSEnabled {
		cfg.Tun.BlockQUIC = false
		cfg.Tun.MapDNSEnabled = true
	}
	if prevVersion < 5 && cfg.Tun.MapDNSAddress == "198.18.0.2" &&
		cfg.Tun.MapDNSNetwork == "100.64.0.0" && cfg.Tun.MapDNSNetmask == "255.192.0.0" {
		cfg.Tun.MapDNSNetwork = defaultTun.MapDNSNetwork
		cfg.Tun.MapDNSNetmask = defaultTun.MapDNSNetmask
	}
	if prevVersion < 5 && goos == "windows" && !cfg.Tun.Enabled && !cfg.Tun.BlockQUIC && cfg.Tun.MapDNSEnabled {
		cfg.Tun.BlockQUIC = true
	}
	if cfg.Tun.SocksUDP == "" {
		cfg.Tun.SocksUDP = defaultTun.SocksUDP
	}
	if cfg.Tun.SocksMark <= 0 {
		cfg.Tun.SocksMark = defaultTun.SocksMark
	}
	if cfg.Tun.RouteTable <= 0 {
		cfg.Tun.RouteTable = defaultTun.RouteTable
	}
	if cfg.Tun.LogLevel == "" {
		cfg.Tun.LogLevel = defaultTun.LogLevel
	}
	if cfg.Tun.MapDNSAddress == "" {
		cfg.Tun.MapDNSAddress = defaultTun.MapDNSAddress
	}
	if cfg.Tun.MapDNSPort <= 0 {
		cfg.Tun.MapDNSPort = defaultTun.MapDNSPort
	}
	if cfg.Tun.MapDNSNetwork == "" {
		cfg.Tun.MapDNSNetwork = defaultTun.MapDNSNetwork
	}
	if cfg.Tun.MapDNSNetmask == "" {
		cfg.Tun.MapDNSNetmask = defaultTun.MapDNSNetmask
	}
	if cfg.Tun.TaskStackSize <= 0 {
		cfg.Tun.TaskStackSize = defaultTun.TaskStackSize
	}
	if cfg.Tun.TCPBufferSize <= 0 {
		cfg.Tun.TCPBufferSize = defaultTun.TCPBufferSize
	}
	if cfg.Tun.ConnectTimeout <= 0 {
		cfg.Tun.ConnectTimeout = defaultTun.ConnectTimeout
	}
	if cfg.Core.LocalPort <= 0 {
		cfg.Core.LocalPort = 1080
	}
	if strings.TrimSpace(cfg.Core.WorkingDir) == "" {
		cfg.Core.WorkingDir = filepath.Join(runtimeDir, "workspace")
	}
	if strings.TrimSpace(cfg.Core.SudokuBinary) == "" {
		cfg.Core.SudokuBinary = defaultBinaryPath(runtimeDir, "sudoku")
	}
	if strings.TrimSpace(cfg.Core.HevBinary) == "" {
		cfg.Core.HevBinary = defaultBinaryPath(runtimeDir, "hev-socks5-tunnel")
	}
	if cfg.Core.LogLevel == "" {
		cfg.Core.LogLevel = "info"
	}
	if cfg.UI.Language == "" {
		cfg.UI.Language = "auto"
	}
	if cfg.UI.Theme == "" {
		cfg.UI.Theme = "auto"
	}
	for i := range cfg.PortForwards {
		if strings.TrimSpace(cfg.PortForwards[i].ID) == "" {
			cfg.PortForwards[i].ID = newID("pf_")
		}
		if strings.TrimSpace(cfg.PortForwards[i].Name) == "" {
			cfg.PortForwards[i].Name = fmt.Sprintf("Forward %d", i+1)
		}
	}
	for i := range cfg.Nodes {
		normalizeNode(&cfg.Nodes[i], cfg.Core.LocalPort)
	}
	if cfg.ActiveNodeID == "" && len(cfg.Nodes) > 0 {
		cfg.ActiveNodeID = cfg.Nodes[0].ID
	}
}

func defaultTunSettings(goos string) TunSettings {
	interfaceName := "sudoku0"
	if goos == "darwin" {
		// HEV's docs use "tun0" on macOS/FreeBSD; using a Linux-ish name breaks route setup.
		interfaceName = "tun0"
	}
	return TunSettings{
		Enabled:        false,
		InterfaceName:  interfaceName,
		MTU:            8500,
		IPv4:           "198.18.0.1",
		IPv6:           "fc00::1",
		BlockQUIC:      goos == "windows",
		SocksUDP:       "udp",
		SocksMark:      438,
		RouteTable:     20,
		LogLevel:       "warn",
		MapDNSEnabled:  true,
		MapDNSAddress:  "198.18.0.2",
		MapDNSPort:     53,
		MapDNSNetwork:  "198.18.0.0",
		MapDNSNetmask:  "255.254.0.0",
		TaskStackSize:  86016,
		TCPBufferSize:  65536,
		MaxSession:     0,
		ConnectTimeout: 10000,
	}
}

func normalizeNode(node *NodeConfig, fallbackPort int) {
	if node == nil {
		return
	}
	if node.LocalPort <= 0 {
		node.LocalPort = fallbackPort
	}
	if node.AEAD == "" {
		node.AEAD = "chacha20-poly1305"
	}
	if node.ASCII == "" {
		node.ASCII = "prefer_entropy"
	}
	if node.PaddingMin < 0 {
		node.PaddingMin = 0
	}
	if node.PaddingMax <= 0 {
		node.PaddingMax = 15
	}
	if node.PaddingMin > node.PaddingMax {
		node.PaddingMin = node.PaddingMax
	}
	if node.HTTPMask.Mode == "" {
		node.HTTPMask.Mode = "legacy"
	}
	if node.HTTPMask.Multiplex == "" {
		node.HTTPMask.Multiplex = "off"
	}
	if node.CustomTables == nil {
		node.CustomTables = []string{}
	}
}

func defaultBinaryPath(runtimeDir string, baseName string) string {
	n := baseName
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	return filepath.Join(runtimeDir, "bin", platform, n)
}
