import type {
  AppConfig,
  RuntimeState,
  StartRequest,
  NodeConfig,
  LatencyResult,
  IPDetectResult,
  LogEntry,
  ActiveConnection,
  UsageDay,
} from './types'

declare global {
  interface Window {
    go?: any
  }
}

const app = () => window.go?.main?.App

function call<T>(method: string, ...args: any[]): Promise<T> {
  const fn = app()?.[method]
  if (!fn) {
    return Promise.reject(new Error(`Wails method not found: ${method}`))
  }
  return fn(...args) as Promise<T>
}

export const backendApi = {
  getConfig: () => call<AppConfig>('GetConfig'),
  saveConfig: (cfg: AppConfig) => call<void>('SaveConfig', cfg),
  getState: () => call<RuntimeState>('GetState'),
  startProxy: (req: StartRequest) => call<void>('StartProxy', req),
  stopProxy: () => call<void>('StopProxy'),
  restartProxy: (req: StartRequest) => call<void>('RestartProxy', req),
  upsertNode: (node: NodeConfig) => call<NodeConfig>('UpsertNode', node),
  deleteNode: (id: string) => call<void>('DeleteNode', id),
  setActiveNode: (id: string) => call<void>('SetActiveNode', id),
  switchNode: (id: string) => call<void>('SwitchNode', id),
  importShortLink: (name: string, link: string) => call<NodeConfig>('ImportShortLink', name, link),
  exportShortLink: (id: string) => call<string>('ExportShortLink', id),
  probeNode: (id: string) => call<LatencyResult>('ProbeNode', id),
  probeAllNodes: () => call<LatencyResult[]>('ProbeAllNodes'),
  autoSelectLowestLatency: () => call<LatencyResult>('AutoSelectLowestLatency'),
  sortNodesByName: () => call<void>('SortNodesByName'),
  sortNodesByLatency: () => call<void>('SortNodesByLatency'),
  detectIPDirect: () => call<IPDetectResult>('DetectIPDirect'),
  detectIPProxy: () => call<IPDetectResult>('DetectIPProxy'),
  startReverseForwarder: () => call<void>('StartReverseForwarder'),
  stopReverseForwarder: () => call<void>('StopReverseForwarder'),
  getLogs: (level: string, limit: number) => call<LogEntry[]>('GetLogs', level, limit),
  getConnections: () => call<ActiveConnection[]>('GetConnections'),
  getUsageHistory: (limit: number) => call<UsageDay[]>('GetUsageHistory', limit),
  ensureCoreBinaries: () => call<void>('EnsureCoreBinaries'),
  buildInfo: () => call<Record<string, string>>('BuildInfo'),
}
