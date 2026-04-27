<script setup lang="ts">
import { computed, ref } from 'vue'
import type { LatencyResult, NodeConfig } from '../../types'

type NodeView = {
  node: NodeConfig
  latency?: LatencyResult
  active: boolean
  editing: boolean
}

const props = defineProps<{
  t: (key: any) => string
  sortedNodes: NodeView[]
  openCreateNode: () => void
  sortByName: () => void
  sortByLatency: () => void
  probeAll: () => void
  autoBest: () => void
  pickNode: (node: NodeConfig) => void
  probeNode: (id: string) => void
  exportShortlink: (id: string) => void
  openEditNode: (node: NodeConfig) => void
  removeNode: (id: string) => void
  switchNode: (id: string) => void
}>()

const query = ref('')

const filteredNodes = computed(() => {
  const kw = query.value.trim().toLowerCase()
  if (!kw) return props.sortedNodes
  return props.sortedNodes.filter(({ node }) => `${node.name} ${node.serverAddress} ${node.httpMask?.mode || ''}`.toLowerCase().includes(kw))
})

const activeNode = computed(() => props.sortedNodes.find((item) => item.active))

const latencyText = (item: NodeView) => {
  if (!item.latency) return '-'
  if (!item.latency.connectOk) return item.latency.error || props.t('failed')
  return `${item.latency.latencyMs} ms`
}

const latencyTone = (item: NodeView) => {
  const n = item.latency?.latencyMs ?? -1
  if (!item.latency || !item.latency.connectOk) return 'bad'
  if (n <= 80) return 'ok'
  if (n <= 220) return 'warn'
  return 'bad'
}

const httpMaskText = (node: NodeConfig) => {
  if (node.httpMask?.disable) return props.t('notEnabled')
  return node.httpMask?.mode?.trim() || 'auto'
}

const selectNode = (item: NodeView) => {
  if (!item.node.enabled) return
  props.pickNode(item.node)
  if (!item.active) props.switchNode(item.node.id)
}
</script>

<template>
  <main class="panel nodes-page">
    <section class="nodes-layout">
      <aside class="nodes-aside">
        <article class="deck-card node-summary-card">
          <span class="eyebrow">{{ props.t('activeNode') }}</span>
          <h3>{{ activeNode?.node.name || activeNode?.node.serverAddress || '-' }}</h3>
          <p>{{ activeNode?.node.serverAddress || props.t('none') }}</p>
          <dl>
            <div><dt>{{ props.t('enabled') }}</dt><dd>{{ activeNode?.node.enabled ? props.t('enabled') : props.t('disabled') }}</dd></div>
            <div><dt>{{ props.t('latency') }}</dt><dd>{{ activeNode ? latencyText(activeNode) : '-' }}</dd></div>
            <div><dt>{{ props.t('localPort') }}</dt><dd>{{ activeNode?.node.localPort || '-' }}</dd></div>
          </dl>
        </article>

        <article class="deck-card node-tools-card">
          <button class="btn primary-wide" @click="props.openCreateNode">{{ props.t('addNode') }}</button>
          <button class="btn ghost" @click="props.probeAll">{{ props.t('checkLatency') }}</button>
          <button class="btn ghost" @click="props.autoBest">{{ props.t('autoBestNode') }}</button>
        </article>
      </aside>

      <section class="nodes-main">
        <div class="node-toolbar pro-toolbar">
          <label class="toolbar-search">
            <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M10.8 18.2a7.4 7.4 0 1 1 0-14.8 7.4 7.4 0 0 1 0 14.8Z" fill="none" stroke="currentColor" stroke-width="1.8"/><path d="m16.2 16.2 4.2 4.2" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"/></svg>
            <input v-model="query" type="search" :placeholder="props.t('nodeSearchPlaceholder')" />
          </label>
          <button class="btn ghost" @click="props.sortByName">{{ props.t('sortByName') }}</button>
          <button class="btn ghost" @click="props.sortByLatency">{{ props.t('sortByLatency') }}</button>
        </div>

        <section class="node-list-grid">
          <article
            v-for="item in filteredNodes"
            :key="item.node.id"
            class="node-card"
            :class="{ active: item.active, editing: item.editing, disabled: !item.node.enabled }"
            @click="selectNode(item)"
          >
            <div class="node-head">
              <div>
                <h4>{{ item.node.name || item.node.serverAddress }}</h4>
                <p>{{ item.node.serverAddress }}</p>
              </div>
              <span class="node-state" :class="item.active ? 'ok' : item.node.enabled ? 'idle' : 'off'">
                {{ item.active ? props.t('nodeStateActive') : item.node.enabled ? props.t('nodeStateIdle') : props.t('disabled') }}
              </span>
            </div>

            <div class="node-kpis node-kpis-compact">
              <div>
                <span>{{ props.t('latency') }}</span>
                <strong :class="latencyTone(item)">{{ latencyText(item) }}</strong>
              </div>
              <div>
                <span>HTTPMask</span>
                <strong>{{ httpMaskText(item.node) }}</strong>
              </div>
            </div>

            <div class="node-actions">
              <button class="icon-action" :title="props.t('probe')" @click.stop="props.probeNode(item.node.id)">
                <svg viewBox="0 0 24 24"><path d="M4 12h6l2-5 3 10 2-5h3" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"/></svg>
              </button>
              <button class="icon-action" :title="props.t('copyLink')" @click.stop="props.exportShortlink(item.node.id)">
                <svg viewBox="0 0 24 24"><path d="M9 8h10v12H9z" fill="none" stroke="currentColor" stroke-width="1.9"/><path d="M5 4h10v12" fill="none" stroke="currentColor" stroke-width="1.9"/></svg>
              </button>
              <button class="icon-action" :title="props.t('edit')" @click.stop="props.openEditNode(item.node)">
                <svg viewBox="0 0 24 24"><path d="M4 20h4l10-10-4-4L4 16v4z" fill="none" stroke="currentColor" stroke-width="1.9"/><path d="M12 6l4 4" fill="none" stroke="currentColor" stroke-width="1.9"/></svg>
              </button>
              <button class="icon-action danger" :title="props.t('delete')" @click.stop="props.removeNode(item.node.id)">
                <svg viewBox="0 0 24 24"><path d="M5 7h14" fill="none" stroke="currentColor" stroke-width="1.9"/><path d="M9 7V5h6v2" fill="none" stroke="currentColor" stroke-width="1.9"/><path d="M8 7l1 12h6l1-12" fill="none" stroke="currentColor" stroke-width="1.9"/></svg>
              </button>
            </div>
          </article>
          <p v-if="filteredNodes.length === 0" class="empty-state">{{ props.t('none') }}</p>
        </section>
      </section>
    </section>
  </main>
</template>
