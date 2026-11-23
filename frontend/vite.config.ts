import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

// The Go API only accepts writes whose Origin header exactly matches
// PUBLIC_ORIGIN. In development the browser talks only to Vite
// (http://localhost:5173) and Vite forwards /api to the Go API, so set
// PUBLIC_ORIGIN=http://localhost:5173 in the backend's .env.
export default defineConfig({
  plugins: [react()],
  server: {
    host: 'localhost',
    port: 5173,
    strictPort: true,
    proxy: {
      // changeOrigin stays false so the browser's Origin header reaches the API unchanged.
      '/api': { target: 'http://127.0.0.1:8080', changeOrigin: false },
    },
  },
  test: {
    // Only unit/component tests. Browser journeys in tests/e2e run with Playwright (npm run test:e2e).
    include: ['src/**/*.test.{ts,tsx}'],
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    css: false,
  },
});
