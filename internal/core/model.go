package core

import "time"

// Events pushed to the frontend. Keep payloads small: the state event fires on
// every lifecycle change, the traffic event once per second while running.
const (
	EventStateUpdated   = "core:state"
	EventTrafficUpdated = "core:traffic"
	EventLogAdded       = "core:log"
)

const (
	// ProxyPort is the fixed local mixed (HTTP/SOCKS) port. It is also the system proxy
	// port and the SOCKS upstream of the TUN. Intentionally not user-configurable.
	ProxyPort = 10233

	// DefaultControllerPort is the default Clash-compatible external controller port
	// (the port web dashboards such as zashboard/metacubexd/yacd default to).
	DefaultControllerPort = 9097

	// BalanceNodeID in AppConfig.ActiveNodeID selects the upstream "Balance" group,
	// which spreads connections over every node.
	BalanceNodeID = "@balance"
)

type Phase string

const (
	PhaseStopped  Phase = "stopped"
	PhaseStarting Phase = "starting"
	PhaseRunning  Phase = "running"
	PhaseStopping Phase = "stopping"
)

type HTTPMaskSettings struct {
	Disable  bool   `json:"disable"`
	Mode     string `json:"mode"`
	TLS      bool   `json:"tls"`
	Host     string `json:"host"`
	PathRoot string `json:"pathRoot"`
	// Multiplex is read only for migration from legacy configs.
	Multiplex string `json:"multiplex,omitempty"`
}

type NodeConfig struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	ServerAddress      string           `json:"serverAddress"`
	Key                string           `json:"key"`
	AEAD               string           `json:"aead"`
	ASCII              string           `json:"ascii"`
	PaddingMin         int              `json:"paddingMin"`
	PaddingMax         int              `json:"paddingMax"`
	EnablePureDownlink bool             `json:"enablePureDownlink"`
	CustomTable        string           `json:"customTable"`
	CustomTables       []string         `json:"customTables"`
	Multiplex          string           `json:"multiplex"`
	HTTPMask           HTTPMaskSettings `json:"httpMask"`
}

type ReverseRoute struct {
	Path        string `json:"path"`
	Target      string `json:"target"`
	StripPrefix *bool  `json:"stripPrefix,omitempty"`
	HostHeader  string `json:"hostHeader"`
}

type ReverseClientSettings struct {
	ClientID string         `json:"clientId"`
	Routes   []ReverseRoute `json:"routes"`
}

type RoutingSettings struct {
	ProxyMode          string   `json:"proxyMode"`
	RuleURLs           []string `json:"ruleUrls"`
	CustomRulesEnabled bool     `json:"customRulesEnabled"`
	CustomRules        string   `json:"customRules"`
}

type TunSettings struct {
	Enabled        bool   `json:"enabled"`
	InterfaceName  string `json:"interfaceName"`
	MTU            int    `json:"mtu"`
	IPv4           string `json:"ipv4"`
	IPv6           string `json:"ipv6"`
	BlockQUIC      bool   `json:"blockQuic"`
	SocksUDP       string `json:"socksUdp"`
	SocksMark      int    `json:"socksMark"`
	RouteTable     int    `json:"routeTable"`
	LogLevel       string `json:"logLevel"`
	MapDNSEnabled  bool   `json:"mapDnsEnabled"`
	MapDNSAddress  string `json:"mapDnsAddress"`
	MapDNSPort     int    `json:"mapDnsPort"`
	MapDNSNetwork  string `json:"mapDnsNetwork"`
	MapDNSNetmask  string `json:"mapDnsNetmask"`
	TaskStackSize  int    `json:"taskStackSize"`
	TCPBufferSize  int    `json:"tcpBufferSize"`
	MaxSession     int    `json:"maxSession"`
	ConnectTimeout int    `json:"connectTimeout"`
}

type CoreSettings struct {
	SudokuBinary string `json:"sudokuBinary"`
	HevBinary    string `json:"hevBinary"`
	WorkingDir   string `json:"workingDir"`
	LogLevel     string `json:"logLevel"`
	AutoStart    bool   `json:"autoStart"`
	// AllowLAN lets other devices on the network use the local proxy port.
	AllowLAN bool `json:"allowLan"`
}

// ControllerSettings configures the Clash-compatible external controller that the
// Sudoku core exposes. The desktop app itself uses it for everything (mode/node
// switching, latency tests, traffic, connections) and web dashboards can connect too.
type ControllerSettings struct {
	// Port is the controller TCP port (1-65535).
	Port int `json:"port"`
	// Secret is the bearer token. Never empty: a random one is generated on first run.
	Secret string `json:"secret"`
	// AllowLAN makes the controller listen on every interface instead of loopback only.
	AllowLAN bool `json:"allowLan"`
}

type ReverseForwarderSettings struct {
	DialURL    string `json:"dialUrl"`
	ListenAddr string `json:"listenAddr"`
	Insecure   bool   `json:"insecure"`
}

type PortForwardRule struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Listen  string `json:"listen"`
	Target  string `json:"target"`
	Enabled bool   `json:"enabled"`
}

type UISettings struct {
	Language      string `json:"language"`
	Theme         string `json:"theme"`
	LaunchAtLogin bool   `json:"launchAtLogin"`
}

type AppConfig struct {
	Version         int                      `json:"version"`
	ActiveNodeID    string                   `json:"activeNodeId"`
	Nodes           []NodeConfig             `json:"nodes"`
	Routing         RoutingSettings          `json:"routing"`
	Tun             TunSettings              `json:"tun"`
	Core            CoreSettings             `json:"core"`
	Controller      ControllerSettings       `json:"controller"`
	ReverseClient   ReverseClientSettings    `json:"reverseClient"`
	ReverseForward  ReverseForwarderSettings `json:"reverseForward"`
	PortForwards    []PortForwardRule        `json:"portForwards"`
	UI              UISettings               `json:"ui"`
	LastStartedNode string                   `json:"lastStartedNode"`
}

