import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Standalone playground for the shared @bbux/* packages. Run: pnpm install &&
// pnpm --filter @bbux/examples dev  (or `pnpm dev` from this dir). Opens on 5199.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { port: 5199, host: true },
});
