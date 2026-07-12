<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { backendApi } from '../../api'
import type { LogEntry } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  logLevelFilter: string
  logSearch: string
  logDisplayLimit: number
  showTrafficLogs: boolean
  filteredLogs: LogEntry[]
  formatLogTimestamp: (ts: string) => string
  logLevelText: (level: string) => string
  logComponentText: (component: string) => string
}>()

const emit = defineEmits<{
  (e: 'update:logLevelFilter', value: string): void
  (e: 'update:logSearch', value: string): void
  (e: 'update:logDisplayLimit', value: number): void
  (e: 'update:showTrafficLogs', value: boolean): void
}>()

const logListRef = ref<HTMLElement | null>(null)
const lastScrollHeight = ref(0)
const hiddenIds = ref<Set<string>>(new Set())

const visibleLogs = computed(() => props.filteredLogs.filter((item) => !hiddenIds.value.has(item.id)))

const countByLevel = computed(() => {
  const out = { debug: 0, info: 0, warn: 0, error: 0 }
  for (const item of props.filteredLogs) {
    const key = item.level === 'error' || item.level === 'warn' || item.level === 'debug' ? item.level : 'info'
    out[key]++
  }
  return out
})

const recentEvents = computed(() => visibleLogs.value.slice(0, 5))

const exportVisibleLogs = async () => {
  await backendApi.exportLogs(visibleLogs.value)
}

const clearVisibleLogs = () => {
  hiddenIds.value = new Set([...hiddenIds.value, ...visibleLogs.value.map((item) => item.id)])
}

onMounted(() => {
  lastScrollHeight.value = logListRef.value?.scrollHeight || 0
})

watch(
  () => [visibleLogs.value[0]?.id || '', visibleLogs.value.length],
  async () => {
    const el = logListRef.value
    if (!el) return
    const beforeHeight = lastScrollHeight.value || el.scrollHeight
    const atTop = el.scrollTop <= 2
    await nextTick()
    const afterHeight = el.scrollHeight
    if (atTop) el.scrollTop = 0
    else {
      const grow = afterHeight - beforeHeight
      if (grow > 0) el.scrollTop += grow
    }
    lastScrollHeight.value = afterHeight
  }
)
</script>

<template>
  <main class="panel logs-page">
    <section class="logs-layout">
      <section class="logs-main">
        <div class="logs-toolbar">
          <label class="field compact">
            <span>{{ props.t('level') }}</span>
            <select :value="props.logLevelFilter" @change="emit('update:logLevelFilter', ($event.target as HTMLSelectElement).value)">
              <option value="all">{{ props.t('all') }}</option>
              <option value="debug">debug</option>
              <option value="info">info</option>
              <option value="warn">warn</option>
              <option value="error">error</option>
            </select>
          </label>
          <label class="field compact toolbar-wide">
            <span>{{ props.t('search') }}</span>
            <input :value="props.logSearch" :placeholder="props.t('logSearchPlaceholder')" @input="emit('update:logSearch', ($event.target as HTMLInputElement).value.trim())" />
          </label>
          <label class="field compact">
            <span>{{ props.t('renderCount') }}</span>
            <select :value="props.logDisplayLimit" @change="emit('update:logDisplayLimit', Number(($event.target as HTMLSelectElement).value))">
              <option :value="300">300</option>
              <option :value="600">600</option>
              <option :value="1000">1000</option>
              <option :value="2000">2000</option>
            </select>
          </label>
          <label class="field compact logs-traffic-field">
            <span>{{ props.t('showTrafficLogs') }}</span>
            <span class="logs-traffic-control">
              <span class="switch-control">
                <input type="checkbox" :checked="props.showTrafficLogs" @change="emit('update:showTrafficLogs', ($event.target as HTMLInputElement).checked)" />
                <span class="switch-ui" />
              </span>
            </span>
          </label>
          <button class="btn ghost toolbar-action" @click="exportVisibleLogs">{{ props.t('exportLogs') }}</button>
          <button class="btn danger toolbar-action" @click="clearVisibleLogs">{{ props.t('clearLogs') }}</button>
        </div>

        <div ref="logListRef" class="log-table" role="log" aria-live="polite">
          <div class="log-table-head">
            <span>{{ props.t('time') }}</span>
            <span>{{ props.t('level') }}</span>
            <span>{{ props.t('module') }}</span>
            <span>{{ props.t('content') }}</span>
          </div>
          <article v-for="item in visibleLogs" :key="item.id" class="log-item" :class="item.level">
            <time class="log-time">{{ props.formatLogTimestamp(item.timestamp) }}</time>
            <span class="log-level-pill" :class="item.level">{{ props.logLevelText(item.level) }}</span>
            <strong class="log-component">{{ props.logComponentText(item.component) }}</strong>
            <span class="log-message">{{ item.message }}</span>
          </article>
          <p v-if="visibleLogs.length === 0" class="empty-state">{{ props.t('none') }}</p>
        </div>
      </section>

      <aside class="logs-aside">
        <article class="deck-card">
          <div class="card-head">
            <h3>{{ props.t('runtimeSummary') }}</h3>
          </div>
          <div class="log-count-grid">
            <div><span>{{ props.t('errorCount') }}</span><strong class="bad">{{ countByLevel.error }}</strong></div>
            <div><span>{{ props.t('warnCount') }}</span><strong class="warn">{{ countByLevel.warn }}</strong></div>
            <div><span>{{ props.t('infoCount') }}</span><strong class="info">{{ countByLevel.info }}</strong></div>
            <div><span>{{ props.t('totalLogs') }}</span><strong>{{ props.filteredLogs.length }}</strong></div>
          </div>
        </article>

        <article class="deck-card">
          <div class="card-head">
            <h3>{{ props.t('recentEvents') }}</h3>
          </div>
          <div class="event-list">
            <div v-for="item in recentEvents" :key="item.id">
              <strong :class="item.level">{{ props.logLevelText(item.level) }}</strong>
              <span>{{ item.message }}</span>
              <time>{{ props.formatLogTimestamp(item.timestamp) }}</time>
            </div>
            <p v-if="recentEvents.length === 0" class="empty-state">{{ props.t('none') }}</p>
          </div>
        </article>
      </aside>
    </section>
  </main>
</template>
