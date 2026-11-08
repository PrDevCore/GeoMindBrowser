import type { IncomingMessage } from "http";
import type { Duplex } from "stream";
import dns from "node:dns/promises";
import net from "node:net";
import { WebSocketServer, type WebSocket as WS } from "ws";
import {
  chromium,
  type Browser,
  type BrowserContext,
  type CDPSession,
  type Page,
} from "playwright";
import { authenticateUpgrade, rejectUpgrade } from "./wsAuth";

/* ---------------------------- Config ---------------------------- */

const MAX_SESSIONS = Number(process.env.BROWSER_MAX_SESSIONS ?? 5);
const IDLE_MS = 10 * 60_000; // no client activity -> close
const GRACE_MS = 60_000; // socket dropped -> keep session this long for reconnect
const JPEG_QUALITY = 60;
const DEFAULT_VIEWPORT = { width: 1280, height: 720 };

/* -------------------------- SSRF guard -------------------------- */

function isPrivateIPv4(ip: string): boolean {
  const [a, b] = ip.split(".").map(Number);
  return (
    a === 0 ||
    a === 10 ||
    a === 127 ||
    (a === 169 && b === 254) || // link-local + cloud metadata
    (a === 172 && b >= 16 && b <= 31) ||
    (a === 192 && b === 168) ||
    (a === 100 && b >= 64 && b <= 127) || // CGNAT
    a >= 224
  );
}

function isPrivateIp(ip: string): boolean {
  if (net.isIPv4(ip)) return isPrivateIPv4(ip);
  if (net.isIPv6(ip)) {
    const l = ip.toLowerCase();
    if (l === "::" || l === "::1") return true;
    const mapped = l.match(/^::ffff:(\d+\.\d+\.\d+\.\d+)$/);
    if (mapped) return isPrivateIPv4(mapped[1]);
    if (l.startsWith("::ffff:")) return true;
    return /^f[cd]/.test(l) || /^fe[89ab]/.test(l) || l.startsWith("ff");
  }
  return true;
}

const hostCache = new Map<string, { ok: boolean; at: number }>();

async function isHostPublic(host: string): Promise<boolean> {
  const h = host.replace(/^\[|\]$/g, "");
  if (net.isIP(h)) return !isPrivateIp(h);
  const cached = hostCache.get(h);
  if (cached && Date.now() - cached.at < 30_000) return cached.ok;
  let ok = false;
  try {
    const addrs = await dns.lookup(h, { all: true });
    ok = addrs.length > 0 && addrs.every((a) => !isPrivateIp(a.address));
  } catch {
    ok = false;
  }
  hostCache.set(h, { ok, at: Date.now() });
  return ok;
}

async function isUrlAllowed(raw: string): Promise<boolean> {
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    return false;
  }
  if (u.protocol === "about:" || u.protocol === "data:" || u.protocol === "blob:") return true;
  if (u.protocol !== "http:" && u.protocol !== "https:") return false;
  return isHostPublic(u.hostname);
}
// NOTE: DNS is resolved separately from Chromium (rebinding is possible) and WebSocket
// traffic is not covered. This is defence in depth; the real boundary must be a
// container/network policy that blocks egress to private ranges and metadata IPs.

function normalizeUrl(input: string): string {
  const s = input.trim().slice(0, 2048);
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(s)) return s;
  if (!/\s/.test(s) && (s.includes(".") || /^localhost(:|$)/i.test(s))) return `https://${s}`;
  return `https://duckduckgo.com/?q=${encodeURIComponent(s)}`;
}

/* ---------------------------- Protocol ---------------------------- */

type ClientMsg =
  | { type: "nav"; url: string }
  | { type: "back" }
  | { type: "forward" }
  | { type: "reload" }
  | { type: "resize"; width: number; height: number }
  | {
      type: "mouse";
      kind: "move" | "down" | "up";
      x: number;
      y: number;
      button?: "left" | "middle" | "right";
      clicks?: number;
    }
  | { type: "wheel"; x: number; y: number; dx: number; dy: number }
  | { type: "key"; kind: "down" | "up"; key: string }
  | { type: "text"; text: string };

type ServerMsg =
  | { type: "ready" }
  | { type: "viewport"; width: number; height: number }
  | { type: "url"; url: string }
  | { type: "title"; title: string }
  | { type: "error"; message: string };

const num = (v: unknown, min: number, max: number): number | null =>
  typeof v === "number" && Number.isFinite(v) ? Math.min(max, Math.max(min, v)) : null;

/* ---------------------------- Session ---------------------------- */

class Session {
  page!: Page;
  private cdp!: CDPSession;
  private ws: WS | null = null;
  private viewport = { ...DEFAULT_VIEWPORT };
  private inputQueue: Promise<void> = Promise.resolve();
  private graceTimer?: NodeJS.Timeout;
  private idleTimer?: NodeJS.Timeout;
  private destroyed = false;

  constructor(readonly userId: string, private context: BrowserContext) {}

