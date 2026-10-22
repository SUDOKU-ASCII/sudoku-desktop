package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const configVersion = 7

// Store owns every path the app writes to. The tree is private to the user (0700) because
// it contains node keys and the controller secret.
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
	st := &Store{
		rootDir:    root,
		configPath: filepath.Join(root, "config.json"),
		runtimeDir: filepath.Join(root, "runtime"),
		logDir:     filepath.Join(root, "logs"),
	}
	if err := st.EnsureDirs(); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) EnsureDirs() error {
	for _, dir := range []string{s.rootDir, s.runtimeDir, s.logDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}

func (s *Store) RootDir() string    { return s.rootDir }
func (s *Store) RuntimeDir() string { return s.runtimeDir }
func (s *Store) LogDir() string     { return s.logDir }
func (s *Store) ConfigPath() string { return s.configPath }

func (s *Store) UsageHistoryPath() string { return filepath.Join(s.runtimeDir, "usage_history.json") }
func (s *Store) JournalPath() string      { return filepath.Join(s.runtimeDir, "journal.json") }
func (s *Store) ConfigsDir() string       { return filepath.Join(s.runtimeDir, "configs") }
func (s *Store) RuleCacheDir() string     { return filepath.Join(s.runtimeDir, "cache", "sudoku-rules") }

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
	migrated := cfg.Version < configVersion
	normalizeConfig(&cfg, s.runtimeDir)
	if migrated {
		// Persist the migration (and the freshly generated controller secret) right away.
		_ = s.Save(&cfg)
	}
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
	if err := writeFileAtomic(s.configPath, buf, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func DefaultConfig(runtimeDir string) *AppConfig {
	cfg := &AppConfig{
		Version: configVersion,
		Nodes:   []NodeConfig{},
		Routing: RoutingSettings{
			ProxyMode: "pac",
			RuleURLs:  defaultPACRuleURLs(),
		},
		Tun: defaultTunSettings(runtime.GOOS),
		Core: CoreSettings{
			SudokuBinary: defaultBinaryPath(runtimeDir, "sudoku"),
			HevBinary:    defaultBinaryPath(runtimeDir, "hev-socks5-tunnel"),
			WorkingDir:   filepath.Join(runtimeDir, "workspace"),
			LogLevel:     "info",
		},
		ReverseClient:  ReverseClientSettings{Routes: []ReverseRoute{}},
		ReverseForward: ReverseForwarderSettings{ListenAddr: "127.0.0.1:2222"},
		PortForwards:   []PortForwardRule{},
		UI:             UISettings{Language: "auto", Theme: "auto"},
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
	cfg.Version = configVersion

	if cfg.Nodes == nil {
		cfg.Nodes = []NodeConfig{}
	}
	if cfg.PortForwards == nil {
		cfg.PortForwards = []PortForwardRule{}
	}
	if cfg.ReverseClient.Routes == nil {
		cfg.ReverseClient.Routes = []ReverseRoute{}
	}

	switch cfg.Routing.ProxyMode {
	case "global", "direct", "pac":
	default:
		cfg.Routing.ProxyMode = "pac"
	}
	if cfg.Routing.RuleURLs == nil {
		cfg.Routing.RuleURLs = []string{}
	}
	// PAC needs rule sources. If the list was cleared by accident (and there are no custom
	// rules), restore the recommended defaults so routing doesn't silently become global.
	if cfg.Routing.ProxyMode == "pac" && len(cfg.Routing.RuleURLs) == 0 &&
		(!cfg.Routing.CustomRulesEnabled || strings.TrimSpace(cfg.Routing.CustomRules) == "") {
		cfg.Routing.RuleURLs = defaultPACRuleURLs()
	}
	if prevVersion < 6 && cfg.Routing.ProxyMode == "pac" && len(cfg.Routing.RuleURLs) > 0 {
		hasReject := false
		for _, raw := range cfg.Routing.RuleURLs {
			if hasRejectRulePrefix(raw) {
				hasReject = true
				break
			}
		}
		if !hasReject {
			cfg.Routing.RuleURLs = append(cfg.Routing.RuleURLs, "!"+defaultPACRejectRuleURL)
		}
	}

	t := &cfg.Tun
	if t.InterfaceName == "" || (goos == "darwin" && t.InterfaceName == "sudoku0") {
		t.InterfaceName = defaultTun.InterfaceName
	}
	if t.MTU <= 0 {
		t.MTU = defaultTun.MTU
	}
	if t.IPv4 == "" {
		t.IPv4 = defaultTun.IPv4
	}
	if t.IPv6 == "" {
		t.IPv6 = defaultTun.IPv6
	}
	if t.SocksUDP == "" {
		t.SocksUDP = defaultTun.SocksUDP
	}
	if t.SocksMark <= 0 {
		t.SocksMark = defaultTun.SocksMark
	}
	if t.RouteTable <= 0 {
		t.RouteTable = defaultTun.RouteTable
	}
	if t.LogLevel == "" {
		t.LogLevel = defaultTun.LogLevel
	}
	if t.MapDNSAddress == "" {
		t.MapDNSAddress = defaultTun.MapDNSAddress
	}
	if t.MapDNSPort <= 0 {
		t.MapDNSPort = defaultTun.MapDNSPort
	}
	if t.MapDNSNetwork == "" {
		t.MapDNSNetwork = defaultTun.MapDNSNetwork
	}
	if t.MapDNSNetmask == "" {
		t.MapDNSNetmask = defaultTun.MapDNSNetmask
	}
	if t.TaskStackSize <= 0 {
		t.TaskStackSize = defaultTun.TaskStackSize
	}
	if t.TCPBufferSize <= 0 {
		t.TCPBufferSize = defaultTun.TCPBufferSize
	}
	if t.ConnectTimeout <= 0 {
		t.ConnectTimeout = defaultTun.ConnectTimeout
	}
	// PAC over TUN depends on MapDNS (FakeIP) to route by domain.
	if cfg.Routing.ProxyMode == "pac" {
		t.MapDNSEnabled = true
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

	if cfg.Controller.Port <= 0 || cfg.Controller.Port > 65535 || cfg.Controller.Port == ProxyPort {
		cfg.Controller.Port = DefaultControllerPort
	}
	if strings.TrimSpace(cfg.Controller.Secret) == "" {
		cfg.Controller.Secret = randomSecret()
	}

	if cfg.UI.Language == "" {
		cfg.UI.Language = "auto"
	}
	if !isValidUITheme(cfg.UI.Theme) {
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
		normalizeNode(&cfg.Nodes[i])
	}
	cfg.ActiveNodeID = resolveActiveNodeID(cfg)
}

// resolveActiveNodeID keeps the selection valid: an existing node, the balance pseudo-node
// (needs >= 2 nodes) or the first node.
func resolveActiveNodeID(cfg *AppConfig) string {
	if cfg.ActiveNodeID == BalanceNodeID && len(cfg.Nodes) >= 2 {
		return BalanceNodeID
	}
	for _, n := range cfg.Nodes {
		if n.ID == cfg.ActiveNodeID {
			return n.ID
		}
	}
	if len(cfg.Nodes) > 0 {
		return cfg.Nodes[0].ID
	}
	return ""
}

func isValidUITheme(theme string) bool {
	switch strings.ToLower(strings.TrimSpace(theme)) {
	case "auto", "atelier", "light", "dark", "qingshanlan", "langhualv", "fengxinzi", "manjianghong":
		return true
	default:
		return false
	}
}

func defaultTunSettings(goos string) TunSettings {
	interfaceName := "sudoku0"
	if goos == "darwin" {
		// HEV uses "tun0" on macOS; a Linux-style name breaks route setup there.
		interfaceName = "tun0"
	}
	return TunSettings{
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
		ConnectTimeout: 10000,
	}
}

func normalizeNode(node *NodeConfig) {
	if node == nil {
		return
	}
	if strings.TrimSpace(node.ID) == "" {
		node.ID = newID("node_")
	}
	if node.AEAD == "" {
		node.AEAD = "chacha20-poly1305"
	}
	node.ASCII = normalizeASCII(node.ASCII)
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
	if node.Multiplex == "" {
		node.Multiplex = strings.TrimSpace(node.HTTPMask.Multiplex)
	}
	if node.Multiplex == "" {
		node.Multiplex = "off"
	}
	node.HTTPMask.Multiplex = ""
	if node.CustomTables == nil {
		node.CustomTables = []string{}
	}
}

func defaultBinaryPath(runtimeDir string, baseName string) string {
	n := baseName
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return filepath.Join(runtimeDir, "bin", runtime.GOOS+"-"+runtime.GOARCH, n)
}

func randomSecret() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return newID("s")
	}
	return hex.EncodeToString(b[:])
}

// writeFileAtomic writes via a temp file + rename so a crash never leaves a torn file.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Chmod(perm); err != nil && runtime.GOOS != "windows" {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}
