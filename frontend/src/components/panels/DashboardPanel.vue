<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ArrowDown, ArrowUp, Cable, Copy, Cpu, Power, RefreshCw, Search, X } from 'lucide-vue-next'
import TrafficChart from '../TrafficChart.vue'
import UsageHistoryChart from '../UsageHistoryChart.vue'
import { useApp } from '../../composables/appContext'
import { BALANCE_NODE_ID, DEFAULT_PROXY_PORT } from '../../types'
import type { IPDetectResult, ProxyMode } from '../../types'

const {
  t,
  config,
  state,
  proxyOpBusy,
  proxyOpState,
  primaryProxyActionLabel,
  runtimeStatusLabel,
  balanceAvailable,
  trafficSamples,
  trafficProxyShare,
  connections,
  connectionOpBusy,
  usageHistory,
  lanProxyInfo,
  proxyIP,
  directIP,
  humanBytes,
  toggleProxy,
  restartProxy,
  switchNode,
  setRoutingMode,
  detectDirectIP,
  detectProxyIP,
  closeConnection,
  closeAllConnections,
  copyText,
} = useApp()

const now = ref(Date.now())
let clock: number | null = null
onMounted(() => {
  clock = window.setInterval(() => {
    if (state.phase === 'running') now.value = Date.now()
  }, 1000)
})
onUnmounted(() => {
  if (clock) window.clearInterval(clock)
})

const uptime = computed(() => {
  if (state.phase !== 'running' || !state.startedAtUnix) return ''
  const start = state.startedAtUnix > 1e12 ? state.startedAtUnix : state.startedAtUnix * 1000
  const sec = Math.max(0, Math.floor((now.value - start) / 1000))
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60).toString().padStart(2, '0')
  const s = Math.floor(sec % 60).toString().padStart(2, '0')
  return h > 0 ? `${h}:${m}:${s}` : `${m}:${s}`
})

const busyPhase = computed(() => state.phase === 'starting' || state.phase === 'stopping')
// Cancelling a pending start must stay possible while the start call is in flight.
const powerDisabled = computed(() => state.phase === 'stopping' || (proxyOpBusy.value && state.phase !== 'starting'))

const modes: Array<{ value: ProxyMode; label: Parameters<typeof t>[0] }> = [
  { value: 'pac', label: 'modePac' },
  { value: 'global', label: 'modeGlobal' },
  { value: 'direct', label: 'modeDirect' },
]

const onNodeChange = (event: Event) => {
  void switchNode((event.target as HTMLSelectElement).value)
}

const rate = (bps: number) => `${humanBytes(bps || 0)}/s`
const latest = computed(() => trafficSamples.value[trafficSamples.value.length - 1])

const proxyPort = computed(() => state.proxyPort || DEFAULT_PROXY_PORT)
const localEndpoint = computed(() => `127.0.0.1:${proxyPort.value}`)
const lanEndpoints = computed(() => (config.core.allowLan ? lanProxyInfo.value.ips.map((ip) => `${ip}:${proxyPort.value}`) : []))

const ipBusy = ref<'' | 'direct' | 'proxy'>('')
const detect = async (kind: 'direct' | 'proxy') => {
  if (ipBusy.value) return
  ipBusy.value = kind
  try {
    await (kind === 'direct' ? detectDirectIP() : detectProxyIP())
  } catch {
    // the result row shows the failure
  } finally {
    ipBusy.value = ''
  }
}

const ipText = (r: IPDetectResult | null) => {
  if (!r) return ''
  if (r.error && !r.ip) return t('failed')
  return r.ip
}
const ipMeta = (r: IPDetectResult | null) => (r ? [r.country, r.region, r.isp].filter(Boolean).join(' · ') : '')

const visibleConnections = computed(() => connections.value.slice(0, 40))
const connMeta = (c: (typeof connections.value)[number]) =>
  [c.network?.toUpperCase(), c.rule, c.node].filter(Boolean).join(' · ')
</script>

