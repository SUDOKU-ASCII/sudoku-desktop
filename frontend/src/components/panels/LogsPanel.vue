<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
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

onMounted(() => {
  lastScrollHeight.value = logListRef.value?.scrollHeight || 0
})

watch(
  () => [props.filteredLogs[0]?.id || '', props.filteredLogs.length],
  async () => {
    const el = logListRef.value
    if (!el) return
    const beforeHeight = lastScrollHeight.value || el.scrollHeight
    const atTop = el.scrollTop <= 2
    await nextTick()
    const afterHeight = el.scrollHeight
    if (atTop) {
      el.scrollTop = 0
    } else {
      const grow = afterHeight - beforeHeight
      if (grow > 0) {
        el.scrollTop += grow
      }
    }
    lastScrollHeight.value = afterHeight
  }
)
</script>

<template>
  <main class="panel">
    <div class="row">
      <label class="field compact"><span>{{ props.t('level') }}</span>
        <select :value="props.logLevelFilter" @change="emit('update:logLevelFilter', ($event.target as HTMLSelectElement).value)">
          <option value="all">{{ props.t('all') }}</option>
          <option value="debug">debug</option>
          <option value="info">info</option>
          <option value="warn">warn</option>
          <option value="error">error</option>
        </select>
      </label>
      <label class="field compact" style="min-width: 240px"><span>{{ props.t('search') }}</span><input :value="props.logSearch" :placeholder="props.t('logSearchPlaceholder')" @input="emit('update:logSearch', ($event.target as HTMLInputElement).value.trim())" /></label>
      <label class="field compact"><span>{{ props.t('renderCount') }}</span>
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
    </div>
    <div ref="logListRef" class="log-list" role="log" aria-live="polite">
      <article v-for="item in props.filteredLogs" :key="item.id" class="log-item" :class="item.level">
        <time class="log-time">{{ props.formatLogTimestamp(item.timestamp) }}</time>
        <span class="log-level-pill" :class="item.level">{{ props.logLevelText(item.level) }}</span>
        <strong class="log-component">[{{ props.logComponentText(item.component) }}]</strong>
        <span class="log-message">{{ item.message }}</span>
      </article>
      <p v-if="props.filteredLogs.length === 0">{{ props.t('none') }}</p>
    </div>
  </main>
</template>
