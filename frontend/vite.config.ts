import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The dev server proxies /api (including the console WebSocket) to the API: a local one by default, or
// another with KLOUDIA_DEV_API=http://192.168.160.64:30080 npm run dev
const devAPI = process.env.KLOUDIA_DEV_API ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: { proxy: { "/api": { target: devAPI, ws: true, changeOrigin: true } } },
});
