<script setup lang="ts">
import type { LogEntry } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  logLevelFilter: string
  logSearch: string
  logDisplayLimit: number
  filteredLogs: LogEntry[]
  formatLogTimestamp: (ts: string) => string
  logLevelText: (level: string) => string
  logComponentText: (component: string) => string
}>()

const emit = defineEmits<{
  (e: 'update:logLevelFilter', value: string): void
  (e: 'update:logSearch', value: string): void
  (e: 'update:logDisplayLimit', value: number): void
}>()
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
    </div>
    <div class="log-list" role="log" aria-live="polite">
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
