import { inject, provide, type InjectionKey } from 'vue'
import { useAppController } from './useAppController'

export type AppContext = ReturnType<typeof useAppController>

const key: InjectionKey<AppContext> = Symbol('app')

// provideApp creates the controller once (in App.vue); panels read it with useApp().
export const provideApp = (): AppContext => {
  const ctx = useAppController()
  provide(key, ctx)
  return ctx
}

export const useApp = (): AppContext => {
  const ctx = inject(key)
  if (!ctx) throw new Error('useApp() called outside App.vue')
  return ctx
}
