import { chromium, expect, test, type Browser, type Page } from "@playwright/test";
import { execFileSync, execSync, spawn, type ChildProcess } from "node:child_process";
import { appendFileSync, mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../..", import.meta.url));
const sessionDir = path.join(root, "session");
const webClientDir = path.join(root, "web-client");
const viteBin = path.join(webClientDir, "node_modules", "vite", "bin", "vite.js");

const CONTROL = "http://127.0.0.1:8080";
const APP = "http://127.0.0.1:4173";
const DEV_TOKEN = "e2e-dev-token-0123456789abcdef";

let sessiond: ChildProcess;
let preview: ChildProcess;
let browser: Browser;
let page: Page;
const logDir = mkdtempSync(path.join(tmpdir(), "geomind-web-e2e-"));

async function waitReady(url: string, label: string, timeoutMs = 60_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastError = "not attempted";
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
      lastError = `status ${response.status}`;
    } catch (error) {
      lastError = String(error);
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`${label} never became ready at ${url}: ${lastError}`);
}

// A busy port produces a server that answers on the wrong process, so fail here
// with the actual cause instead of timing out later.
async function assertPortFree(port: number): Promise<void> {
  const inUse = await new Promise<boolean>((resolve) => {
    const probe = createServer();
    probe.once("error", () => resolve(true));
    probe.listen(port, "127.0.0.1", () => {
      probe.close(() => resolve(false));
    });
  });
  if (inUse) throw new Error(`port ${port} is already in use; stop the other process first`);
}

function spawnChild(
  command: string,
  args: string[],
  cwd: string,
  env: NodeJS.ProcessEnv,
  label: string,
): ChildProcess {
  const child = spawn(command, args, { cwd, env, stdio: ["ignore", "pipe", "pipe"] });
  const log = path.join(logDir, `${label}.log`);
  const append = (chunk: Buffer): void => {
    appendFileSync(log, chunk);
    process.stdout.write(`${label}: ${chunk}`);
  };
  child.stdout?.on("data", append);
  child.stderr?.on("data", append);
  return child;
}

test.describe.configure({ mode: "serial" });

test.beforeAll(async () => {
  await assertPortFree(8080);
  await assertPortFree(4173);

  const exe = process.platform === "win32" ? "sessiond.exe" : "sessiond";
  const sessiondPath = path.join(logDir, exe);
  execFileSync("go", ["build", "-o", sessiondPath, "./cmd/sessiond"], {
    cwd: sessionDir,
    stdio: "pipe",
  });
  execSync("npm run build", { cwd: webClientDir, stdio: "pipe" });

  // An explicit env: the control plane must not inherit anything from the shell
  // that happens to widen its listening address or change its regions.
  sessiond = spawnChild(
    sessiondPath,
    [],
    sessionDir,
    {
      PATH: process.env["PATH"] ?? "",
      SYSTEMROOT: process.env["SYSTEMROOT"] ?? "",
      GEOMIND_SESSION_ADDR: "127.0.0.1:8080",
      GEOMIND_SESSION_RUNTIME: "fake",
      GEOMIND_SESSION_DEV_TOKEN: DEV_TOKEN,
      GEOMIND_SESSION_REGIONS: "de-test",
    },
    "sessiond",
  );
  await waitReady(`${CONTROL}/healthz`, "sessiond");

  preview = spawnChild(process.execPath, [viteBin, "preview"], webClientDir, process.env, "preview");
  await waitReady(APP, "vite preview");

  browser = await chromium.launch({ headless: false });
  page = await browser.newPage();
});

test.afterAll(async () => {
  await browser?.close();
  for (const child of [preview, sessiond]) {
    if (child !== undefined && !child.killed) child.kill();
  }
  rmSync(logDir, { recursive: true, force: true });
});

async function newSession(token: string, region: string): Promise<void> {
  await page.goto(APP);
  await page.fill("#token", token);
  await page.fill("#region", region);
  await page.click("#start");
}

test("the launcher starts a fake session, streams, and closes it for real", async () => {
  await newSession(DEV_TOKEN, "de-test");
  await expect(page.locator("#status")).toContainText("Streaming:", { timeout: 30_000 });
  await expect(page.locator("#session")).toHaveText("Session: de-test");
  await expect(page.locator("#remaining")).toContainText("Time remaining:");

  const href = await page.locator("#open").getAttribute("href");
  expect(href).toMatch(/^https:\/\/stream\.invalid\/s\/[A-Za-z0-9_-]+$/);
  expect(await page.locator("#open").getAttribute("rel")).toBe("noreferrer noopener");
  const id = href?.split("/").pop() ?? "";
  expect(id.length).toBeGreaterThan(0);

  // The token is read once and dropped from the field.
  await expect(page.locator("#token")).toHaveValue("");

  await page.click("#stop");
  await expect(page.locator("#status")).toContainText("Stopped: Session closed.");
  await expect(page.locator("#open")).toBeHidden();

  const closed = await fetch(`${CONTROL}/v1/sessions/${id}`, {
    headers: { Authorization: `Bearer ${DEV_TOKEN}` },
  });
  expect(closed.status).toBe(404);
});

test("a rejected token stops the launcher without starting anything", async () => {
  await newSession("wrong-token-0000000000000000", "de-test");
  await expect(page.locator("#status")).toContainText("Stopped:", { timeout: 30_000 });
  await expect(page.locator("#status")).toContainText("token was rejected");
  await expect(page.locator("#open")).toBeHidden();
  await expect(page.locator("#session")).toHaveText("Session: none");
});

test("the launcher page keeps no browsing state behind", async () => {
  await page.goto(APP);
  const stored = await page.evaluate(() => {
    const tokens: string[] = [];
    for (const jar of [localStorage, sessionStorage]) {
      for (let i = 0; i < jar.length; i++) tokens.push(jar.key(i) ?? "");
    }
    return {
      keys: tokens,
      cookie: document.cookie,
      tokenInDom: document.body.innerHTML.includes("e2e-dev-token"),
      indexedDB: Object.keys(window).filter((name) => name.toLowerCase().includes("indexeddb")),
    };
  });
  expect(stored.keys).toEqual([]);
  expect(stored.cookie).toBe("");
  expect(stored.tokenInDom).toBe(false);
});
