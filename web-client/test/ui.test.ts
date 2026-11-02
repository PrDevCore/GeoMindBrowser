import { describe, expect, it } from "vitest";
import { createLifecycle } from "../src/lifecycle.js";
import { render, type Targets } from "../src/ui.js";
import { view } from "./helpers.js";
import type { Snapshot } from "../src/lifecycle.js";

function recorder(): { targets: Targets; text: Record<string, string>; flags: Record<string, boolean> } {
  const text: Record<string, string> = { status: "", session: "", remaining: "" };
  const flags: Record<string, boolean> = { start: true, stop: true, openVisible: false };
  const href: { value: string | null } = { value: "stale" };
  return {
    targets: {
      status: { setText: (v) => (text.status = v) },
      session: { setText: (v) => (text.session = v) },
      remaining: { setText: (v) => (text.remaining = v) },
      start: { setEnabled: (v) => (flags.start = v) },
      stop: { setEnabled: (v) => (flags.stop = v) },
      open: {
        setHref: (v) => (href.value = v),
        setVisible: (v) => (flags.openVisible = v),
      },
    },
    text,
    flags,
  };
}

const idle: Snapshot = { phase: "idle", view: null, message: "No session." };

describe("render", () => {
  it("keeps Start available and Stop unavailable with no session", () => {
    const { targets, text, flags } = recorder();
    render(idle, targets, 0);
    expect(flags.start).toBe(true);
    expect(flags.stop).toBe(false);
    expect(flags.openVisible).toBe(false);
    expect(text.status).toBe("Ready: No session.");
  });

  it("disables both controls while a session is being created", async () => {
    const plane = {
      health: async () => true,
      create: async () => view({ state: "provisioning" }),
      get: async () => view({ state: "provisioning" }),
      remove: async () => undefined,
    };
    let nowMs = 1_000;
    const lc = createLifecycle({
      plane,
      now: () => nowMs,
      wait: async (ms) => {
        nowMs += ms;
      },
      pollIntervalMs: 5,
      provisioningBudgetMs: 10,
    });

    const { targets, flags } = recorder();
    const pending = lc.start("de-test");
    render(lc.snapshot(), targets, nowMs);
    expect(flags.start).toBe(false);

    // The loop only exits once the provisioning budget runs out.
    await pending;
    expect(lc.snapshot().phase).toBe("failed");
  });

  it("shows the countdown and the stream link only while streaming", () => {
    const { targets, text, flags } = recorder();
    const now = Date.parse("2026-11-02T12:00:00Z");
    render(
      {
        phase: "ready",
        view: view({ state: "ready", expiresAt: new Date(now + 90_000).toISOString() }),
        message: "streaming",
      },
      targets,
      now
    );
    expect(flags.openVisible).toBe(true);
    expect(flags.stop).toBe(true);
    expect(flags.start).toBe(false);
    expect(text.remaining).toBe("Time remaining: 1:30");
    expect(text.session).toBe("Session: de-test");
  });

  it("hides the link and clears the countdown once ended", () => {
    const { targets, text, flags } = recorder();
    render({ phase: "ended", view: null, message: "time limit" }, targets, 0);
    expect(flags.openVisible).toBe(false);
    expect(text.remaining).toBe("");
    expect(text.status).toBe("Ended: time limit");
  });

  it("never re-enables Start while a session object is still held", () => {
    const { targets, flags } = recorder();
    render({ phase: "ready", view: view({ state: "ready" }), message: "streaming" }, targets, 0);
    expect(flags.start).toBe(false);
  });
});
