<script setup lang="ts">
import { computed } from 'vue'
import type { UsageDay } from '../types'

const props = defineProps<{
  days: UsageDay[]
}>()

const width = 680
const height = 170
const paddingX = 18
const paddingY = 16

const recent = computed(() => {
  const all = [...(props.days || [])].sort((a, b) => a.date.localeCompare(b.date))
  return all.slice(Math.max(0, all.length - 30))
})

const totals = computed(() => recent.value.map((d) => (d.tx || 0) + (d.rx || 0)))

const maxTotal = computed(() => {
  return Math.max(...totals.value, 1)
})

const toPoints = computed(() => {
  if (recent.value.length === 0) return [] as Array<{ x: number; y: number; label: string; total: number }>
  const step = recent.value.length > 1 ? (width - paddingX * 2) / (recent.value.length - 1) : 0
  const spanY = height - paddingY * 2
  return recent.value.map((d, i) => {
    const total = (d.tx || 0) + (d.rx || 0)
    const x = paddingX + i * step
    const y = height - paddingY - (total / maxTotal.value) * spanY
    const label = (() => {
      const dt = new Date(d.date)
      if (!Number.isNaN(dt.getTime())) return `${dt.getDate()}`
      const parts = d.date.split('-')
      return parts[parts.length - 1] || d.date
    })()
    return { x, y, label, total }
  })
})

const smoothPath = (points: Array<{ x: number; y: number }>): string => {
  if (points.length === 0) return ''
  if (points.length === 1) return `M${points[0].x.toFixed(2)},${points[0].y.toFixed(2)}`

  let d = `M${points[0].x.toFixed(2)},${points[0].y.toFixed(2)}`
  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[i - 1] ?? points[i]
    const p1 = points[i]
    const p2 = points[i + 1]
    const p3 = points[i + 2] ?? p2

    const cp1x = p1.x + (p2.x - p0.x) / 6
    const cp1y = p1.y + (p2.y - p0.y) / 6
    const cp2x = p2.x - (p3.x - p1.x) / 6
    const cp2y = p2.y - (p3.y - p1.y) / 6
    d += ` C${cp1x.toFixed(2)},${cp1y.toFixed(2)} ${cp2x.toFixed(2)},${cp2y.toFixed(2)} ${p2.x.toFixed(2)},${p2.y.toFixed(2)}`
  }
  return d
}

const linePath = computed(() => smoothPath(toPoints.value))

const fillPath = computed(() => {
  if (toPoints.value.length === 0) return ''
  const start = toPoints.value[0]
  const end = toPoints.value[toPoints.value.length - 1]
  return `${linePath.value} L${end.x.toFixed(2)},${(height - paddingY).toFixed(2)} L${start.x.toFixed(2)},${(height - paddingY).toFixed(2)} Z`
})

const gridLines = computed(() => {
  const count = 4
  const out: Array<{ y: number; value: number }> = []
  for (let i = 0; i <= count; i++) {
    const ratio = i / count
    const y = height - paddingY - ratio * (height - paddingY * 2)
    const value = Math.round(maxTotal.value * ratio)
    out.push({ y, value })
  }
  return out
})

const labelStep = computed(() => {
  if (toPoints.value.length <= 10) return 1
  return Math.ceil(toPoints.value.length / 10)
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

const totalInWindow = computed(() => totals.value.reduce((acc, n) => acc + n, 0))
</script>

<template>
  <div class="usage-wrap">
    <svg :viewBox="`0 0 ${width} ${height}`" class="usage-svg" preserveAspectRatio="none">
      <rect x="0" y="0" :width="width" :height="height" class="usage-bg" />

      <g class="grid">
        <line
          v-for="line in gridLines"
          :key="`y-${line.y}`"
          :x1="paddingX"
          :x2="width - paddingX"
          :y1="line.y"
          :y2="line.y"
          class="grid-line"
        />
      </g>

      <path v-if="fillPath" :d="fillPath" class="usage-fill" />
      <path v-if="linePath" :d="linePath" class="usage-line" pathLength="100" />

      <circle
        v-for="(p, idx) in toPoints"
        :key="`dot-${idx}`"
        :cx="p.x"
        :cy="p.y"
        r="2.8"
        class="usage-dot"
      />

      <line :x1="paddingX" :x2="width - paddingX" :y1="height - paddingY" :y2="height - paddingY" class="axis" />

      <text
        v-for="(p, idx) in toPoints"
        v-show="idx % labelStep === 0 || idx === toPoints.length - 1"
        :key="`label-${idx}`"
        :x="p.x"
        :y="height - 3"
        class="axis-label"
      >
        {{ p.label }}
      </text>
    </svg>

    <div class="usage-meta">
      <span>30d total: {{ humanBytes(totalInWindow) }}</span>
      <span v-if="recent.length">{{ recent[0].date }} - {{ recent[recent.length - 1].date }}</span>
      <span v-else>-</span>
    </div>
  </div>
</template>

<style scoped>
.usage-wrap {
  width: 100%;
  min-height: 186px;
}

.usage-svg {
  width: 100%;
  height: 165px;
  border: 1px solid var(--line);
  border-radius: 12px;
}

.usage-bg {
  fill: color-mix(in srgb, var(--surface) 94%, var(--accent) 6%);
}

.grid-line {
  stroke: color-mix(in srgb, var(--line) 80%, transparent);
  stroke-width: 0.8;
  stroke-dasharray: 2.5 4;
}

.usage-fill {
  fill: color-mix(in srgb, var(--accent) 18%, transparent);
}

.usage-line {
  fill: none;
  stroke: var(--accent-strong);
  stroke-width: 2.6;
  stroke-linecap: round;
  stroke-linejoin: round;
  stroke-dasharray: 100;
  stroke-dashoffset: 100;
  animation: drawLine 1s ease forwards;
}

.usage-dot {
  fill: color-mix(in srgb, var(--surface) 78%, var(--accent-strong) 22%);
  stroke: var(--accent-strong);
  stroke-width: 0.8;
  opacity: 0;
  animation: dotIn 0.35s ease forwards;
  animation-delay: 0.45s;
}

.axis {
  stroke: var(--line);
  stroke-width: 1;
}

.axis-label {
  text-anchor: middle;
  font-size: 10px;
  fill: var(--muted);
}

.usage-meta {
  display: flex;
  justify-content: space-between;
  margin-top: 8px;
  font-size: 12px;
  color: var(--muted);
}

@keyframes drawLine {
  to {
    stroke-dashoffset: 0;
  }
}

@keyframes dotIn {
  from {
    opacity: 0;
    transform: scale(0.5);
  }
  to {
    opacity: 1;
    transform: scale(1);
  }
}
</style>
