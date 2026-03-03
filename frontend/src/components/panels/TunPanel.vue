<script setup lang="ts">
import type { AppConfig } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  config: AppConfig
  resetTunFactory: () => void
  saveConfig: () => void
}>()
</script>

<template>
  <main class="panel">
    <section class="group-card warn">
      <h3>{{ props.t('tunAdvancedTitle') }}</h3>
      <p>{{ props.t('tunAdvancedHint') }}</p>
      <div class="row">
        <button class="btn ghost" @click="props.resetTunFactory">{{ props.t('restoreTunDefaults') }}</button>
        <button class="btn" @click="props.saveConfig">{{ props.t('apply') }}</button>
      </div>
    </section>

    <section class="tun-grid">
      <article class="group-card">
        <h3>{{ props.t('toggleOptions') }}</h3>
        <div class="switch-stack">
          <label class="switch-row"><span>{{ props.t('tunEnabled') }}</span><span class="switch-control"><input type="checkbox" v-model="props.config.tun.enabled" /><span class="switch-ui" /></span></label>
          <label class="switch-row"><span>{{ props.t('blockQuic') }}</span><span class="switch-control"><input type="checkbox" v-model="props.config.tun.blockQuic" /><span class="switch-ui" /></span></label>
          <label class="switch-row"><span>{{ props.t('mapDns') }}</span><span class="switch-control"><input type="checkbox" v-model="props.config.tun.mapDnsEnabled" /><span class="switch-ui" /></span></label>
          <label class="switch-row"><span>{{ props.t('autoStart') }}</span><span class="switch-control"><input type="checkbox" v-model="props.config.core.autoStart" /><span class="switch-ui" /></span></label>
        </div>
      </article>

      <article class="group-card">
        <h3>{{ props.t('enumOptions') }}</h3>
        <label class="field"><span>{{ props.t('socksUdp') }}</span><select v-model="props.config.tun.socksUdp"><option value="udp">udp</option><option value="tcp">tcp</option></select></label>
        <label class="field"><span>{{ props.t('tunLogLevel') }}</span><select v-model="props.config.tun.logLevel"><option value="debug">debug</option><option value="info">info</option><option value="warn">warn</option><option value="error">error</option></select></label>
        <label class="field"><span>{{ props.t('coreLogLevel') }}</span><select v-model="props.config.core.logLevel"><option value="debug">debug</option><option value="info">info</option><option value="warn">warn</option><option value="error">error</option></select></label>
      </article>
    </section>

    <section class="group-card">
      <h3>{{ props.t('networkOptions') }}</h3>
      <div class="form-grid">
        <label class="field"><span>{{ props.t('interface') }}</span><input v-model="props.config.tun.interfaceName" /></label>
        <label class="field"><span>MTU</span><input v-model.number="props.config.tun.mtu" type="number" /></label>
        <label class="field"><span>IPv4</span><input v-model="props.config.tun.ipv4" /></label>
        <label class="field"><span>IPv6</span><input v-model="props.config.tun.ipv6" /></label>
        <label class="field"><span>{{ props.t('socksMark') }}</span><input v-model.number="props.config.tun.socksMark" type="number" /></label>
        <label class="field"><span>{{ props.t('routeTable') }}</span><input v-model.number="props.config.tun.routeTable" type="number" /></label>
        <label class="field"><span>{{ props.t('mapDnsAddress') }}</span><input v-model="props.config.tun.mapDnsAddress" :disabled="!props.config.tun.mapDnsEnabled" /></label>
        <label class="field"><span>{{ props.t('mapDnsPort') }}</span><input v-model.number="props.config.tun.mapDnsPort" type="number" :disabled="!props.config.tun.mapDnsEnabled" /></label>
        <label class="field"><span>{{ props.t('mapDnsNetwork') }}</span><input v-model="props.config.tun.mapDnsNetwork" :disabled="!props.config.tun.mapDnsEnabled" /></label>
        <label class="field"><span>{{ props.t('mapDnsNetmask') }}</span><input v-model="props.config.tun.mapDnsNetmask" :disabled="!props.config.tun.mapDnsEnabled" /></label>
      </div>
    </section>

    <section class="group-card">
      <h3>{{ props.t('runtimeOptions') }}</h3>
      <div class="form-grid">
        <label class="field"><span>{{ props.t('taskStackSize') }}</span><input v-model.number="props.config.tun.taskStackSize" type="number" /></label>
        <label class="field"><span>{{ props.t('tcpBufferSize') }}</span><input v-model.number="props.config.tun.tcpBufferSize" type="number" /></label>
        <label class="field"><span>{{ props.t('maxSession') }}</span><input v-model.number="props.config.tun.maxSession" type="number" /></label>
        <label class="field"><span>{{ props.t('connectTimeoutMs') }}</span><input v-model.number="props.config.tun.connectTimeout" type="number" /></label>
        <label class="field"><span>{{ props.t('corePort') }}</span><input v-model.number="props.config.core.localPort" type="number" /></label>
        <label class="field"><span>{{ props.t('sudokuBinary') }}</span><input v-model="props.config.core.sudokuBinary" /></label>
        <label class="field"><span>{{ props.t('hevBinary') }}</span><input v-model="props.config.core.hevBinary" /></label>
        <label class="field"><span>{{ props.t('workDir') }}</span><input v-model="props.config.core.workingDir" /></label>
      </div>
    </section>
  </main>
</template>
