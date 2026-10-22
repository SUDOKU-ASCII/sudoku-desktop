<script setup lang="ts">
import { computed } from 'vue'
import { ArrowDown, ArrowUp, CircleAlert, CircleCheck, LoaderCircle, Plus, Trash2 } from 'lucide-vue-next'
import { useApp } from '../../composables/appContext'
import type { ProxyMode } from '../../types'

const { t, config, customRulesValidation, setRoutingMode, addPacRule, removePacRule, normalizePacRules, saveConfig } = useApp()

type RuleKind = 'direct' | 'proxy' | 'reject'

const modes: Array<{ value: ProxyMode; label: Parameters<typeof t>[0]; hint: Parameters<typeof t>[0] }> = [
  { value: 'pac', label: 'modePac', hint: 'modePacHint' },
  { value: 'global', label: 'modeGlobal', hint: 'modeGlobalHint' },
  { value: 'direct', label: 'modeDirect', hint: 'modeDirectHint' },
]
const modeHint = computed(() => modes.find((m) => m.value === config.routing.proxyMode)?.hint ?? 'modePacHint')

// A rule URL carries its action as a prefix: none = direct, "-" = proxy, "!" = reject.
const kindOf = (raw: string): RuleKind => (/^\s*[!！]/.test(raw) ? 'reject' : /^\s*[-_]/.test(raw) ? 'proxy' : 'direct')
const bodyOf = (raw: string) => raw.replace(/^\s*[!！\-_]\s*/, '')
const prefix: Record<RuleKind, string> = { direct: '', proxy: '-', reject: '!' }

const setKind = (idx: number, kind: RuleKind) => {
  config.routing.ruleUrls[idx] = prefix[kind] + bodyOf(config.routing.ruleUrls[idx])
}
const setBody = (idx: number, value: string) => {
  config.routing.ruleUrls[idx] = prefix[kindOf(config.routing.ruleUrls[idx])] + value
}

const move = (idx: number, delta: -1 | 1) => {
  const rows = config.routing.ruleUrls
  const next = idx + delta
  if (next < 0 || next >= rows.length) return
  const [item] = rows.splice(idx, 1)
  rows.splice(next, 0, item)
}

const apply = async () => {
  normalizePacRules()
  await saveConfig()
}
</script>

<template>
  <div class="page routing">
    <section class="card">
      <div class="card-body pad-top">
        <div class="segmented block" role="radiogroup" :aria-label="t('proxyMode')">
          <button
            v-for="m in modes"
            :key="m.value"
            type="button"
            role="radio"
            :aria-checked="config.routing.proxyMode === m.value"
            :class="{ active: config.routing.proxyMode === m.value }"
            @click="setRoutingMode(m.value)"
          >
            {{ t(m.label) }}
          </button>
        </div>
        <p class="hint">{{ t(modeHint) }}</p>
      </div>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>{{ t('pacRules') }} <span class="count num">{{ config.routing.ruleUrls.length }}</span></h2>
        <div class="actions">
          <button class="icon-btn" :title="t('addRule')" @click="addPacRule"><Plus :size="16" aria-hidden="true" /></button>
        </div>
      </div>
      <div v-if="config.routing.ruleUrls.length" class="list">
        <div v-for="(rule, idx) in config.routing.ruleUrls" :key="idx" class="list-item rule-row">
          <select
            class="select rule-kind"
            :class="kindOf(rule)"
            :value="kindOf(rule)"
            :aria-label="t('ruleAction')"
            @change="setKind(idx, ($event.target as HTMLSelectElement).value as RuleKind)"
          >
            <option value="direct">{{ t('direct') }}</option>
            <option value="proxy">{{ t('proxy') }}</option>
            <option value="reject">{{ t('reject') }}</option>
          </select>
          <input
            class="input mono"
            :value="bodyOf(rule)"
            placeholder="https://…"
            spellcheck="false"
            @input="setBody(idx, ($event.target as HTMLInputElement).value)"
          />
          <div class="list-actions">
            <button class="icon-btn" :disabled="idx === 0" :title="t('moveUp')" @click="move(idx, -1)"><ArrowUp :size="14" aria-hidden="true" /></button>
            <button class="icon-btn" :disabled="idx === config.routing.ruleUrls.length - 1" :title="t('moveDown')" @click="move(idx, 1)"><ArrowDown :size="14" aria-hidden="true" /></button>
            <button class="icon-btn danger" :title="t('delete')" @click="removePacRule(idx)"><Trash2 :size="14" aria-hidden="true" /></button>
          </div>
        </div>
      </div>
      <p v-else class="empty">{{ t('noPacUrl') }}</p>
      <p class="hint card-note">{{ t('pacRulesHint') }}</p>
    </section>

    <section class="card">
      <label class="card-head">
        <h2>{{ t('customRules') }}</h2>
        <span class="actions"><span class="switch"><input v-model="config.routing.customRulesEnabled" type="checkbox" /><i /></span></span>
      </label>
      <div v-if="config.routing.customRulesEnabled" class="card-body">
        <textarea
          v-model="config.routing.customRules"
          class="textarea mono"
          wrap="off"
          rows="9"
          spellcheck="false"
          :placeholder="t('customRulesPlaceholder')"
        />
        <p class="validation" :class="customRulesValidation.status">
          <LoaderCircle v-if="customRulesValidation.status === 'checking'" :size="14" class="spin" aria-hidden="true" />
          <CircleCheck v-else-if="customRulesValidation.status === 'ok'" :size="14" aria-hidden="true" />
          <CircleAlert v-else-if="customRulesValidation.status === 'error'" :size="14" aria-hidden="true" />
          <span>{{ customRulesValidation.message || t('customRulesYamlHint') }}</span>
        </p>
      </div>
      <p v-else class="hint card-note">{{ t('customRulesYamlHint') }}</p>
    </section>

    <div class="page-actions">
      <button class="btn primary" @click="apply">{{ t('apply') }}</button>
    </div>
  </div>
</template>
