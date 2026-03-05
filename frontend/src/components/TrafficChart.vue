<script setup lang="ts">
import { computed, ref } from 'vue'
import type { BandwidthSample } from '../types'

const props = defineProps<{
  samples: BandwidthSample[]
}>()

const width = 700
const height = 216
const paddingX = 16
const paddingY = 18

const svgRef = ref<SVGSVGElement | null>(null)
const hoverIndex = ref<number | null>(null)

const rawTotalSeries = computed(() => props.samples.map((s) => (s.txBps || 0) + (s.rxBps || 0)))
const rawDirectSeries = computed(() => props.samples.map((s) => s.directBps || 0))
const rawProxySeries = computed(() => props.samples.map((s) => s.proxyBps || 0))

const movingAverage = (arr: number[], windowSize = 4): number[] => {
  if (arr.length <= 2) return [...arr]
  const out = new Array<number>(arr.length)
  for (let i = 0; i < arr.length; i++) {
    const start = Math.max(0, i-windowSize + 1)
    let sum = 0
    let count = 0
    for (let j = start; j <= i; j++) {
      sum += arr[j]
      count++
    }
    out[i] = count > 0 ? sum / count : arr[i]
  }
  return out
}

const totalSeries = computed(() => movingAverage(rawTotalSeries.value, 4))
const directSeries = computed(() => movingAverage(rawDirectSeries.value, 3))
const proxySeries = computed(() => movingAverage(rawProxySeries.value, 3))

const maxValue = computed(() => Math.max(...totalSeries.value, ...directSeries.value, ...proxySeries.value, 1))
const peakRaw = computed(() => Math.max(...rawTotalSeries.value, ...rawDirectSeries.value, ...rawProxySeries.value, 1))

