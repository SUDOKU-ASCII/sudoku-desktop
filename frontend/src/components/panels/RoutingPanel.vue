<script setup lang="ts">
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
</script>

<template>
  <main class="panel">
    <section class="group-card">
      <h3>{{ props.t('proxyMode') }}</h3>
      <div class="mode-segment">
        <button class="mode-btn" :class="{ active: props.config.routing.proxyMode === 'global' }" @click="props.setRoutingMode('global')">{{ props.t('modeGlobal') }}</button>
        <button class="mode-btn" :class="{ active: props.config.routing.proxyMode === 'direct' }" @click="props.setRoutingMode('direct')">{{ props.t('modeDirect') }}</button>
        <button class="mode-btn" :class="{ active: props.config.routing.proxyMode === 'pac' }" @click="props.setRoutingMode('pac')">{{ props.t('modePac') }}</button>
      </div>
    </section>

    <section class="group-card">
      <div class="group-head">
        <h3>{{ props.t('pacRules') }}</h3>
        <button class="btn mini" @click="props.addPacRule">{{ props.t('addRule') }}</button>
      </div>
      <div class="pac-list">
        <div class="pac-row" v-for="(_rule, idx) in props.config.routing.ruleUrls" :key="idx">
          <input
            :value="props.config.routing.ruleUrls[idx]"
            placeholder="https://example.com/rules.txt"
            @input="props.config.routing.ruleUrls[idx] = ($event.target as HTMLInputElement).value"
          />
          <button class="btn mini danger" @click="props.removePacRule(idx)">{{ props.t('delete') }}</button>
        </div>
        <p v-if="props.config.routing.ruleUrls.length === 0" class="muted">{{ props.t('noPacUrl') }}</p>
      </div>
    </section>

    <section class="group-card">
      <div class="group-head">
        <h3>{{ props.t('customRules') }}</h3>
        <label class="switch-row compact">
          <span>{{ props.t('customRulesEnabled') }}</span>
          <span class="switch-control">
            <input type="checkbox" v-model="props.config.routing.customRulesEnabled" />
            <span class="switch-ui" />
          </span>
        </label>
      </div>
      <textarea
        v-model="props.config.routing.customRules"
        rows="18"
        :disabled="!props.config.routing.customRulesEnabled"
        :placeholder="props.t('customRulesPlaceholder')"
        class="wide-editor"
      />
      <p class="yaml-state" :class="props.customRulesValidation.status">
        {{ props.customRulesValidation.message || props.t('customRulesYamlHint') }}
      </p>
    </section>

    <button class="btn" @click="props.normalizePacRules(); props.saveConfig()">{{ props.t('apply') }}</button>
  </main>
</template>
