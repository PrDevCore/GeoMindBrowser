import { chromium, expect, test, type BrowserContext, type Worker } from "@playwright/test";
import { execFileSync, execSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../..", import.meta.url));
const extensionDir = path.join(root, "extension");
const companionDir = path.join(root, "companion");
const HOST_NAME = "com.geomind.companion";
const ROUTED_HOST = "routed.test";
const DIRECT_HOST = "direct.test";

const manifest = JSON.parse(readFileSync(path.join(extensionDir, "manifest.json"), "utf8")) as {
  key: string;
};
const extensionId = deriveExtensionId(manifest.key);

let tempDir: string;
let context: BrowserContext;
let serviceWorker: Worker;
let server: Server;
let policyPath: string;
let hits = 0;
let port = 0;

test.describe.configure({ mode: "serial" });

test.beforeAll(async () => {
  tempDir = mkdtempSync(path.join(tmpdir(), "geomind-e2e-"));
  const exe = process.platform === "win32" ? "geomind-companion.exe" : "geomind-companion";
  const companionExe = path.join(tempDir, exe);
  policyPath = path.join(tempDir, "policy.json");
  writeFileSync(
    policyPath,
    JSON.stringify({
      version: 1,
      defaultAction: "direct",
      rules: [{ id: "routed", host: ROUTED_HOST, action: "route", exit: "de-test" }],
    }),
  );

  execSync("npm run build", { cwd: extensionDir, stdio: "pipe" });
  execFileSync("go", ["build", "-o", companionExe, "./cmd/geomind-companion"], {
    cwd: companionDir,
    stdio: "pipe",
  });

  const hostManifest = path.join(tempDir, `${HOST_NAME}.json`);
  writeFileSync(
    hostManifest,
    JSON.stringify({
      name: HOST_NAME,
      description: "GeoMind companion test host",
      path: companionExe,
      type: "stdio",
      allowed_origins: [`chrome-extension://${extensionId}/`],
    }),
  );
  installNativeHost(hostManifest);

  server = createServer((req, res) => {
    if (req.url === "/") hits += 1;
    res.writeHead(200, { "content-type": "text/plain" });
    res.end("ok");
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  port = (server.address() as AddressInfo).port;

  context = await chromium.launchPersistentContext(path.join(tempDir, "profile"), {
    headless: false,
    args: [
      `--disable-extensions-except=${extensionDir}`,
      `--load-extension=${extensionDir}`,
      `--host-resolver-rules=MAP ${ROUTED_HOST} 127.0.0.1, MAP ${DIRECT_HOST} 127.0.0.1`,
      "--no-first-run",
      "--no-default-browser-check",
    ],
    env: browserEnv({
      GEOMIND_POLICY: policyPath,
      GEOMIND_KNOWN_EXITS: "de-test",
      GEOMIND_EXIT_ID: "test-exit",
      GEOMIND_EXIT_LOCALE: "de-DE",
    }),
  });

  serviceWorker = await waitForWorker(context);
});

test.afterAll(async () => {
  await context?.close().catch(() => undefined);
  await new Promise<void>((resolve) => (server === undefined ? resolve() : server.close(() => resolve())));
  removeNativeHost();
  if (tempDir !== undefined) {
    rmSync(tempDir, { recursive: true, force: true });
  }
});

test("disables non-proxied WebRTC UDP", async () => {
  const privacy = await waitForPrivacy();
  expect(privacy.state).toBe("active");

  const setting = await serviceWorker.evaluate(
    () =>
      new Promise<{ levelOfControl: string; value: unknown }>((resolve) => {
        const c = (globalThis as { chrome: any }).chrome;
        c.privacy.network.webRTCIPHandlingPolicy.get({}, (details: any) => resolve(details));
      }),
  );
  expect(setting.levelOfControl).toBe("controlled_by_this_extension");
  expect(setting.value).toBe("disable_non_proxied_udp");
});

test("installs the companion PAC and reports routing active", async () => {
  const status = await waitForRouting("active");
  expect(status.policyLoaded).toBe(true);
  expect(status.ruleCount).toBe(1);
  expect(status.tunnel?.state).toBe("down");

  // The companion runs without WireGuard here, so routing is installed but the
  // tunnel is down. The badge must say so instead of implying working geo-routing.
  await expect
    .poll(() => serviceWorker.evaluate(() => (globalThis as { chrome: any }).chrome.action.getBadgeText({})), {
      timeout: 10_000,
    })
    .toBe("!");
  await expect
    .poll(() => serviceWorker.evaluate(() => (globalThis as { chrome: any }).chrome.action.getTitle({})), {
      timeout: 10_000,
    })
    .toBe("GeoMind: tunnel down");

  const proxy = await serviceWorker.evaluate(
    () =>
      new Promise<{ levelOfControl: string; value: any }>((resolve) => {
        const c = (globalThis as { chrome: any }).chrome;
        c.proxy.settings.get({}, (details: any) => resolve(details));
      }),
  );
  expect(proxy.levelOfControl).toBe("controlled_by_this_extension");
  expect(proxy.value.mode).toBe("pac_script");
  const pac = String(proxy.value.pacScript?.data ?? "");
  expect(pac).toContain(`"host":"${ROUTED_HOST}"`);
  expect(pac).toContain("SOCKS5 127.0.0.1:");
  expect(pac).not.toContain('"proxy":"DIRECT"');
});

test("restores the PAC when another controller changes the proxy", async () => {
  const before = await readProxy();
  expect(before.levelOfControl).toBe("controlled_by_this_extension");
  const pac = String(before.value.pacScript?.data ?? "");
  expect(pac).toContain(`"host":"${ROUTED_HOST}"`);

  await serviceWorker.evaluate(async () => {
    const c = (globalThis as { chrome: any }).chrome;
    await new Promise<void>((resolve, reject) => {
      c.proxy.settings.set({ value: { mode: "direct" }, scope: "regular" }, () => {
        const err = c.runtime.lastError;
        if (err) reject(new Error(err.message));
        else resolve();
      });
    });
  });

  await expect
    .poll(async () => {
      const setting = await readProxy();
      return setting.value.mode === "pac_script" && setting.value.pacScript?.data === pac;
    }, { timeout: 10_000, intervals: [250] })
    .toBe(true);

  const status = await readStatus();
  expect(status?.routing).toBe("active");
  expect(status?.routingDetail).toBeUndefined();
});

test("polls tunnel status while connected", async () => {
  const first = await readStatus();
  expect(first?.state).toBe("connected");
  expect(first?.tunnel?.state).toBe("down");
  const firstAt = Date.parse(first?.at ?? "");
  expect(Number.isNaN(firstAt)).toBe(false);

  await expect
    .poll(
      async () => {
        const current = await readStatus();
        return Date.parse(current?.at ?? "");
      },
      { timeout: 15_000, intervals: [500] },
    )
    .toBeGreaterThan(firstAt);

  const latest = await readStatus();
  expect(latest?.state).toBe("connected");
  expect(latest?.tunnel?.state).toBe("down");
});

test("reloads an edited policy without restarting the companion", async () => {
  const before = await readStatus();
  expect(before?.ruleCount).toBe(1);

  writeFileSync(
    policyPath,
    JSON.stringify({
      version: 1,
      defaultAction: "direct",
      rules: [
        { id: "routed", host: ROUTED_HOST, action: "route", exit: "de-test" },
        { id: "added", host: "added.test", action: "route", exit: "de-test" },
      ],
    }),
  );

  await expect
    .poll(async () => (await readStatus())?.ruleCount, { timeout: 15_000, intervals: [250] })
    .toBe(2);
  expect(await readPac()).toContain('"host":"added.test"');

  // A broken policy must leave the previous PAC and routing state untouched.
  writeFileSync(policyPath, "{not json");
  await new Promise((resolve) => setTimeout(resolve, 2_500));
  const after = await readStatus();
  expect(after?.routing).toBe("active");
  expect(after?.ruleCount).toBe(2);
  expect(await readPac()).toContain('"host":"added.test"');
});

test("offers per-site locale headers and keeps them off until granted", async () => {
  const status = await readStatus();
  expect(status?.tunnel?.locale).toBe("de-DE");

  // Without an explicit grant, the extension must not install header rules.
  const rules = await serviceWorker.evaluate(async () => {
    const c = (globalThis as { chrome: any }).chrome;
    return c.declarativeNetRequest.getDynamicRules();
  });
  expect(rules).toEqual([]);

  const page = await context.newPage();
  await page.goto(`chrome-extension://${extensionId}/options.html`);
  await expect(page.locator("#locale")).toContainText("de-DE");
  await expect(page.locator("#privacy")).toContainText("active");
  await expect(page.locator("#hosts")).toContainText(ROUTED_HOST);
  await expect(page.locator("#headers-state")).toContainText("off");
  await expect(page.locator("#enable")).toBeEnabled();
  await page.close();
});

test("fails closed for a routed host and still loads a direct host", async () => {
  const page = await context.newPage();
  const failure = await page
    .goto(`http://${ROUTED_HOST}:${port}/`)
    .then(() => null)
    .catch((error: Error) => error);
  expect(failure).not.toBeNull();
  expect(hits).toBe(0);

  const directPage = await context.newPage();
  const response = await directPage.goto(`http://${DIRECT_HOST}:${port}/`);
  expect(response?.status()).toBe(200);
  expect(hits).toBe(1);
});

function deriveExtensionId(key: string): string {
  const hash = createHash("sha256").update(Buffer.from(key, "base64")).digest().subarray(0, 16);
  let id = "";
  for (const byte of hash) {
    id += "abcdefghijklmnop"[byte >> 4] ?? "";
    id += "abcdefghijklmnop"[byte & 15] ?? "";
  }
  return id;
}

function browserEnv(extra: Record<string, string>): Record<string, string> {
  const env: Record<string, string> = {};
  for (const [key, value] of Object.entries(process.env)) {
    if (value !== undefined) env[key] = value;
  }
  for (const key of Object.keys(env)) {
    if (key.startsWith("GEOMIND_WG_")) delete env[key];
  }
  return { ...env, ...extra };
}

function installNativeHost(hostManifest: string): void {
  if (process.platform === "win32") {
    for (const vendor of ["Google\\Chrome", "Chromium"]) {
      execFileSync(
        "reg",
        [
          "add",
          `HKCU\\Software\\${vendor}\\NativeMessagingHosts\\${HOST_NAME}`,
          "/ve",
          "/t",
          "REG_SZ",
          "/d",
          hostManifest,
          "/f",
        ],
        { stdio: "pipe" },
      );
    }
    return;
  }
  const home = path.join(tempDir, "home");
  for (const vendor of ["google-chrome", "chromium"]) {
    const dir = path.join(home, ".config", vendor, "NativeMessagingHosts");
    mkdirSync(dir, { recursive: true });
    writeFileSync(path.join(dir, `${HOST_NAME}.json`), readFileSync(hostManifest));
  }
  process.env.HOME = home;
  delete process.env.XDG_CONFIG_HOME;
}

function removeNativeHost(): void {
  if (process.platform !== "win32") return;
  for (const vendor of ["Google\\Chrome", "Chromium"]) {
    try {
      execFileSync(
        "reg",
        ["delete", `HKCU\\Software\\${vendor}\\NativeMessagingHosts\\${HOST_NAME}`, "/f"],
        { stdio: "pipe" },
      );
    } catch {
      // The key may not exist if setup failed early.
    }
  }
}

async function waitForWorker(ctx: BrowserContext): Promise<Worker> {
  const existing = ctx.serviceWorkers().find((worker) => worker.url().includes(extensionId));
  if (existing !== undefined) return existing;
  for (;;) {
    const worker = await ctx.waitForEvent("serviceworker", { timeout: 30_000 });
    if (worker.url().includes(extensionId)) return worker;
  }
}

interface CompanionStatus {
  routing?: string;
  routingDetail?: string;
  policyLoaded?: boolean;
  ruleCount?: number;
  state?: string;
  at?: string;
  tunnel?: { state?: string; locale?: string | null };
}

async function readProxy(): Promise<{ levelOfControl: string; value: any }> {
  return serviceWorker.evaluate(
    () =>
      new Promise<{ levelOfControl: string; value: any }>((resolve) => {
        const c = (globalThis as { chrome: any }).chrome;
        c.proxy.settings.get({}, (details: any) => resolve(details));
      }),
  );
}

async function readPac(): Promise<string> {
  const proxy = await serviceWorker.evaluate(
    () =>
      new Promise<{ value: any }>((resolve) => {
        const c = (globalThis as { chrome: any }).chrome;
        c.proxy.settings.get({}, (details: any) => resolve(details));
      }),
  );
  return String(proxy.value.pacScript?.data ?? "");
}

async function readStatus(): Promise<CompanionStatus | null> {  return serviceWorker.evaluate(
    () =>
      new Promise<CompanionStatus | null>((resolve) => {
        const c = (globalThis as { chrome: any }).chrome;
        c.storage.session.get("companionStatus", (all: Record<string, CompanionStatus>) =>
          resolve(all.companionStatus ?? null),
        );
      }),
  );
}

async function waitForRouting(state: string): Promise<CompanionStatus> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const status = await readStatus();
    if (status?.routing === state) return status;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`routing did not reach ${state}`);
}

async function waitForPrivacy(): Promise<{ state: string }> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const status = await serviceWorker.evaluate(
      () =>
        new Promise<{ state: string } | null>((resolve) => {
          const c = (globalThis as { chrome: any }).chrome;
          c.storage.session.get("privacyStatus", (all: Record<string, { state: string }>) =>
            resolve(all.privacyStatus ?? null),
          );
        }),
    );
    if (status !== null) return status;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error("privacy status was never written");
}
