<script setup lang="ts">
import { computed } from 'vue'
import {
  Activity,
  ArrowDown,
  ArrowUp,
  Cable,
  Gauge,
  Network,
  Power,
  RefreshCw,
  Split,
} from 'lucide-vue-next'
import TrafficChart from '../TrafficChart.vue'
import UsageHistoryChart from '../UsageHistoryChart.vue'
import type { AppConfig, IPDetectResult, LANProxyInfo, RuntimeState, UsageDay } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  config: AppConfig
  state: RuntimeState
  proxyOpBusy: boolean
  proxyOpState: 'idle' | 'starting' | 'stopping' | 'restarting'
  primaryProxyActionLabel: string
  primaryProxyActionHint: string
  directIp: IPDetectResult | null
  proxyIp: IPDetectResult | null
  usageHistory: UsageDay[]
  lanProxyInfo: LANProxyInfo
  trafficProxyShare: number
  trafficDirectShare: number
  connectionOpBusy: boolean
  humanBytes: (value: number) => string
  humanTime: (value: number) => string
  startProxy: () => void
  stopProxy: () => void
  restartProxy: () => void
  switchNode: (id: string) => void
  detectDirectIp: () => void
  detectProxyIp: () => void
  closeConnection: (id: string) => void
  closeAllConnections: () => void
  openLogs: () => void
}>()

const lanEndpointsText = computed(() => {
  const port = props.lanProxyInfo?.port || props.config.core.localPort || 1080
  const ips = Array.isArray(props.lanProxyInfo?.ips) ? props.lanProxyInfo.ips : []
  if (ips.length === 0) return `127.0.0.1:${port}`
  if (ips.length <= 2) return ips.map((ip) => `${ip}:${port}`).join(' / ')
  return `${ips.slice(0, 2).map((ip) => `${ip}:${port}`).join(' / ')} +${ips.length - 2}`
})

const kernelLatencyText = computed(() => {
  if (String(props.state.kernel?.latencyError || '').trim()) return 'NaN'
  const latency = props.state.kernel?.latencyMs ?? -1
  if (latency >= 0) return `${latency} ms`
  if ((props.state.kernel?.latencyCheckedAt ?? 0) > 0) return 'NaN'
  return '-'
})

const kernelVersionText = computed(() => props.state.kernel?.version?.trim() || 'unknown')

const kernelMemoryText = computed(() => {
  const bytes = props.state.kernel?.memoryBytes ?? 0
  if (bytes > 0) return props.humanBytes(bytes)
  return props.state.coreRunning ? '0 B' : '-'
})

const runtimeText = computed(() => {
  if (!props.state.startedAtUnix) return '00:00:00'
  const start = props.state.startedAtUnix > 1e12 ? props.state.startedAtUnix : props.state.startedAtUnix * 1000
  const sec = Math.max(0, Math.floor((Date.now() - start) / 1000))
  const h = Math.floor(sec / 3600).toString().padStart(2, '0')
  const m = Math.floor((sec % 3600) / 60).toString().padStart(2, '0')
  const s = Math.floor(sec % 60).toString().padStart(2, '0')
  return `${h}:${m}:${s}`
})

const startedAtText = computed(() => {
  if (!props.state.startedAtUnix) return '-'
  const start = props.state.startedAtUnix > 1e12 ? props.state.startedAtUnix : props.state.startedAtUnix * 1000
  return new Date(start).toLocaleString()
})

const activeNodeLabel = computed(() => props.state.activeNodeName || props.config.nodes.find((n) => n.id === props.config.activeNodeId)?.name || '-')

const recentActivity = computed(() => {
  const logs = Array.isArray(props.state.recentLogs) ? props.state.recentLogs : []
  const out = logs
    .filter((item) => !String(item.component || '').toLowerCase().includes('traffic'))
    .slice()
    .reverse()
    .slice(0, 5)
    .map((item) => ({
    key: item.id,
    title: item.message || item.raw || '-',
    level: item.level || 'info',
    time: new Date(item.timestamp).toLocaleTimeString([], { hour12: false }),
    }))
  if (out.length > 0) return out
  return [
    { key: 'state', title: props.state.running ? props.t('systemRunning') : props.t('systemStopped'), meta: 'core', level: props.state.running ? 'info' : 'warn', time: '-' },
    { key: 'node', title: `${props.t('runningNode')}: ${activeNodeLabel.value}`, meta: 'node', level: 'info', time: '-' },
    { key: 'idle', title: props.t('waitingForActivity'), meta: 'runtime', level: 'debug', time: '-' },
  ]
})

