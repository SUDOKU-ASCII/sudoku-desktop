import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'
import wails from '@wailsio/runtime/plugins/vite'

// https://vitejs.dev/config/
export default defineConfig({
  // Wails production loads assets from an embedded filesystem/custom scheme.
  // Using a relative base avoids absolute `/assets/...` URLs that can break in release builds.
  base: './',
  plugins: [vue(), wails('./bindings')],
  define: {
    __VUE_OPTIONS_API__: true,
    __VUE_PROD_DEVTOOLS__: false,
    __VUE_PROD_HYDRATION_MISMATCH_DETAILS__: false,
  },
})
