<script setup lang="ts">
import { computed } from 'vue'
import type { AppConfig, ProxyMode } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  config: AppConfig
  customRulesValidation: { status: 'idle' | 'checking' | 'ok' | 'error'; message: string }
  setRoutingMode: (mode: ProxyMode) => void
  addPacRule: () => void
  removePacRule: (idx: number) => void
  normalizePacRules: () => void
  saveConfig: () => void
}>()

const isRejectRule = (raw: string) => /^[!！]/.test(raw.trim())

const enabledRuleCount = computed(() => props.config.routing.ruleUrls.length)
const rejectRuleCount = computed(() => props.config.routing.ruleUrls.filter((rule) => isRejectRule(rule)).length)
const customRuleCount = computed(() => props.config.routing.customRules.split(/\r?\n/).map((x) => x.trim()).filter((x) => x && !x.startsWith('#')).length)

const ruleName = (raw: string) => {
  const text = raw.replace(/^[!！]/, '').trim()
  try {
    const url = new URL(text)
    const last = url.pathname.split('/').filter(Boolean).pop() || url.hostname
    return decodeURIComponent(last).replace(/\.(yaml|yml|list|txt)$/i, '')
  } catch {
    return text || props.t('pacRule')
  }
}

const ruleKindText = (raw: string) => isRejectRule(raw) ? props.t('rejectRule') : props.t('directRule')
const ruleKindClass = (raw: string) => isRejectRule(raw) ? 'reject' : 'ok'

const moveRule = (idx: number, delta: -1 | 1) => {
  const next = idx + delta
  if (next < 0 || next >= props.config.routing.ruleUrls.length) return
  const rows = props.config.routing.ruleUrls
  const item = rows[idx]
  rows.splice(idx, 1)
  rows.splice(next, 0, item)
}
</script>

<template>
  <main class="panel routing-page">
    <section class="routing-layout">
      <section class="routing-main">
        <article class="deck-card mode-card">
          <div class="card-head">
            <div>
              <h3>{{ props.t('proxyMode') }}</h3>
              <p>{{ props.t('proxyModeHint') }}</p>
            </div>
          </div>
          <div class="mode-segment xl">
            <button class="mode-btn" :class="{ active: props.config.routing.proxyMode === 'global' }" @click="props.setRoutingMode('global')">{{ props.t('modeGlobal') }}</button>
            <button class="mode-btn" :class="{ active: props.config.routing.proxyMode === 'direct' }" @click="props.setRoutingMode('direct')">{{ props.t('modeDirect') }}</button>
            <button class="mode-btn" :class="{ active: props.config.routing.proxyMode === 'pac' }" @click="props.setRoutingMode('pac')">{{ props.t('modePac') }}</button>
          </div>
        </article>

        <article class="deck-card pac-card">
          <div class="card-head">
            <div>
              <h3>{{ props.t('pacRules') }}</h3>
              <p>{{ props.t('pacRulesHint') }}</p>
            </div>
            <button class="btn mini" @click="props.addPacRule">{{ props.t('addRule') }}</button>
          </div>
          <div class="pac-pro-list">
            <div v-for="(rule, idx) in props.config.routing.ruleUrls" :key="idx" class="pac-pro-row" :class="{ reject: isRejectRule(rule) }">
              <button class="order-btn" type="button" :disabled="idx === 0" :title="props.t('moveUp')" @click="moveRule(idx, -1)">↑</button>
              <button class="order-btn" type="button" :disabled="idx === props.config.routing.ruleUrls.length - 1" :title="props.t('moveDown')" @click="moveRule(idx, 1)">↓</button>
              <div class="pac-rule-copy">
                <strong>{{ ruleName(rule) }}</strong>
                <input
                  v-model="props.config.routing.ruleUrls[idx]"
                  placeholder="https://example.com/rules.txt 或 !https://example.com/reject.yaml"
                />
              </div>
              <span class="rule-state" :class="ruleKindClass(rule)">{{ ruleKindText(rule) }}</span>
              <button class="btn mini danger" @click="props.removePacRule(idx)">{{ props.t('delete') }}</button>
            </div>
            <p v-if="props.config.routing.ruleUrls.length === 0" class="empty-state">{{ props.t('noPacUrl') }}</p>
          </div>
        </article>

        <article class="deck-card custom-rules-card">
          <div class="card-head">
            <div>
              <h3>{{ props.t('customRules') }}</h3>
              <p>{{ props.t('customRulesYamlHint') }}</p>
            </div>
            <label class="switch-row compact inline-switch">
              <span>{{ props.t('customRulesEnabled') }}</span>
              <span class="switch-control">
                <input type="checkbox" v-model="props.config.routing.customRulesEnabled" />
                <span class="switch-ui" />
              </span>
            </label>
          </div>
          <textarea
            v-model="props.config.routing.customRules"
            rows="14"
            :disabled="!props.config.routing.customRulesEnabled"
            :placeholder="props.t('customRulesPlaceholder')"
            class="wide-editor code-editor"
          />
          <div class="editor-foot">
            <p class="yaml-state" :class="props.customRulesValidation.status">
              {{ props.customRulesValidation.message || props.t('customRulesYamlHint') }}
            </p>
            <button class="btn mini ghost" @click="props.normalizePacRules">{{ props.t('format') }}</button>
          </div>
        </article>
      </section>

      <aside class="routing-aside">
        <article class="deck-card">
          <div class="card-head">
            <h3>{{ props.t('ruleOverview') }}</h3>
          </div>
          <dl class="status-list">
            <div><dt>{{ props.t('pacRuleCount') }}</dt><dd>{{ props.config.routing.ruleUrls.length }}</dd></div>
            <div><dt>{{ props.t('enabledRules') }}</dt><dd>{{ enabledRuleCount }}</dd></div>
            <div><dt>{{ props.t('rejectRules') }}</dt><dd>{{ rejectRuleCount }}</dd></div>
            <div><dt>{{ props.t('customRuleCount') }}</dt><dd>{{ customRuleCount }}</dd></div>
            <div><dt>{{ props.t('proxyMode') }}</dt><dd>{{ props.config.routing.proxyMode.toUpperCase() }}</dd></div>
          </dl>
        </article>

        <article class="deck-card">
          <div class="card-head">
            <h3>{{ props.t('tips') }}</h3>
          </div>
          <ul class="hint-list">
            <li>{{ props.t('routingTipOrder') }}</li>
            <li>{{ props.t('routingTipPriority') }}</li>
            <li>{{ props.t('routingTipCustom') }}</li>
          </ul>
        </article>

        <article class="deck-card quick-card">
          <div class="card-head">
            <h3>{{ props.t('quickActions') }}</h3>
          </div>
          <button class="btn ghost" @click="props.normalizePacRules">{{ props.t('format') }}</button>
          <button class="btn" @click="props.normalizePacRules(); props.saveConfig()">{{ props.t('apply') }}</button>
        </article>
      </aside>
    </section>
  </main>
</template>
