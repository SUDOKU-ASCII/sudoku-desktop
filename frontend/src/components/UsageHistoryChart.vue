<script setup lang="ts">
import { computed, ref } from 'vue'
import type { UsageDay } from '../types'
import { useI18n } from '../i18n'

const props = withDefaults(defineProps<{ days: UsageDay[]; limit?: number; format: (n: number) => string }>(), { limit: 14 })

const { t } = useI18n()

const bars = computed(() => {
  const rows = [...props.days].sort((a, b) => a.date.localeCompare(b.date)).slice(-props.limit)
  const max = Math.max(1, ...rows.map((d) => d.tx + d.rx))
  return rows.map((d) => {
    const total = d.tx + d.rx
    const proxy = Math.min(total, d.proxyTx + d.proxyRx)
    return {
      date: d.date,
      label: d.date.slice(5).replace('-', '/'),
      total,
      proxy,
      direct: Math.max(0, total - proxy),
      height: (total / max) * 100,
      proxyPart: total ? (proxy / total) * 100 : 0,
    }
  })
})

const periodTotal = computed(() => bars.value.reduce((sum, b) => sum + b.total, 0))
const active = ref<number | null>(null)
const focus = computed(() => (active.value == null ? bars.value[bars.value.length - 1] : bars.value[active.value]))
</script>

<template>
  <div class="usage">
    <div class="usage-summary">
      <div>
        <small>{{ focus ? focus.date : t('usagePeriod') }}</small>
        <strong class="num">{{ format(focus ? focus.total : 0) }}</strong>
      </div>
      <div v-if="focus" class="usage-split">
        <span class="p"><i />{{ t('proxy') }} {{ format(focus.proxy) }}</span>
        <span class="d"><i />{{ t('direct') }} {{ format(focus.direct) }}</span>
      </div>
      <div class="usage-total">
        <small>{{ t('usagePeriod') }}</small>
        <strong class="num">{{ format(periodTotal) }}</strong>
      </div>
    </div>
    <div v-if="bars.length" class="usage-bars" @mouseleave="active = null">
      <div
        v-for="(b, i) in bars"
        :key="b.date"
        class="usage-col"
        :class="{ on: active === i }"
        @mouseenter="active = i"
      >
        <div class="usage-track">
          <div class="usage-bar" :style="{ height: `${Math.max(b.height, b.total ? 3 : 1)}%` }">
            <span class="proxy" :style="{ height: `${b.proxyPart}%` }" />
          </div>
        </div>
        <small>{{ b.label }}</small>
      </div>
    </div>
    <p v-else class="empty">{{ t('none') }}</p>
  </div>
</template>

<style scoped>
.usage {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.usage-summary {
  display: flex;
  align-items: flex-end;
  gap: 14px;
  min-width: 0;
}

.usage-summary small {
  display: block;
  color: var(--muted);
  font-size: 11px;
}

.usage-summary strong {
  font-size: 16px;
  font-weight: 650;
}

.usage-split {
  display: flex;
  flex-direction: column;
  min-width: 0;
  color: var(--muted);
  font-size: 11px;
  white-space: nowrap;
}

.usage-split span {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.usage-split i {
  width: 7px;
  height: 7px;
  border-radius: 2px;
}

.usage-split .p i { background: var(--accent); }
.usage-split .d i { background: color-mix(in srgb, var(--info) 45%, var(--surface-2)); }

.usage-total {
  margin-left: auto;
  text-align: right;
}

.usage-bars {
  display: flex;
  align-items: stretch;
  gap: 4px;
  height: 92px;
}

.usage-col {
  display: flex;
  flex: 1;
  flex-direction: column;
  justify-content: flex-end;
  gap: 4px;
  min-width: 0;
}

.usage-col small {
  margin: 0 -8px;
  color: var(--muted);
  font-size: 9.5px;
  text-align: center;
  white-space: nowrap;
}

.usage-col:nth-child(even) small {
  visibility: hidden;
}

.usage-track {
  position: relative;
  flex: 1;
}

.usage-bar {
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  overflow: hidden;
  border-radius: 3px;
  background: color-mix(in srgb, var(--info) 45%, var(--surface-2));
  transition: opacity 0.15s;
}

.usage-bar .proxy {
  display: block;
  background: var(--accent);
}

.usage-bars:hover .usage-col:not(.on) .usage-bar {
  opacity: 0.55;
}

.empty {
  padding: 18px 0;
}
</style>
