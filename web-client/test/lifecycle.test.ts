import { describe, expect, it } from "vitest";
import type { ControlPlane } from "../src/api.js";
import { createLifecycle } from "../src/lifecycle.js";
import type { SessionView } from "../src/session.js";
import { view } from "./helpers.js";

type Call = { id: string; keepalive?: boolean };

type Stub = ControlPlane & {
  creates: string[];
  removes: Call[];
  getCalls: number;
};

function stubPlane(behaviour: {
  create?: () => Promise<SessionView>;
  get?: (call: number) => Promise<SessionView>;
  remove?: () => Promise<void>;
}): Stub {
  const removes: Call[] = [];
  const creates: string[] = [];
  let getCalls = 0;
  return {
    creates,
    removes,
    get getCalls() {
      return getCalls;
    },
    async health() {
      return true;
    },
    async create(region: string) {
      creates.push(region);
      if (behaviour.create) return behaviour.create();
      return view({ state: "ready" });
    },
    async get() {
      getCalls += 1;
      if (behaviour.get) return behaviour.get(getCalls);
      return view({ state: "ready" });
    },
    async remove(id: string, keepalive?: boolean) {
      removes.push({ id, ...(keepalive === undefined ? {} : { keepalive }) });
      if (behaviour.remove) return behaviour.remove();
    },
  };
}

function clock(startMs = 1_000_000) {
  let nowMs = startMs;
  return {
    now: () => nowMs,
    wait: async (ms: number) => {
      nowMs += ms;
    },
    advance: (ms: number) => {
      nowMs += ms;
    },
  };
}

function error(code: string, status: number, retryAfterSeconds?: number): Promise<never> {
  const e = new Error(`stub: ${code}`) as Error & { status: number; code: string };
  e.status = status;
  e.code = code;
  if (retryAfterSeconds !== undefined) {
    Object.assign(e, { retryAfterSeconds });
  }
  return Promise.reject(e);
}

describe("lifecycle", () => {
  it("ignores a second click while a session is being requested", async () => {
    const plane = stubPlane({
      create: async () => view({ state: "provisioning" }),
      get: async () => view({ state: "provisioning" }),
    });
    const tick = clock();
    const lc = createLifecycle({
      plane,
      now: tick.now,
      wait: tick.wait,
      pollIntervalMs: 10,
      provisioningBudgetMs: 20,
    });

    const first = lc.start("de-test");
    const second = lc.start("de-test");
    await Promise.all([first, second]);

    expect(plane.creates).toEqual(["de-test"]);
    expect(plane.removes).toHaveLength(1);
    expect(lc.snapshot().phase).toBe("failed");
  });

  it("polls until the session is streaming", async () => {
    const plane = stubPlane({
      create: async () => view({ state: "provisioning" }),
      get: async (call) =>
        call === 1 ? view({ state: "provisioning" }) : view({ state: "ready", streamUrl: "https://s.example/x" }),
    });
    const tick = clock();
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait, pollIntervalMs: 10 });

    await lc.start("de-test");

    const snapshot = lc.snapshot();
    expect(snapshot.phase).toBe("ready");
    expect(snapshot.view?.streamUrl).toBe("https://s.example/x");
    expect(plane.getCalls).toBe(2);
  });

  it("closes the session once when polling fails", async () => {
    const plane = stubPlane({
      create: async () => view({ state: "provisioning" }),
      get: () => error("unavailable", 503),
    });
    const tick = clock();
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait, pollIntervalMs: 10 });

    await lc.start("de-test");

    expect(lc.snapshot().phase).toBe("failed");
    expect(plane.removes).toEqual([{ id: "sess-1" }]);
  });

  it("starts nothing and closes nothing when rate limited", async () => {
    const plane = stubPlane({ create: () => error("rate_limited", 429, 60) });
    const tick = clock();
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait });

    await lc.start("de-test");

    const snapshot = lc.snapshot();
    expect(snapshot.phase).toBe("failed");
    expect(snapshot.message).toContain("60s");
    expect(snapshot.view).toBeNull();
    expect(plane.removes).toEqual([]);
  });

  it("treats an impossible server transition as a leak and closes", async () => {
    const plane = stubPlane({
      create: async () => view({ state: "provisioning" }),
      get: async () => view({ state: "requested" }),
    });
    const tick = clock();
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait, pollIntervalMs: 10 });

    await lc.start("de-test");

    expect(lc.snapshot().phase).toBe("failed");
    expect(plane.removes).toHaveLength(1);
  });

  it("closes when the server reports a failed session", async () => {
    const plane = stubPlane({ create: async () => view({ state: "failed" }) });
    const tick = clock();
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait });

    await lc.start("de-test");

    expect(lc.snapshot().phase).toBe("failed");
    expect(plane.removes).toHaveLength(1);
  });

  it("gives up on provisioning that never becomes ready", async () => {
    const plane = stubPlane({
      create: async () => view({ state: "provisioning" }),
      get: async () => view({ state: "provisioning" }),
    });
    const tick = clock();
    const lc = createLifecycle({
      plane,
      now: tick.now,
      wait: tick.wait,
      pollIntervalMs: 10,
      provisioningBudgetMs: 25,
    });

    await lc.start("de-test");

    expect(lc.snapshot().phase).toBe("failed");
    expect(plane.getCalls).toBe(3);
    expect(plane.removes).toHaveLength(1);
  });

  it("stops exactly once", async () => {
    const plane = stubPlane({});
    const tick = clock();
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait });

    await lc.start("de-test");
    await lc.stop();
    await lc.stop();

    expect(plane.removes).toEqual([{ id: "sess-1" }]);
    expect(lc.snapshot().phase).toBe("stopped");
  });

  it("closes with keepalive on unload and does not double close", async () => {
    const plane = stubPlane({});
    const tick = clock();
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait });

    await lc.start("de-test");
    lc.unload();
    await lc.stop();
    lc.unload();

    expect(plane.removes).toEqual([{ id: "sess-1", keepalive: true }]);
  });

  it("ends the session when its expiry passes", async () => {
    const tick = clock();
    const plane = stubPlane({
      create: async () => view({ expiresAt: new Date(tick.now() + 5_000).toISOString() }),
    });
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait });

    await lc.start("de-test");
    expect(lc.snapshot().phase).toBe("ready");

    tick.advance(10_000);
    await lc.expireCheck();

    expect(lc.snapshot().phase).toBe("ended");
    expect(plane.removes).toHaveLength(1);
  });

  it("reports a failed close without retrying it", async () => {
    const plane = stubPlane({ remove: () => error("unavailable", 503) });
    const tick = clock();
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait });

    await lc.start("de-test");
    await lc.stop();

    expect(plane.removes).toHaveLength(1);
    expect(lc.snapshot().message).toContain("expires on its own");
  });

  it("allows a new session after the previous one ended", async () => {
    const plane = stubPlane({});
    const tick = clock();
    const lc = createLifecycle({ plane, now: tick.now, wait: tick.wait });

    await lc.start("de-test");
    await lc.stop();
    await lc.start("fr-test");

    expect(plane.creates).toEqual(["de-test", "fr-test"]);
    expect(lc.snapshot().phase).toBe("ready");
  });
});
