package core

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	sudokutable "github.com/SUDOKU-ASCII/sudoku/pkg/obfs/sudoku"
	"gopkg.in/yaml.v3"
)

type sudokuHTTPMask struct {
	Disable  bool   `json:"disable"`
	Mode     string `json:"mode"`
	TLS      bool   `json:"tls"`
	Host     string `json:"host"`
	PathRoot string `json:"path_root"`
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
	Name               string         `json:"name,omitempty"`
	Mode               string         `json:"mode"`
	Transport          string         `json:"transport"`
	LocalPort          int            `json:"local_port"`
	LocalAddress       string         `json:"local_address,omitempty"`
	ServerAddress      string         `json:"server_address"`
	Key                string         `json:"key"`
	AEAD               string         `json:"aead"`
	PaddingMin         int            `json:"padding_min"`
	PaddingMax         int            `json:"padding_max"`
	CustomTable        string         `json:"custom_table,omitempty"`
	CustomTables       []string       `json:"custom_tables,omitempty"`
	ASCII              string         `json:"ascii"`
	EnablePureDownlink bool           `json:"enable_pure_downlink"`
	Multiplex          string         `json:"multiplex"`
	HTTPMask           sudokuHTTPMask `json:"httpmask"`
	RuleURLs           []string       `json:"rule_urls"`
	Reverse            *sudokuReverse `json:"reverse,omitempty"`
	ExternalController string         `json:"external_controller,omitempty"`
	Secret             string         `json:"secret,omitempty"`
}

// kernelConfigFile is one `-c` config handed to the Sudoku core: one per node.
type kernelConfigFile struct {
	NodeID string
	Name   string // outbound name the controller reports for this node
	Path   string
	Config *sudokuClientConfig
}

// nodeProxyNames mirrors the core's own outbound naming (repeats get "#2", "#3"…, and the
// reserved group names are skipped) so we can address nodes through the controller.
func nodeProxyNames(nodes []NodeConfig) map[string]string {
	taken := map[string]bool{"DIRECT": true, "REJECT": true, "PROXY": true, "GLOBAL": true, "Balance": true}
	out := make(map[string]string, len(nodes))
	for _, n := range nodes {
		name := strings.TrimSpace(n.Name)
		if name == "" {
			name = n.ID
		}
		unique := name
		for i := 2; taken[unique]; i++ {
			unique = name + "#" + strconv.Itoa(i)
		}
		taken[unique] = true
		out[n.ID] = unique
	}
	return out
}

// controllerListenAddr is the `external_controller` value for the settings.
func controllerListenAddr(c ControllerSettings) string {
	if c.AllowLAN {
		return ":" + strconv.Itoa(c.Port)
	}
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(c.Port))
}

// effectiveRuleURLs returns the rule sources the core is always started with. The routing
// mode itself (pac|global|direct) is switched at runtime over the controller, so the full
// list is passed even in global/direct mode and changing mode never needs a restart.
func effectiveRuleURLs(r RoutingSettings, customRuleURL string) []string {
	urls := make([]string, 0, len(r.RuleURLs)+1)
	for _, u := range r.RuleURLs {
		if u = strings.TrimSpace(u); u != "" {
			urls = append(urls, u)
		}
	}
	if len(urls) == 0 {
		urls = defaultPACRuleURLs()
	}
	if r.CustomRulesEnabled && strings.TrimSpace(r.CustomRules) != "" && customRuleURL != "" {
		urls = append([]string{customRuleURL}, urls...)
	}
	return urls
}

// buildKernelConfigs renders every node into a core client config. The controller,
// secret, rule sources and reverse settings ride on the first config (the core reads them
// from there only).
func buildKernelConfigs(cfg *AppConfig, customRuleURL string) ([]kernelConfigFile, error) {
	if len(cfg.Nodes) == 0 {
		return nil, errors.New("no nodes configured")
	}
	names := nodeProxyNames(cfg.Nodes)
	files := make([]kernelConfigFile, 0, len(cfg.Nodes))
	for _, node := range cfg.Nodes {
		c, err := buildSudokuClientConfig(node)
		if err != nil {
			return nil, fmt.Errorf("node %q: %w", node.Name, err)
		}
		c.LocalAddress = proxyListenAddress(cfg.Core)
		files = append(files, kernelConfigFile{NodeID: node.ID, Name: names[node.ID], Config: c})
	}
	// An active node that is broken must not block the others: only drop broken nodes if
	// at least one is usable (buildSudokuClientConfig already errors on empty address/key).
	first := files[0].Config
	first.ExternalController = controllerListenAddr(cfg.Controller)
	first.Secret = cfg.Controller.Secret
	first.RuleURLs = effectiveRuleURLs(cfg.Routing, customRuleURL)
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
		first.Reverse = rev
	}
	return files, nil
}