const toPoints = (arr: number[], max: number): Array<{ x: number; y: number }> => {
  if (arr.length === 0) return []
  const spanX = width - paddingX * 2
  const spanY = height - paddingY * 2
  const step = arr.length > 1 ? spanX / (arr.length - 1) : 0
  const denom = Math.max(max, 1)
  return arr.map((v, i) => ({
    x: paddingX + i * step,
    y: height - paddingY - (v / denom) * spanY,
  }))
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

const totalPoints = computed(() => toPoints(totalSeries.value, maxValue.value))
const directPoints = computed(() => toPoints(directSeries.value, maxValue.value))
const proxyPoints = computed(() => toPoints(proxySeries.value, maxValue.value))

const totalPath = computed(() => smoothPath(totalPoints.value))
const directPath = computed(() => smoothPath(directPoints.value))
const proxyPath = computed(() => smoothPath(proxyPoints.value))

const totalFillPath = computed(() => {
  if (!totalPoints.value.length || !totalPath.value) return ''
  const first = totalPoints.value[0]
  const last = totalPoints.value[totalPoints.value.length - 1]
  const baselineY = height - paddingY
  return `${totalPath.value} L${last.x.toFixed(1)},${baselineY.toFixed(1)} L${first.x.toFixed(1)},${baselineY.toFixed(1)} Z`
})

const gridLines = computed(() => {
  const out: Array<{ y: number; label: string }> = []
  const count = 4
  for (let i = 0; i <= count; i++) {
    const ratio = i / count
    const y = height - paddingY - ratio * (height - paddingY * 2)
    out.push({ y, label: formatRate(maxValue.value * ratio) })
  }
  return out
})

const clampIndex = (idx: number): number => {
  if (props.samples.length === 0) return 0
  return Math.min(props.samples.length - 1, Math.max(0, idx))
}

const onPointerMove = (event: MouseEvent) => {
  if (!svgRef.value || props.samples.length === 0) {
    hoverIndex.value = null
    return
  }
  const rect = svgRef.value.getBoundingClientRect()
  if (!rect.width) return
  const ratio = (event.clientX - rect.left) / rect.width
  const clampedRatio = Math.min(1, Math.max(0, ratio))
  const idx = Math.round(clampedRatio * Math.max(0, props.samples.length - 1))
  hoverIndex.value = clampIndex(idx)
}

const onPointerLeave = () => {
  hoverIndex.value = null
}

const hoverSample = computed(() => {
  if (hoverIndex.value === null) return null
  return props.samples[hoverIndex.value] || null
})

const hoverTotalPoint = computed(() => {
  if (hoverIndex.value === null) return null
  return totalPoints.value[hoverIndex.value] || null
})

const hoverDirectPoint = computed(() => {
  if (hoverIndex.value === null) return null
  return directPoints.value[hoverIndex.value] || null
})

const hoverProxyPoint = computed(() => {
  if (hoverIndex.value === null) return null
  return proxyPoints.value[hoverIndex.value] || null
})

const formatRate = (value: number): string => {
  if (!value) return '0 B/s'
  const units = ['B/s', 'KiB/s', 'MiB/s', 'GiB/s']
  let v = value
  let idx = 0
  while (v >= 1024 && idx < units.length - 1) {
    v /= 1024
    idx++
  }
  return `${v.toFixed(v < 10 && idx > 0 ? 2 : 1)} ${units[idx]}`
}

const formatTime = (raw: string): string => {
  const d = new Date(raw)
  if (Number.isNaN(d.getTime())) return '--:--:--'
  return d.toLocaleTimeString([], { hour12: false })
}

const hoverTotalRate = computed(() => {
  const item = hoverSample.value
  if (!item) return '0 B/s'
  return formatRate((item.txBps || 0) + (item.rxBps || 0))
})

const hoverDirectRate = computed(() => {
  const item = hoverSample.value
  if (!item) return '0 B/s'
  return formatRate(item.directBps || 0)
})

const hoverProxyRate = computed(() => {
  const item = hoverSample.value
  if (!item) return '0 B/s'
  return formatRate(item.proxyBps || 0)
})
</script>

<template>
  <div class="chart-wrap">
    <svg
      ref="svgRef"
      :viewBox="`0 0 ${width} ${height}`"
      class="chart-svg"
      preserveAspectRatio="none"
      @mousemove="onPointerMove"
      @mouseleave="onPointerLeave"
    >
      <defs>
        <linearGradient id="traffic-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="color-mix(in srgb, var(--accent) 28%, transparent)" />
          <stop offset="100%" stop-color="color-mix(in srgb, var(--accent) 3%, transparent)" />
        </linearGradient>
      </defs>

      <rect x="0" y="0" :width="width" :height="height" class="chart-bg" />

      <g class="chart-grid">
        <line
          v-for="line in gridLines"
          :key="`grid-${line.y}`"
          :x1="paddingX"
          :x2="width - paddingX"
          :y1="line.y"
          :y2="line.y"
          class="grid-line"
        />
      </g>

      <path v-if="totalFillPath" :d="totalFillPath" class="area total" />
      <path :d="totalPath" class="line total" />
      <path :d="directPath" class="line direct" />
      <path :d="proxyPath" class="line proxy" />

      <line v-if="hoverTotalPoint" :x1="hoverTotalPoint.x" :x2="hoverTotalPoint.x" :y1="paddingY" :y2="height - paddingY" class="cursor" />
      <circle v-if="hoverTotalPoint" :cx="hoverTotalPoint.x" :cy="hoverTotalPoint.y" r="3" class="hover-dot total" />
      <circle v-if="hoverDirectPoint" :cx="hoverDirectPoint.x" :cy="hoverDirectPoint.y" r="3" class="hover-dot direct" />
      <circle v-if="hoverProxyPoint" :cx="hoverProxyPoint.x" :cy="hoverProxyPoint.y" r="3" class="hover-dot proxy" />
    </svg>

    <div class="chart-meta">
      <span>Peak: {{ formatRate(peakRaw) }}</span>
      <span>{{ samples.length }} samples</span>
    </div>

    <div class="chart-hover">
      <template v-if="hoverSample">
        <span>{{ formatTime(hoverSample.at) }}</span>
        <span>Total {{ hoverTotalRate }}</span>
        <span>Direct {{ hoverDirectRate }}</span>
        <span>Proxy {{ hoverProxyRate }}</span>
      </template>
      <span v-else>Move mouse on chart to inspect rate</span>
    </div>
  </div>
</template>

<style scoped>
.chart-wrap {
  width: 100%;
  min-height: 250px;
}

.chart-svg {
  width: 100%;
  height: 208px;
  border: 1px solid var(--line);
  border-radius: 12px;
}

.chart-bg {
  fill: color-mix(in srgb, var(--surface) 95%, var(--accent) 5%);
}

.grid-line {
  stroke: color-mix(in srgb, var(--line) 78%, transparent);
  stroke-width: 0.8;
  stroke-dasharray: 2.5 4;
}

.area.total {
  fill: url(#traffic-fill);
}

.line {
  fill: none;
  stroke-linecap: round;
  stroke-linejoin: round;
  animation: reveal 0.45s ease;
}

.line.total {
  stroke: color-mix(in srgb, var(--ink) 24%, var(--line) 76%);
  stroke-width: 1.8;
}

.line.direct {
  stroke: var(--ok);
  stroke-width: 2.3;
}

.line.proxy {
  stroke: var(--accent-strong);
  stroke-width: 2.3;
}

.cursor {
  stroke: color-mix(in srgb, var(--ink) 60%, transparent);
  stroke-width: 1;
  stroke-dasharray: 3 4;
}

.hover-dot {
  stroke: var(--surface);
  stroke-width: 1;
}

.hover-dot.total {
  fill: color-mix(in srgb, var(--ink) 45%, var(--surface) 55%);
}

.hover-dot.direct {
  fill: var(--ok);
}

.hover-dot.proxy {
  fill: var(--accent-strong);
}

.chart-meta,
.chart-hover {
  display: flex;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
  font-size: 12px;
  color: var(--muted);
  flex-wrap: wrap;
}

.chart-hover {
  margin-top: 4px;
  color: color-mix(in srgb, var(--ink) 74%, var(--muted) 26%);
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
