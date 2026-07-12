<script setup lang="ts">
import { Check } from 'lucide-vue-next'
import { onBeforeUnmount, watch } from 'vue'
import type { AppConfig } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  config: AppConfig
  saveConfig: (silent?: boolean) => void
}>()

let autoSaveTimer: number | null = null
const themes = [
  { value: 'auto', label: 'auto', colors: ['#f7f8fa', '#17191d', '#42b883'] },
  { value: 'atelier', label: 'themeAtelier', colors: ['#f3f5f6', '#202429', '#18a98b'] },
  { value: 'light', label: 'light', colors: ['#ffffff', '#20242b', '#ff4c5e'] },
  { value: 'dark', label: 'dark', colors: ['#111419', '#f4f6f8', '#ff596a'] },
  { value: 'qingshanlan', label: 'themeQingshanlan', colors: ['#eef6f9', '#20465a', '#5f93ad'] },
  { value: 'langhualv', label: 'themeLanghualv', colors: ['#eef8f3', '#21463c', '#4f967b'] },
  { value: 'fengxinzi', label: 'themeFengxinzi', colors: ['#f8f1f7', '#513650', '#9c6d98'] },
  { value: 'manjianghong', label: 'themeManjianghong', colors: ['#fbf1f1', '#552d31', '#a54751'] },
] as const

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
      </div>
      <div class="theme-field">
        <span>{{ props.t('theme') }}</span>
        <div class="theme-picker">
          <button
            v-for="theme in themes"
            :key="theme.value"
            type="button"
            class="theme-option"
            :class="{ active: props.config.ui.theme === theme.value }"
            @click="props.config.ui.theme = theme.value"
          >
            <span class="theme-swatches" aria-hidden="true">
              <i v-for="color in theme.colors" :key="color" :style="{ background: color }" />
            </span>
            <span>{{ props.t(theme.label) }}</span>
            <Check v-if="props.config.ui.theme === theme.value" :size="15" aria-hidden="true" />
          </button>
        </div>
      </div>
      <div class="switch-stack">
        <label class="switch-row"><span>{{ props.t('launchAtLogin') }}</span><span class="switch-control"><input type="checkbox" v-model="props.config.ui.launchAtLogin" /><span class="switch-ui" /></span></label>
      </div>
    </section>
  </main>
</template>
