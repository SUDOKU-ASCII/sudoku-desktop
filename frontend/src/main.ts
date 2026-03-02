import { createApp } from 'vue'
import './style.css'

type AnyRecord = Record<string, any>
const wailsRuntime = (window as unknown as AnyRecord).runtime as AnyRecord | undefined
const wailsLogInfo = (message: string) => {
  try {
    wailsRuntime?.LogInfo?.(message)
  } catch {
    // ignore
  }
}
const wailsLogError = (message: string) => {
  try {
    wailsRuntime?.LogError?.(message)
  } catch {
    // ignore
  }
}

const bootRoot = document.getElementById('app')
if (bootRoot) {
  bootRoot.innerHTML = `<div id="boot-marker" style="padding:16px;font-family:ui-monospace,Menlo,monospace;">Booting…</div>`
}
wailsLogInfo('frontend: boot')

const renderFatal = (err: unknown) => {
  // Ensure we never end up with a blank window in production builds.
  // eslint-disable-next-line no-console
  console.error(err)
  const message = err instanceof Error ? `${err.name}: ${err.message}\n${err.stack || ''}` : String(err)
  wailsLogError(`frontend: fatal\n${message}`)

  const container = document.createElement('div')
  container.style.padding = '16px'
  container.style.fontFamily = 'ui-monospace, Menlo, monospace'
  container.style.whiteSpace = 'pre-wrap'
  container.textContent = message
  document.body.innerHTML = ''
  document.body.appendChild(container)
}

window.addEventListener('error', (e) => renderFatal((e as ErrorEvent).error || (e as any).message))
window.addEventListener('unhandledrejection', (e) => renderFatal((e as PromiseRejectionEvent).reason))

import('./App.vue')
  .then(({ default: App }) => {
    wailsLogInfo('frontend: App.vue loaded')
    const app = createApp(App)
    app.config.errorHandler = (err) => renderFatal(err)
    app.mount('#app')
    wailsLogInfo('frontend: mounted')
  })
  .catch(renderFatal)
