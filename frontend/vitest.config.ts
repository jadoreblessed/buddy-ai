import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: ['./test/setup.ts'],
    coverage: {
      include: ['src/App.tsx', 'src/main.tsx'],
      reporter: ['text', 'json-summary'],
      reportsDirectory: './coverage',
    },
  },
})
