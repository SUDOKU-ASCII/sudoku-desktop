<script setup lang="ts">
import type { AppConfig, RuntimeState } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  config: AppConfig
  state: RuntimeState
  addPortForward: () => void
  removePortForward: (idx: number) => void
  addReverseRoute: () => void
  removeReverseRoute: (idx: number) => void
  startReverse: () => void
  stopReverse: () => void
  saveConfig: () => void
}>()
</script>

<template>
  <main class="panel">
    <section class="group-card">
      <div class="group-head">
        <h3>{{ props.t('forwards') }}</h3>
        <button class="btn mini" @click="props.addPortForward">{{ props.t('addForward') }}</button>
      </div>
      <div class="relay-list">
        <article v-for="(rule, idx) in props.config.portForwards" :key="rule.id || idx" class="relay-card">
          <div class="form-grid compact-grid">
            <label class="field"><span>{{ props.t('name') }}</span><input v-model="rule.name" /></label>
            <label class="field"><span>{{ props.t('listen') }}</span><input v-model="rule.listen" placeholder="0.0.0.0:1080" /></label>
            <label class="field"><span>{{ props.t('target') }}</span><input v-model="rule.target" placeholder="127.0.0.1:1080" /></label>
            <label class="switch-row compact"><span>{{ props.t('enabled') }}</span><span class="switch-control"><input type="checkbox" v-model="rule.enabled" /><span class="switch-ui" /></span></label>
          </div>
          <button class="btn mini danger" @click="props.removePortForward(idx)">{{ props.t('delete') }}</button>
        </article>
        <p v-if="props.config.portForwards.length === 0">{{ props.t('none') }}</p>
      </div>
      <p class="hint">{{ props.t('forwardHint') }}</p>
    </section>

    <section class="group-card">
      <h3>{{ props.t('reverseClient') }}</h3>
      <div class="form-grid compact-grid">
        <label class="field"><span>{{ props.t('reverseClientId') }}</span><input v-model="props.config.reverseClient.clientId" placeholder="client-id" /></label>
      </div>
      <div class="row">
        <button class="btn mini" @click="props.addReverseRoute">{{ props.t('addRoute') }}</button>
      </div>
      <div class="relay-list">
        <article v-for="(route, idx) in props.config.reverseClient.routes" :key="idx" class="relay-card">
          <div class="form-grid compact-grid">
            <label class="field"><span>{{ props.t('path') }}</span><input v-model="route.path" /></label>
            <label class="field"><span>{{ props.t('target') }}</span><input v-model="route.target" /></label>
            <label class="field"><span>{{ props.t('hostHeader') }}</span><input v-model="route.hostHeader" placeholder="example.com" /></label>
            <label class="field"><span>{{ props.t('stripPrefix') }}</span>
              <select
                :value="route.stripPrefix == null ? 'auto' : route.stripPrefix ? 'yes' : 'no'"
                @change="route.stripPrefix = ($event.target as HTMLSelectElement).value === 'auto' ? null : ($event.target as HTMLSelectElement).value === 'yes'"
              >
                <option value="auto">{{ props.t('auto') }}</option>
                <option value="yes">{{ props.t('yes') }}</option>
                <option value="no">{{ props.t('no') }}</option>
              </select>
            </label>
          </div>
          <button class="btn mini danger" @click="props.removeReverseRoute(idx)">{{ props.t('delete') }}</button>
        </article>
        <p v-if="props.config.reverseClient.routes.length === 0">{{ props.t('none') }}</p>
      </div>
      <p class="hint">{{ props.t('reverseClientHint') }}</p>
    </section>

    <section class="group-card">
      <h3>{{ props.t('reverseForwarder') }}</h3>
      <div class="form-grid compact-grid">
        <label class="field"><span>{{ props.t('dialUrl') }}</span><input v-model="props.config.reverseForward.dialUrl" placeholder="wss://example.com/ssh" /></label>
        <label class="field"><span>{{ props.t('listen') }}</span><input v-model="props.config.reverseForward.listenAddr" placeholder="127.0.0.1:2222" /></label>
        <label class="switch-row compact"><span>{{ props.t('insecure') }}</span><span class="switch-control"><input type="checkbox" v-model="props.config.reverseForward.insecure" /><span class="switch-ui" /></span></label>
      </div>
      <div class="row">
        <button class="btn" :disabled="props.state.reverseRunning" @click="props.startReverse">{{ props.t('reverseStart') }}</button>
        <button class="btn ghost" :disabled="!props.state.reverseRunning" @click="props.stopReverse">{{ props.t('reverseStop') }}</button>
        <button class="btn" @click="props.saveConfig">{{ props.t('apply') }}</button>
      </div>
    </section>
  </main>
</template>
