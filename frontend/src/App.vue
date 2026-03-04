<script setup lang="ts">
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
import './app-shell.css'

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
  filteredLogs,
  proxyOpBusy,
  proxyOpState,
  runtimeStatusLabel,
  primaryProxyActionLabel,
  primaryProxyActionHint,
  directIP,
  proxyIP,
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
} = useAppController()
</script>

<template>
  <div class="app-shell" :data-theme="config.ui.theme">
    <aside class="sidebar" :class="{ collapsed: sidebarCollapsed }">
      <div class="brand">
        <img class="brand-logo" :src="logoUrl" alt="" />
        <div v-if="!sidebarCollapsed" class="brand-text">
          <div class="brand-title">{{ t('appTitle') }}</div>
          <div class="brand-sub">{{ t('subtitle') }}</div>
        </div>
        <button class="iconbtn" type="button" @click="toggleSidebar" :title="sidebarCollapsed ? t('expandSidebar') : t('collapseSidebar')">
          <svg viewBox="0 0 24 24" aria-hidden="true">
            <path
              v-if="sidebarCollapsed"
              d="M9 18l6-6-6-6"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
            <path
              v-else
              d="M15 18l-6-6 6-6"
              fill="none"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
        </button>
      </div>

      <nav class="nav">
        <div class="nav-group">
          <button
            v-for="item in navMain"
            :key="item.key"
            class="navbtn"
            :class="{ active: currentTab === item.key }"
            :title="sidebarCollapsed ? t(item.key) : undefined"
            @click="currentTab = item.key"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                v-for="(d, idx) in item.icon"
                :key="idx"
                :d="d"
                fill="none"
                stroke="currentColor"
                stroke-width="1.7"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
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
            :title="sidebarCollapsed ? t(item.key) : undefined"
            @click="currentTab = item.key"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path
                v-for="(d, idx) in item.icon"
                :key="idx"
                :d="d"
                fill="none"
                stroke="currentColor"
                stroke-width="1.7"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
            </svg>
            <span v-if="!sidebarCollapsed">{{ t(item.key) }}</span>
          </button>
        </div>
      </nav>

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
        :save-config="() => saveConfig()"
      />

      <MiscPanel v-if="currentTab === 'misc'" :t="t" :config="config" :save-config="() => saveConfig()" />

      <LogsPanel
        v-if="currentTab === 'logs'"
        :t="t"
        :log-level-filter="logLevelFilter"
        :log-search="logSearch"
        :log-display-limit="logDisplayLimit"
        :filtered-logs="filteredLogs"
        :format-log-timestamp="formatLogTimestamp"
        :log-level-text="logLevelText"
        :log-component-text="logComponentText"
        @update:log-level-filter="logLevelFilter = $event"
        @update:log-search="logSearch = $event"
        @update:log-display-limit="logDisplayLimit = $event"
      />

      <main class="panel" v-if="currentTab === 'game'">
        <SudokuGame />
      </main>

      <footer class="footbar">
        <span>{{ state.lastError || state.routeSetupError }}</span>
      </footer>
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
