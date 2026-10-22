<script setup lang="ts">
import { computed } from 'vue'
import { ChevronDown, ShieldCheck } from 'lucide-vue-next'
import { useApp } from '../../composables/appContext'

const { t, config, state, isWindowsLike, hasAdminPrivileges, forgetAdminPassword, resetTunFactory, saveConfig } = useApp()

const tunStatus = computed(() => {
  if (state.tunRunning) return { tone: 'ok', text: t('statusRunning') }
  if (config.tun.enabled && state.phase === 'running') return { tone: 'warn', text: t('tunNotActive') }
  return null
})
</script>

<template>
  <div class="page tun">
    <section class="card">
      <div class="rows">
        <label class="row">
          <div class="row-text">
            <strong>{{ t('tunEnabled') }} <span v-if="tunStatus" class="badge" :class="tunStatus.tone">{{ tunStatus.text }}</span></strong>
            <small>{{ t('tunHint') }}</small>
          </div>
          <span class="switch"><input v-model="config.tun.enabled" type="checkbox" /><i /></span>
        </label>
        <div class="row">
          <ShieldCheck :size="17" class="row-icon" aria-hidden="true" />
          <div class="row-text">
            <strong>{{ t('adminRights') }}</strong>
            <small>{{ isWindowsLike ? t('tunElevationHintWindows') : t('tunElevationHint') }}</small>
          </div>
          <button v-if="!isWindowsLike && hasAdminPrivileges && !state.tunRunning" class="btn sm" @click="forgetAdminPassword">
            {{ t('adminForget') }}
          </button>
        </div>
      </div>
    </section>

    <section class="card">
      <div class="rows">
        <label class="row">
          <div class="row-text">
            <strong>{{ t('blockQuic') }}</strong>
            <small>{{ t('blockQuicHint') }}</small>
          </div>
          <span class="switch"><input v-model="config.tun.blockQuic" type="checkbox" /><i /></span>
        </label>
        <label class="row">
          <div class="row-text">
            <strong>{{ t('mapDns') }}</strong>
            <small>{{ t('mapDnsHint') }}</small>
          </div>
          <span class="switch"><input v-model="config.tun.mapDnsEnabled" type="checkbox" /><i /></span>
        </label>
      </div>
    </section>

    <details class="card">
      <summary>{{ t('advancedOptions') }}<ChevronDown :size="16" aria-hidden="true" /></summary>
      <div class="card-body">
        <p class="hint">{{ t('tunAdvancedHint') }}</p>
        <div class="fields">
          <label class="field"><span>{{ t('interface') }}</span><input v-model="config.tun.interfaceName" class="input mono" spellcheck="false" /></label>
          <label class="field"><span>MTU</span><input v-model.number="config.tun.mtu" class="input num" type="number" /></label>
          <label class="field"><span>IPv4</span><input v-model="config.tun.ipv4" class="input mono" spellcheck="false" /></label>
          <label class="field"><span>IPv6</span><input v-model="config.tun.ipv6" class="input mono" spellcheck="false" /></label>
          <label class="field"><span>{{ t('socksUdp') }}</span>
            <select v-model="config.tun.socksUdp" class="select"><option value="udp">udp</option><option value="tcp">tcp</option></select>
          </label>
          <label class="field"><span>{{ t('tunLogLevel') }}</span>
            <select v-model="config.tun.logLevel" class="select"><option>debug</option><option>info</option><option>warn</option><option>error</option></select>
          </label>
          <label class="field"><span>{{ t('socksMark') }}</span><input v-model.number="config.tun.socksMark" class="input num" type="number" /></label>
          <label class="field"><span>{{ t('routeTable') }}</span><input v-model.number="config.tun.routeTable" class="input num" type="number" /></label>
        </div>
        <template v-if="config.tun.mapDnsEnabled">
          <div class="section-title">{{ t('mapDns') }}</div>
          <div class="fields">
            <label class="field"><span>{{ t('mapDnsAddress') }}</span><input v-model="config.tun.mapDnsAddress" class="input mono" spellcheck="false" /></label>
            <label class="field"><span>{{ t('mapDnsPort') }}</span><input v-model.number="config.tun.mapDnsPort" class="input num" type="number" /></label>
            <label class="field"><span>{{ t('mapDnsNetwork') }}</span><input v-model="config.tun.mapDnsNetwork" class="input mono" spellcheck="false" /></label>
            <label class="field"><span>{{ t('mapDnsNetmask') }}</span><input v-model="config.tun.mapDnsNetmask" class="input mono" spellcheck="false" /></label>
          </div>
        </template>
        <div class="section-title">{{ t('runtimeOptions') }}</div>
        <div class="fields">
          <label class="field"><span>{{ t('taskStackSize') }}</span><input v-model.number="config.tun.taskStackSize" class="input num" type="number" /></label>
          <label class="field"><span>{{ t('tcpBufferSize') }}</span><input v-model.number="config.tun.tcpBufferSize" class="input num" type="number" /></label>
          <label class="field"><span>{{ t('maxSession') }}</span><input v-model.number="config.tun.maxSession" class="input num" type="number" /></label>
          <label class="field"><span>{{ t('connectTimeoutMs') }}</span><input v-model.number="config.tun.connectTimeout" class="input num" type="number" /></label>
        </div>
      </div>
      <div class="card-foot">
        <button class="btn" @click="resetTunFactory">{{ t('restoreTunDefaults') }}</button>
      </div>
    </details>

    <div class="page-actions">
      <button class="btn primary" @click="saveConfig()">{{ t('apply') }}</button>
    </div>
  </div>
</template>
