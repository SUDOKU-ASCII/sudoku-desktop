<script setup lang="ts">
import { computed } from 'vue'
import type { BandwidthSample } from '../types'

const props = defineProps<{
  samples: BandwidthSample[]
}>()

const width = 680
const height = 200
const padding = 16

const normalize = (arr: number[], max: number, w: number, h: number, p: number): string => {
  if (arr.length === 0) return ''
  const denom = Math.max(max, 1)
  const step = arr.length > 1 ? (w - p * 2) / (arr.length - 1) : 0
  return arr
    .map((v, i) => {
      const x = p + i * step
      const y = h - p - (v / denom) * (h - p * 2)
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}

const totalSeries = computed(() => props.samples.map((s) => (s.txBps || 0) + (s.rxBps || 0)))
const directSeries = computed(() => props.samples.map((s) => s.directBps || 0))
const proxySeries = computed(() => props.samples.map((s) => s.proxyBps || 0))

const maxValue = computed(() => Math.max(...totalSeries.value, ...directSeries.value, ...proxySeries.value, 1))
const totalPath = computed(() => normalize(totalSeries.value, maxValue.value, width, height, padding))
const directPath = computed(() => normalize(directSeries.value, maxValue.value, width, height, padding))
const proxyPath = computed(() => normalize(proxySeries.value, maxValue.value, width, height, padding))
</script>

<template>
  <div class="chart-wrap">
    <svg :viewBox="`0 0 ${width} ${height}`" class="chart-svg" preserveAspectRatio="none">
      <defs>
        <linearGradient id="totalGradient" x1="0" y1="0" x2="1" y2="0">
          <stop offset="0%" stop-color="var(--accent-b)"/>
          <stop offset="100%" stop-color="var(--accent-a)"/>
        </linearGradient>
      </defs>
      <rect x="0" y="0" :width="width" :height="height" class="chart-bg"/>
      <path :d="totalPath" class="line total" />
      <path :d="directPath" class="line direct" />
      <path :d="proxyPath" class="line proxy" />
    </svg>
    <div class="chart-meta">
      <span>Peak: {{ (maxValue / 1024).toFixed(1) }} KiB/s</span>
      <span>{{ samples.length }} samples</span>
    </div>
  </div>
</template>

<style scoped>
.chart-wrap {
  width: 100%;
  min-height: 240px;
}

.chart-svg {
  width: 100%;
  height: 210px;
  border: 3px solid var(--ink);
  border-radius: 14px;
}

.chart-bg {
  fill: var(--paper-soft);
}

.line {
  fill: none;
  stroke-width: 3.2;
  stroke-linecap: round;
  stroke-linejoin: round;
  animation: reveal 0.55s ease;
}

.line.total {
  stroke: url(#totalGradient);
  opacity: 0.55;
  stroke-dasharray: 7 7;
}

.line.direct {
  stroke: var(--ok);
}

.line.proxy {
  stroke: var(--accent-c);
}

.chart-meta {
  display: flex;
  justify-content: space-between;
  margin-top: 8px;
  font-size: 12px;
}

@keyframes reveal {
  from {
    opacity: 0.2;
    transform: translateY(6px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>
