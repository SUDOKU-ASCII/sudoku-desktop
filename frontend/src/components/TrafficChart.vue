<script setup lang="ts">
import { computed } from 'vue'
import type { BandwidthSample } from '../types'

const props = defineProps<{
  samples: BandwidthSample[]
}>()

const width = 680
const height = 200
const padding = 16

const toPoints = (arr: number[], max: number, w: number, h: number, p: number): Array<{ x: number; y: number }> => {
  if (arr.length === 0) return []
  const denom = Math.max(max, 1)
  const step = arr.length > 1 ? (w - p * 2) / (arr.length - 1) : 0
  return arr.map((v, i) => {
    const x = p + i * step
    const y = h - p - (v / denom) * (h - p * 2)
    return { x, y }
  })
}

const smoothPath = (points: Array<{ x: number; y: number }>): string => {
  if (points.length === 0) return ''
  if (points.length === 1) return `M${points[0].x.toFixed(1)},${points[0].y.toFixed(1)}`

  let d = `M${points[0].x.toFixed(1)},${points[0].y.toFixed(1)}`
  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[i - 1] ?? points[i]
    const p1 = points[i]
    const p2 = points[i + 1]
    const p3 = points[i + 2] ?? p2

    const cp1x = p1.x + (p2.x - p0.x) / 6
    const cp1y = p1.y + (p2.y - p0.y) / 6
    const cp2x = p2.x - (p3.x - p1.x) / 6
    const cp2y = p2.y - (p3.y - p1.y) / 6

    d += ` C${cp1x.toFixed(1)},${cp1y.toFixed(1)} ${cp2x.toFixed(1)},${cp2y.toFixed(1)} ${p2.x.toFixed(1)},${p2.y.toFixed(1)}`
  }
  return d
}

const totalSeries = computed(() => props.samples.map((s) => (s.txBps || 0) + (s.rxBps || 0)))
const directSeries = computed(() => props.samples.map((s) => s.directBps || 0))
const proxySeries = computed(() => props.samples.map((s) => s.proxyBps || 0))

const maxValue = computed(() => Math.max(...totalSeries.value, ...directSeries.value, ...proxySeries.value, 1))
const totalPath = computed(() => smoothPath(toPoints(totalSeries.value, maxValue.value, width, height, padding)))
const directPath = computed(() => smoothPath(toPoints(directSeries.value, maxValue.value, width, height, padding)))
const proxyPath = computed(() => smoothPath(toPoints(proxySeries.value, maxValue.value, width, height, padding)))
</script>

<template>
  <div class="chart-wrap">
    <svg :viewBox="`0 0 ${width} ${height}`" class="chart-svg" preserveAspectRatio="none">
      <rect x="0" y="0" :width="width" :height="height" class="chart-bg" />
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
  min-height: 220px;
}

.chart-svg {
  width: 100%;
  height: 196px;
  border: 1px solid var(--line);
  border-radius: 12px;
}

.chart-bg {
  fill: color-mix(in srgb, var(--surface) 93%, var(--accent) 7%);
}

.line {
  fill: none;
  stroke-width: 2.4;
  stroke-linecap: round;
  stroke-linejoin: round;
  animation: reveal 0.45s ease;
}

.line.total {
  stroke: color-mix(in srgb, var(--ink) 32%, var(--line) 68%);
  opacity: 0.7;
}

.line.direct {
  stroke: var(--ok);
}

.line.proxy {
  stroke: var(--accent-strong);
}

.chart-meta {
  display: flex;
  justify-content: space-between;
  margin-top: 8px;
  font-size: 12px;
  color: var(--muted);
}

@keyframes reveal {
  from {
    opacity: 0.2;
    transform: translateY(4px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}
</style>
