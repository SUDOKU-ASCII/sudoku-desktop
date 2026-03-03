<script setup lang="ts">
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
</script>

<template>
  <main class="panel">
    <div class="node-toolbar">
      <button class="btn" @click="props.openCreateNode">{{ props.t('addNode') }}</button>
      <button class="btn ghost" @click="props.sortByName">{{ props.t('sortByName') }}</button>
      <button class="btn ghost" @click="props.sortByLatency">{{ props.t('sortByLatency') }}</button>
      <button class="btn ghost" @click="props.probeAll">{{ props.t('checkLatency') }}</button>
      <button class="btn ghost" @click="props.autoBest">{{ props.t('autoBestNode') }}</button>
    </div>

    <section class="node-list-grid">
      <article
        v-for="item in props.sortedNodes"
        :key="item.node.id"
        class="node-card"
        :class="{ active: item.active, editing: item.editing }"
        @click="props.pickNode(item.node)"
      >
        <div class="node-head">
          <div>
            <h4>{{ item.node.name || item.node.serverAddress }}</h4>
            <p>{{ item.node.serverAddress }}</p>
          </div>
          <span class="pill" :class="item.active ? 'ok' : 'off'">{{ item.active ? 'ACTIVE' : 'IDLE' }}</span>
        </div>
        <div class="node-meta">
          <small>AEAD: {{ item.node.aead }}</small>
          <small>ASCII: {{ item.node.ascii }}</small>
          <small v-if="item.latency">{{ item.latency.connectOk ? `${item.latency.latencyMs} ms` : item.latency.error || 'failed' }}</small>
          <small v-else>Latency: -</small>
        </div>
        <div class="node-actions">
          <button class="icon-action" title="Probe" @click.stop="props.probeNode(item.node.id)">
            <svg viewBox="0 0 24 24"><path d="M4 12h6l2-5 3 10 2-5h3" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round"/></svg>
          </button>
          <button class="icon-action" title="Copy link" @click.stop="props.exportShortlink(item.node.id)">
            <svg viewBox="0 0 24 24"><path d="M9 8h10v12H9z" fill="none" stroke="currentColor" stroke-width="1.9"/><path d="M5 4h10v12" fill="none" stroke="currentColor" stroke-width="1.9"/></svg>
          </button>
          <button class="icon-action" title="Edit" @click.stop="props.openEditNode(item.node)">
            <svg viewBox="0 0 24 24"><path d="M4 20h4l10-10-4-4L4 16v4z" fill="none" stroke="currentColor" stroke-width="1.9"/><path d="M12 6l4 4" fill="none" stroke="currentColor" stroke-width="1.9"/></svg>
          </button>
          <button class="icon-action danger" title="Delete" @click.stop="props.removeNode(item.node.id)">
            <svg viewBox="0 0 24 24"><path d="M5 7h14" fill="none" stroke="currentColor" stroke-width="1.9"/><path d="M9 7V5h6v2" fill="none" stroke="currentColor" stroke-width="1.9"/><path d="M8 7l1 12h6l1-12" fill="none" stroke="currentColor" stroke-width="1.9"/></svg>
          </button>
          <button class="btn mini" @click.stop="props.switchNode(item.node.id)">Use</button>
        </div>
      </article>
      <p v-if="props.sortedNodes.length === 0">{{ props.t('none') }}</p>
    </section>
  </main>
</template>
