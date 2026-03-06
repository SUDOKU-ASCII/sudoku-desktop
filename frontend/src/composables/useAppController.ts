import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { Clipboard, Dialogs, Events } from '@wailsio/runtime'
import { backendApi } from '../api'
import { useI18n } from '../i18n'
import type {
  AppConfig,
  LANProxyInfo,
  IPDetectResult,
  LatencyResult,
  LogEntry,
  NodeConfig,
  PortForwardRule,
  ProxyMode,
  RuntimeState,
  UsageDay,
} from '../types'

export const useAppController = () => {
const { locale, t } = useI18n()

const logoUrl = new URL('../assets/images/logo-universal.png', import.meta.url).href

const navItems = [
  {
    key: 'dashboard',
    section: 'main',
    icon: ['M4 13.5L12 6l8 7.5', 'M6 12.8V20h4.5v-4h3V20H18v-7.2'],
  },
  {
    key: 'nodes',
    section: 'main',
    icon: ['M6 7h12v4H6z', 'M6 14h12v4H6z', 'M9 9h.01', 'M9 16h.01'],
  },
  {
    key: 'routing',
    section: 'main',
    icon: ['M5 8h8', 'M5 16h8', 'M13 8l3-3 3 3', 'M13 16l3 3 3-3'],
  },
  {
    key: 'tun',
    section: 'main',
    icon: ['M4 12h16', 'M7 8h10', 'M7 16h10', 'M12 4v16'],
  },
  {
    key: 'relay',
    section: 'main',
    icon: ['M4 12h11', 'M10 7l5 5-5 5', 'M20 7v10'],
  },
  {
    key: 'logs',
    section: 'main',
    icon: ['M8 7h13', 'M8 12h13', 'M8 17h13', 'M3 7h.01', 'M3 12h.01', 'M3 17h.01'],
  },
  {
    key: 'misc',
    section: 'main',
    icon: ['M12 3v3', 'M12 18v3', 'M4.8 6.8l2.1 2.1', 'M17.1 15.1l2.1 2.1', 'M3 12h3', 'M18 12h3', 'M4.8 17.2l2.1-2.1', 'M17.1 8.9l2.1-2.1', 'M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6'],
  },
  {
    key: 'game',
    section: 'extra',
    icon: ['M4 4h16v16H4z', 'M4 12h16', 'M12 4v16'],
  },
] as const

type TabKey = (typeof navItems)[number]['key']
const currentTab = ref<TabKey>('dashboard')
const navMain = navItems.filter((x) => x.section === 'main')
const navExtra = navItems.filter((x) => x.section === 'extra')

const safeLocalStorageGet = (key: string): string | null => {
  try {
    return window.localStorage?.getItem(key) ?? null
  } catch {
    return null
  }
}
const safeLocalStorageSet = (key: string, value: string) => {
  try {
    window.localStorage?.setItem(key, value)
  } catch {
    // ignore
  }
}

const sidebarCollapsed = ref(false)
onMounted(() => {
  sidebarCollapsed.value = safeLocalStorageGet('ui.sidebarCollapsed') === '1'
})
watch(sidebarCollapsed, (v) => safeLocalStorageSet('ui.sidebarCollapsed', v ? '1' : '0'))
const toggleSidebar = () => {
  sidebarCollapsed.value = !sidebarCollapsed.value
}

const busy = ref(false)
const proxyOpBusy = ref(false)
type ProxyOpState = 'idle' | 'starting' | 'stopping' | 'restarting'
const proxyOpState = ref<ProxyOpState>('idle')
const notice = ref('')
const noticeType = ref<'ok' | 'error'>('ok')
const loadReady = ref(false)
const isMacLike = /Mac|Darwin/i.test(navigator.userAgent)
const isWindowsLike = /Windows/i.test(navigator.userAgent)

const tunAdminModalOpen = ref(false)
const tunAdminPassword = ref('')
const tunAdminBusy = ref(false)
const tunAdminError = ref('')
let tunAdminPromise: Promise<boolean> | null = null
let tunAdminPromiseResolve: ((ok: boolean) => void) | null = null

const emptyNode = (): NodeConfig => ({
  id: '',
  name: '',
  serverAddress: '',
  key: '',
  aead: 'chacha20-poly1305',
  ascii: 'prefer_entropy',
  paddingMin: 5,
  paddingMax: 15,
  enablePureDownlink: true,
  customTable: '',
  customTables: [],
  httpMask: {
    disable: false,
    mode: 'auto',
    tls: true,
    host: '',
    pathRoot: '',
    multiplex: 'auto',
  },
  localPort: 1080,
  enabled: true,
})

const defaultTunConfig = (macLike: boolean, windowsLike: boolean) => ({
  enabled: false,
  interfaceName: macLike ? 'tun0' : 'sudoku0',
  mtu: 8500,
  ipv4: '198.18.0.1',
  ipv6: 'fc00::1',
  blockQuic: windowsLike,
  socksUdp: 'udp',
  socksMark: 438,
  routeTable: 20,
  logLevel: 'warn',
  mapDnsEnabled: true,
  mapDnsAddress: '198.18.0.2',
  mapDnsPort: 53,
  mapDnsNetwork: '198.18.0.0',
  mapDnsNetmask: '255.254.0.0',
  taskStackSize: 86016,
  tcpBufferSize: 65536,
  maxSession: 0,
  connectTimeout: 10000,
})

const config = reactive<AppConfig>({
  version: 5,
  activeNodeId: '',
  nodes: [],
  routing: { proxyMode: 'pac', ruleUrls: [], customRulesEnabled: false, customRules: '' },
  tun: defaultTunConfig(isMacLike, isWindowsLike),
  core: {
    sudokuBinary: '',
    hevBinary: '',
    workingDir: '',
    localPort: 1080,
    allowLan: false,
    logLevel: 'info',
    autoStart: false,
  },
  reverseClient: { clientId: '', routes: [] },
  reverseForward: { dialUrl: '', listenAddr: '127.0.0.1:2222', insecure: false },
  portForwards: [],
  ui: { language: 'auto', theme: 'auto', launchAtLogin: false },
  lastStartedNode: '',
})

const state = reactive<RuntimeState>({
  running: false,
  coreRunning: false,
  tunRunning: false,
  reverseRunning: false,
  startedAtUnix: 0,
  activeNodeId: '',
  activeNodeName: '',
  lastError: '',
  traffic: {
    totalTx: 0,
    totalRx: 0,
    estimatedDirectTx: 0,
    estimatedDirectRx: 0,
    estimatedProxyTx: 0,
    estimatedProxyRx: 0,
    directConnDecisions: 0,
    proxyConnDecisions: 0,
    recentBandwidth: [],
    interface: '',
    interfaceFound: false,
    lastSampleUnixMillis: 0,
  },
  latencies: [],
  connections: [],
  recentLogs: [],
  needsAdmin: false,
  routeSetupError: '',
})

const cloneDeep = <T,>(input: T): T => JSON.parse(JSON.stringify(input)) as T

const editableNode = reactive<NodeConfig>(emptyNode())
const selectedNodeId = ref('')
const nodeEditorOpen = ref(false)
const nodeEditorMode = ref<'create' | 'edit'>('create')
const shortlinkInput = ref('')
const shortlinkName = ref('')
const logLevelFilter = ref('all')
const logSearch = ref('')
const logDisplayLimit = ref(600)
const showTrafficLogs = ref(false)
const logs = ref<LogEntry[]>([])
const connectionOpBusy = ref(false)
let logQueue: LogEntry[] = []
let logFlushTimer: number | null = null
let pendingState: RuntimeState | null = null
let stateFlushTimer: number | null = null
const proxyIP = ref<IPDetectResult | null>(null)
const directIP = ref<IPDetectResult | null>(null)
const lanProxyInfo = ref<LANProxyInfo>({ port: 1080, ips: [], ready: false })
const usageHistory = ref<UsageDay[]>([])
let usageHistoryTimer: number | null = null
let customRulesValidateTimer: number | null = null
const customRulesValidation = ref<{ status: 'idle' | 'checking' | 'ok' | 'error'; message: string }>({
  status: 'idle',
  message: '',
})
const tunAutoSaveLock = ref(true)
const skipTunEnabledAutoSaveOnce = ref(false)

const applyLocaleFromConfig = () => {
  if (config.ui.language === 'auto') {
    return
  }
  if (config.ui.language.startsWith('ru')) locale.value = 'ru'
  else if (config.ui.language.startsWith('zh')) locale.value = 'zh'
  else locale.value = 'en'
}

const resolveThemeMode = (): 'light' | 'dark' => {
  if (config.ui.theme === 'dark') return 'dark'
  if (config.ui.theme === 'light') return 'light'
  return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

const applyDocumentTheme = () => {
  const mode = resolveThemeMode()
  document.documentElement.setAttribute('data-app-theme', mode)
  document.body?.setAttribute('data-app-theme', mode)
}

const flash = (message: string, type: 'ok' | 'error' = 'ok') => {
  notice.value = message
  noticeType.value = type
  setTimeout(() => {
    if (notice.value === message) {
      notice.value = ''
    }
  }, 2600)
}

const isAdminRequiredError = (e: any): boolean => {
  const msg = String(e?.message || '').toLowerCase()
  return msg.includes('administrator privileges required') || msg.includes('admin privileges required')
}

const openTunAdminModal = async (): Promise<boolean> => {
  if (tunAdminPromise) return tunAdminPromise
  tunAdminModalOpen.value = true
  tunAdminPassword.value = ''
  tunAdminError.value = ''
  tunAdminBusy.value = false
  tunAdminPromise = new Promise<boolean>((resolve) => {
    tunAdminPromiseResolve = resolve
  })
  return tunAdminPromise
}

const closeTunAdminModal = (ok = false) => {
  tunAdminModalOpen.value = false
  tunAdminBusy.value = false
  tunAdminError.value = ''
  tunAdminPassword.value = ''
  tunAdminPromiseResolve?.(ok)
  tunAdminPromise = null
  tunAdminPromiseResolve = null
}

const submitTunAdminModal = async () => {
  if (tunAdminBusy.value) return
  tunAdminBusy.value = true
  tunAdminError.value = ''
  try {
    await backendApi.tunAcquirePrivileges(tunAdminPassword.value)
    closeTunAdminModal(true)
  } catch (e: any) {
    tunAdminError.value = e?.message || t('tunAdminFailed')
  } finally {
    tunAdminBusy.value = false
  }
}

const ensureTunAdmin = async (): Promise<boolean> => {
  try {
    const has = await backendApi.tunHasPrivileges()
    if (has) return true
  } catch {
    // ignore
  }
  return await openTunAdminModal()
}

const assignConfig = (next: AppConfig) => {
  Object.assign(config, next)
  applyLocaleFromConfig()
}

const assignState = (next: RuntimeState) => {
  Object.assign(state, next)
  if (!Array.isArray(state.connections)) state.connections = []
  if (!Array.isArray(state.latencies)) state.latencies = []
  if (!Array.isArray(state.recentLogs)) state.recentLogs = []
  if (!Array.isArray(state.traffic?.recentBandwidth)) state.traffic.recentBandwidth = []
}

const sortedNodes = computed(() => {
  const latencyMap = new Map(state.latencies.map((x) => [x.nodeId, x]))
  return [...config.nodes].map((node) => ({
    node,
    latency: latencyMap.get(node.id),
    active: node.id === config.activeNodeId,
    editing: node.id === selectedNodeId.value,
  }))
})

const filteredLogs = computed(() => {
  const maxItems = logDisplayLimit.value
  const level = logLevelFilter.value
  const keyword = logSearch.value.trim().toLowerCase()
  const out: LogEntry[] = []
  for (const item of logs.value) {
    const component = String(item.component || '').toLowerCase()
    if (!showTrafficLogs.value && (component === 'traffic' || component.includes('traffic'))) continue
    if (level !== 'all' && item.level !== level) continue
    if (keyword) {
      const haystack = `${item.message} ${item.component} ${item.raw}`.toLowerCase()
      if (!haystack.includes(keyword)) continue
    }
    out.push(item)
    if (out.length >= maxItems) break
  }
  return out
})

const trafficProxyShare = computed(() => {
  const total = state.traffic.estimatedDirectRx + state.traffic.estimatedDirectTx + state.traffic.estimatedProxyRx + state.traffic.estimatedProxyTx
  if (!total) return 0
  return ((state.traffic.estimatedProxyRx + state.traffic.estimatedProxyTx) / total) * 100
})

const trafficDirectShare = computed(() => 100 - trafficProxyShare.value)

const runtimeStatusLabel = computed(() => {
  switch (proxyOpState.value) {
    case 'starting':
      return t('statusStarting')
    case 'stopping':
      return t('statusStopping')
    case 'restarting':
      return t('statusRestarting')
    default:
      return state.running ? t('statusRunning') : t('statusStopped')
  }
})

const primaryProxyActionLabel = computed(() => {
  switch (proxyOpState.value) {
    case 'starting':
      return t('startInProgress')
    case 'stopping':
      return t('stopInProgress')
    default:
      return state.running ? t('stop') : t('start')
  }
})

const primaryProxyActionHint = computed(() => {
  switch (proxyOpState.value) {
    case 'starting':
      return t('startSessionInProgress')
    case 'stopping':
      return t('stopSessionInProgress')
    default:
      return state.running ? t('stopSessionNow') : t('startSessionNow')
  }
})

const humanBytes = (value: number): string => {
  if (!value) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let v = value
  let idx = 0
  while (v >= 1024 && idx < units.length - 1) {
    v /= 1024
    idx++
  }
  return `${v.toFixed(v < 10 && idx > 0 ? 2 : 1)} ${units[idx]}`
}

const humanTime = (unixMs: number): string => {
  if (!unixMs) return '-'
  return new Date(unixMs).toLocaleTimeString()
}

const formatLogTimestamp = (timestamp: string): string => {
  const d = new Date(timestamp)
  if (Number.isNaN(d.getTime())) return '--:--:--'
  return d.toLocaleTimeString([], { hour12: false })
}

const logLevelText = (level: string): string => {
  switch ((level || '').toLowerCase()) {
    case 'debug':
      return 'DEBUG'
    case 'warn':
      return 'WARN'
    case 'error':
      return 'ERROR'
    default:
      return 'INFO'
  }
}

const logComponentText = (component: string): string => {
  const name = (component || '').trim()
  return name || 'core'
}

const pushUiLog = (level: string, component: string, message: string) => {
  const text = String(message || '').trim()
  if (!text) return
  const ts = new Date().toISOString()
  const rid = Math.random().toString(36).slice(2, 9)
  logs.value.unshift({
    id: `ui-${Date.now()}-${rid}`,
    timestamp: ts,
    level,
    component,
    message: text,
    raw: text,
  })
  if (logs.value.length > 20000) {
    logs.value = logs.value.slice(0, 20000)
  }
}

const stateErrorSnapshot: { lastError: string; routeSetupError: string } = {
  lastError: '',
  routeSetupError: '',
}

const ingestStateErrors = (next: RuntimeState) => {
  const nextLast = String(next.lastError || '').trim()
  const nextRoute = String(next.routeSetupError || '').trim()
  if (nextLast && nextLast !== stateErrorSnapshot.lastError) {
    pushUiLog('error', 'runtime', nextLast)
  }
  if (nextRoute && nextRoute !== stateErrorSnapshot.routeSetupError) {
    pushUiLog('error', 'route', nextRoute)
  }
  stateErrorSnapshot.lastError = nextLast
  stateErrorSnapshot.routeSetupError = nextRoute
}

const refreshLANProxyInfo = async () => {
  try {
    const info = await backendApi.getLANProxyInfo()
    lanProxyInfo.value = {
      port: Number(info?.port || 0) || config.core.localPort || 1080,
      ips: Array.isArray(info?.ips) ? info.ips : [],
      ready: !!info?.ready,
    }
  } catch {
    lanProxyInfo.value = {
      ...lanProxyInfo.value,
      port: config.core.localPort || 1080,
    }
  }
}

const normalizedCustomTables = (tables: unknown, legacyTable = ''): string[] => {
  const out = (Array.isArray(tables) ? tables : [])
    .map((item) => String(item ?? '').trim())
    .filter(Boolean)
  const legacy = String(legacyTable || '').trim()
  if (out.length === 0 && legacy) {
    out.push(legacy)
  }
  return out
}

const pickNode = (node: NodeConfig) => {
  selectedNodeId.value = node.id
  Object.assign(editableNode, cloneDeep(node))
  editableNode.customTables = normalizedCustomTables(editableNode.customTables, editableNode.customTable)
}

const resetEditableNode = () => {
  selectedNodeId.value = ''
  Object.assign(editableNode, emptyNode())
  editableNode.localPort = config.core.localPort
}

const refreshBasics = async () => {
  const [cfg, st, logItems] = await Promise.all([
    backendApi.getConfig(),
    backendApi.getState(),
    backendApi.getLogs('all', 5000),
  ])
  assignConfig(cfg)
  assignState(st)
  logs.value = [...(Array.isArray(logItems) ? logItems : [])].sort(
    (a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime()
  )
  ingestStateErrors(st)
  await refreshLANProxyInfo()
  if (cfg.nodes.length > 0) {
    pickNode(cfg.nodes[0])
  } else {
    resetEditableNode()
  }
}

const refreshUsage = async () => {
  try {
    const days = await backendApi.getUsageHistory(30)
    usageHistory.value = Array.isArray(days) ? days : []
  } catch {
    // ignore
  }
}

const saveConfig = async (silentOrEvent: boolean | Event = false) => {
  const silent = typeof silentOrEvent === 'boolean' ? silentOrEvent : false
  busy.value = true
  try {
    await backendApi.saveConfig(cloneDeep(config))
    if (!silent) flash(t('saved'))
  } catch (e: any) {
    flash(e?.message || t('saveFailed'), 'error')
  } finally {
    busy.value = false
  }
}

type I18nKey = Parameters<typeof t>[0]

const runProxyAction = async (
  opState: Exclude<ProxyOpState, 'idle'>,
  action: () => Promise<void>,
  successKey: I18nKey,
  failKey: I18nKey
) => {
  if (proxyOpBusy.value) return
  proxyOpBusy.value = true
  proxyOpState.value = opState
  try {
    await action()
    flash(t(successKey))
  } catch (e: any) {
    if (isAdminRequiredError(e)) {
      const ok = await ensureTunAdmin()
      if (ok) {
        try {
          await action()
          flash(t(successKey))
          return
        } catch (e2: any) {
          flash(e2?.message || t(failKey), 'error')
          return
        }
      }
    }
    flash(e?.message || t(failKey), 'error')
  } finally {
    proxyOpState.value = 'idle'
    proxyOpBusy.value = false
  }
}

const startProxy = async () => {
  await runProxyAction('starting', () => backendApi.startProxy({ withTun: config.tun.enabled }), 'started', 'startFailed')
}

const stopProxy = async () => {
  await runProxyAction('stopping', () => backendApi.stopProxy(), 'stopped', 'stopFailed')
}

const restartProxy = async () => {
  await runProxyAction('restarting', () => backendApi.restartProxy({ withTun: config.tun.enabled }), 'restarted', 'restartFailed')
}

const saveNode = async () => {
  busy.value = true
  try {
    const payload = cloneDeep(editableNode)
    payload.customTables = normalizedCustomTables(payload.customTables, payload.customTable)
    payload.customTable = ''
    const node = await backendApi.upsertNode(payload)
    await refreshBasics()
    pickNode(node)
    nodeEditorOpen.value = false
    flash(t('nodeSaved'))
  } catch (e: any) {
    flash(e?.message || t('nodeSaveFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const removeNode = async (id: string) => {
  let confirmed = false
  try {
    const action = await Dialogs.Question({
      Message: t('confirmDeleteNode'),
      Buttons: [
        { Label: t('cancel'), IsCancel: true, IsDefault: true },
        { Label: t('delete') },
      ],
    })
    confirmed = action === t('delete')
  } catch {
    confirmed = window.confirm(t('confirmDeleteNode'))
  }
  if (!confirmed) return
  busy.value = true
  try {
    await backendApi.deleteNode(id)
    await refreshBasics()
    flash(t('nodeDeleted'))
  } catch (e: any) {
    flash(e?.message || t('deleteFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const switchNode = async (id: string) => {
  busy.value = true
  try {
    await backendApi.switchNode(id)
    config.activeNodeId = id
    flash(t('nodeSwitched'))
  } catch (e: any) {
    flash(e?.message || t('switchFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const exportShortlink = async (id: string) => {
  busy.value = true
  try {
    const link = await backendApi.exportShortLink(id)
    await Clipboard.SetText(link)
    flash(t('copied'))
  } catch (e: any) {
    flash(e?.message || t('exportFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const probeAll = async () => {
  busy.value = true
  try {
    const results = await backendApi.probeAllNodes()
    state.latencies = results
    flash(t('latencyChecked'))
  } catch (e: any) {
    flash(e?.message || t('probeFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const probeNode = async (id: string) => {
  busy.value = true
  try {
    const result = await backendApi.probeNode(id)
    const idx = state.latencies.findIndex((x) => x.nodeId === result.nodeId)
    if (idx >= 0) state.latencies.splice(idx, 1, result)
    else state.latencies.push(result)
    flash(`${result.nodeName || t('node')} : ${result.connectOk ? `${result.latencyMs}ms` : result.error || t('failed')}`)
  } catch (e: any) {
    flash(e?.message || t('probeFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const autoBest = async () => {
  busy.value = true
  try {
    const best: LatencyResult = await backendApi.autoSelectLowestLatency()
    flash(`${t('switchedTo')} ${best.nodeName} (${best.latencyMs}ms)`)
  } catch (e: any) {
    flash(e?.message || t('autoSelectFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const sortByName = async () => {
  await backendApi.sortNodesByName()
  await refreshBasics()
}

const sortByLatency = async () => {
  await backendApi.sortNodesByLatency()
  await refreshBasics()
}

const detectDirectIP = async () => {
  directIP.value = await backendApi.detectIPDirect()
}

const detectProxyIP = async () => {
  proxyIP.value = await backendApi.detectIPProxy()
}

const startReverse = async () => {
  busy.value = true
  try {
    await backendApi.startReverseForwarder()
  } catch (e: any) {
    flash(e?.message || t('reverseStartFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const stopReverse = async () => {
  busy.value = true
  try {
    await backendApi.stopReverseForwarder()
  } catch (e: any) {
    flash(e?.message || t('reverseStopFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const closeConnection = async (id: string) => {
  connectionOpBusy.value = true
  try {
    await backendApi.closeConnection(id)
    state.connections = state.connections.filter((x) => x.id !== id)
    flash(t('connectionClosed'))
  } catch (e: any) {
    flash(e?.message || t('closeConnectionFailed'), 'error')
  } finally {
    connectionOpBusy.value = false
  }
}

const closeAllConnections = async () => {
  connectionOpBusy.value = true
  try {
    await backendApi.closeAllConnections()
    state.connections = []
    flash(t('allConnectionsClosed'))
  } catch (e: any) {
    flash(e?.message || t('closeAllConnectionsFailed'), 'error')
  } finally {
    connectionOpBusy.value = false
  }
}

const addPortForward = () => {
  const rule: PortForwardRule = {
    id: '',
    name: `Forward ${config.portForwards.length + 1}`,
    listen: '127.0.0.1:0',
    target: '127.0.0.1:0',
    enabled: true,
  }
  config.portForwards.push(rule)
}

const removePortForward = (idx: number) => {
  config.portForwards.splice(idx, 1)
}

const addReverseRoute = () => {
  config.reverseClient.routes.push({
    path: '/',
    target: 'http://127.0.0.1:8080',
    stripPrefix: null,
    hostHeader: '',
  })
}

const removeReverseRoute = (idx: number) => {
  config.reverseClient.routes.splice(idx, 1)
}

const openCreateNode = () => {
  nodeEditorMode.value = 'create'
  shortlinkInput.value = ''
  shortlinkName.value = ''
  resetEditableNode()
  nodeEditorOpen.value = true
}

const openEditNode = (node: NodeConfig) => {
  nodeEditorMode.value = 'edit'
  shortlinkInput.value = ''
  shortlinkName.value = ''
  pickNode(node)
  nodeEditorOpen.value = true
}

const closeNodeEditor = () => {
  nodeEditorOpen.value = false
}

type ShortlinkPayload = {
  h?: string
  p?: number
  k?: string
  a?: string
  e?: string
  m?: number
  x?: boolean
  t?: string
  ts?: string[]
  hd?: boolean
  hm?: string
  ht?: boolean
  hh?: string
  hy?: string
  hx?: string
}

const decodeRawUrlBase64 = (raw: string): string => {
  const normalized = raw.replace(/-/g, '+').replace(/_/g, '/')
  const padLen = (4 - (normalized.length % 4)) % 4
  const padded = normalized + '='.repeat(padLen)
  const bin = window.atob(padded)
  const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}

const applyShortlinkToEditable = (link: string, nameOverride = '', preferHostAsName = false) => {
  const raw = link.trim()
  if (!raw.startsWith('sudoku://')) {
    throw new Error(t('invalidShortlinkScheme'))
  }
  const payloadText = decodeRawUrlBase64(raw.slice('sudoku://'.length))
  const payload = JSON.parse(payloadText) as ShortlinkPayload
  if (!payload.h || !payload.p || !payload.k) {
    throw new Error(t('shortlinkMissingFields'))
  }

  editableNode.serverAddress = `${payload.h}:${payload.p}`
  editableNode.key = payload.k
  editableNode.aead = payload.e || 'chacha20-poly1305'
  editableNode.ascii = payload.a === 'ascii' ? 'prefer_ascii' : 'prefer_entropy'
  editableNode.localPort = payload.m && payload.m > 0 ? payload.m : config.core.localPort
  editableNode.enablePureDownlink = !payload.x
  editableNode.customTable = ''
  editableNode.customTables = normalizedCustomTables(payload.ts, payload.t || '')
  editableNode.httpMask.disable = !!payload.hd
  editableNode.httpMask.mode = payload.hm || 'auto'
  editableNode.httpMask.tls = payload.ht ?? true
  editableNode.httpMask.host = payload.hh || ''
  editableNode.httpMask.pathRoot = payload.hy || ''
  editableNode.httpMask.multiplex = payload.hx || 'auto'

  if (nameOverride.trim()) {
    editableNode.name = nameOverride.trim()
  } else if (preferHostAsName || !editableNode.name.trim()) {
    editableNode.name = payload.h
  }
}

const parseShortlinkFromInput = () => {
  try {
    applyShortlinkToEditable(shortlinkInput.value, shortlinkName.value)
    flash(t('shortlinkParsed'))
  } catch (e: any) {
    flash(e?.message || t('shortlinkParseFailed'), 'error')
  }
}

const parseShortlinkFromClipboard = async () => {
  try {
    const text = await Clipboard.Text()
    if (!text.trim()) {
      flash(t('clipboardEmpty'), 'error')
      return
    }
    shortlinkInput.value = text.trim()
    applyShortlinkToEditable(shortlinkInput.value, shortlinkName.value, true)
    flash(t('importedFromClipboard'))
  } catch (e: any) {
    flash(e?.message || t('clipboardImportFailed'), 'error')
  }
}

const setRoutingMode = (mode: ProxyMode) => {
  config.routing.proxyMode = mode
}

const addPacRule = () => {
  config.routing.ruleUrls.push('')
}

const removePacRule = (idx: number) => {
  config.routing.ruleUrls.splice(idx, 1)
}

const normalizePacRules = () => {
  config.routing.ruleUrls = config.routing.ruleUrls.map((x) => x.trim()).filter(Boolean)
}

const canHandleNodePaste = (target: EventTarget | null): boolean => {
  const active = (target as HTMLElement | null) || (document.activeElement as HTMLElement | null)
  if (!active) return true
  if (active instanceof HTMLInputElement || active instanceof HTMLTextAreaElement || active instanceof HTMLSelectElement) {
    return false
  }
  return !active.isContentEditable
}

const importNodeFromShortlink = async (rawText: string) => {
  const link = rawText.trim()
  if (!link.startsWith('sudoku://')) return
  busy.value = true
  try {
    const node = await backendApi.importShortLink('', link)
    await refreshBasics()
    const imported = config.nodes.find((x) => x.id === node.id)
    if (imported) {
      pickNode(imported)
    }
    flash(t('importedFromClipboard'))
  } catch (e: any) {
    flash(e?.message || t('clipboardImportFailed'), 'error')
  } finally {
    busy.value = false
  }
}

const onWindowPaste = (event: ClipboardEvent) => {
  if (currentTab.value !== 'nodes') return
  if (!canHandleNodePaste(event.target)) return
  const text = event.clipboardData?.getData('text/plain') ?? event.clipboardData?.getData('text') ?? ''
  if (!text.trim().startsWith('sudoku://')) return
  event.preventDefault()
  void importNodeFromShortlink(text)
}

const validateCustomRules = async () => {
  if (!config.routing.customRulesEnabled || !config.routing.customRules.trim()) {
    customRulesValidation.value = { status: 'idle', message: '' }
    return
  }
  const raw = config.routing.customRules
  const yamlLike = /(^|\n)\s*[^#\n][^\n]*:\s*/.test(raw) || /(^|\n)\s*-\s+/.test(raw)
  if (!yamlLike) {
    customRulesValidation.value = { status: 'ok', message: t('customRulesListValid') }
    return
  }
  customRulesValidation.value = { status: 'checking', message: t('customRulesYamlChecking') }
  try {
    await backendApi.validateYAML(raw)
    customRulesValidation.value = { status: 'ok', message: t('customRulesYamlValid') }
  } catch (e: any) {
    customRulesValidation.value = { status: 'error', message: e?.message || t('customRulesYamlInvalid') }
  }
}

const resetTunFactory = () => {
  const macLike = /Mac|Darwin/i.test(navigator.userAgent)
  const windowsLike = /Windows/i.test(navigator.userAgent)
  Object.assign(config.tun, defaultTunConfig(macLike, windowsLike))
  flash(t('tunRestoredDefaults'))
}

watch(
  () => config.ui.language,
  () => {
    if (config.ui.language === 'auto') return
    if (config.ui.language.startsWith('ru')) locale.value = 'ru'
    else if (config.ui.language.startsWith('zh')) locale.value = 'zh'
    else locale.value = 'en'
  }
)

watch(
  () => config.ui.theme,
  () => {
    applyDocumentTheme()
  }
)

watch(
  () => config.tun.enabled,
  async (next, prev) => {
    if (!loadReady.value || tunAutoSaveLock.value) return
    if (skipTunEnabledAutoSaveOnce.value) {
      skipTunEnabledAutoSaveOnce.value = false
      return
    }
    if (next && !prev) {
      const ok = await ensureTunAdmin()
      if (!ok) {
        skipTunEnabledAutoSaveOnce.value = true
        config.tun.enabled = false
        return
      }
    }
    await saveConfig(true)
    flash(t('tunAutoSaved'))
  }
)

watch(
  () => state.needsAdmin,
  async (next) => {
    if (!loadReady.value) return
    if (!next) return
    void ensureTunAdmin()
  }
)

watch(
  () => [config.routing.customRulesEnabled, config.routing.customRules],
  () => {
    if (customRulesValidateTimer) {
      window.clearTimeout(customRulesValidateTimer)
      customRulesValidateTimer = null
    }
    customRulesValidateTimer = window.setTimeout(() => {
      void validateCustomRules()
    }, 280)
  }
)

watch(
  () => [state.running, config.core.localPort, config.activeNodeId],
  () => {
    void refreshLANProxyInfo()
  }
)

onMounted(async () => {
  applyDocumentTheme()
  await refreshBasics()
  await refreshUsage()
  tunAutoSaveLock.value = false
  loadReady.value = true
  usageHistoryTimer = window.setInterval(() => refreshUsage(), 60_000)
  window.addEventListener('paste', onWindowPaste)

  Events.On('core:state', (event) => {
    pendingState = event.data as RuntimeState
    if (stateFlushTimer) return
    stateFlushTimer = window.setTimeout(() => {
      stateFlushTimer = null
      if (!pendingState) return
      const next = pendingState
      pendingState = null
      assignState(next)
      ingestStateErrors(next)
    }, 80)
  })

  Events.On('core:log', (event) => {
    logQueue.push(event.data as LogEntry)
    if (logFlushTimer) return
    logFlushTimer = window.setTimeout(() => {
      logFlushTimer = null
      if (logQueue.length === 0) return
      const batch = logQueue
      logQueue = []
      batch.reverse()
      logs.value.unshift(...batch)
      if (logs.value.length > 20000) {
        logs.value = logs.value.slice(0, 20000)
      }
    }, 100)
  })
})

onUnmounted(() => {
  Events.Off('core:state')
  Events.Off('core:log')
  window.removeEventListener('paste', onWindowPaste)
  if (stateFlushTimer) {
    window.clearTimeout(stateFlushTimer)
    stateFlushTimer = null
  }
  pendingState = null
  if (logFlushTimer) {
    window.clearTimeout(logFlushTimer)
    logFlushTimer = null
  }
  if (customRulesValidateTimer) {
    window.clearTimeout(customRulesValidateTimer)
    customRulesValidateTimer = null
  }
  logQueue = []
  if (usageHistoryTimer) {
    window.clearInterval(usageHistoryTimer)
    usageHistoryTimer = null
  }
})

  return {
    locale,
    t,
    logoUrl,
    currentTab,
    navMain,
    navExtra,
    sidebarCollapsed,
    toggleSidebar,
    busy,
    proxyOpBusy,
    proxyOpState,
    runtimeStatusLabel,
    primaryProxyActionLabel,
    primaryProxyActionHint,
    notice,
    noticeType,
    tunAdminModalOpen,
    tunAdminPassword,
    tunAdminBusy,
    tunAdminError,
    closeTunAdminModal,
    submitTunAdminModal,
    config,
    state,
    editableNode,
    nodeEditorOpen,
    nodeEditorMode,
    shortlinkInput,
    shortlinkName,
    logLevelFilter,
    logSearch,
    logDisplayLimit,
    showTrafficLogs,
    filteredLogs,
    proxyIP,
    directIP,
    lanProxyInfo,
    usageHistory,
    customRulesValidation,
    sortedNodes,
    trafficProxyShare,
    trafficDirectShare,
    humanBytes,
    humanTime,
    formatLogTimestamp,
    logLevelText,
    logComponentText,
    startProxy,
    stopProxy,
    restartProxy,
    switchNode,
    detectDirectIP,
    detectProxyIP,
    closeConnection,
    closeAllConnections,
    openCreateNode,
    sortByName,
    sortByLatency,
    probeAll,
    autoBest,
    pickNode,
    probeNode,
    exportShortlink,
    openEditNode,
    removeNode,
    setRoutingMode,
    addPacRule,
    removePacRule,
    normalizePacRules,
    saveConfig,
    resetTunFactory,
    addPortForward,
    removePortForward,
    addReverseRoute,
    removeReverseRoute,
    startReverse,
    stopReverse,
    closeNodeEditor,
    saveNode,
    parseShortlinkFromInput,
    parseShortlinkFromClipboard,
    connectionOpBusy,
  }
}
