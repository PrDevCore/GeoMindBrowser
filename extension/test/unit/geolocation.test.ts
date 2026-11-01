import { afterEach, describe, expect, it, vi } from "vitest";
import { installGeolocationBlocks, planGeolocationBlocks } from "../../src/background/geolocation";

interface Call {
  primaryPattern?: string;
  setting?: string;
  scope?: string;
}

const policy = {
  version: 1,
  defaultAction: "direct",
  rules: [
    { id: "shop", host: "shop.example.org", action: "route", exit: "de-test" },
    { id: "wild", host: "*.example.com", action: "route", exit: "de-test" },
    { id: "dup", host: "shop.example.org", action: "route", exit: "de-test" },
    { id: "blocked", host: "blocked.example.net", action: "block" },
  ],
};

function stubChrome(failing: readonly string[] = []): Call[] {
  const calls: Call[] = [];
  const runtime: { lastError?: { message: string } | undefined } = {};
  vi.stubGlobal("chrome", {
    runtime,
    contentSettings: {
      location: {
        set: (details: Call, callback?: () => void) => {
          calls.push(details);
          runtime.lastError =
            details.primaryPattern !== undefined && failing.includes(details.primaryPattern)
              ? { message: "denied" }
              : undefined;
          callback?.();
          runtime.lastError = undefined;
        },
      },
    },
  });
  return calls;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("planGeolocationBlocks", () => {
  it("blocks every routed pattern once and ignores blocked hosts", () => {
    expect(planGeolocationBlocks(policy)).toEqual(["*://shop.example.org/*", "*://*.example.com/*"]);
  });

  it("blocks nothing for an unreadable policy", () => {
    expect(planGeolocationBlocks({ version: 2 })).toEqual([]);
    expect(planGeolocationBlocks(null)).toEqual([]);
  });
});

describe("installGeolocationBlocks", () => {
  it("blocks routed origins for regular browsing", async () => {
    const calls = stubChrome();
    await expect(installGeolocationBlocks(policy)).resolves.toBe(2);

    expect(calls).toEqual([
      { primaryPattern: "*://shop.example.org/*", setting: "block", scope: "regular" },
      { primaryPattern: "*://*.example.com/*", setting: "block", scope: "regular" },
    ]);
  });

  it("still blocks the remaining origins before reporting a failure", async () => {
    const calls = stubChrome(["*://shop.example.org/*"]);
    await expect(installGeolocationBlocks(policy)).rejects.toThrow("denied");
    expect(calls.map((call) => call.primaryPattern)).toEqual([
      "*://shop.example.org/*",
      "*://*.example.com/*",
    ]);
  });

  it("makes no API call when nothing is routed", async () => {
    const calls = stubChrome();
    await expect(installGeolocationBlocks(null)).resolves.toBe(0);
    expect(calls).toHaveLength(0);
  });
});
