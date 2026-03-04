<script setup lang="ts">
const props = defineProps<{
  open: boolean
  password: string
  busy: boolean
  error: string
  t: (key: any) => string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit'): void
  (e: 'update:password', value: string): void
}>()

const onSubmit = () => {
  if (props.busy) return
  emit('submit')
}
</script>

<template>
  <div v-if="props.open" class="modal-overlay" @click.self="emit('close')">
    <section class="modal-card tun-admin-modal">
      <header class="modal-head">
        <h3>{{ props.t('tunAdminTitle') }}</h3>
        <button class="iconbtn" @click="emit('close')" :title="props.t('close')">
          <svg viewBox="0 0 24 24">
            <path d="M6 6l12 12M18 6L6 18" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
          </svg>
        </button>
      </header>

      <div class="modal-content">
        <section class="group-card warn">
          <p class="hint">{{ props.t('tunAdminHint') }}</p>
          <p class="muted">{{ props.t('tunAdminHint2') }}</p>
        </section>

        <section class="group-card">
          <label class="field">
            <span>{{ props.t('password') }}</span>
            <input
              type="password"
              :value="props.password"
              autocomplete="current-password"
              :placeholder="props.t('tunAdminPlaceholder')"
              :disabled="props.busy"
              autofocus
              @input="emit('update:password', ($event.target as HTMLInputElement).value)"
              @keyup.enter="onSubmit"
            />
          </label>
          <p v-if="props.error" class="hint tun-admin-error">{{ props.error }}</p>
        </section>
      </div>

      <footer class="modal-foot">
        <button class="btn ghost" :disabled="props.busy" @click="emit('close')">{{ props.t('cancel') }}</button>
        <button class="btn" :disabled="props.busy || !props.password" @click="onSubmit">{{ props.t('confirm') }}</button>
      </footer>
    </section>
  </div>
</template>