  async init() {
    this.page = await this.context.newPage();
    this.cdp = await this.context.newCDPSession(this.page);
    this.cdp.on("Page.screencastFrame", ({ data, sessionId }) => this.onFrame(data, sessionId));

    this.page.on("framenavigated", (frame) => {
      if (frame === this.page.mainFrame()) this.emit({ type: "url", url: frame.url() });
    });
    this.page.on("load", () => {
      this.page
        .title()
        .then((title) => this.emit({ type: "title", title }))
        .catch(() => {});
    });
    this.page.on("dialog", (d) => void d.dismiss().catch(() => {}));
    this.page.on("popup", (p) => void this.followPopup(p));
    this.page.on("crash", () =>
      this.emit({ type: "error", message: "The page crashed. Reload to continue." }),
    );
    await this.startScreencast();
  }

  private async startScreencast() {
    await this.cdp.send("Page.startScreencast", {
      format: "jpeg",
      quality: JPEG_QUALITY,
      maxWidth: this.viewport.width,
      maxHeight: this.viewport.height,
      everyNthFrame: 1,
    });
  }

  private async restartScreencast() {
    await this.cdp.send("Page.stopScreencast").catch(() => {});
    await this.startScreencast().catch(() => {});
  }

  private onFrame(data: string, sessionId: number) {
    // Chromium sends the next frame only after the previous one is acked, so acking
    // when the socket write completes gives us backpressure for free.
    const ack = () => void this.cdp.send("Page.screencastFrameAck", { sessionId }).catch(() => {});
    const ws = this.ws;
    if (!ws || ws.readyState !== 1) {
      ack();
      return;
    }
    ws.send(Buffer.from(data, "base64"), { binary: true }, ack);
  }

  attach(ws: WS) {
    if (this.ws && this.ws !== ws) this.ws.close(4000, "Opened in another tab");
    this.ws = ws;
    clearTimeout(this.graceTimer);
    this.touch();

    ws.on("message", (data, isBinary) => {
      if (isBinary) return;
      this.touch();
      let msg: ClientMsg;
      try {
        msg = JSON.parse(data.toString());
      } catch {
        return;
      }
      this.handle(msg);
    });

    ws.on("close", () => {
      if (this.ws !== ws) return;
      this.ws = null;
      clearTimeout(this.idleTimer);
      this.graceTimer = setTimeout(() => void this.destroy(), GRACE_MS);
    });

    this.emit({ type: "ready" });
    this.emit({ type: "viewport", ...this.viewport });
    this.emit({ type: "url", url: this.page.url() });
    void this.restartScreencast(); // push a fresh frame to the new client
  }

  private touch() {
    clearTimeout(this.idleTimer);
    this.idleTimer = setTimeout(
      () => void this.destroy("Session closed after 10 minutes of inactivity."),
      IDLE_MS,
    );
  }

  private emit(msg: ServerMsg) {
    if (this.ws && this.ws.readyState === 1) this.ws.send(JSON.stringify(msg));
  }

  async destroy(reason?: string) {
    if (this.destroyed) return;
    this.destroyed = true;
    clearTimeout(this.graceTimer);
    clearTimeout(this.idleTimer);
    if (sessions.get(this.userId) === this) sessions.delete(this.userId);
    if (reason) this.emit({ type: "error", message: reason });
    this.ws?.close(1000);
    await this.context.close().catch(() => {});
  }

  private handle(msg: ClientMsg) {
    if (!msg || typeof msg.type !== "string") return;
    switch (msg.type) {
      // Navigation is not queued: a slow page load must not block input.
      case "nav":
        if (typeof msg.url === "string") void this.navigate(normalizeUrl(msg.url));
        return;
      case "back":
        void this.page.goBack({ waitUntil: "domcontentloaded" }).catch(() => {});
        return;
      case "forward":
        void this.page.goForward({ waitUntil: "domcontentloaded" }).catch(() => {});
        return;
      case "reload":
        void this.page.reload({ waitUntil: "domcontentloaded" }).catch(() => {});
        return;
      case "resize":
        void this.resize(msg.width, msg.height);
        return;
    }
    // Input events run strictly in order.
    this.inputQueue = this.inputQueue.then(() => this.handleInput(msg)).catch(() => {});
  }

  private async navigate(url: string) {
    if (!(await isUrlAllowed(url))) {
      this.emit({ type: "error", message: "That address is not allowed." });
      return;
    }
    try {
      await this.page.goto(url, { waitUntil: "domcontentloaded", timeout: 30_000 });
    } catch (e) {
      const m = e instanceof Error ? e.message : "";
      if (!/ERR_ABORTED|interrupted by another navigation/i.test(m)) {
        this.emit({ type: "error", message: "Could not load that page." });
      }
    }
  }

  private async resize(w: unknown, h: unknown) {
    const width = num(w, 320, 1920);
    const height = num(h, 240, 1080);
    if (width === null || height === null) return;
    this.viewport = { width: Math.round(width), height: Math.round(height) };
    await this.page.setViewportSize(this.viewport).catch(() => {});
    await this.restartScreencast();
    this.emit({ type: "viewport", ...this.viewport });
  }

