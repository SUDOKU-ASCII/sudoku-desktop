<script setup lang="ts">
import { computed, ref } from 'vue'
import { Activity, Download, Eraser, Search } from 'lucide-vue-next'
import { backendApi } from '../../api'
import { useApp } from '../../composables/appContext'

const { t, logLevelFilter, logSearch, showTrafficLogs, filteredLogs, formatLogTimestamp, logComponentText, flash, localizeError } = useApp()

// "Clear" hides what is on screen; the backend keeps its ring buffer for export.
const clearedBefore = ref(0)
const visible = computed(() => {
  const cut = clearedBefore.value
  if (!cut) return filteredLogs.value
  return filteredLogs.value.filter((x) => new Date(x.timestamp).getTime() > cut)
})

const clear = () => {
  const newest = filteredLogs.value[0] ? new Date(filteredLogs.value[0].timestamp).getTime() : NaN
  clearedBefore.value = Number.isNaN(newest) ? Date.now() : newest
}

const exporting = ref(false)
const exportLogs = async () => {
  if (exporting.value) return
  exporting.value = true
  try {
    const path = await backendApi.exportLogs(visible.value)
    if (path) flash(t('logsExported'))
  } catch (e) {
    flash(localizeError(e, 'exportFailed'), 'error')
  } finally {
    exporting.value = false
  }
}

const levelTag = (level: string) => {
  switch ((level || '').toLowerCase()) {
    case 'error':
      return 'ERR'
    case 'warn':
      return 'WRN'
    case 'debug':
      return 'DBG'
    default:
      return 'INF'
  }
}
</script>

<template>
  <div class="page fill logs">
    <div class="toolbar">
      <select v-model="logLevelFilter" class="select level-select" :aria-label="t('level')">
        <option value="all">{{ t('all') }}</option>
        <option value="error">error</option>
        <option value="warn">warn</option>
        <option value="info">info</option>
        <option value="debug">debug</option>
      </select>
      <label class="input-group grow">
        <Search :size="15" aria-hidden="true" />
        <input v-model="logSearch" type="search" :placeholder="t('logSearchPlaceholder')" />
      </label>
      <button class="icon-btn boxed" :class="{ on: showTrafficLogs }" :title="t('showTrafficLogs')" :aria-pressed="showTrafficLogs" @click="showTrafficLogs = !showTrafficLogs">
        <Activity :size="15" aria-hidden="true" />
      </button>
      <button class="icon-btn boxed" :disabled="exporting || visible.length === 0" :title="t('exportLogs')" @click="exportLogs">
        <Download :size="15" aria-hidden="true" />
      </button>
      <button class="icon-btn boxed" :disabled="visible.length === 0" :title="t('clearLogs')" @click="clear">
        <Eraser :size="15" aria-hidden="true" />
      </button>
    </div>

    <section class="card log-card" role="log">
      <div v-for="item in visible" :key="item.id" class="log-line" :class="item.level">
        <time class="num">{{ formatLogTimestamp(item.timestamp) }}</time>
        <span class="log-level">{{ levelTag(item.level) }}</span>
        <span class="log-msg selectable"><b>{{ logComponentText(item.component) }}</b>{{ item.message }}</span>
      </div>
      <p v-if="visible.length === 0" class="empty">{{ t('none') }}</p>
    </section>
  </div>
</template>
