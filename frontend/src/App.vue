<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Application, System, Window } from '@wailsio/runtime'
import {
  ArrowLeftRight,
  Gauge,
  Grid3X3,
  Network,
  PanelLeftClose,
  PanelLeftOpen,
  Route,
  ScrollText,
  Server,
  Settings2,
} from 'lucide-vue-next'
import SudokuGame from './components/SudokuGame.vue'
import DashboardPanel from './components/panels/DashboardPanel.vue'
import LogsPanel from './components/panels/LogsPanel.vue'
import MiscPanel from './components/panels/MiscPanel.vue'
import NodeEditorModal from './components/panels/NodeEditorModal.vue'
import NodesPanel from './components/panels/NodesPanel.vue'
import RelayPanel from './components/panels/RelayPanel.vue'
import RoutingPanel from './components/panels/RoutingPanel.vue'
import TunPanel from './components/panels/TunPanel.vue'
import TunAdminModal from './components/panels/TunAdminModal.vue'
import { useAppController } from './composables/useAppController'
import './styles/index.css'

const navIcons = {
  dashboard: Gauge,
  nodes: Server,
  routing: Route,
  tun: Network,
  relay: ArrowLeftRight,
  logs: ScrollText,
  misc: Settings2,
  game: Grid3X3,
} as const

const {
  t,
  logoUrl,
  currentTab,
  navMain,
  navExtra,
  sidebarCollapsed,
  toggleSidebar,
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
  proxyOpBusy,
  proxyOpState,
  runtimeStatusLabel,
  primaryProxyActionLabel,
  primaryProxyActionHint,
  directIP,
  proxyIP,
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
  openLogs,
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
} = useAppController()

const navRef = ref<HTMLElement | null>(null)
const navScrollbarStyle = ref<Record<string, string>>({})
let navResizeObserver: ResizeObserver | null = null

const updateNavScrollbar = () => {
  const nav = navRef.value
  if (!nav) return

  const trackWidth = nav.clientWidth
  const overflow = Math.max(0, nav.scrollWidth - trackWidth)
  const thumbWidth = overflow > 1 ? Math.max(36, trackWidth * (trackWidth / nav.scrollWidth)) : trackWidth
  const thumbOffset = overflow > 1 ? (nav.scrollLeft / overflow) * Math.max(0, trackWidth - thumbWidth) : 0

  navScrollbarStyle.value = {
    '--nav-scroll-thumb-width': `${thumbWidth}px`,
    '--nav-scroll-thumb-offset': `${thumbOffset}px`,
    opacity: overflow > 1 ? '1' : '0',
  }
}

const closeWindow = () => {
  if (System.IsWindows()) {
    void Window.Hide()
    void Application.Quit()
    return
  }
  void Window.Hide()
}

const minimiseWindow = () => {
  void Window.Minimise()
}

const toggleMaximiseWindow = () => {
  void Window.ToggleMaximise()
}

const handleWindowShortcut = (event: KeyboardEvent) => {
  if (!System.IsMac() || !event.metaKey || event.ctrlKey || event.altKey || event.shiftKey || event.key.toLowerCase() !== 'w') {
    return
  }
  event.preventDefault()
  event.stopPropagation()
  closeWindow()
}

watch(currentTab, async () => {
  await nextTick()
  if (!window.matchMedia('(max-aspect-ratio: 1/1)').matches) return
  navRef.value
    ?.querySelector<HTMLElement>(`.navbtn[data-nav-key="${currentTab.value}"]`)
    ?.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'nearest' })
  updateNavScrollbar()
})

onMounted(async () => {
  await nextTick()
  updateNavScrollbar()
  navResizeObserver = new ResizeObserver(updateNavScrollbar)
  if (navRef.value) navResizeObserver.observe(navRef.value)
  window.addEventListener('resize', updateNavScrollbar)
  window.addEventListener('keydown', handleWindowShortcut, true)
})

onBeforeUnmount(() => {
  navResizeObserver?.disconnect()
  window.removeEventListener('resize', updateNavScrollbar)
  window.removeEventListener('keydown', handleWindowShortcut, true)
})

