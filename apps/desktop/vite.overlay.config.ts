// The overlay window's renderer: a tiny Vue app loaded from disk (never remote content).
import { resolve } from 'node:path'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  root: resolve(import.meta.dirname, 'src/overlay'),
  base: './',
  plugins: [vue()],
  build: {
    outDir: resolve(import.meta.dirname, 'dist/overlay'),
    emptyOutDir: true,
    target: 'chrome140',
    sourcemap: true,
  },
})
