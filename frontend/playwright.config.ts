import { defineConfig } from "@playwright/test";

// UI end-to-end tests against a deployed console, e.g.
// KLOUDIA_UI_URL=http://192.168.160.64:30080 npx playwright test
export default defineConfig({
  testDir: "./e2e",
  timeout: 15 * 60 * 1000,
  expect: { timeout: 30 * 1000 },
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL: process.env.KLOUDIA_UI_URL ?? "http://localhost:5173",
    locale: "ko-KR",
    viewport: { width: 1440, height: 1000 },
    trace: "retain-on-failure",
  },
});
