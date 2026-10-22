<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { Check, ChevronDown, Copy, ExternalLink, Eye, EyeOff, KeyRound } from 'lucide-vue-next'
import { useApp } from '../../composables/appContext'
import { DEFAULT_PROXY_PORT } from '../../types'

const {
  t,
  config,
  state,
  controllerInfo,
  lanProxyInfo,
  saveConfig,
  copyText,
  copyControllerSecret,
  openBoard,
  generateControllerSecret,
  applyControllerSettings,
} = useApp()

const showSecret = ref(false)

const themes = [
  { value: 'auto', label: 'auto', colors: ['#f7f8fa', '#1a1d21'] },
  { value: 'atelier', label: 'themeAtelier', colors: ['#f4f6f6', '#14a085'] },
  { value: 'light', label: 'light', colors: ['#f7f8fa', '#f0465a'] },
  { value: 'dark', label: 'dark', colors: ['#1a1d21', '#ff6474'] },
  { value: 'qingshanlan', label: 'themeQingshanlan', colors: ['#f0f6f8', '#3f81a3'] },
  { value: 'langhualv', label: 'themeLanghualv', colors: ['#eff7f3', '#3f9273'] },
  { value: 'fengxinzi', label: 'themeFengxinzi', colors: ['#f8f2f7', '#9665a0'] },
  { value: 'manjianghong', label: 'themeManjianghong', colors: ['#fbf3f3', '#b2434f'] },
] as const

const proxyEndpoint = () => `127.0.0.1:${state.proxyPort || DEFAULT_PROXY_PORT}`

let autoSaveTimer: number | null = null
watch(
  () => [config.ui.language, config.ui.theme, config.ui.launchAtLogin, config.core.autoStart, config.core.allowLan],
  () => {
    if (autoSaveTimer) window.clearTimeout(autoSaveTimer)
    autoSaveTimer = window.setTimeout(() => {
      autoSaveTimer = null
      void saveConfig(true)
    }, 260)
  }
)
onBeforeUnmount(() => {
  if (autoSaveTimer) window.clearTimeout(autoSaveTimer)
})
</script>