// writeKernelConfigs persists the configs (0600: they contain keys) and returns the paths in
// node order. Stale files from earlier runs are removed.
func writeKernelConfigs(dir string, files []kernelConfigFile) ([]string, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	if old, _ := filepath.Glob(filepath.Join(dir, "node-*.json")); len(old) > 0 {
		for _, p := range old {
			_ = os.Remove(p)
		}
	}
	paths := make([]string, 0, len(files))
	for i := range files {
		p := filepath.Join(dir, fmt.Sprintf("node-%02d.json", i))
		if err := writeJSONFile(p, files[i].Config); err != nil {
			return nil, fmt.Errorf("write sudoku config: %w", err)
		}
		files[i].Path = p
		paths = append(paths, p)
	}
	return paths, nil
}

func buildSudokuClientConfig(node NodeConfig) (*sudokuClientConfig, error) {
	if strings.TrimSpace(node.ServerAddress) == "" {
		return nil, errors.New("server address is empty")
	}
	if strings.TrimSpace(node.Key) == "" {
		return nil, errors.New("key is empty")
	}
	c := &sudokuClientConfig{
		Name:               strings.TrimSpace(node.Name),
		Mode:               "client",
		Transport:          "tcp",
		LocalPort:          ProxyPort,
		ServerAddress:      strings.TrimSpace(node.ServerAddress),
		Key:                strings.TrimSpace(node.Key),
		AEAD:               strings.TrimSpace(node.AEAD),
		PaddingMin:         node.PaddingMin,
		PaddingMax:         node.PaddingMax,
		CustomTable:        strings.TrimSpace(node.CustomTable),
		CustomTables:       append([]string(nil), node.CustomTables...),
		ASCII:              normalizeASCII(node.ASCII),
		EnablePureDownlink: node.EnablePureDownlink,
		Multiplex:          strings.TrimSpace(node.Multiplex),
		HTTPMask: sudokuHTTPMask{
			Disable:  node.HTTPMask.Disable,
			Mode:     strings.TrimSpace(node.HTTPMask.Mode),
			TLS:      node.HTTPMask.TLS,
			Host:     strings.TrimSpace(node.HTTPMask.Host),
			PathRoot: strings.TrimSpace(node.HTTPMask.PathRoot),
		},
		// Placeholder so a lone node never trips the "no rule sources" default; the first
		// config's list is replaced with the real one.
		RuleURLs: []string{"global"},
	}
	if c.AEAD == "" {
		c.AEAD = "chacha20-poly1305"
	}
	if c.HTTPMask.Mode == "" {
		c.HTTPMask.Mode = "legacy"
	}
	if c.Multiplex == "" {
		c.Multiplex = "off"
	}
	if c.PaddingMax <= 0 {
		c.PaddingMax = 15
	}
	if c.PaddingMin > c.PaddingMax {
		c.PaddingMin = c.PaddingMax
	}
	return c, nil
}

func normalizeASCII(s string) string {
	normalized, err := sudokutable.NormalizeASCIIMode(s)
	if err == nil {
		return normalized
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
	Port        int    `yaml:"port"`
	Address     string `yaml:"address"`
	UDP         string `yaml:"udp"`
	Mark        int    `yaml:"mark"`
	TCPFastOpen bool   `yaml:"tcp-fastopen"`
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

// buildHevConfig renders the hev-socks5-tunnel config for the fixed local SOCKS5 port.
func buildHevConfig(tun TunSettings) *hevTunnelConfig {
	c := &hevTunnelConfig{
		Tunnel: hevTunnelSection{
			Name: tun.InterfaceName,
			MTU:  tun.MTU,
			IPv4: tun.IPv4,
			IPv6: tun.IPv6,
		},
		Socks5: hevSocks5Section{
			Port:        ProxyPort,
			Address:     "127.0.0.1",
			UDP:         strings.TrimSpace(tun.SocksUDP),
			Mark:        tun.SocksMark,
			TCPFastOpen: true,
		},
		Misc: &hevMiscSection{
			TaskStackSize:   tun.TaskStackSize,
			TCPBufferSize:   tun.TCPBufferSize,
			MaxSessionCount: tun.MaxSession,
			ConnectTimeout:  tun.ConnectTimeout,
			LogFile:         "stderr",
			LogLevel:        strings.TrimSpace(tun.LogLevel),
		},
	}
	if c.Socks5.UDP == "" {
		c.Socks5.UDP = "udp"
	}
	if tun.MapDNSEnabled {
		c.MapDNS = &hevMapDNSSection{
			Address:   tun.MapDNSAddress,
			Port:      tun.MapDNSPort,
			Network:   tun.MapDNSNetwork,
			Netmask:   tun.MapDNSNetmask,
			CacheSize: 10000,
		}
	}
	if c.Misc.LogLevel == "" {
		c.Misc.LogLevel = "warn"
	}
	return c
}

func writeHevConfig(path string, tun TunSettings) error {
	buf, err := yaml.Marshal(buildHevConfig(tun))
	if err != nil {
		return fmt.Errorf("marshal hev config: %w", err)
	}
	return writeFileAtomic(path, buf, 0o644)
}

// proxyListenAddress is the IP the mixed proxy port binds to: loopback unless the user
// shares the proxy with the LAN. Cores that predate local_address ignore it and keep
// listening on every interface.
func proxyListenAddress(core CoreSettings) string {
	if core.AllowLAN {
		return ""
	}
	return localLoopbackIPv4
}
