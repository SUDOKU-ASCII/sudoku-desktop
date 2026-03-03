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
        <label class="field"><span>Language</span>
          <select v-model="props.config.ui.language">
            <option value="auto">auto</option>
            <option value="zh">中文</option>
            <option value="en">English</option>
            <option value="ru">Русский</option>
          </select>
        </label>
        <label class="field"><span>Theme</span>
          <select v-model="props.config.ui.theme">
            <option value="auto">auto</option>
            <option value="light">light</option>
            <option value="dark">dark</option>
          </select>
        </label>
      </div>
      <button class="btn" @click="props.saveConfig">{{ props.t('apply') }}</button>
    </section>
  </main>
</template>