type LogEntry struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Component string    `json:"component"`
	Message   string    `json:"message"`
	Raw       string    `json:"raw"`
}

// ActiveConnection is one live connection as reported by the core's /connections.
type ActiveConnection struct {
	ID          string    `json:"id"`
	Network     string    `json:"network"`
	Type        string    `json:"type"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Direction   string    `json:"direction"` // "direct" | "proxy" | "reject"
	Node        string    `json:"node"`      // outbound node name ("DIRECT" for direct)
	Rule        string    `json:"rule"`
	Upload      uint64    `json:"upload"`
	Download    uint64    `json:"download"`
	StartedAt   time.Time `json:"startedAt"`
}

type BandwidthSample struct {
	At      time.Time `json:"at"`
	TxBps   float64   `json:"txBps"`
	RxBps   float64   `json:"rxBps"`
	Direct  float64   `json:"directBps"`
	Proxy   float64   `json:"proxyBps"`
	TotalTx uint64    `json:"totalTx"`
	TotalRx uint64    `json:"totalRx"`
}

type TrafficState struct {
	TotalTx           uint64 `json:"totalTx"`
	TotalRx           uint64 `json:"totalRx"`
	EstimatedDirectTx uint64 `json:"estimatedDirectTx"`
	EstimatedDirectRx uint64 `json:"estimatedDirectRx"`
	EstimatedProxyTx  uint64 `json:"estimatedProxyTx"`
	EstimatedProxyRx  uint64 `json:"estimatedProxyRx"`
	DirectConns       int    `json:"directConns"`
	ProxyConns        int    `json:"proxyConns"`
}

type KernelState struct {
	Version     string `json:"version"`
	MemoryBytes uint64 `json:"memoryBytes"`
}

type LatencyResult struct {
	NodeID        string `json:"nodeId"`
	NodeName      string `json:"nodeName"`
	LatencyMs     int64  `json:"latencyMs"`
	ConnectOK     bool   `json:"connectOk"`
	CheckedAtUnix int64  `json:"checkedAtUnix"`
	Error         string `json:"error"`
}

type IPDetectResult struct {
	IP            string `json:"ip"`
	Region        string `json:"region"`
	Country       string `json:"country"`
	ISP           string `json:"isp"`
	Source        string `json:"source"`
	UsedProxy     bool   `json:"usedProxy"`
	CheckedAtUnix int64  `json:"checkedAtUnix"`
	Error         string `json:"error"`
}

type LANProxyInfo struct {
	Port  int      `json:"port"`
	IPs   []string `json:"ips"`
	Ready bool     `json:"ready"`
}

// ControllerInfo describes how to reach the external controller (for the web dashboard
// buttons and the "copy endpoint" helpers in the UI).
type ControllerInfo struct {
	Ready    bool     `json:"ready"`
	Port     int      `json:"port"`
	Secret   string   `json:"secret"`
	AllowLAN bool     `json:"allowLan"`
	URL      string   `json:"url"`     // http://127.0.0.1:9097
	LANURLs  []string `json:"lanUrls"` // http://192.168.x.x:9097 (only when AllowLAN)
	Boards   []Board  `json:"boards"`  // ready-to-open dashboard links
	Version  string   `json:"version"` // core version string reported by /version
}

// Board is a hosted web dashboard with the connection parameters pre-filled.
type Board struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type RuntimeState struct {
	Phase          Phase  `json:"phase"`
	Running        bool   `json:"running"`
	CoreRunning    bool   `json:"coreRunning"`
	TunRunning     bool   `json:"tunRunning"`
	TunRequested   bool   `json:"tunRequested"`
	ReverseRunning bool   `json:"reverseRunning"`
	SystemProxyOn  bool   `json:"systemProxyOn"`
	StartedAtUnix  int64  `json:"startedAtUnix"`
	ActiveNodeID   string `json:"activeNodeId"`
	ActiveNodeName string `json:"activeNodeName"`
	// Mode is the routing mode the core is using right now (pac|global|direct).
	Mode            string          `json:"mode"`
	LastError       string          `json:"lastError"`
	Traffic         TrafficState    `json:"traffic"`
	Kernel          KernelState     `json:"kernel"`
	Latencies       []LatencyResult `json:"latencies"`
	ConnectionCount int             `json:"connectionCount"`
	NeedsNetRepair  bool            `json:"needsNetRepair"`
	ProxyPort       int             `json:"proxyPort"`
	// NeedsAdmin asks the UI for the login password (macOS / Linux): an action that needs
	// the tunnel helper is waiting for it. AdminError is set after a rejected password.
	NeedsAdmin bool   `json:"needsAdmin"`
	AdminError string `json:"adminError"`
}

// TrafficTick is the once-per-second payload of the traffic event.
type TrafficTick struct {
	Sample          BandwidthSample `json:"sample"`
	Traffic         TrafficState    `json:"traffic"`
	MemoryBytes     uint64          `json:"memoryBytes"`
	ConnectionCount int             `json:"connectionCount"`
}

type StartRequest struct {
	WithTun bool `json:"withTun"`
}

type UsageDay struct {
	Date     string `json:"date"`
	Tx       uint64 `json:"tx"`
	Rx       uint64 `json:"rx"`
	DirectTx uint64 `json:"directTx"`
	DirectRx uint64 `json:"directRx"`
	ProxyTx  uint64 `json:"proxyTx"`
	ProxyRx  uint64 `json:"proxyRx"`
}
