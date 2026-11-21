import { defineConfig, devices } from "@playwright/test";

// E2E runs against a running server (the compose stack or `locarr serve`).
export default defineConfig({
  testDir: "./e2e",
  timeout: 60_000,
  // The tests share one server and its first-run setup: run them one after another.
  workers: 1,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:8484",
    trace: "retain-on-failure",
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        launchOptions: process.env.PW_CHROMIUM_PATH ? { executablePath: process.env.PW_CHROMIUM_PATH } : {},
      },
    },
  ],
});
