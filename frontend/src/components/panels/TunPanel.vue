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
      <h3>TUN 高级设置</h3>
      <p>以下参数会直接影响网络连通性；不确定时请保持默认值。</p>
      <div class="row">
        <button class="btn ghost" @click="props.resetTunFactory">恢复默认参数</button>
        <button class="btn" @click="props.saveConfig">{{ props.t('apply') }}</button>
      </div>
    </section>

    <section class="tun-grid">
      <article class="group-card">
        <h3>开关项</h3>
        <label class="switch-row"><span>{{ props.t('tunEnabled') }}</span><span class="switch-control"><input type="checkbox" v-model="props.config.tun.enabled" /><span class="switch-ui" /></span></label>
        <label class="switch-row"><span>Block QUIC</span><span class="switch-control"><input type="checkbox" v-model="props.config.tun.blockQuic" /><span class="switch-ui" /></span></label>
        <label class="switch-row"><span>MapDNS</span><span class="switch-control"><input type="checkbox" v-model="props.config.tun.mapDnsEnabled" /><span class="switch-ui" /></span></label>
        <label class="switch-row"><span>Auto Start</span><span class="switch-control"><input type="checkbox" v-model="props.config.core.autoStart" /><span class="switch-ui" /></span></label>
      </article>

      <article class="group-card">
        <h3>枚举项</h3>
        <label class="field"><span>Socks UDP</span><select v-model="props.config.tun.socksUdp"><option value="udp">udp</option><option value="tcp">tcp</option></select></label>
        <label class="field"><span>TUN Log Level</span><select v-model="props.config.tun.logLevel"><option value="debug">debug</option><option value="info">info</option><option value="warn">warn</option><option value="error">error</option></select></label>
        <label class="field"><span>Core Log Level</span><select v-model="props.config.core.logLevel"><option value="debug">debug</option><option value="info">info</option><option value="warn">warn</option><option value="error">error</option></select></label>
      </article>
    </section>

    <section class="group-card">
      <h3>网络参数</h3>
      <div class="form-grid">
        <label class="field"><span>Interface</span><input v-model="props.config.tun.interfaceName" /></label>
        <label class="field"><span>MTU</span><input v-model.number="props.config.tun.mtu" type="number" /></label>
        <label class="field"><span>IPv4</span><input v-model="props.config.tun.ipv4" /></label>
        <label class="field"><span>IPv6</span><input v-model="props.config.tun.ipv6" /></label>
        <label class="field"><span>Socks Mark</span><input v-model.number="props.config.tun.socksMark" type="number" /></label>
        <label class="field"><span>Route Table</span><input v-model.number="props.config.tun.routeTable" type="number" /></label>
        <label class="field"><span>MapDNS Address</span><input v-model="props.config.tun.mapDnsAddress" :disabled="!props.config.tun.mapDnsEnabled" /></label>
        <label class="field"><span>MapDNS Port</span><input v-model.number="props.config.tun.mapDnsPort" type="number" :disabled="!props.config.tun.mapDnsEnabled" /></label>
        <label class="field"><span>MapDNS Network</span><input v-model="props.config.tun.mapDnsNetwork" :disabled="!props.config.tun.mapDnsEnabled" /></label>
        <label class="field"><span>MapDNS Netmask</span><input v-model="props.config.tun.mapDnsNetmask" :disabled="!props.config.tun.mapDnsEnabled" /></label>
      </div>
    </section>

    <section class="group-card">
      <h3>运行参数</h3>
      <div class="form-grid">
        <label class="field"><span>Task Stack Size</span><input v-model.number="props.config.tun.taskStackSize" type="number" /></label>
        <label class="field"><span>TCP Buffer Size</span><input v-model.number="props.config.tun.tcpBufferSize" type="number" /></label>
        <label class="field"><span>Max Session</span><input v-model.number="props.config.tun.maxSession" type="number" /></label>
        <label class="field"><span>Connect Timeout (ms)</span><input v-model.number="props.config.tun.connectTimeout" type="number" /></label>
        <label class="field"><span>Core Port</span><input v-model.number="props.config.core.localPort" type="number" /></label>
        <label class="field"><span>Sudoku Binary</span><input v-model="props.config.core.sudokuBinary" /></label>
        <label class="field"><span>HEV Binary</span><input v-model="props.config.core.hevBinary" /></label>
        <label class="field"><span>Work Dir</span><input v-model="props.config.core.workingDir" /></label>
      </div>
    </section>
  </main>
</template>
