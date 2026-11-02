import { defineConfig } from "@playwright/test";

// End-to-end run of the Mode B launcher against a real sessiond in fake-runtime
// mode. It needs Go, a display, and free loopback ports 8080 and 4173, so it is
// not part of CI; run it locally with `npm run test:web`.
export default defineConfig({
  testDir: "./tests/browser",
  timeout: 180_000,
  expect: { timeout: 30_000 },
  workers: 1,
  fullyParallel: false,
  reporter: [["list"]],
  use: {
    headless: false,
  },
});
