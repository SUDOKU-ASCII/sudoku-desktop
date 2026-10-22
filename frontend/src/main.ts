import { createApp } from 'vue'
import './style.css'

type AnyRecord = Record<string, any>
const wailsRuntime = (window as unknown as AnyRecord).runtime as AnyRecord | undefined
const wailsLog = (level: 'LogInfo' | 'LogError', message: string) => {
  try {
    wailsRuntime?.[level]?.(message)
  } catch {
    // ignore
  }
}

const describe = (err: unknown) => (err instanceof Error ? `${err.name}: ${err.message}\n${err.stack || ''}` : String(err))

// Before mount a thrown error would leave a blank window, so render it. After mount the
// UI is already usable: a stray rejection (a failed probe, a closed dialog) must not wipe it.
let mounted = false
const onError = (err: unknown) => {
  // eslint-disable-next-line no-console
  console.error(err)
  const message = describe(err)
  wailsLog('LogError', `frontend: ${mounted ? 'error' : 'fatal'}\n${message}`)
  if (mounted) return
  const container = document.createElement('div')
  container.style.cssText = 'padding:16px;font-family:ui-monospace,Menlo,monospace;white-space:pre-wrap'
  container.textContent = message
  document.body.replaceChildren(container)
}

window.addEventListener('error', (e) => onError(e.error || e.message))
window.addEventListener('unhandledrejection', (e) => onError(e.reason))

import('./App.vue')
  .then(({ default: App }) => {
    const app = createApp(App)
    app.config.errorHandler = (err) => onError(err)
    app.mount('#app')
    mounted = true
    wailsLog('LogInfo', 'frontend: mounted')
  })
  .catch(onError)
