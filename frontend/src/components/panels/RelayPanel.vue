<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import { ArrowRight, Plus, Trash2 } from 'lucide-vue-next'
import { useApp } from '../../composables/appContext'
import type { ReverseRoute } from '../../types'

const {
  t,
  config,
  state,
  busy,
  addPortForward,
  removePortForward,
  addReverseRoute,
  removeReverseRoute,
  startReverse,
  stopReverse,
  saveConfig,
} = useApp()

let autoSaveTimer: number | null = null
watch(
  () => [config.portForwards, config.reverseClient, config.reverseForward],
  () => {
    if (autoSaveTimer) window.clearTimeout(autoSaveTimer)
    autoSaveTimer = window.setTimeout(() => {
      autoSaveTimer = null
      void saveConfig(true)
    }, 400)
  },
  { deep: true }
)
onBeforeUnmount(() => {
  if (autoSaveTimer) window.clearTimeout(autoSaveTimer)
})

const stripValue = (r: ReverseRoute) => (r.stripPrefix == null ? 'auto' : r.stripPrefix ? 'yes' : 'no')
const setStrip = (r: ReverseRoute, v: string) => {
  r.stripPrefix = v === 'auto' ? null : v === 'yes'
}
</script>

<template>
  <div class="page relay">
    <section class="card">
      <div class="card-head">
        <h2>{{ t('forwards') }} <span class="count num">{{ config.portForwards.length }}</span></h2>
        <div class="actions">
          <button class="icon-btn" :title="t('addForward')" @click="addPortForward"><Plus :size="16" aria-hidden="true" /></button>
        </div>
      </div>
      <div v-if="config.portForwards.length" class="list">
        <div v-for="(rule, idx) in config.portForwards" :key="rule.id || idx" class="list-item stack">
          <div class="stack-line">
            <input v-model="rule.name" class="input name-input" :placeholder="t('name')" />
            <span class="switch" :title="t('enabled')"><input v-model="rule.enabled" type="checkbox" /><i /></span>
            <button class="icon-btn danger" :title="t('delete')" @click="removePortForward(idx)"><Trash2 :size="15" aria-hidden="true" /></button>
          </div>
          <div class="stack-line">
            <input v-model="rule.listen" class="input mono" :aria-label="t('listen')" placeholder="127.0.0.1:1080" spellcheck="false" />
            <ArrowRight :size="15" class="muted flow" aria-hidden="true" />
            <input v-model="rule.target" class="input mono" :aria-label="t('target')" placeholder="10.0.0.2:80" spellcheck="false" />
          </div>
        </div>
      </div>
      <p v-else class="empty">{{ t('none') }}</p>
      <p class="hint card-note">{{ t('forwardHint') }}</p>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>{{ t('reverseClient') }}</h2>
        <div class="actions">
          <button class="icon-btn" :title="t('addRoute')" @click="addReverseRoute"><Plus :size="16" aria-hidden="true" /></button>
        </div>
      </div>
      <div class="card-body">
        <label class="field"><span>{{ t('reverseClientId') }}</span><input v-model="config.reverseClient.clientId" class="input mono" placeholder="client-id" spellcheck="false" /></label>
      </div>
      <div v-if="config.reverseClient.routes.length" class="list bordered">
        <div v-for="(route, idx) in config.reverseClient.routes" :key="idx" class="list-item stack">
          <div class="stack-line">
            <input v-model="route.path" class="input mono path-input" :aria-label="t('path')" placeholder="/" spellcheck="false" />
            <ArrowRight :size="15" class="muted flow" aria-hidden="true" />
            <input v-model="route.target" class="input mono" :aria-label="t('target')" placeholder="http://127.0.0.1:8080" spellcheck="false" />
            <button class="icon-btn danger" :title="t('delete')" @click="removeReverseRoute(idx)"><Trash2 :size="15" aria-hidden="true" /></button>
          </div>
          <div class="stack-line">
            <input v-model="route.hostHeader" class="input mono" :placeholder="t('hostHeader')" spellcheck="false" />
            <select class="select strip-select" :value="stripValue(route)" :aria-label="t('stripPrefix')" @change="setStrip(route, ($event.target as HTMLSelectElement).value)">
              <option value="auto">{{ t('stripPrefix') }}: {{ t('auto') }}</option>
              <option value="yes">{{ t('stripPrefix') }}: {{ t('yes') }}</option>
              <option value="no">{{ t('stripPrefix') }}: {{ t('no') }}</option>
            </select>
          </div>
        </div>
      </div>
      <p class="hint card-note">{{ t('reverseClientHint') }}</p>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>{{ t('reverseForwarder') }}</h2>
        <div class="actions">
          <span class="badge" :class="state.reverseRunning ? 'ok' : ''">{{ state.reverseRunning ? t('statusRunning') : t('statusStopped') }}</span>
        </div>
      </div>
      <div class="card-body">
        <div class="fields">
          <label class="field wide"><span>{{ t('dialUrl') }}</span><input v-model="config.reverseForward.dialUrl" class="input mono" placeholder="wss://example.com/ssh" spellcheck="false" /></label>
          <label class="field"><span>{{ t('listen') }}</span><input v-model="config.reverseForward.listenAddr" class="input mono" placeholder="127.0.0.1:2222" spellcheck="false" /></label>
        </div>
        <label class="inline-row">
          <div class="row-text"><strong>{{ t('insecure') }}</strong></div>
          <span class="switch"><input v-model="config.reverseForward.insecure" type="checkbox" /><i /></span>
        </label>
      </div>
      <div class="card-foot">
        <button v-if="state.reverseRunning" class="btn" :disabled="busy" @click="stopReverse">{{ t('reverseStop') }}</button>
        <button v-else class="btn primary" :disabled="busy || !config.reverseForward.dialUrl.trim()" @click="startReverse">{{ t('reverseStart') }}</button>
      </div>
    </section>
  </div>
</template>
