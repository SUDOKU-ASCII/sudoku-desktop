export type ProxyMode = 'global' | 'direct' | 'pac'

export interface HTTPMaskSettings {
  disable: boolean
  mode: string
  tls: boolean
  host: string
  pathRoot: string
  multiplex: string
}

export interface NodeConfig {
  id: string
  name: string
  serverAddress: string
  key: string
  aead: string
  ascii: string
  paddingMin: number
  paddingMax: number
  enablePureDownlink: boolean
  customTable: string
  customTables: string[]
  httpMask: HTTPMaskSettings
  localPort: number
  enabled: boolean
}

export interface ReverseRoute {
  path: string
  target: string
  stripPrefix?: boolean | null
  hostHeader: string
}

export interface ReverseClientSettings {
  clientId: string
  routes: ReverseRoute[]
}

export interface RoutingSettings {
  proxyMode: ProxyMode
  ruleUrls: string[]
  customRulesEnabled: boolean
  customRules: string
}

export interface TunSettings {
  enabled: boolean
  interfaceName: string
  mtu: number
  ipv4: string
  ipv6: string
  blockQuic: boolean
  socksUdp: string
  socksMark: number
  routeTable: number
  logLevel: string
  mapDnsEnabled: boolean
  mapDnsAddress: string
  mapDnsPort: number
  mapDnsNetwork: string
  mapDnsNetmask: string
  taskStackSize: number
  tcpBufferSize: number
  maxSession: number
  connectTimeout: number
}

export interface CoreSettings {
  sudokuBinary: string
  hevBinary: string
  workingDir: string
  localPort: number
  allowLan: boolean
  logLevel: string
  autoStart: boolean
}

export interface ReverseForwarderSettings {
  dialUrl: string
  listenAddr: string
  insecure: boolean
}

export interface PortForwardRule {
  id: string
  name: string
  listen: string
  target: string
  enabled: boolean
}

export interface UISettings {
  language: string
  theme: string
  launchAtLogin: boolean
}

export interface AppConfig {
  version: number
  activeNodeId: string
  nodes: NodeConfig[]
  routing: RoutingSettings
  tun: TunSettings
  core: CoreSettings
  reverseClient: ReverseClientSettings
  reverseForward: ReverseForwarderSettings
  portForwards: PortForwardRule[]
  ui: UISettings
  lastStartedNode: string
}

export interface LogEntry {
  id: string
  timestamp: string
  level: string
  component: string
  message: string
  raw: string
}

export interface ActiveConnection {
  id: string
  network: string
  source: string
  destination: string
  direction: string
  lastSeen: string
  hits: number
}

export interface BandwidthSample {
  at: string
  txBps: number
  rxBps: number
  directBps: number
  proxyBps: number
  totalTx: number
  totalRx: number
}

export interface TrafficState {
  totalTx: number
  totalRx: number
  estimatedDirectTx: number
  estimatedDirectRx: number
  estimatedProxyTx: number
  estimatedProxyRx: number
  directConnDecisions: number
  proxyConnDecisions: number
  recentBandwidth: BandwidthSample[]
  interface: string
  interfaceFound: boolean
  lastSampleUnixMillis: number
}

export interface LatencyResult {
  nodeId: string
  nodeName: string
  latencyMs: number
  connectOk: boolean
  statusCode: number
  checkedAtUnix: number
  error: string
}

export interface IPDetectResult {
  ip: string
  region: string
  country: string
  isp: string
  source: string
  usedProxy: boolean
  checkedAtUnix: number
  error: string
}

export interface LANProxyInfo {
  port: number
  ips: string[]
  ready: boolean
}

export interface RuntimeState {
  running: boolean
  coreRunning: boolean
  tunRunning: boolean
  reverseRunning: boolean
  startedAtUnix: number
  activeNodeId: string
  activeNodeName: string
  lastError: string
  traffic: TrafficState
  latencies: LatencyResult[]
  connections: ActiveConnection[]
  recentLogs: LogEntry[]
  needsAdmin: boolean
  routeSetupError: string
}

export interface StartRequest {
  withTun: boolean
}

export interface UsageDay {
  date: string
  tx: number
  rx: number
  directTx: number
  directRx: number
  proxyTx: number
  proxyRx: number
}
