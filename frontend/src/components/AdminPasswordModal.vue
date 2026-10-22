<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { LockKeyhole } from 'lucide-vue-next'
import { useApp } from '../composables/appContext'

const { t, state, adminBusy, adminFormError, submitAdminPassword, cancelAdminPrompt, localizeError } = useApp()

const password = ref('')
const inputRef = ref<HTMLInputElement | null>(null)

const errorText = computed(() => {
  if (adminFormError.value) return adminFormError.value
  return state.adminError ? localizeError(state.adminError, 'adminBadPassword') : ''
})

watch(
  () => state.needsAdmin,
  async (open) => {
    password.value = ''
    if (!open) adminFormError.value = ''
    if (open) {
      await nextTick()
      inputRef.value?.focus()
    }
  }
)

const submit = async () => {
  if (!password.value || adminBusy.value) return
  const value = password.value
  password.value = ''
  await submitAdminPassword(value)
  if (state.needsAdmin) {
    await nextTick()
    inputRef.value?.focus()
  }
}
</script>

<template>
  <Transition name="modal">
    <div v-if="state.needsAdmin" class="modal-overlay">
      <form class="modal narrow" @submit.prevent="submit" @keydown.esc="cancelAdminPrompt">
        <div class="modal-body admin-body">
          <span class="admin-icon"><LockKeyhole :size="22" aria-hidden="true" /></span>
          <h2>{{ t('adminTitle') }}</h2>
          <p class="hint">{{ t('adminHint') }}</p>
          <input
            ref="inputRef"
            v-model="password"
            class="input"
            type="password"
            autocomplete="current-password"
            :placeholder="t('adminPasswordPlaceholder')"
            :disabled="adminBusy"
          />
          <p v-if="errorText" class="admin-error" role="alert">{{ errorText }}</p>
        </div>
        <div class="modal-foot">
          <button class="btn" type="button" :disabled="adminBusy" @click="cancelAdminPrompt">{{ t('cancel') }}</button>
          <button class="btn primary" type="submit" :disabled="adminBusy || !password">
            {{ adminBusy ? t('adminChecking') : t('adminAuthorize') }}
          </button>
        </div>
      </form>
    </div>
  </Transition>
</template>
