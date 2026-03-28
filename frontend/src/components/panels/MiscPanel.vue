<script setup lang="ts">
import { onBeforeUnmount, watch } from 'vue'
import type { AppConfig } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  config: AppConfig
  saveConfig: (silent?: boolean) => void
}>()

let autoSaveTimer: number | null = null

const queueAutoSave = () => {
  if (autoSaveTimer) window.clearTimeout(autoSaveTimer)
  autoSaveTimer = window.setTimeout(() => {
    autoSaveTimer = null
    void props.saveConfig(true)
  }, 260)
}

watch(
  () => [props.config.ui.language, props.config.ui.theme, props.config.ui.launchAtLogin],
  () => {
    queueAutoSave()
  }
)

onBeforeUnmount(() => {
  if (autoSaveTimer) window.clearTimeout(autoSaveTimer)
})
</script>

<template>
  <main class="panel">
    <section class="group-card">
      <h3>{{ props.t('misc') }}</h3>
      <div class="form-grid compact-grid">
        <label class="field"><span>{{ props.t('language') }}</span>
          <select v-model="props.config.ui.language">
            <option value="auto">{{ props.t('auto') }}</option>
            <option value="zh">{{ props.t('langZh') }}</option>
            <option value="en">{{ props.t('langEn') }}</option>
            <option value="ru">{{ props.t('langRu') }}</option>
          </select>
        </label>
        <label class="field"><span>{{ props.t('theme') }}</span>
          <select v-model="props.config.ui.theme">
            <option value="auto">{{ props.t('auto') }}</option>
            <option value="light">{{ props.t('light') }}</option>
            <option value="dark">{{ props.t('dark') }}</option>
            <option value="qingshanlan">{{ props.t('themeQingshanlan') }}</option>
            <option value="langhualv">{{ props.t('themeLanghualv') }}</option>
            <option value="fengxinzi">{{ props.t('themeFengxinzi') }}</option>
            <option value="manjianghong">{{ props.t('themeManjianghong') }}</option>
          </select>
        </label>
      </div>
      <div class="switch-stack">
        <label class="switch-row"><span>{{ props.t('launchAtLogin') }}</span><span class="switch-control"><input type="checkbox" v-model="props.config.ui.launchAtLogin" /><span class="switch-ui" /></span></label>
      </div>
    </section>
  </main>
</template>
