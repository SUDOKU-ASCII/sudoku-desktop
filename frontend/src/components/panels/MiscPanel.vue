<script setup lang="ts">
import type { AppConfig } from '../../types'

const props = defineProps<{
  t: (key: any) => string
  config: AppConfig
  saveConfig: () => void
}>()
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
          </select>
        </label>
      </div>
      <div class="row row-spaced">
        <button class="btn" @click="props.saveConfig">{{ props.t('apply') }}</button>
      </div>
    </section>
  </main>
</template>
