import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/leak/browser",
  timeout: 90_000,
  expect: { timeout: 15_000 },
  workers: 1,
  fullyParallel: false,
  reporter: [["list"]],
  use: {
    headless: false,
  },
});
