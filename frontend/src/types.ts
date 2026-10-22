export type ProxyMode = 'global' | 'direct' | 'pac'

export const BALANCE_NODE_ID = '@balance'
export const DEFAULT_PROXY_PORT = 10233
export const DEFAULT_CONTROLLER_PORT = 9097

export interface HTTPMaskSettings {
  disable: boolean
  mode: string
  tls: boolean
  host: string
  pathRoot: string
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
  multiplex: string
  httpMask: HTTPMaskSettings
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
  logLevel: string
  autoStart: boolean
  allowLan: boolean
}

export interface ControllerSettings {
  port: number
  secret: string
  allowLan: boolean
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
  controller: ControllerSettings
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
  type: string
  source: string
  destination: string
  direction: 'direct' | 'proxy' | 'reject' | string
  node: string
  rule: string
  upload: number
  download: number
  startedAt: string
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
  directConns: number
  proxyConns: number
}

export interface KernelState {
  version: string
  memoryBytes: number
}

export interface LatencyResult {
  nodeId: string
  nodeName: string
  latencyMs: number
  connectOk: boolean
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

export type Phase = 'stopped' | 'starting' | 'running' | 'stopping'

export interface RuntimeState {
  phase: Phase
  running: boolean
  coreRunning: boolean
  tunRunning: boolean
  tunRequested: boolean
  reverseRunning: boolean
  systemProxyOn: boolean
  startedAtUnix: number
  activeNodeId: string
  activeNodeName: string
  mode: string
  lastError: string
  traffic: TrafficState
  kernel: KernelState
  latencies: LatencyResult[]
  connectionCount: number
  needsNetRepair: boolean
  proxyPort: number
  needsAdmin: boolean
  adminError: string
}

export interface TrafficTick {
  sample: BandwidthSample
  traffic: TrafficState
  memoryBytes: number
  connectionCount: number
}

export interface ControllerBoard {
  id: string
  name: string
  url: string
}

export interface ControllerInfo {
  ready: boolean
  port: number
  secret: string
  allowLan: boolean
  url: string
  lanUrls: string[]
  boards: ControllerBoard[]
  version: string
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
