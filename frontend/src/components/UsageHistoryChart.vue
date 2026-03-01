<script setup lang="ts">
import { computed } from 'vue'
import type { UsageDay } from '../types'

const props = defineProps<{
  days: UsageDay[]
}>()

const width = 680
const height = 220
const padding = 16
const gap = 6

const recent = computed(() => {
  const all = [...(props.days || [])].sort((a, b) => a.date.localeCompare(b.date))
  return all.slice(Math.max(0, all.length - 14))
})

const maxTotal = computed(() => {
  const totals = recent.value.map((d) => (d.tx || 0) + (d.rx || 0))
  return Math.max(...totals, 1)
})

const bars = computed(() => {
  const count = Math.max(recent.value.length, 1)
  const usableW = width - padding * 2
  const barW = Math.max(10, Math.floor((usableW - gap * (count - 1)) / count))
  const scaleH = height - padding * 2
  return recent.value.map((d, i) => {
    const total = (d.tx || 0) + (d.rx || 0)
    const direct = (d.directTx || 0) + (d.directRx || 0)
    const proxy = (d.proxyTx || 0) + (d.proxyRx || 0)
    const totalH = (total / maxTotal.value) * scaleH
    const directH = (direct / maxTotal.value) * scaleH
    const proxyH = Math.max(0, totalH - directH)
    const x = padding + i * (barW + gap)
    const y = height - padding - totalH
    return { d, x, y, barW, totalH, directH, proxyH, total, direct, proxy }
  })
})

const humanBytes = (value: number): string => {
  if (!value) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let v = value
  let idx = 0
  while (v >= 1024 && idx < units.length - 1) {
    v /= 1024
    idx++
  }
  return `${v.toFixed(v < 10 && idx > 0 ? 2 : 1)} ${units[idx]}`
}

const totalInWindow = computed(() => {
  return recent.value.reduce((acc, d) => acc + (d.tx || 0) + (d.rx || 0), 0)
})
</script>

<template>
  <div class="wrap">
    <svg :viewBox="`0 0 ${width} ${height}`" class="svg" preserveAspectRatio="none">
      <rect x="0" y="0" :width="width" :height="height" class="bg" />

      <g v-for="b in bars" :key="b.d.date">
        <rect
          :x="b.x"
          :y="height - padding - b.directH"
          :width="b.barW"
          :height="b.directH"
          class="bar direct"
        />
        <rect
          :x="b.x"
          :y="height - padding - b.directH - b.proxyH"
          :width="b.barW"
          :height="b.proxyH"
          class="bar proxy"
        />
      </g>

      <line :x1="padding" :x2="width - padding" :y1="height - padding" :y2="height - padding" class="axis" />
    </svg>

    <div class="meta">
      <span>14d total: {{ humanBytes(totalInWindow) }}</span>
      <span v-if="recent.length">{{ recent[recent.length - 1].date }}</span>
      <span v-else>-</span>
    </div>
  </div>
</template>

<style scoped>
.wrap {
  width: 100%;
  min-height: 250px;
}

.svg {
  width: 100%;
  height: 220px;
  border: 3px solid var(--ink);
  border-radius: 14px;
}

.bg {
  fill: var(--paper-soft);
}

.axis {
  stroke: var(--ink);
  stroke-width: 2;
  opacity: 0.4;
}

.bar {
  shape-rendering: geometricPrecision;
}

.bar.direct {
  fill: var(--ok);
  opacity: 0.9;
}

.bar.proxy {
  fill: var(--accent-c);
  opacity: 0.85;
}

.meta {
  display: flex;
  justify-content: space-between;
  margin-top: 8px;
  font-size: 12px;
}
</style>

