<script setup lang="ts">
import TrafficChart from '../TrafficChart.vue'
import UsageHistoryChart from '../UsageHistoryChart.vue'
import type { AppConfig, IPDetectResult, RuntimeState, UsageDay } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  config: AppConfig
  state: RuntimeState
  proxyOpBusy: boolean
  directIp: IPDetectResult | null
  proxyIp: IPDetectResult | null
  usageHistory: UsageDay[]
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
}>()
</script>

<template>
  <main class="panel">
    <section class="hero-row">
      <div class="hero-main">
        <div class="hero-text">
          <h3>{{ props.t('dashboard') }}</h3>
          <p>核心状态与流量总览</p>
        </div>
        <div class="hero-controls">
          <label class="field compact">
            <span>{{ props.t('runningNode') }}</span>
            <select
              v-model="props.config.activeNodeId"
              :disabled="props.config.nodes.length === 0"
              @change="props.switchNode(props.config.activeNodeId)"
            >
              <option v-for="n in props.config.nodes" :key="n.id" :value="n.id">{{ n.name || n.serverAddress }}</option>
            </select>
          </label>
          <label class="switch-row">
            <span>{{ props.t('tunEnabled') }}</span>
            <span class="switch-control">
              <input type="checkbox" v-model="props.config.tun.enabled" />
              <span class="switch-ui" />
            </span>
          </label>
        </div>
      </div>

      <div class="hero-actions">
        <button
          class="power-btn"
          :class="props.state.running ? 'stop' : 'start'"
          :disabled="props.proxyOpBusy"
          @click="props.state.running ? props.stopProxy() : props.startProxy()"
        >
          <span class="power-indicator" />
          <strong>{{ props.state.running ? props.t('stop') : props.t('start') }}</strong>
          <small>{{ props.state.running ? '立即断开当前代理会话' : '立即启动代理会话' }}</small>
        </button>
        <button class="btn ghost" :disabled="props.proxyOpBusy || !props.state.running" @click="props.restartProxy">{{ props.t('restart') }}</button>
      </div>
    </section>

    <div class="metrics-grid">
      <article class="metric">
        <h3>{{ props.t('totalUpload') }}</h3>
        <strong>{{ props.humanBytes(props.state.traffic.totalTx) }}</strong>
        <small>{{ props.state.traffic.interface }} · {{ props.state.traffic.interfaceFound ? 'OK' : 'Missing' }}</small>
      </article>
      <article class="metric">
        <h3>{{ props.t('totalDownload') }}</h3>
        <strong>{{ props.humanBytes(props.state.traffic.totalRx) }}</strong>
        <small>{{ props.humanTime(props.state.traffic.lastSampleUnixMillis) }}</small>
      </article>
      <article class="metric">
        <h3>{{ props.t('proxyShare') }}</h3>
        <strong>{{ props.trafficProxyShare.toFixed(1) }}%</strong>
        <small>{{ props.humanBytes(props.state.traffic.estimatedProxyTx + props.state.traffic.estimatedProxyRx) }}</small>
      </article>
      <article class="metric">
        <h3>{{ props.t('directShare') }}</h3>
        <strong>{{ props.trafficDirectShare.toFixed(1) }}%</strong>
        <small>{{ props.humanBytes(props.state.traffic.estimatedDirectTx + props.state.traffic.estimatedDirectRx) }}</small>
      </article>
    </div>

    <TrafficChart :samples="props.state.traffic.recentBandwidth" />

    <h3 class="section-title">{{ props.t('usageHistory') }}</h3>
    <UsageHistoryChart :days="props.usageHistory" />

    <div class="dashboard-actions">
      <button class="btn" @click="props.detectDirectIp">{{ props.t('detectDirect') }}</button>
      <button class="btn" @click="props.detectProxyIp">{{ props.t('detectProxy') }}</button>
    </div>

    <div class="ip-grid">
      <article class="metric">
        <h3>{{ props.t('directIp') }}</h3>
        <strong>{{ props.directIp?.ip || '-' }}</strong>
        <small>{{ props.directIp?.country }} {{ props.directIp?.region }} {{ props.directIp?.isp }}</small>
        <small v-if="props.directIp?.error" class="error-text">{{ props.directIp.error }}</small>
      </article>
      <article class="metric">
        <h3>{{ props.t('proxyIp') }}</h3>
        <strong>{{ props.proxyIp?.ip || '-' }}</strong>
        <small>{{ props.proxyIp?.country }} {{ props.proxyIp?.region }} {{ props.proxyIp?.isp }}</small>
        <small v-if="props.proxyIp?.error" class="error-text">{{ props.proxyIp.error }}</small>
      </article>
    </div>

    <div class="section-head">
      <h3 class="section-title">{{ props.t('connections') }}</h3>
      <button class="btn mini danger" :disabled="props.connectionOpBusy || props.state.connections.length === 0" @click="props.closeAllConnections">
        关闭全部连接
      </button>
    </div>
    <div class="table-wrap">
      <table>
        <thead>
          <tr>
            <th>{{ props.t('network') }}</th>
            <th>{{ props.t('source') }}</th>
            <th>{{ props.t('destination') }}</th>
            <th>{{ props.t('direction') }}</th>
            <th>{{ props.t('seen') }}</th>
            <th>{{ props.t('hits') }}</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in props.state.connections.slice(0, 16)" :key="item.id">
            <td>{{ item.network }}</td>
            <td>{{ item.source }}</td>
            <td>{{ item.destination }}</td>
            <td><span class="pill" :class="item.direction">{{ item.direction }}</span></td>
            <td>{{ new Date(item.lastSeen).toLocaleTimeString() }}</td>
            <td>{{ item.hits }}</td>
            <td><button class="btn mini danger" :disabled="props.connectionOpBusy" @click="props.closeConnection(item.id)">断开</button></td>
          </tr>
          <tr v-if="props.state.connections.length === 0"><td colspan="7">{{ props.t('none') }}</td></tr>
        </tbody>
      </table>
    </div>
  </main>
</template>
