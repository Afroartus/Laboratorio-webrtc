import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'

// Solo variables VITE_* llegan al navegador: nunca definir secretos con ese prefijo.
export default defineConfig({
  plugins: [svelte()],
  build: {
    sourcemap: false,
  },
})
