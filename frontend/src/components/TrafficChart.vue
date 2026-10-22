<script setup lang="ts">
import { computed, ref } from 'vue'
import type { BandwidthSample } from '../types'
import { useI18n } from '../i18n'

const props = withDefaults(defineProps<{ samples: BandwidthSample[]; window?: number }>(), { window: 60 })

const { t } = useI18n()

const W = 600
const H = 120
const PAD_TOP = 8

const rate = (bps: number) => {
  if (!bps || bps < 1) return '0 B/s'
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s']
  let v = bps
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`
}

// Round the scale up to a 1/2/5 step so the axis label reads cleanly.
const niceMax = (v: number) => {
  if (v <= 1024) return 1024
  const exp = Math.pow(10, Math.floor(Math.log10(v)))
  for (const m of [1, 2, 5, 10]) if (v <= m * exp) return m * exp
  return 10 * exp
}

const points = computed(() => props.samples.slice(-props.window))
const maxValue = computed(() => niceMax(Math.max(0, ...points.value.map((s) => Math.max(s.txBps, s.rxBps)))))

const xAt = (i: number) => {
  const offset = props.window - points.value.length
  return ((offset + i) / (props.window - 1)) * W
}
const yAt = (v: number) => H - (Math.max(0, v) / maxValue.value) * (H - PAD_TOP)

// Monotone cubic interpolation (Fritsch–Carlson): smooth without overshooting below zero.
const monotonePath = (xs: number[], ys: number[]) => {
  const n = xs.length
  if (n === 0) return ''
  if (n === 1) return `M${xs[0]},${ys[0]}`
  const dx: number[] = []
  const m: number[] = []
  for (let i = 0; i < n - 1; i++) {
    dx.push(xs[i + 1] - xs[i])
    m.push((ys[i + 1] - ys[i]) / dx[i])
  }
  const tan: number[] = [m[0]]
  for (let i = 1; i < n - 1; i++) {
    tan.push(m[i - 1] * m[i] <= 0 ? 0 : (3 * (dx[i - 1] + dx[i])) / ((2 * dx[i] + dx[i - 1]) / m[i - 1] + (dx[i] + 2 * dx[i - 1]) / m[i]))
  }
  tan.push(m[n - 2])
  let d = `M${xs[0].toFixed(1)},${ys[0].toFixed(1)}`
  for (let i = 0; i < n - 1; i++) {
    const h = dx[i] / 3
    d += `C${(xs[i] + h).toFixed(1)},${(ys[i] + h * tan[i]).toFixed(1)} ${(xs[i + 1] - h).toFixed(1)},${(ys[i + 1] - h * tan[i + 1]).toFixed(1)} ${xs[i + 1].toFixed(1)},${ys[i + 1].toFixed(1)}`
  }
  return d
}

const series = (pick: (s: BandwidthSample) => number) => {
  const xs = points.value.map((_, i) => xAt(i))
  const ys = points.value.map((s) => yAt(pick(s)))
  return { xs, line: monotonePath(xs, ys) }
}

const up = computed(() => series((s) => s.txBps).line)
const down = computed(() => series((s) => s.rxBps))
const downArea = computed(() => {
  const { xs, line } = down.value
  if (xs.length < 2) return ''
  return `${line}L${xs[xs.length - 1].toFixed(1)},${H}L${xs[0].toFixed(1)},${H}Z`
})

const latest = computed(() => points.value[points.value.length - 1])

const hover = ref<number | null>(null)
const onMove = (event: MouseEvent) => {
  const el = event.currentTarget as HTMLElement
  const rect = el.getBoundingClientRect()
  const slot = Math.round(((event.clientX - rect.left) / rect.width) * (props.window - 1))
  const idx = slot - (props.window - points.value.length)
  hover.value = idx >= 0 && idx < points.value.length ? idx : null
}
const hovered = computed(() => (hover.value == null ? null : points.value[hover.value]))
const hoverLeft = computed(() => (hover.value == null ? 0 : (xAt(hover.value) / W) * 100))
</script>

<template>
  <div class="tchart">
    <div class="tchart-legend">
      <span class="up"><i />{{ t('upload') }} <b class="num">{{ rate(latest?.txBps || 0) }}</b></span>
      <span class="down"><i />{{ t('download') }} <b class="num">{{ rate(latest?.rxBps || 0) }}</b></span>
      <span class="tchart-scale num">{{ rate(maxValue) }}</span>
    </div>
    <div class="tchart-plot" @mousemove="onMove" @mouseleave="hover = null">
      <svg :viewBox="`0 0 ${W} ${H}`" preserveAspectRatio="none" aria-hidden="true">
        <line v-for="g in 3" :key="g" class="grid" x1="0" :x2="W" :y1="PAD_TOP + ((H - PAD_TOP) / 4) * g" :y2="PAD_TOP + ((H - PAD_TOP) / 4) * g" />
        <path v-if="downArea" class="area" :d="downArea" />
        <path class="line down" :d="down.line" />
        <path class="line up" :d="up" />
      </svg>
      <div v-if="hovered" class="tchart-cursor" :style="{ left: `${hoverLeft}%` }">
        <div class="tchart-tip" :class="{ flip: hoverLeft > 60 }">
          <span class="up">↑ {{ rate(hovered.txBps) }}</span>
          <span class="down">↓ {{ rate(hovered.rxBps) }}</span>
        </div>
      </div>
      <p v-if="points.length === 0" class="tchart-empty">{{ t('chartIdle') }}</p>
    </div>
  </div>
</template>

<style scoped>
.tchart {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.tchart-legend {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px 14px;
  font-size: 12px;
  color: var(--muted);
  white-space: nowrap;
}

.tchart-legend span {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.tchart-legend i {
  width: 8px;
  height: 8px;
  border-radius: 2px;
}

.tchart-legend b {
  color: var(--ink);
  font-weight: 600;
}

.up i { background: var(--info); }
.down i { background: var(--accent); }

.tchart-scale {
  margin-left: auto;
  font-size: 11px;
}

.tchart-plot {
  position: relative;
  height: 120px;
}

svg {
  display: block;
  width: 100%;
  height: 100%;
  overflow: visible;
}

.grid {
  stroke: var(--line);
  stroke-dasharray: 3 4;
  vector-effect: non-scaling-stroke;
}

.line {
  fill: none;
  stroke-width: 1.8;
  stroke-linejoin: round;
  vector-effect: non-scaling-stroke;
}

.line.up { stroke: var(--info); }
.line.down { stroke: var(--accent); }

.area {
  fill: color-mix(in srgb, var(--accent) 14%, transparent);
}

.tchart-cursor {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: var(--line-strong);
  pointer-events: none;
}

.tchart-tip {
  position: absolute;
  top: 4px;
  left: 8px;
  display: flex;
  flex-direction: column;
  padding: 5px 8px;
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface);
  box-shadow: var(--shadow-pop);
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.tchart-tip.flip {
  right: 8px;
  left: auto;
}

.tchart-tip .up { color: var(--info); }
.tchart-tip .down { color: var(--accent-strong); }

.tchart-empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  color: var(--muted);
  font-size: 12px;
}
</style>
