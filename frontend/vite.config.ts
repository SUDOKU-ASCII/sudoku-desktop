import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
export default defineConfig({
  // Wails production loads assets from an embedded filesystem/custom scheme.
  // Using a relative base avoids absolute `/assets/...` URLs that can break in release builds.
  base: './',
  plugins: [vue()]
})
