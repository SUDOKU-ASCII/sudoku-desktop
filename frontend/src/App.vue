<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { EventsOff, EventsOn } from '../wailsjs/runtime/runtime'
import TrafficChart from './components/TrafficChart.vue'
import UsageHistoryChart from './components/UsageHistoryChart.vue'
import SudokuGame from './components/SudokuGame.vue'
import { backendApi } from './api'
import { useI18n } from './i18n'
import type {
  ActiveConnection,
  AppConfig,
  IPDetectResult,
  LatencyResult,
  LogEntry,
  NodeConfig,
  PortForwardRule,
  RuntimeState,
  UsageDay,
} from './types'

const { locale, t } = useI18n()

const tabs = ['dashboard', 'game', 'nodes', 'routing', 'tun', 'forwards', 'reverse', 'logs'] as const
const currentTab = ref<(typeof tabs)[number]>('dashboard')

const busy = ref(false)
const notice = ref('')
const noticeType = ref<'ok' | 'error'>('ok')

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
    mode: 'legacy',
    tls: false,
    host: '',
    pathRoot: '',
    multiplex: 'off',
  },
  localPort: 1080,
  enabled: true,
})

const config = reactive<AppConfig>({
  version: 2,
  activeNodeId: '',
  nodes: [],
  routing: { proxyMode: 'pac', ruleUrls: [], customRulesEnabled: false, customRules: '' },
  tun: {
    enabled: true,
    interfaceName: 'sudoku0',
    mtu: 8500,
    ipv4: '198.18.0.1',
    ipv6: 'fc00::1',
    socksUdp: 'udp',
    socksMark: 438,
    routeTable: 20,
    logLevel: 'warn',
    mapDnsEnabled: true,
    mapDnsAddress: '198.18.0.2',
    mapDnsPort: 53,
    mapDnsNetwork: '100.64.0.0',
    mapDnsNetmask: '255.192.0.0',
    taskStackSize: 86016,
    tcpBufferSize: 65536,
    maxSession: 0,
    connectTimeout: 10000,
  },
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
  ui: { language: 'auto', theme: 'auto' },
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

const editableNode = reactive<NodeConfig>(emptyNode())
const shortlinkInput = ref('')
const shortlinkName = ref('')
const logLevelFilter = ref('all')
const logs = ref<LogEntry[]>([])
const proxyIP = ref<IPDetectResult | null>(null)
const directIP = ref<IPDetectResult | null>(null)
const usageHistory = ref<UsageDay[]>([])
let usageHistoryTimer: number | null = null

const applyLocaleFromConfig = () => {
  if (config.ui.language === 'auto') {
    return
  }
  if (config.ui.language.startsWith('ru')) locale.value = 'ru'
  else if (config.ui.language.startsWith('zh')) locale.value = 'zh'
  else locale.value = 'en'
}

const flash = (message: string, type: 'ok' | 'error' = 'ok') => {
  notice.value = message
  noticeType.value = type
  setTimeout(() => {
    if (notice.value === message) {
      notice.value = ''
    }
  }, 3200)
}

const assignConfig = (next: AppConfig) => {
  Object.assign(config, next)
  applyLocaleFromConfig()
}

const assignState = (next: RuntimeState) => {
  Object.assign(state, next)
}

const activeNode = computed(() => config.nodes.find((n) => n.id === config.activeNodeId) ?? null)

const sortedNodes = computed(() => {
  const latencyMap = new Map(state.latencies.map((x) => [x.nodeId, x]))
  return [...config.nodes].map((node) => ({
    node,
    latency: latencyMap.get(node.id),
  }))
})

const filteredLogs = computed(() => {
  if (logLevelFilter.value === 'all') return logs.value
  return logs.value.filter((x) => x.level === logLevelFilter.value)
})

const trafficProxyShare = computed(() => {
  const total = state.traffic.estimatedDirectRx + state.traffic.estimatedDirectTx + state.traffic.estimatedProxyRx + state.traffic.estimatedProxyTx
  if (!total) return 0
  return ((state.traffic.estimatedProxyRx + state.traffic.estimatedProxyTx) / total) * 100
})

const trafficDirectShare = computed(() => {
  return 100 - trafficProxyShare.value
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

const pickNode = (node: NodeConfig) => {
  Object.assign(editableNode, JSON.parse(JSON.stringify(node)))
}

const resetEditableNode = () => {
  Object.assign(editableNode, emptyNode())
  editableNode.localPort = config.core.localPort
}

const refreshBasics = async () => {
  const [cfg, st, logItems] = await Promise.all([
    backendApi.getConfig(),
    backendApi.getState(),
    backendApi.getLogs('all', 300),
  ])
  assignConfig(cfg)
  assignState(st)
  logs.value = logItems
  if (cfg.nodes.length > 0) {
    pickNode(cfg.nodes[0])
  }
}

const refreshUsage = async () => {
  try {
    usageHistory.value = await backendApi.getUsageHistory(30)
  } catch {
    // ignore
  }
}

const saveConfig = async () => {
  busy.value = true
  try {
    await backendApi.saveConfig(JSON.parse(JSON.stringify(config)))
    flash('Saved')
  } catch (e: any) {
    flash(e?.message || 'Save failed', 'error')
  } finally {
    busy.value = false
  }
}

const startProxy = async () => {
  busy.value = true
  try {
    await backendApi.startProxy({ withTun: config.tun.enabled })
    flash('Started')
  } catch (e: any) {
    flash(e?.message || 'Start failed', 'error')
  } finally {
    busy.value = false
  }
}

const stopProxy = async () => {
  busy.value = true
  try {
    await backendApi.stopProxy()
    flash('Stopped')
  } catch (e: any) {
    flash(e?.message || 'Stop failed', 'error')
  } finally {
    busy.value = false
  }
}

const restartProxy = async () => {
  busy.value = true
  try {
    await backendApi.restartProxy({ withTun: config.tun.enabled })
    flash('Restarted')
  } catch (e: any) {
    flash(e?.message || 'Restart failed', 'error')
  } finally {
    busy.value = false
  }
}

const saveNode = async () => {
  busy.value = true
  try {
    const node = await backendApi.upsertNode(JSON.parse(JSON.stringify(editableNode)))
    await refreshBasics()
    pickNode(node)
    flash('Node saved')
  } catch (e: any) {
    flash(e?.message || 'Node save failed', 'error')
  } finally {
    busy.value = false
  }
}

const removeNode = async (id: string) => {
  busy.value = true
  try {
    await backendApi.deleteNode(id)
    await refreshBasics()
    resetEditableNode()
    flash('Node deleted')
  } catch (e: any) {
    flash(e?.message || 'Delete failed', 'error')
  } finally {
    busy.value = false
  }
}

const switchNode = async (id: string) => {
  busy.value = true
  try {
    await backendApi.switchNode(id)
    config.activeNodeId = id
    flash('Node switched')
  } catch (e: any) {
    flash(e?.message || 'Switch failed', 'error')
  } finally {
    busy.value = false
  }
}

const importShortlink = async () => {
  if (!shortlinkInput.value.trim()) {
    return
  }
  busy.value = true
  try {
    await backendApi.importShortLink(shortlinkName.value.trim(), shortlinkInput.value.trim())
    shortlinkInput.value = ''
    shortlinkName.value = ''
    await refreshBasics()
    flash('Imported')
  } catch (e: any) {
    flash(e?.message || 'Import failed', 'error')
  } finally {
    busy.value = false
  }
}

const exportShortlink = async (id: string) => {
  busy.value = true
  try {
    const link = await backendApi.exportShortLink(id)
    await navigator.clipboard.writeText(link)
    flash('Copied to clipboard')
  } catch (e: any) {
    flash(e?.message || 'Export failed', 'error')
  } finally {
    busy.value = false
  }
}

const probeAll = async () => {
  busy.value = true
  try {
    const results = await backendApi.probeAllNodes()
    state.latencies = results
    flash('Latency checked')
  } catch (e: any) {
    flash(e?.message || 'Probe failed', 'error')
  } finally {
    busy.value = false
  }
}

const autoBest = async () => {
  busy.value = true
  try {
    const best: LatencyResult = await backendApi.autoSelectLowestLatency()
    flash(`Switched to ${best.nodeName} (${best.latencyMs}ms)`)
  } catch (e: any) {
    flash(e?.message || 'Auto select failed', 'error')
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
    flash(e?.message || 'Reverse start failed', 'error')
  } finally {
    busy.value = false
  }
}

const stopReverse = async () => {
  busy.value = true
  try {
    await backendApi.stopReverseForwarder()
  } catch (e: any) {
    flash(e?.message || 'Reverse stop failed', 'error')
  } finally {
    busy.value = false
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

watch(
  () => config.ui.language,
  () => {
    if (config.ui.language === 'auto') return
    if (config.ui.language.startsWith('ru')) locale.value = 'ru'
    else if (config.ui.language.startsWith('zh')) locale.value = 'zh'
    else locale.value = 'en'
  }
)

onMounted(async () => {
  await refreshBasics()
  await refreshUsage()
  usageHistoryTimer = window.setInterval(() => refreshUsage(), 60_000)

  EventsOn('core:state', (payload: RuntimeState) => {
    assignState(payload)
  })

  EventsOn('core:log', (entry: LogEntry) => {
    logs.value.push(entry)
    if (logs.value.length > 1000) {
      logs.value = logs.value.slice(logs.value.length - 1000)
    }
  })
})

onUnmounted(() => {
  EventsOff('core:state')
  EventsOff('core:log')
  if (usageHistoryTimer) {
    window.clearInterval(usageHistoryTimer)
    usageHistoryTimer = null
  }
})
</script>

<template>
  <div class="app-shell" :data-theme="config.ui.theme">
    <header class="topbar brutal-card">
      <div class="titlebox">
        <h1>{{ t('appTitle') }}</h1>
        <p>{{ t('subtitle') }}</p>
      </div>
      <div class="statusbox" :class="state.running ? 'ok' : 'off'">
        <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="8" /></svg>
        <span>{{ state.running ? t('statusRunning') : t('statusStopped') }}</span>
      </div>
      <div class="actions">
        <button class="btn" :disabled="busy || state.running" @click="startProxy">{{ t('start') }}</button>
        <button class="btn" :disabled="busy || !state.running" @click="stopProxy">{{ t('stop') }}</button>
        <button class="btn" :disabled="busy || !state.running" @click="restartProxy">{{ t('restart') }}</button>
      </div>
    </header>

    <nav class="tabs brutal-card">
      <button v-for="tab in tabs" :key="tab" class="tabbtn" :class="{ active: currentTab === tab }" @click="currentTab = tab">
        {{ t(tab) }}
      </button>
    </nav>

    <section v-if="notice" class="notice" :class="noticeType">{{ notice }}</section>

    <main class="panel brutal-card" v-show="currentTab === 'dashboard'">
      <div class="metrics-grid">
        <article class="metric">
          <h3>{{ t('runningNode') }}</h3>
          <strong>{{ state.activeNodeName || '-' }}</strong>
          <small>{{ state.activeNodeId }}</small>
        </article>
        <article class="metric">
          <h3>{{ t('totalUpload') }}</h3>
          <strong>{{ humanBytes(state.traffic.totalTx) }}</strong>
          <small>{{ state.traffic.interface }} · {{ state.traffic.interfaceFound ? 'OK' : 'Missing' }}</small>
        </article>
        <article class="metric">
          <h3>{{ t('totalDownload') }}</h3>
          <strong>{{ humanBytes(state.traffic.totalRx) }}</strong>
          <small>{{ humanTime(state.traffic.lastSampleUnixMillis) }}</small>
        </article>
        <article class="metric">
          <h3>{{ t('proxyShare') }}</h3>
          <strong>{{ trafficProxyShare.toFixed(1) }}%</strong>
          <small>{{ state.traffic.proxyConnDecisions }} decisions</small>
        </article>
        <article class="metric">
          <h3>{{ t('directShare') }}</h3>
          <strong>{{ trafficDirectShare.toFixed(1) }}%</strong>
          <small>{{ state.traffic.directConnDecisions }} decisions</small>
        </article>
      </div>

      <TrafficChart :samples="state.traffic.recentBandwidth" />

      <h3 class="section-title">{{ t('usageHistory') }}</h3>
      <UsageHistoryChart :days="usageHistory" />

      <div class="dashboard-actions">
        <button class="btn" @click="probeAll">{{ t('checkLatency') }}</button>
        <button class="btn" @click="autoBest">{{ t('autoBestNode') }}</button>
        <button class="btn" @click="detectDirectIP">{{ t('detectDirect') }}</button>
        <button class="btn" @click="detectProxyIP">{{ t('detectProxy') }}</button>
      </div>

      <div class="ip-grid">
        <article class="metric">
          <h3>{{ t('directIp') }}</h3>
          <strong>{{ directIP?.ip || '-' }}</strong>
          <small>{{ directIP?.country }} {{ directIP?.region }} {{ directIP?.isp }}</small>
        </article>
        <article class="metric">
          <h3>{{ t('proxyIp') }}</h3>
          <strong>{{ proxyIP?.ip || '-' }}</strong>
          <small>{{ proxyIP?.country }} {{ proxyIP?.region }} {{ proxyIP?.isp }}</small>
        </article>
      </div>

      <h3 class="section-title">{{ t('connections') }}</h3>
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>{{ t('network') }}</th>
              <th>{{ t('source') }}</th>
              <th>{{ t('destination') }}</th>
              <th>{{ t('direction') }}</th>
              <th>{{ t('seen') }}</th>
              <th>{{ t('hits') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in state.connections.slice(0, 16)" :key="item.id">
              <td>{{ item.network }}</td>
              <td>{{ item.source }}</td>
              <td>{{ item.destination }}</td>
              <td><span class="pill" :class="item.direction">{{ item.direction }}</span></td>
              <td>{{ new Date(item.lastSeen).toLocaleTimeString() }}</td>
              <td>{{ item.hits }}</td>
            </tr>
            <tr v-if="state.connections.length === 0"><td colspan="6">{{ t('none') }}</td></tr>
          </tbody>
        </table>
      </div>
    </main>

    <main class="panel brutal-card" v-show="currentTab === 'nodes'">
      <div class="node-layout">
        <aside class="node-list">
          <div class="row">
            <button class="btn mini" @click="resetEditableNode">{{ t('addNode') }}</button>
            <button class="btn mini" @click="sortByName">{{ t('sortByName') }}</button>
            <button class="btn mini" @click="sortByLatency">{{ t('sortByLatency') }}</button>
          </div>
          <article v-for="item in sortedNodes" :key="item.node.id" class="node-card" :class="{ active: item.node.id === config.activeNodeId }" @click="pickNode(item.node)">
            <h4>{{ item.node.name || item.node.serverAddress }}</h4>
            <p>{{ item.node.serverAddress }}</p>
            <small>
              <span v-if="item.latency">{{ item.latency.connectOk ? `${item.latency.latencyMs} ms` : item.latency.error }}</span>
              <span v-else>-</span>
            </small>
            <div class="row">
              <button class="btn mini" @click.stop="switchNode(item.node.id)">{{ t('switch') }}</button>
              <button class="btn mini" @click.stop="exportShortlink(item.node.id)">{{ t('exportShare') }}</button>
              <button class="btn mini danger" @click.stop="removeNode(item.node.id)">{{ t('delete') }}</button>
            </div>
          </article>
        </aside>

        <section class="node-editor">
          <h3>{{ editableNode.id ? t('save') : t('addNode') }}</h3>
          <div class="form-grid">
            <label>Name<input v-model="editableNode.name" /></label>
            <label>Server<input v-model="editableNode.serverAddress" placeholder="host:port" /></label>
            <label>Key<textarea v-model="editableNode.key" rows="3" /></label>
            <label>AEAD<select v-model="editableNode.aead"><option>chacha20-poly1305</option><option>aes-128-gcm</option><option>none</option></select></label>
            <label>ASCII<select v-model="editableNode.ascii"><option>prefer_entropy</option><option>prefer_ascii</option></select></label>
            <label>Local Port<input v-model.number="editableNode.localPort" type="number" /></label>
            <label>Padding Min<input v-model.number="editableNode.paddingMin" type="number" /></label>
            <label>Padding Max<input v-model.number="editableNode.paddingMax" type="number" /></label>
            <label>HTTP Mode<select v-model="editableNode.httpMask.mode"><option>legacy</option><option>stream</option><option>poll</option><option>auto</option><option>ws</option></select></label>
            <label>TLS<input v-model="editableNode.httpMask.tls" type="checkbox" /></label>
            <label>Path Root<input v-model="editableNode.httpMask.pathRoot" /></label>
            <label>Custom Table<input v-model="editableNode.customTable" /></label>
            <label>Packed Downlink<input type="checkbox" :checked="!editableNode.enablePureDownlink" @change="editableNode.enablePureDownlink = !($event.target as HTMLInputElement).checked"/></label>
          </div>
          <div class="row">
            <button class="btn" @click="saveNode">{{ t('save') }}</button>
            <button class="btn" @click="editableNode.id && backendApi.probeNode(editableNode.id)">Probe</button>
          </div>

          <h4>{{ t('importShortlink') }}</h4>
          <div class="form-grid">
            <label>Name<input v-model="shortlinkName" /></label>
            <label>Link<textarea v-model="shortlinkInput" rows="3" placeholder="sudoku://..." /></label>
          </div>
          <button class="btn" @click="importShortlink">{{ t('importShortlink') }}</button>
        </section>
      </div>
    </main>

    <main class="panel brutal-card" v-show="currentTab === 'routing'">
      <div class="form-grid">
        <label>{{ t('proxyMode') }}
          <select v-model="config.routing.proxyMode">
            <option value="global">global</option>
            <option value="direct">direct</option>
            <option value="pac">pac</option>
          </select>
        </label>
        <label>{{ t('pacRules') }}
          <textarea :value="config.routing.ruleUrls.join('\n')" rows="8" @input="config.routing.ruleUrls = ($event.target as HTMLTextAreaElement).value.split('\n').map(x => x.trim()).filter(Boolean)" />
        </label>
        <label>{{ t('customRulesEnabled') }}<input type="checkbox" v-model="config.routing.customRulesEnabled" /></label>
        <label>{{ t('customRules') }}
          <textarea v-model="config.routing.customRules" rows="10" :disabled="!config.routing.customRulesEnabled" :placeholder="t('customRulesPlaceholder')" />
        </label>
      </div>
      <button class="btn" @click="saveConfig">{{ t('apply') }}</button>
    </main>

    <main class="panel brutal-card" v-show="currentTab === 'tun'">
      <div class="form-grid">
        <label>{{ t('tunEnabled') }}<input type="checkbox" v-model="config.tun.enabled" /></label>
        <label>Interface<input v-model="config.tun.interfaceName" /></label>
        <label>MTU<input v-model.number="config.tun.mtu" type="number" /></label>
        <label>IPv4<input v-model="config.tun.ipv4" /></label>
        <label>IPv6<input v-model="config.tun.ipv6" /></label>
        <label>Socks Mark<input v-model.number="config.tun.socksMark" type="number" /></label>
        <label>Route Table<input v-model.number="config.tun.routeTable" type="number" /></label>
        <label>Sudoku Binary<input v-model="config.core.sudokuBinary" /></label>
        <label>HEV Binary<input v-model="config.core.hevBinary" /></label>
        <label>Work Dir<input v-model="config.core.workingDir" /></label>
        <label>Core Port<input v-model.number="config.core.localPort" type="number" /></label>
        <label>Auto Start<input type="checkbox" v-model="config.core.autoStart" /></label>
        <label>Language
          <select v-model="config.ui.language">
            <option value="auto">auto</option>
            <option value="zh">中文</option>
            <option value="en">English</option>
            <option value="ru">Русский</option>
          </select>
        </label>
        <label>Theme
          <select v-model="config.ui.theme">
            <option value="auto">auto</option>
            <option value="light">light</option>
            <option value="dark">dark</option>
          </select>
        </label>
      </div>
      <p class="hint">{{ t('lanHint') }}</p>
      <button class="btn" @click="saveConfig">{{ t('apply') }}</button>
    </main>

    <main class="panel brutal-card" v-show="currentTab === 'forwards'">
      <div class="row">
        <button class="btn" @click="addPortForward">{{ t('addForward') }}</button>
        <button class="btn" @click="saveConfig">{{ t('apply') }}</button>
      </div>

      <div class="forward-list">
        <article v-for="(rule, idx) in config.portForwards" :key="rule.id || idx" class="forward-card">
          <div class="form-grid">
            <label>{{ t('name') }}<input v-model="rule.name" /></label>
            <label>{{ t('listen') }}<input v-model="rule.listen" placeholder="0.0.0.0:1080" /></label>
            <label>{{ t('target') }}<input v-model="rule.target" placeholder="127.0.0.1:1080" /></label>
            <label>{{ t('enabled') }}<input type="checkbox" v-model="rule.enabled" /></label>
          </div>
          <div class="row">
            <button class="btn mini danger" @click="removePortForward(idx)">{{ t('delete') }}</button>
          </div>
        </article>
        <p v-if="config.portForwards.length === 0">{{ t('none') }}</p>
      </div>
      <p class="hint">{{ t('forwardHint') }}</p>
    </main>

    <main class="panel brutal-card" v-show="currentTab === 'reverse'">
      <h3 class="section-title">{{ t('reverseClient') }}</h3>
      <div class="form-grid">
        <label>{{ t('reverseClientId') }}<input v-model="config.reverseClient.clientId" placeholder="client-id" /></label>
      </div>
      <div class="row">
        <button class="btn mini" @click="addReverseRoute">{{ t('addRoute') }}</button>
        <button class="btn mini" @click="saveConfig">{{ t('apply') }}</button>
      </div>
      <div class="route-list">
        <article v-for="(route, idx) in config.reverseClient.routes" :key="idx" class="route-card">
          <div class="form-grid">
            <label>{{ t('path') }}<input v-model="route.path" /></label>
            <label>{{ t('target') }}<input v-model="route.target" /></label>
            <label>{{ t('hostHeader') }}<input v-model="route.hostHeader" placeholder="example.com" /></label>
            <label>{{ t('stripPrefix') }}
              <select
                :value="route.stripPrefix == null ? 'auto' : (route.stripPrefix ? 'yes' : 'no')"
                @change="route.stripPrefix = ($event.target as HTMLSelectElement).value === 'auto' ? null : ($event.target as HTMLSelectElement).value === 'yes'"
              >
                <option value="auto">{{ t('auto') }}</option>
                <option value="yes">{{ t('yes') }}</option>
                <option value="no">{{ t('no') }}</option>
              </select>
            </label>
          </div>
          <div class="row">
            <button class="btn mini danger" @click="removeReverseRoute(idx)">{{ t('delete') }}</button>
          </div>
        </article>
        <p v-if="config.reverseClient.routes.length === 0">{{ t('none') }}</p>
      </div>
      <p class="hint">{{ t('reverseClientHint') }}</p>

      <h3 class="section-title">{{ t('reverseForwarder') }}</h3>
      <div class="form-grid">
        <label>{{ t('dialUrl') }}<input v-model="config.reverseForward.dialUrl" placeholder="wss://example.com/ssh" /></label>
        <label>{{ t('listen') }}<input v-model="config.reverseForward.listenAddr" placeholder="127.0.0.1:2222" /></label>
        <label>{{ t('insecure') }}<input type="checkbox" v-model="config.reverseForward.insecure" /></label>
      </div>
      <div class="row">
        <button class="btn" :disabled="state.reverseRunning" @click="startReverse">{{ t('reverseStart') }}</button>
        <button class="btn" :disabled="!state.reverseRunning" @click="stopReverse">{{ t('reverseStop') }}</button>
      </div>
    </main>

    <main class="panel brutal-card" v-show="currentTab === 'game'">
      <SudokuGame />
    </main>

    <main class="panel brutal-card" v-show="currentTab === 'logs'">
      <div class="row">
        <label>{{ t('level') }}
          <select v-model="logLevelFilter">
            <option value="all">{{ t('all') }}</option>
            <option value="debug">debug</option>
            <option value="info">info</option>
            <option value="warn">warn</option>
            <option value="error">error</option>
          </select>
        </label>
      </div>
      <div class="log-list">
        <article v-for="item in filteredLogs.slice().reverse()" :key="item.id" class="log-item" :class="item.level">
          <time>{{ new Date(item.timestamp).toLocaleTimeString() }}</time>
          <strong>[{{ item.component }}]</strong>
          <span>{{ item.message }}</span>
        </article>
        <p v-if="filteredLogs.length === 0">{{ t('none') }}</p>
      </div>
    </main>

    <footer class="footbar">
      <span>{{ state.lastError || state.routeSetupError }}</span>
    </footer>
  </div>
</template>

<style>
:root {
  --paper: #f4efdf;
  --paper-soft: #fff9e7;
  --ink: #1d1b1a;
  --ink-soft: #403b39;
  --accent-a: #e4572e;
  --accent-b: #f3a712;
  --accent-c: #118ab2;
  --accent-d: #06d6a0;
  --ok: #1f8f4c;
  --bad: #c44536;
  --shadow: 6px 6px 0 #1d1b1a;
  --radius: 14px;
}

@media (prefers-color-scheme: dark) {
  :root {
    --paper: #181614;
    --paper-soft: #25211e;
    --ink: #f4efdf;
    --ink-soft: #d0c8b8;
    --accent-a: #f45d48;
    --accent-b: #ffba49;
    --accent-c: #4ea8de;
    --accent-d: #57cc99;
    --ok: #65d28b;
    --bad: #ff7e6b;
    --shadow: 6px 6px 0 #f4efdf;
  }
}

:root [data-theme='light'] {
  color-scheme: light;
}

:root [data-theme='dark'] {
  color-scheme: dark;
}

* {
  box-sizing: border-box;
}

html,
body,
#app {
  width: 100%;
  min-height: 100%;
  margin: 0;
  font-family: Nunito, 'Avenir Next', 'Helvetica Neue', sans-serif;
  background: radial-gradient(circle at 20% 0%, #ffe3b6 0%, transparent 40%),
    radial-gradient(circle at 90% 20%, #b8ffe8 0%, transparent 35%),
    var(--paper);
  color: var(--ink);
}

.app-shell {
  max-width: 1360px;
  margin: 0 auto;
  padding: 16px;
  display: grid;
  gap: 14px;
}

.brutal-card {
  border: 3px solid var(--ink);
  border-radius: var(--radius);
  background: var(--paper-soft);
  box-shadow: var(--shadow);
}

.topbar {
  padding: 16px;
  display: grid;
  grid-template-columns: 1.8fr 1fr auto;
  gap: 12px;
  align-items: center;
}

.titlebox h1 {
  margin: 0;
  font-size: clamp(24px, 3vw, 34px);
}

.titlebox p {
  margin: 4px 0 0;
  opacity: 0.85;
}

.statusbox {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: 3px solid var(--ink);
  border-radius: 12px;
  font-weight: 700;
}

.statusbox svg {
  width: 16px;
  height: 16px;
}

.statusbox.ok {
  color: var(--ok);
}

.statusbox.ok svg {
  fill: var(--ok);
}

.statusbox.off {
  color: var(--bad);
}

.statusbox.off svg {
  fill: var(--bad);
}

.actions,
.row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.btn {
  border: 3px solid var(--ink);
  background: var(--paper);
  color: var(--ink);
  border-radius: 10px;
  padding: 8px 12px;
  font-weight: 700;
  cursor: pointer;
  transition: transform 0.15s ease;
}

.btn:hover {
  transform: translateY(-2px);
}

.btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

.btn.mini {
  padding: 4px 8px;
  font-size: 12px;
}

.btn.danger {
  border-color: var(--bad);
  color: var(--bad);
}

.tabs {
  padding: 8px;
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.tabbtn {
  border: 3px solid var(--ink);
  background: transparent;
  border-radius: 10px;
  padding: 8px 12px;
  font-weight: 800;
  cursor: pointer;
}

.tabbtn.active {
  background: var(--ink);
  color: var(--paper);
}

.panel {
  padding: 16px;
  min-height: 560px;
  animation: pop 0.2s ease;
}

@keyframes pop {
  from {
    opacity: 0.2;
    transform: translateY(5px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.notice {
  padding: 10px 12px;
  border-radius: 10px;
  border: 3px solid var(--ink);
}

.notice.ok {
  background: #dffbe9;
}

.notice.error {
  background: #ffe0dc;
}

.metrics-grid {
  display: grid;
  gap: 10px;
  grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
  margin-bottom: 14px;
}

.metric {
  border: 3px solid var(--ink);
  border-radius: 12px;
  padding: 12px;
  background: var(--paper);
}

.metric h3 {
  margin: 0 0 8px;
  font-size: 12px;
  opacity: 0.8;
}

.metric strong {
  display: block;
  font-size: 20px;
}

.metric small {
  display: block;
  margin-top: 4px;
  color: var(--ink-soft);
}

.dashboard-actions {
  margin: 14px 0;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.ip-grid {
  display: grid;
  gap: 10px;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  margin-bottom: 10px;
}

.section-title {
  margin: 18px 0 8px;
}

.table-wrap {
  overflow: auto;
}

table {
  width: 100%;
  border-collapse: collapse;
  min-width: 680px;
}

th,
td {
  border: 2px solid var(--ink);
  padding: 6px 8px;
  text-align: left;
  font-size: 12px;
}

th {
  background: var(--paper);
}

.pill {
  display: inline-block;
  border: 2px solid var(--ink);
  border-radius: 999px;
  padding: 2px 7px;
}

.pill.proxy {
  color: var(--accent-c);
}

.pill.direct {
  color: var(--ok);
}

.node-layout {
  display: grid;
  grid-template-columns: 360px 1fr;
  gap: 14px;
}

.node-list {
  display: grid;
  gap: 8px;
  align-content: start;
}

.node-card {
  border: 3px solid var(--ink);
  border-radius: 12px;
  padding: 10px;
  background: var(--paper);
  cursor: pointer;
}

.node-card.active {
  background: #fff1cf;
}

.node-card h4 {
  margin: 0;
}

.node-card p {
  margin: 5px 0;
  font-size: 13px;
}

.node-editor {
  display: grid;
  gap: 10px;
}

.form-grid {
  display: grid;
  gap: 8px;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
}

label {
  display: grid;
  gap: 5px;
  font-weight: 700;
  font-size: 13px;
}

input,
textarea,
select {
  width: 100%;
  border: 3px solid var(--ink);
  border-radius: 10px;
  background: var(--paper);
  color: var(--ink);
  padding: 7px 10px;
  font: inherit;
}

textarea {
  resize: vertical;
}

.log-list {
  max-height: 640px;
  overflow: auto;
  display: grid;
  gap: 6px;
}

.log-item {
  border: 2px solid var(--ink);
  border-radius: 10px;
  padding: 7px 9px;
  display: grid;
  grid-template-columns: 80px 100px 1fr;
  gap: 6px;
  font-size: 12px;
}

.log-item.debug {
  background: #ebf8ff;
}

.log-item.info {
  background: #eefcef;
}

.log-item.warn {
  background: #fff3d6;
}

.log-item.error {
  background: #ffe0dc;
}

.hint {
  font-size: 12px;
  opacity: 0.8;
}

.forward-list,
.route-list {
  margin-top: 12px;
  display: grid;
  gap: 10px;
}

.forward-card,
.route-card {
  border: 3px solid var(--ink);
  border-radius: 12px;
  padding: 12px;
  background: var(--paper);
}

.footbar {
  min-height: 20px;
  font-size: 12px;
  color: var(--bad);
}

@media (max-width: 980px) {
  .topbar {
    grid-template-columns: 1fr;
  }

  .node-layout {
    grid-template-columns: 1fr;
  }

  .panel {
    min-height: 0;
  }
}
</style>