<template>
  <div class="page settings">
    <section class="card">
      <div class="rows">
        <div class="row">
          <div class="row-text"><strong>{{ t('language') }}</strong></div>
          <select v-model="config.ui.language" class="select">
            <option value="auto">{{ t('auto') }}</option>
            <option value="zh">简体中文</option>
            <option value="en">English</option>
            <option value="ru">Русский</option>
          </select>
        </div>
        <label class="row">
          <div class="row-text"><strong>{{ t('launchAtLogin') }}</strong></div>
          <span class="switch"><input v-model="config.ui.launchAtLogin" type="checkbox" /><i /></span>
        </label>
        <label class="row">
          <div class="row-text"><strong>{{ t('autoStart') }}</strong><small>{{ t('autoStartHint') }}</small></div>
          <span class="switch"><input v-model="config.core.autoStart" type="checkbox" /><i /></span>
        </label>
      </div>
    </section>

    <section class="card">
      <div class="card-head"><h2>{{ t('theme') }}</h2></div>
      <div class="card-body">
        <div class="themes">
          <button
            v-for="theme in themes"
            :key="theme.value"
            type="button"
            class="theme"
            :class="{ active: config.ui.theme === theme.value }"
            :aria-pressed="config.ui.theme === theme.value"
            @click="config.ui.theme = theme.value"
          >
            <span class="theme-chip" :style="{ background: theme.colors[0] }">
              <i :style="{ background: theme.colors[1] }" />
              <Check v-if="config.ui.theme === theme.value" :size="12" :stroke-width="3" aria-hidden="true" />
            </span>
            <span class="theme-name">{{ t(theme.label) }}</span>
          </button>
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-head"><h2>{{ t('localProxy') }}</h2></div>
      <div class="rows">
        <div class="row">
          <div class="row-text">
            <strong class="mono selectable">{{ proxyEndpoint() }}</strong>
            <small>{{ t('proxyPortFixedHint') }}</small>
          </div>
          <button class="icon-btn" :title="t('copy')" @click="copyText(proxyEndpoint())"><Copy :size="15" aria-hidden="true" /></button>
        </div>
        <label class="row">
          <div class="row-text">
            <strong>{{ t('proxyAllowLan') }}</strong>
            <small v-if="config.core.allowLan && lanProxyInfo.ips.length" class="mono selectable">
              {{ lanProxyInfo.ips.map((ip) => `${ip}:${lanProxyInfo.port}`).join('  ') }}
            </small>
            <small v-else>{{ t('proxyAllowLanHint') }}</small>
          </div>
          <span class="switch"><input v-model="config.core.allowLan" type="checkbox" /><i /></span>
        </label>
      </div>
    </section>

    <section class="card">
      <div class="card-head">
        <h2>{{ t('controllerTitle') }}</h2>
        <div class="actions">
          <span class="badge" :class="controllerInfo?.ready ? 'ok' : ''">
            {{ controllerInfo?.ready ? t('controllerReady') : t('controllerOffline') }}
          </span>
        </div>
      </div>
      <div class="card-body">
        <p class="hint">{{ t('controllerHint') }}</p>
        <div class="fields controller-fields">
          <label class="field"><span>{{ t('controllerPort') }}</span>
            <input v-model.number="config.controller.port" class="input num" type="number" min="1024" max="65535" />
          </label>
          <label class="field"><span>{{ t('controllerSecret') }}</span>
            <span class="input-group">
              <input v-model="config.controller.secret" class="mono" :type="showSecret ? 'text' : 'password'" autocomplete="off" spellcheck="false" />
              <button class="icon-btn" type="button" :title="showSecret ? t('hide') : t('show')" @click="showSecret = !showSecret">
                <EyeOff v-if="showSecret" :size="14" aria-hidden="true" /><Eye v-else :size="14" aria-hidden="true" />
              </button>
              <button class="icon-btn" type="button" :title="t('controllerGenerate')" @click="generateControllerSecret"><KeyRound :size="14" aria-hidden="true" /></button>
              <button class="icon-btn" type="button" :title="t('controllerCopySecret')" @click="copyControllerSecret"><Copy :size="14" aria-hidden="true" /></button>
            </span>
          </label>
        </div>
        <label class="inline-row">
          <div class="row-text"><strong>{{ t('controllerAllowLan') }}</strong><small>{{ t('controllerRestartNote') }}</small></div>
          <span class="switch"><input v-model="config.controller.allowLan" type="checkbox" /><i /></span>
        </label>
        <div v-if="controllerInfo?.url" class="endpoints">
          <button class="endpoint mono" :title="t('copy')" @click="copyText(controllerInfo.url)">{{ controllerInfo.url }}<Copy :size="12" aria-hidden="true" /></button>
          <button v-for="u in controllerInfo.lanUrls || []" :key="u" class="endpoint mono" :title="t('copy')" @click="copyText(u)">{{ u }}<Copy :size="12" aria-hidden="true" /></button>
        </div>
        <div v-if="controllerInfo?.boards?.length" class="boards">
          <button v-for="board in controllerInfo.boards" :key="board.id" class="btn sm" :disabled="!controllerInfo.ready" @click="openBoard(board)">
            <ExternalLink :size="13" aria-hidden="true" />{{ board.name }}
          </button>
        </div>
      </div>
      <div class="card-foot">
        <button class="btn primary" @click="applyControllerSettings">{{ t('apply') }}</button>
      </div>
    </section>

    <details class="card">
      <summary>{{ t('advancedOptions') }}<ChevronDown :size="16" aria-hidden="true" /></summary>
      <div class="card-body">
        <div class="fields">
          <label class="field"><span>{{ t('coreLogLevel') }}</span>
            <select v-model="config.core.logLevel" class="select"><option>debug</option><option>info</option><option>warn</option><option>error</option></select>
          </label>
          <label class="field wide"><span>{{ t('sudokuBinary') }}</span><input v-model="config.core.sudokuBinary" class="input mono" :placeholder="t('bundledDefault')" spellcheck="false" /></label>
          <label class="field wide"><span>{{ t('hevBinary') }}</span><input v-model="config.core.hevBinary" class="input mono" :placeholder="t('bundledDefault')" spellcheck="false" /></label>
          <label class="field wide"><span>{{ t('workDir') }}</span><input v-model="config.core.workingDir" class="input mono" spellcheck="false" /></label>
        </div>
        <p class="hint">{{ t('kernelVersion') }}: <span class="mono selectable">{{ state.kernel.version || controllerInfo?.version || '—' }}</span></p>
      </div>
      <div class="card-foot">
        <button class="btn primary" @click="saveConfig()">{{ t('apply') }}</button>
      </div>
    </details>
  </div>
</template>
