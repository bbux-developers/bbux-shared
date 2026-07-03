import { defineConfig } from 'vitest/config';

export default defineConfig({
  esbuild: {
    // Use the automatic runtime so tests don't need an explicit `import React`.
    jsx: 'automatic',
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
  },
});
