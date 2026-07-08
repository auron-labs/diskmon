import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: '../web/dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    passWithNoTests: true,
  }
})
