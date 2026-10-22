<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { Application, System, Window } from '@wailsio/runtime'
import {
  ArrowLeftRight,
  CircleAlert,
  Gauge,
  Grid3X3,
  Minus,
  Network,
  Route,
  ScrollText,
  Server,
  Settings2,
  Square,
  Wrench,
  X,
} from 'lucide-vue-next'
import SudokuGame from './components/SudokuGame.vue'
import AdminPasswordModal from './components/AdminPasswordModal.vue'
import DashboardPanel from './components/panels/DashboardPanel.vue'
import LogsPanel from './components/panels/LogsPanel.vue'
import MiscPanel from './components/panels/MiscPanel.vue'
import NodeEditorModal from './components/panels/NodeEditorModal.vue'
import NodesPanel from './components/panels/NodesPanel.vue'
import RelayPanel from './components/panels/RelayPanel.vue'
import RoutingPanel from './components/panels/RoutingPanel.vue'
import TunPanel from './components/panels/TunPanel.vue'
import { provideApp } from './composables/appContext'
import './styles/index.css'

const {
  t,
  logoUrl,
  currentTab,
  navMain,
  navExtra,
  notice,
  noticeType,
  resolvedTheme,
  state,
  runtimeStatusLabel,
  repairBusy,
  repairNetwork,
  localizeError,
} = provideApp()

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

const panels = {
  dashboard: DashboardPanel,
  nodes: NodesPanel,
  routing: RoutingPanel,
  tun: TunPanel,
  relay: RelayPanel,
  logs: LogsPanel,
  misc: MiscPanel,
  game: SudokuGame,
} as const

const isMac = System.IsMac()
const isWindows = System.IsWindows()

const statusTone = computed(() => {
  if (state.phase === 'running') return 'ok'
  if (state.phase === 'starting' || state.phase === 'stopping') return 'busy'
  return 'off'
})

// The last error is shown once per distinct message until dismissed.
const dismissedError = ref('')
const errorBanner = computed(() => {
  const raw = String(state.lastError || '').trim()
  if (!raw || raw === dismissedError.value || state.phase === 'running' || state.phase === 'starting') return ''
  return localizeError(raw, 'startFailed')
})

const scrollRef = ref<HTMLElement | null>(null)
const scrollTops: Record<string, number> = {}
watch(currentTab, (next, prev) => {
  const el = scrollRef.value
  if (!el) return
  scrollTops[prev] = el.scrollTop
  requestAnimationFrame(() => {
    el.scrollTop = scrollTops[next] ?? 0
  })
})

const closeWindow = () => {
  if (isWindows) {
    void Window.Hide()
    void Application.Quit()
    return
  }
  void Window.Hide()
}
const minimiseWindow = () => void Window.Minimise()
const toggleMaximiseWindow = () => void Window.ToggleMaximise()

const onKeydown = (event: KeyboardEvent) => {
  if (!isMac || !event.metaKey || event.ctrlKey || event.altKey || event.shiftKey || event.key.toLowerCase() !== 'w') return
  event.preventDefault()
  event.stopPropagation()
  closeWindow()
}

onMounted(() => window.addEventListener('keydown', onKeydown, true))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown, true))
</script>

<template>
  <div class="app-shell" :class="{ rounded: !isWindows }" :data-theme="resolvedTheme">
    <aside class="sidebar" @dblclick.self="toggleMaximiseWindow">
      <div v-if="isMac" class="window-dots">
        <button class="close" type="button" aria-label="Close" @click="closeWindow" />
        <button class="min" type="button" aria-label="Minimise" @click="minimiseWindow" />
        <button class="zoom" type="button" aria-label="Zoom" @click="toggleMaximiseWindow" />
      </div>
      <div class="brand">
        <img :src="logoUrl" alt="" />
        <div class="brand-name">{{ t('appTitle') }}<small>{{ t('subtitle') }}</small></div>
      </div>

      <nav class="nav">
        <button
          v-for="item in navMain"
          :key="item.key"
          class="nav-btn"
          :class="{ active: currentTab === item.key }"
          :title="t(item.key)"
          @click="currentTab = item.key"
        >
          <component :is="navIcons[item.key]" :size="18" :stroke-width="1.9" aria-hidden="true" />
          <span class="nav-label">{{ t(item.key) }}</span>
          <span v-if="item.key === 'tun' && state.tunRunning" class="nav-badge" />
        </button>
      </nav>
      <div class="nav-spacer" />
      <button
        v-for="item in navExtra"
        :key="item.key"
        class="nav-btn"
        :class="{ active: currentTab === item.key }"
        :title="t(item.key)"
        @click="currentTab = item.key"
      >
        <component :is="navIcons[item.key]" :size="18" :stroke-width="1.9" aria-hidden="true" />
        <span class="nav-label">{{ t(item.key) }}</span>
      </button>
    </aside>

    <div class="main">
      <header class="topbar" @dblclick.self="toggleMaximiseWindow">
        <h1>{{ t(currentTab) }}</h1>
        <div class="topbar-right">
          <span class="status-chip" :class="statusTone">
            <span class="dot" />{{ runtimeStatusLabel }}
            <span v-if="state.tunRunning" class="tag">TUN</span>
          </span>
          <div v-if="!isMac" class="win-btns">
            <button type="button" aria-label="Minimise" @click="minimiseWindow"><Minus :size="15" /></button>
            <button type="button" aria-label="Maximise" @click="toggleMaximiseWindow"><Square :size="12" /></button>
            <button class="close" type="button" aria-label="Close" @click="closeWindow"><X :size="16" /></button>
          </div>
        </div>
      </header>

      <div class="banners">
        <section v-if="state.needsNetRepair" class="banner" role="alert">
          <Wrench :size="17" aria-hidden="true" />
          <div class="banner-text">
            <strong>{{ t('repairTitle') }}</strong>
            <span>{{ t('repairHint') }}</span>
          </div>
          <button class="btn sm" :disabled="repairBusy" @click="repairNetwork">{{ t('repairNetwork') }}</button>
        </section>
        <section v-if="errorBanner" class="banner warn" role="alert">
          <CircleAlert :size="17" aria-hidden="true" />
          <div class="banner-text">
            <strong>{{ t('lastErrorTitle') }}</strong>
            <span>{{ errorBanner }}</span>
          </div>
          <button class="icon-btn" :title="t('close')" @click="dismissedError = String(state.lastError || '').trim()">
            <X :size="15" aria-hidden="true" />
          </button>
        </section>
      </div>

      <div ref="scrollRef" class="page-scroll">
        <KeepAlive>
          <component :is="panels[currentTab]" :key="currentTab" />
        </KeepAlive>
      </div>
    </div>

    <NodeEditorModal />
    <AdminPasswordModal />

    <Transition name="toast">
      <div v-if="notice" class="toast" :class="noticeType" role="status">{{ notice }}</div>
    </Transition>
  </div>
</template>
