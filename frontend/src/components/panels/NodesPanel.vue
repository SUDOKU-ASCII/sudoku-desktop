<script setup lang="ts">
import { computed, ref } from 'vue'
import { Activity, Copy, Pencil, Search, Trash2 } from 'lucide-vue-next'
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
            <Search :size="17" aria-hidden="true" />
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
                <Activity :size="16" aria-hidden="true" />
              </button>
              <button class="icon-action" :title="props.t('copyLink')" @click.stop="props.exportShortlink(item.node.id)">
                <Copy :size="16" aria-hidden="true" />
              </button>
              <button class="icon-action" :title="props.t('edit')" @click.stop="props.openEditNode(item.node)">
                <Pencil :size="16" aria-hidden="true" />
              </button>
              <button class="icon-action danger" :title="props.t('delete')" @click.stop="props.removeNode(item.node.id)">
                <Trash2 :size="16" aria-hidden="true" />
              </button>
            </div>
          </article>
          <p v-if="filteredNodes.length === 0" class="empty-state">{{ props.t('none') }}</p>
        </section>
      </section>
    </section>
  </main>
</template>