  private async handleInput(msg: ClientMsg) {
    const { width, height } = this.viewport;
    switch (msg.type) {
      case "mouse": {
        const x = num(msg.x, 0, width);
        const y = num(msg.y, 0, height);
        if (x === null || y === null) return;
        const button = msg.button === "middle" || msg.button === "right" ? msg.button : "left";
        const clickCount = num(msg.clicks, 1, 3) ?? 1;
        if (msg.kind === "move") await this.page.mouse.move(x, y);
        else if (msg.kind === "down") {
          await this.page.mouse.move(x, y);
          await this.page.mouse.down({ button, clickCount });
        } else if (msg.kind === "up") {
          await this.page.mouse.up({ button, clickCount });
        }
        return;
      }
      case "wheel": {
        const x = num(msg.x, 0, width);
        const y = num(msg.y, 0, height);
        const dx = num(msg.dx, -5000, 5000);
        const dy = num(msg.dy, -5000, 5000);
        if (x === null || y === null || dx === null || dy === null) return;
        await this.page.mouse.move(x, y);
        await this.page.mouse.wheel(dx, dy);
        return;
      }
      case "key": {
        if (typeof msg.key !== "string" || msg.key.length === 0 || msg.key.length > 20) return;
        if (msg.kind === "down") await this.page.keyboard.down(msg.key);
        else if (msg.kind === "up") await this.page.keyboard.up(msg.key);
        return;
      }
      case "text": {
        if (typeof msg.text === "string")
          await this.page.keyboard.insertText(msg.text.slice(0, 10_000));
        return;
      }
    }
  }

  // target=_blank / window.open: we render one page, so load the popup's URL here.
  private async followPopup(p: Page) {
    try {
      await p.waitForLoadState("domcontentloaded", { timeout: 10_000 });
    } catch {
      /* use whatever URL it has */
    }
    const url = p.url();
    await p.close().catch(() => {});
    if (url && url !== "about:blank") await this.navigate(url);
  }
}

/* ------------------- Browser + session manager ------------------- */

const sessions = new Map<string, Session>();
const creating = new Map<string, Promise<Session>>();
let browserPromise: Promise<Browser> | null = null;

function getBrowser(): Promise<Browser> {
  if (!browserPromise) {
    browserPromise = chromium
      .launch({
        headless: true,
        // Chromium's sandbox needs user namespaces. In Docker you may need
        // BROWSER_NO_SANDBOX=1; if so, isolate the container tightly.
        args: [
          "--disable-dev-shm-usage",
          ...(process.env.BROWSER_NO_SANDBOX === "1" ? ["--no-sandbox"] : []),
        ],
      })
      .then((b) => {
        b.on("disconnected", () => {
          browserPromise = null;
          for (const s of [...sessions.values()])
            void s.destroy("The browser process stopped. Reconnect to start a new one.");
          sessions.clear();
        });
        return b;
      })
      .catch((e) => {
        browserPromise = null;
        throw e;
      });
  }
  return browserPromise;
}

async function createSession(userId: string): Promise<Session> {
  const browser = await getBrowser();
  const context = await browser.newContext({
    viewport: DEFAULT_VIEWPORT,
    acceptDownloads: false,
    serviceWorkers: "block",
    permissions: [],
  });
  await context.route("**/*", async (route) => {
    if (await isUrlAllowed(route.request().url())) await route.continue().catch(() => {});
    else await route.abort("blockedbyclient").catch(() => {});
  });
  const session = new Session(userId, context);
  try {
    await session.init();
  } catch (e) {
    await context.close().catch(() => {});
    throw e;
  }
  sessions.set(userId, session);
  return session;
}

async function getOrCreateSession(userId: string): Promise<Session> {
  const existing = sessions.get(userId);
  if (existing) return existing;
  const pending = creating.get(userId);
  if (pending) return pending;
  if (sessions.size + creating.size >= MAX_SESSIONS) throw new Error("busy");
  const p = createSession(userId).finally(() => creating.delete(userId));
  creating.set(userId, p);
  return p;
}

/* ---------------------------- Public API ---------------------------- */

const wss = new WebSocketServer({ noServer: true, maxPayload: 64 * 1024 });

/** Called from the central `upgrade` router in server.ts for path /ws/browser. */
export function handleBrowserUpgrade(req: IncomingMessage, socket: Duplex, head: Buffer) {
  const user = authenticateUpgrade(req);
  if (!user) {
    rejectUpgrade(socket);
    return;
  }
  wss.handleUpgrade(req, socket, head, async (ws) => {
    try {
      const session = await getOrCreateSession(user.userId);
      session.attach(ws);
    } catch (e) {
      const busy = e instanceof Error && e.message === "busy";
      const msg: ServerMsg = {
        type: "error",
        message: busy
          ? "All browser slots are in use. Try again in a few minutes."
          : "Could not start the browser.",
      };
      ws.send(JSON.stringify(msg));
      ws.close(busy ? 1013 : 1011);
      if (!busy) console.error("[browser] session start failed:", e);
    }
  });
}

export async function shutdownBrowser() {
  await Promise.allSettled([...sessions.values()].map((s) => s.destroy()));
  if (browserPromise) await (await browserPromise).close().catch(() => {});
}