</script>

<template>
  <div class="app-shell" :data-theme="config.ui.theme">
    <header class="window-chrome" @dblclick="toggleMaximiseWindow">
      <div class="window-controls" @dblclick.stop>
        <button class="traffic-dot close-dot" type="button" aria-label="Close" title="Close" @click="closeWindow" />
        <button class="traffic-dot minimize-dot" type="button" aria-label="Minimise" title="Minimise" @click="minimiseWindow" />
        <button class="traffic-dot zoom-dot" type="button" aria-label="Maximise" title="Maximise" @click="toggleMaximiseWindow" />
      </div>
    </header>

    <aside class="sidebar" :class="{ collapsed: sidebarCollapsed }">
      <div class="brand">
        <img class="brand-logo" :src="logoUrl" alt="" />
        <div v-if="!sidebarCollapsed" class="brand-text">
          <div class="brand-title">{{ t('appTitle') }}</div>
          <div class="brand-sub">{{ t('subtitle') }}</div>
        </div>
        <button class="iconbtn" type="button" @click="toggleSidebar" :title="sidebarCollapsed ? t('expandSidebar') : t('collapseSidebar')">
          <PanelLeftOpen v-if="sidebarCollapsed" :size="18" aria-hidden="true" />
          <PanelLeftClose v-else :size="18" aria-hidden="true" />
        </button>
      </div>

      <nav ref="navRef" class="nav" @scroll.passive="updateNavScrollbar">
        <div class="nav-group">
          <button
            v-for="item in navMain"
            :key="item.key"
            class="navbtn"
            :class="{ active: currentTab === item.key }"
            :data-nav-key="item.key"
            :title="sidebarCollapsed ? t(item.key) : undefined"
            @click="currentTab = item.key"
          >
            <component :is="navIcons[item.key]" :size="18" aria-hidden="true" />
            <span v-if="!sidebarCollapsed">{{ t(item.key) }}</span>
          </button>
        </div>

        <div class="nav-divider" />

        <div class="nav-group">
          <button
            v-for="item in navExtra"
            :key="item.key"
            class="navbtn"
            :class="{ active: currentTab === item.key }"
            :data-nav-key="item.key"
            :title="sidebarCollapsed ? t(item.key) : undefined"
            @click="currentTab = item.key"
          >
            <component :is="navIcons[item.key]" :size="18" aria-hidden="true" />
            <span v-if="!sidebarCollapsed">{{ t(item.key) }}</span>
          </button>
        </div>
      </nav>
      <div class="nav-scroll-track" :style="navScrollbarStyle" aria-hidden="true">
        <span />
      </div>

      <div class="sidebar-foot">
        <div class="statusbox" :class="state.running ? 'ok' : 'off'">
          <span class="dot" />
          <span v-if="!sidebarCollapsed">{{ runtimeStatusLabel }}</span>
        </div>
        <small v-if="!sidebarCollapsed" class="sidebar-node">{{ t('runningNode') }}: {{ state.activeNodeName || '-' }}</small>
      </div>
    </aside>

    <div class="content">
      <header class="topbar">
        <div class="pagehead">
          <h2>{{ t(currentTab) }}</h2>
          <p>{{ state.activeNodeName || '-' }}</p>
        </div>
        <div class="topbar-right">
          <span class="pill" :class="state.running ? 'ok' : 'off'">{{ runtimeStatusLabel }}</span>
          <span class="pill" :class="state.tunRunning ? 'ok' : 'off'">TUN</span>
        </div>
      </header>

      <section v-if="notice" class="notice" :class="noticeType">{{ notice }}</section>

      <DashboardPanel
        v-if="currentTab === 'dashboard'"
        :t="t"
        :config="config"
        :state="state"
        :proxy-op-busy="proxyOpBusy"
        :proxy-op-state="proxyOpState"
        :primary-proxy-action-label="primaryProxyActionLabel"
        :primary-proxy-action-hint="primaryProxyActionHint"
        :direct-ip="directIP"
        :proxy-ip="proxyIP"
        :usage-history="usageHistory"
        :lan-proxy-info="lanProxyInfo"
        :traffic-proxy-share="trafficProxyShare"
        :traffic-direct-share="trafficDirectShare"
        :connection-op-busy="connectionOpBusy"
        :human-bytes="humanBytes"
        :human-time="humanTime"
        :start-proxy="startProxy"
        :stop-proxy="stopProxy"
        :restart-proxy="restartProxy"
        :switch-node="switchNode"
        :detect-direct-ip="detectDirectIP"
        :detect-proxy-ip="detectProxyIP"
        :close-connection="closeConnection"
        :close-all-connections="closeAllConnections"
        :open-logs="openLogs"
      />

      <NodesPanel
        v-if="currentTab === 'nodes'"
        :t="t"
        :sorted-nodes="sortedNodes"
        :open-create-node="openCreateNode"
        :sort-by-name="sortByName"
        :sort-by-latency="sortByLatency"
        :probe-all="probeAll"
        :auto-best="autoBest"
        :pick-node="pickNode"
        :probe-node="probeNode"
        :export-shortlink="exportShortlink"
        :open-edit-node="openEditNode"
        :remove-node="removeNode"
        :switch-node="switchNode"
      />

      <RoutingPanel
        v-if="currentTab === 'routing'"
        :t="t"
        :config="config"
        :custom-rules-validation="customRulesValidation"
        :set-routing-mode="setRoutingMode"
        :add-pac-rule="addPacRule"
        :remove-pac-rule="removePacRule"
        :normalize-pac-rules="normalizePacRules"
        :save-config="() => saveConfig()"
      />

      <TunPanel
        v-if="currentTab === 'tun'"
        :t="t"
        :config="config"
        :reset-tun-factory="resetTunFactory"
        :save-config="() => saveConfig()"
      />

      <RelayPanel
        v-if="currentTab === 'relay'"
        :t="t"
        :config="config"
        :state="state"
        :add-port-forward="addPortForward"
        :remove-port-forward="removePortForward"
        :add-reverse-route="addReverseRoute"
        :remove-reverse-route="removeReverseRoute"
        :start-reverse="startReverse"
        :stop-reverse="stopReverse"
        :save-config="saveConfig"
      />

      <MiscPanel v-if="currentTab === 'misc'" :t="t" :config="config" :save-config="saveConfig" />

      <LogsPanel
        v-if="currentTab === 'logs'"
        :t="t"
        :log-level-filter="logLevelFilter"
        :log-search="logSearch"
        :log-display-limit="logDisplayLimit"
        :show-traffic-logs="showTrafficLogs"
        :filtered-logs="filteredLogs"
        :format-log-timestamp="formatLogTimestamp"
        :log-level-text="logLevelText"
        :log-component-text="logComponentText"
        @update:log-level-filter="logLevelFilter = $event"
        @update:log-search="logSearch = $event"
        @update:log-display-limit="logDisplayLimit = $event"
        @update:show-traffic-logs="showTrafficLogs = $event"
      />

      <main class="panel" v-if="currentTab === 'game'">
        <SudokuGame />
      </main>

    </div>

    <NodeEditorModal
      :open="nodeEditorOpen"
      :node-editor-mode="nodeEditorMode"
      :editable-node="editableNode"
      :shortlink-input="shortlinkInput"
      :shortlink-name="shortlinkName"
      :t="t"
      @close="closeNodeEditor"
      @save="saveNode"
      @parse-shortlink="parseShortlinkFromInput"
      @parse-clipboard="parseShortlinkFromClipboard"
      @update:shortlink-input="shortlinkInput = $event"
      @update:shortlink-name="shortlinkName = $event"
    />

    <TunAdminModal
      :open="tunAdminModalOpen"
      :password="tunAdminPassword"
      :busy="tunAdminBusy"
      :error="tunAdminError"
      :t="t"
      @close="closeTunAdminModal"
      @submit="submitTunAdminModal"
      @update:password="tunAdminPassword = $event"
    />
  </div>
</template>