<template>
  <div class="page dashboard">
    <section class="card hero" :class="{ online: state.phase === 'running', busy: busyPhase }">
      <div class="hero-main">
        <button
          class="power"
          :disabled="powerDisabled"
          :title="primaryProxyActionLabel"
          :aria-label="primaryProxyActionLabel"
          @click="toggleProxy"
        >
          <Power :size="24" :stroke-width="2.2" aria-hidden="true" />
        </button>
        <div class="hero-text">
          <div class="hero-status">
            <strong>{{ runtimeStatusLabel }}</strong>
            <span v-if="uptime" class="num muted">{{ uptime }}</span>
          </div>
          <select
            class="select"
            :value="config.activeNodeId"
            :disabled="config.nodes.length === 0"
            :aria-label="t('activeNode')"
            @change="onNodeChange"
          >
            <option v-if="config.nodes.length === 0" value="">{{ t('noNodesYet') }}</option>
            <option v-if="balanceAvailable" :value="BALANCE_NODE_ID">{{ t('balanceNode') }}</option>
            <option v-for="n in config.nodes" :key="n.id" :value="n.id">{{ n.name || n.serverAddress }}</option>
          </select>
        </div>
        <button
          class="icon-btn boxed"
          :disabled="proxyOpBusy || state.phase !== 'running'"
          :title="t('restart')"
          @click="restartProxy"
        >
          <RefreshCw :size="15" :class="{ spin: proxyOpState === 'restarting' }" aria-hidden="true" />
        </button>
      </div>
      <div class="hero-foot">
        <label class="hero-tun">
          <span class="switch"><input v-model="config.tun.enabled" type="checkbox" /><i /></span>
          <span class="ellipsis">{{ t('tunMode') }}</span>
        </label>
        <div class="segmented" role="radiogroup" :aria-label="t('proxyMode')">
          <button
            v-for="m in modes"
            :key="m.value"
            type="button"
            role="radio"
            :aria-checked="config.routing.proxyMode === m.value"
            :class="{ active: config.routing.proxyMode === m.value }"
            @click="setRoutingMode(m.value)"
          >
            {{ t(m.label) }}
          </button>
        </div>
      </div>
    </section>

    <section class="stats">
      <div class="stat">
        <span class="stat-label"><ArrowUp :size="13" aria-hidden="true" /><span class="ellipsis">{{ t('upload') }}</span></span>
        <strong class="num">{{ rate(latest?.txBps || 0) }}</strong>
        <small class="num">{{ t('total') }} {{ humanBytes(state.traffic.totalTx) }}</small>
      </div>
      <div class="stat">
        <span class="stat-label"><ArrowDown :size="13" aria-hidden="true" /><span class="ellipsis">{{ t('download') }}</span></span>
        <strong class="num">{{ rate(latest?.rxBps || 0) }}</strong>
        <small class="num">{{ t('total') }} {{ humanBytes(state.traffic.totalRx) }}</small>
      </div>
      <div class="stat">
        <span class="stat-label"><Cable :size="13" aria-hidden="true" /><span class="ellipsis">{{ t('connections') }}</span></span>
        <strong class="num">{{ state.connectionCount }}</strong>
        <small class="num">{{ t('proxy') }} {{ trafficProxyShare.toFixed(0) }}%</small>
      </div>
      <div class="stat">
        <span class="stat-label"><Cpu :size="13" aria-hidden="true" /><span class="ellipsis">{{ t('kernelMemory') }}</span></span>
        <strong class="num">{{ state.coreRunning && state.kernel.memoryBytes ? humanBytes(state.kernel.memoryBytes) : '—' }}</strong>
        <small class="ellipsis">{{ state.kernel.version || t('kernelIdle') }}</small>
      </div>
    </section>

    <section class="card">
      <div class="card-head"><h2>{{ t('realtimeTraffic') }}</h2></div>
      <div class="card-body">
        <TrafficChart :samples="trafficSamples" />
      </div>
    </section>

    <section class="card">
      <div class="card-head"><h2>{{ t('network') }}</h2></div>
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <strong>{{ t('localProxy') }}</strong>
            <small class="mono selectable">{{ localEndpoint }}<template v-for="e in lanEndpoints" :key="e"> · {{ e }}</template></small>
          </div>
          <button class="icon-btn" :title="t('copy')" @click="copyText(localEndpoint)"><Copy :size="15" aria-hidden="true" /></button>
        </div>
        <div class="row">
          <div class="row-text">
            <strong>{{ t('directIp') }}</strong>
            <small v-if="directIP" class="selectable"><span class="mono">{{ ipText(directIP) }}</span> {{ ipMeta(directIP) }}</small>
            <small v-else>{{ t('notChecked') }}</small>
          </div>
          <button class="icon-btn" :disabled="!!ipBusy" :title="t('detectDirect')" @click="detect('direct')">
            <RefreshCw v-if="ipBusy === 'direct'" :size="15" class="spin" aria-hidden="true" />
            <Search v-else :size="15" aria-hidden="true" />
          </button>
        </div>
        <div class="row">
          <div class="row-text">
            <strong>{{ t('proxyIp') }}</strong>
            <small v-if="proxyIP" class="selectable"><span class="mono">{{ ipText(proxyIP) }}</span> {{ ipMeta(proxyIP) }}</small>
            <small v-else>{{ state.phase === 'running' ? t('notChecked') : t('proxyNotRunning') }}</small>
          </div>
          <button class="icon-btn" :disabled="!!ipBusy || state.phase !== 'running'" :title="t('detectProxy')" @click="detect('proxy')">
            <RefreshCw v-if="ipBusy === 'proxy'" :size="15" class="spin" aria-hidden="true" />
            <Search v-else :size="15" aria-hidden="true" />
          </button>
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>{{ t('connections') }} <span class="count num">{{ connections.length }}</span></h2>
        <div class="actions">
          <button class="btn sm danger" :disabled="connectionOpBusy || connections.length === 0" @click="closeAllConnections">
            {{ t('closeAllConnections') }}
          </button>
        </div>
      </div>
      <div v-if="visibleConnections.length" class="list conn-list">
        <div v-for="c in visibleConnections" :key="c.id" class="list-item">
          <span class="conn-dot" :class="c.direction" :title="c.direction" />
          <div class="list-main">
            <strong class="selectable">{{ c.destination || '—' }}</strong>
            <small>{{ connMeta(c) }}</small>
          </div>
          <span class="conn-bytes num">↑{{ humanBytes(c.upload) }}<br />↓{{ humanBytes(c.download) }}</span>
          <button class="icon-btn danger" :disabled="connectionOpBusy" :title="t('disconnect')" @click="closeConnection(c.id)">
            <X :size="15" aria-hidden="true" />
          </button>
        </div>
      </div>
      <p v-else class="empty">{{ state.phase === 'running' ? t('noConnections') : t('proxyNotRunning') }}</p>
    </section>

    <section class="card">
      <div class="card-head"><h2>{{ t('usageHistory') }}</h2></div>
      <div class="card-body">
        <UsageHistoryChart :days="usageHistory" :format="humanBytes" />
      </div>
    </section>
  </div>
</template>
