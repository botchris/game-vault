import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// In development the UI runs on :5173 and Connect calls (paths like /gamevault.v1.GameService/ListGames)
// are proxied to the Go backend. Set VITE_API_URL to talk to a backend on another origin instead.
const backend = 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/gamevault.v1.': backend,
      '/media/': backend,
    },
  },
  build: { outDir: 'dist', emptyOutDir: true },
});
