import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { resolve } from 'node:path'

// Library build for the Claude Design sync: not part of the app's own build.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: resolve(__dirname, 'dist'),
    emptyOutDir: true,
    assetsInlineLimit: 1_000_000,
    cssCodeSplit: false,
    lib: { entry: resolve(__dirname, 'src/index.ts'), formats: ['es'], fileName: () => 'index.js', cssFileName: 'style' },
    rollupOptions: { external: ['react', 'react-dom', 'react/jsx-runtime'] },
  },
})
