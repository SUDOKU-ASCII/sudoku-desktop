package core

import "time"

const (
	EventStateUpdated = "core:state"
	EventLogAdded     = "core:log"
)

type HTTPMaskSettings struct {
	Disable   bool   `json:"disable"`
	Mode      string `json:"mode"`
	TLS       bool   `json:"tls"`
	Host      string `json:"host"`
	PathRoot  string `json:"pathRoot"`
	Multiplex string `json:"multiplex"`
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
	HTTPMask           HTTPMaskSettings `json:"httpMask"`
	LocalPort          int              `json:"localPort"`
	Enabled            bool             `json:"enabled"`
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
	LocalPort    int    `json:"localPort"`
	AllowLAN     bool   `json:"allowLan"`
	LogLevel     string `json:"logLevel"`
	AutoStart    bool   `json:"autoStart"`
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

type ActiveConnection struct {
	ID          string    `json:"id"`
	Network     string    `json:"network"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Direction   string    `json:"direction"`
	LastSeen    time.Time `json:"lastSeen"`
	Hits        int64     `json:"hits"`
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
	TotalTx              uint64            `json:"totalTx"`
	TotalRx              uint64            `json:"totalRx"`
	EstimatedDirectTx    uint64            `json:"estimatedDirectTx"`
	EstimatedDirectRx    uint64            `json:"estimatedDirectRx"`
	EstimatedProxyTx     uint64            `json:"estimatedProxyTx"`
	EstimatedProxyRx     uint64            `json:"estimatedProxyRx"`
	DirectConnDecisions  uint64            `json:"directConnDecisions"`
	ProxyConnDecisions   uint64            `json:"proxyConnDecisions"`
	RecentBandwidth      []BandwidthSample `json:"recentBandwidth"`
	Interface            string            `json:"interface"`
	InterfaceFound       bool              `json:"interfaceFound"`
	LastSampleUnixMillis int64             `json:"lastSampleUnixMillis"`
}

type LatencyResult struct {
	NodeID        string `json:"nodeId"`
	NodeName      string `json:"nodeName"`
	LatencyMs     int64  `json:"latencyMs"`
	ConnectOK     bool   `json:"connectOk"`
	StatusCode    int    `json:"statusCode"`
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

type RuntimeState struct {
	Running         bool               `json:"running"`
	CoreRunning     bool               `json:"coreRunning"`
	TunRunning      bool               `json:"tunRunning"`
	ReverseRunning  bool               `json:"reverseRunning"`
	StartedAtUnix   int64              `json:"startedAtUnix"`
	ActiveNodeID    string             `json:"activeNodeId"`
	ActiveNodeName  string             `json:"activeNodeName"`
	LastError       string             `json:"lastError"`
	Traffic         TrafficState       `json:"traffic"`
	Latencies       []LatencyResult    `json:"latencies"`
	Connections     []ActiveConnection `json:"connections"`
	RecentLogs      []LogEntry         `json:"recentLogs"`
	NeedsAdmin      bool               `json:"needsAdmin"`
	RouteSetupError string             `json:"routeSetupError"`
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