const coreRows = computed(() => [
  { label: props.t('configLoad'), value: props.t('statusNormal'), tone: 'ok' },
  { label: props.t('kernelService'), value: props.state.coreRunning ? props.t('statusRunning') : props.t('statusStopped'), tone: props.state.coreRunning ? 'ok' : 'bad' },
  { label: 'DNS', value: props.state.running ? props.t('statusRunning') : props.t('statusStopped'), tone: props.state.running ? 'ok' : 'bad' },
  { label: 'TUN', value: props.state.tunRunning ? props.t('statusRunning') : props.config.tun.enabled ? props.t('enabled') : props.t('disabled'), tone: props.state.tunRunning ? 'ok' : 'muted' },
  { label: props.t('reverseForwarder'), value: props.state.reverseRunning ? props.t('statusRunning') : props.t('statusStopped'), tone: props.state.reverseRunning ? 'ok' : 'bad' },
])
</script>

<template>
  <main class="panel dashboard-page">
    <section class="connection-hub" :class="{ online: props.state.running }">
      <div class="connection-copy">
        <span class="connection-kicker">
          <span class="connection-dot" />
          {{ props.primaryProxyActionHint }}
        </span>
        <h3>{{ activeNodeLabel }}</h3>
        <label class="node-selector">
          <Network :size="17" aria-hidden="true" />
          <select
            v-model="props.config.activeNodeId"
            :disabled="props.config.nodes.length === 0"
            @change="props.switchNode(props.config.activeNodeId)"
          >
            <option v-for="n in props.config.nodes" :key="n.id" :value="n.id">{{ n.name || n.serverAddress }}</option>
          </select>
        </label>
      </div>

      <div class="connection-primary">
        <button
          class="power-orb"
          :class="{ online: props.state.running }"
          :disabled="props.proxyOpBusy"
          :title="props.primaryProxyActionLabel"
          @click="props.state.running ? props.stopProxy() : props.startProxy()"
        >
          <Power :size="30" :stroke-width="1.8" aria-hidden="true" />
        </button>
        <strong>{{ props.primaryProxyActionLabel }}</strong>
      </div>

      <div class="connection-quick-actions">
        <label class="quick-toggle">
          <span>
            <Network :size="16" aria-hidden="true" />
            {{ props.t('tunEnabled') }}
          </span>
          <span class="switch-control">
            <input type="checkbox" v-model="props.config.tun.enabled" />
            <span class="switch-ui" />
          </span>
        </label>
        <button class="quick-command" :disabled="props.proxyOpBusy || !props.state.running" @click="props.restartProxy">
          <RefreshCw :size="16" :class="{ spinning: props.proxyOpState === 'restarting' }" aria-hidden="true" />
          <span>{{ props.proxyOpState === 'restarting' ? props.t('restartInProgress') : props.t('restart') }}</span>
        </button>
      </div>
    </section>

    <section class="metric-strip">
      <article class="metric tile-upload">
        <div class="metric-icon"><ArrowUp :size="17" aria-hidden="true" /></div>
        <h3>{{ props.t('totalUpload') }}</h3>
        <strong>{{ props.humanBytes(props.state.traffic.totalTx) }}</strong>
        <small>{{ props.state.traffic.interface || 'tun0' }} · {{ props.state.traffic.interfaceFound ? props.t('interfaceOk') : props.t('interfaceMissing') }}</small>
      </article>
      <article class="metric tile-download">
        <div class="metric-icon"><ArrowDown :size="17" aria-hidden="true" /></div>
        <h3>{{ props.t('totalDownload') }}</h3>
        <strong>{{ props.humanBytes(props.state.traffic.totalRx) }}</strong>
        <small>{{ props.humanTime(props.state.traffic.lastSampleUnixMillis) }}</small>
      </article>
      <article class="metric">
        <div class="metric-icon"><Gauge :size="17" aria-hidden="true" /></div>
        <h3>{{ props.t('proxyShare') }}</h3>
        <strong>{{ props.trafficProxyShare.toFixed(1) }}%</strong>
        <small>{{ props.humanBytes(props.state.traffic.estimatedProxyTx + props.state.traffic.estimatedProxyRx) }}</small>
      </article>
      <article class="metric">
        <div class="metric-icon"><Split :size="17" aria-hidden="true" /></div>
        <h3>{{ props.t('directShare') }}</h3>
        <strong>{{ props.trafficDirectShare.toFixed(1) }}%</strong>
        <small>{{ props.humanBytes(props.state.traffic.estimatedDirectTx + props.state.traffic.estimatedDirectRx) }}</small>
      </article>
      <article class="metric">
        <div class="metric-icon"><Cable :size="17" aria-hidden="true" /></div>
        <h3>{{ props.t('proxyAccess') }}</h3>
        <strong>SOCKS5</strong>
        <small>{{ lanEndpointsText }}</small>
      </article>
    </section>

    <section class="dashboard-grid">
      <article class="deck-card chart-card">
        <div class="card-head">
          <div>
            <h3>{{ props.t('realtimeTraffic') }}</h3>
            <p>{{ props.t('trafficWindow60s') }}</p>
          </div>
          <span class="soft-select">60s</span>
        </div>
        <TrafficChart :samples="props.state.traffic.recentBandwidth" />
      </article>

      <aside class="deck-card system-card">
        <div class="card-head">
          <h3>{{ props.t('systemStatus') }}</h3>
        </div>
        <dl class="status-list">
          <div><dt>{{ props.t('realLatency') }}</dt><dd>{{ kernelLatencyText }}</dd></div>
          <div><dt>{{ props.t('kernelMemory') }}</dt><dd>{{ kernelMemoryText }}</dd></div>
          <div><dt>{{ props.t('kernelVersion') }}</dt><dd>{{ kernelVersionText }}</dd></div>
          <div><dt>{{ props.t('runtime') }}</dt><dd>{{ runtimeText }}</dd></div>
        </dl>
      </aside>
    </section>

    <section class="dashboard-bottom">
      <article class="deck-card activity-card">
        <div class="card-head">
          <h3><Activity :size="17" aria-hidden="true" />{{ props.t('recentActivity') }}</h3>
          <button class="ghost-link" @click="props.openLogs">{{ props.t('viewDetails') }}</button>
        </div>
        <div class="activity-list">
          <div v-for="item in recentActivity" :key="item.key" class="activity-row">
            <span class="activity-time">{{ item.time }}</span>
            <span class="activity-title">{{ item.title }}</span>
            <span class="activity-level" :class="item.level">{{ item.level }}</span>
          </div>
        </div>
      </article>

      <article class="deck-card core-card">
        <div class="card-head">
          <h3>{{ props.t('coreStatus') }}</h3>
          <span class="muted">{{ startedAtText }}</span>
        </div>
        <div class="core-list">
          <div v-for="row in coreRows" :key="row.label" class="core-row">
            <span>{{ row.label }}</span>
            <strong :class="row.tone">{{ row.value }}</strong>
          </div>
        </div>
      </article>
    </section>

    <section class="dashboard-grid secondary">
      <article class="deck-card usage-card">
        <div class="card-head">
          <h3>{{ props.t('usageHistory') }}</h3>
        </div>
        <UsageHistoryChart :days="props.usageHistory" />
      </article>
      <article class="deck-card ip-card">
        <div class="card-head">
          <h3>IP</h3>
          <div class="inline-actions">
            <button class="btn mini ghost" @click="props.detectDirectIp">{{ props.t('detectDirect') }}</button>
            <button class="btn mini ghost" @click="props.detectProxyIp">{{ props.t('detectProxy') }}</button>
          </div>
        </div>
        <div class="ip-stack">
          <div>
            <span>{{ props.t('directIp') }}</span>
            <strong>{{ props.directIp?.ip || '-' }}</strong>
            <small>{{ props.directIp?.country }} {{ props.directIp?.region }} {{ props.directIp?.isp }}</small>
          </div>
          <div>
            <span>{{ props.t('proxyIp') }}</span>
            <strong>{{ props.proxyIp?.ip || '-' }}</strong>
            <small>{{ props.proxyIp?.country }} {{ props.proxyIp?.region }} {{ props.proxyIp?.isp }}</small>
          </div>
        </div>
      </article>
    </section>

    <section class="deck-card connections-card">
      <div class="card-head">
        <h3>{{ props.t('connections') }}</h3>
        <button class="btn mini danger" :disabled="props.connectionOpBusy || props.state.connections.length === 0" @click="props.closeAllConnections">
          {{ props.t('closeAllConnections') }}
        </button>
      </div>
      <div class="table-wrap flush connections-table-wrap">
        <table class="connections-table">
          <thead>
            <tr>
              <th>{{ props.t('network') }}</th>
              <th>{{ props.t('source') }}</th>
              <th>{{ props.t('destination') }}</th>
              <th>{{ props.t('direction') }}</th>
              <th>{{ props.t('seen') }}</th>
              <th>{{ props.t('hits') }}</th>
              <th>{{ props.t('action') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in props.state.connections.slice(0, 16)" :key="item.id">
              <td :data-label="props.t('network')">{{ item.network }}</td>
              <td :data-label="props.t('source')">{{ item.source }}</td>
              <td :data-label="props.t('destination')">{{ item.destination }}</td>
              <td :data-label="props.t('direction')"><span class="pill" :class="item.direction">{{ item.direction }}</span></td>
              <td :data-label="props.t('seen')">{{ new Date(item.lastSeen).toLocaleTimeString() }}</td>
              <td :data-label="props.t('hits')">{{ item.hits }}</td>
              <td :data-label="props.t('action')"><button class="btn mini danger" :disabled="props.connectionOpBusy" @click="props.closeConnection(item.id)">{{ props.t('disconnect') }}</button></td>
            </tr>
            <tr v-if="props.state.connections.length === 0" class="connections-empty"><td colspan="7">{{ props.t('none') }}</td></tr>
          </tbody>
        </table>
      </div>
    </section>
  </main>
</template>
