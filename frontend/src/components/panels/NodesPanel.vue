<script setup lang="ts">
import { computed, ref } from 'vue'
import { ArrowDownAZ, ArrowDownWideNarrow, Copy, Gauge, Pencil, Plus, Scale, Search, Trash2, Zap } from 'lucide-vue-next'
import { useApp } from '../../composables/appContext'
import { BALANCE_NODE_ID } from '../../types'
import type { LatencyResult } from '../../types'

const {
  t,
  config,
  busy,
  sortedNodes,
  balanceAvailable,
  openCreateNode,
  openEditNode,
  removeNode,
  switchNode,
  probeNode,
  probeAll,
  autoBest,
  sortByName,
  sortByLatency,
  exportShortlink,
} = useApp()

const query = ref('')
const probing = ref<Set<string>>(new Set())
const probingAll = ref(false)

const filtered = computed(() => {
  const kw = query.value.trim().toLowerCase()
  if (!kw) return sortedNodes.value
  return sortedNodes.value.filter(({ node }) => `${node.name} ${node.serverAddress}`.toLowerCase().includes(kw))
})

const balanceActive = computed(() => config.activeNodeId === BALANCE_NODE_ID)

const latencyText = (l?: LatencyResult) => {
  if (!l) return t('test')
  if (!l.connectOk) return t('timeout')
  return `${l.latencyMs} ms`
}

const latencyTone = (l?: LatencyResult) => {
  if (!l) return ''
  if (!l.connectOk) return 'bad'
  if (l.latencyMs <= 150) return 'ok'
  if (l.latencyMs <= 350) return 'warn'
  return 'bad'
}

const probeOne = async (id: string) => {
  if (probing.value.has(id)) return
  probing.value = new Set([...probing.value, id])
  try {
    await probeNode(id)
  } finally {
    const next = new Set(probing.value)
    next.delete(id)
    probing.value = next
  }
}

const probeEvery = async () => {
  if (probingAll.value) return
  probingAll.value = true
  try {
    await probeAll()
  } finally {
    probingAll.value = false
  }
}

const select = (id: string, active: boolean) => {
  if (!active) void switchNode(id)
}
</script>

<template>
  <div class="page nodes">
    <div class="toolbar">
      <label class="input-group grow">
        <Search :size="15" aria-hidden="true" />
        <input v-model="query" type="search" :placeholder="t('nodeSearchPlaceholder')" />
      </label>
      <button class="icon-btn boxed" :disabled="probingAll || config.nodes.length === 0" :title="t('checkLatency')" @click="probeEvery">
        <Gauge :size="15" :class="{ spin: probingAll }" aria-hidden="true" />
      </button>
      <button class="icon-btn boxed" :disabled="busy || config.nodes.length < 2" :title="t('autoBestNode')" @click="autoBest">
        <Zap :size="15" aria-hidden="true" />
      </button>
      <button class="icon-btn boxed" :disabled="config.nodes.length < 2" :title="t('sortByName')" @click="sortByName">
        <ArrowDownAZ :size="15" aria-hidden="true" />
      </button>
      <button class="icon-btn boxed" :disabled="config.nodes.length < 2" :title="t('sortByLatency')" @click="sortByLatency">
        <ArrowDownWideNarrow :size="15" aria-hidden="true" />
      </button>
      <button class="btn primary" :title="t('add')" @click="openCreateNode"><Plus :size="15" aria-hidden="true" /><span class="label">{{ t('add') }}</span></button>
    </div>

    <section class="card">
      <div v-if="config.nodes.length === 0" class="empty nodes-empty">
        <strong>{{ t('noNodesYet') }}</strong>
        <span>{{ t('noNodesHint') }}</span>
      </div>
      <div v-else class="list">
        <div
          v-if="balanceAvailable && !query"
          class="list-item node-row"
          :class="{ active: balanceActive }"
          @click="select(BALANCE_NODE_ID, balanceActive)"
        >
          <span class="node-mark"><Scale :size="13" aria-hidden="true" /></span>
          <div class="list-main">
            <strong>{{ t('balanceNode') }}</strong>
            <small>{{ t('balanceHint') }}</small>
          </div>
        </div>
        <div
          v-for="item in filtered"
          :key="item.node.id"
          class="list-item node-row"
          :class="{ active: item.active }"
          @click="select(item.node.id, item.active)"
        >
          <span class="node-mark" />
          <div class="list-main">
            <strong>{{ item.node.name || item.node.serverAddress }}</strong>
            <small class="mono">{{ item.node.serverAddress }}</small>
          </div>
          <button
            class="badge latency"
            :class="latencyTone(item.latency)"
            :disabled="probing.has(item.node.id)"
            :title="item.latency?.error || t('probe')"
            @click.stop="probeOne(item.node.id)"
          >
            <span class="num">{{ probing.has(item.node.id) ? '…' : latencyText(item.latency) }}</span>
          </button>
          <div class="list-actions">
            <button class="icon-btn" :title="t('copyLink')" @click.stop="exportShortlink(item.node.id)"><Copy :size="15" aria-hidden="true" /></button>
            <button class="icon-btn" :title="t('edit')" @click.stop="openEditNode(item.node)"><Pencil :size="15" aria-hidden="true" /></button>
            <button class="icon-btn danger" :title="t('delete')" @click.stop="removeNode(item.node.id)"><Trash2 :size="15" aria-hidden="true" /></button>
          </div>
        </div>
        <p v-if="filtered.length === 0" class="empty">{{ t('noMatch') }}</p>
      </div>
    </section>
    <p class="hint">{{ t('pasteLinkHint') }}</p>
  </div>
</template>
