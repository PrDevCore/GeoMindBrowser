import { describe, expect, it } from "vitest";
import { ProtocolError } from "../../src/background/native-client";
import { badgeFor, parseTunnelStatus } from "../../src/background/status";

describe("parseTunnelStatus", () => {
  it("accepts every tunnel state and keeps the exit metadata", () => {
    for (const state of ["up", "down", "starting"] as const) {
      const status = parseTunnelStatus({
        state,
        exit: "de-test",
        region: "DE",
        locale: "de-DE",
        since: "2026-10-30T00:00:00Z",
      });
      expect(status).toEqual({
        state,
        exit: "de-test",
        region: "DE",
        locale: "de-DE",
        since: "2026-10-30T00:00:00Z",
      });
    }
  });

  it("accepts null exit, region, and locale", () => {
    const status = parseTunnelStatus({
      state: "down",
      exit: null,
      region: null,
      locale: null,
      since: "2026-10-30T00:00:00Z",
    });
    expect(status.exit).toBeNull();
    expect(status.region).toBeNull();
    expect(status.locale).toBeNull();
  });

  it("rejects malformed status payloads", () => {
    const base = {
      state: "down",
      exit: null,
      region: null,
      locale: null,
      since: "2026-10-30T00:00:00Z",
    };
    const bad: (Record<string, unknown> | undefined)[] = [
      undefined,
      { ...base, state: "connected" },
      { ...base, exit: 7 },
      { ...base, region: 7 },
      { ...base, locale: 7 },
      { ...base, locale: "not a locale!" },
      { ...base, since: "" },
      { ...base, since: undefined },
    ];
    for (const payload of bad) {
      expect(() => parseTunnelStatus(payload)).toThrow(ProtocolError);
    }
  });
});

describe("badgeFor", () => {
  it("clears the badge only when routing is active and the tunnel is up", () => {
    expect(badgeFor("active", undefined, "up")).toEqual({
      text: "",
      title: "GeoMind: routing active",
      color: "#b00020",
    });
    // Unknown state during bootstrap must not fake an error.
    expect(badgeFor("active", undefined, undefined).text).toBe("");
  });

  it("flags a down tunnel while routing stays active", () => {
    const badge = badgeFor("active", undefined, "down");
    expect(badge.text).toBe("!");
    expect(badge.title).toBe("GeoMind: tunnel down");
    expect(badge.color).toBe("#b00020");
  });

  it("distinguishes a starting tunnel from a failure", () => {
    const badge = badgeFor("active", undefined, "starting");
    expect(badge.text).toBe("…");
    expect(badge.title).toBe("GeoMind: tunnel starting");
    expect(badge.color).toBe("#b26a00");
  });

  it("reports missing policy and routing errors", () => {
    expect(badgeFor("unavailable", "policy unavailable", "down")).toEqual({
      text: "!",
      title: "GeoMind: no policy loaded",
      color: "#b00020",
    });
    expect(badgeFor("error", "invalid policy", "up").title).toBe(
      "GeoMind: routing error (invalid policy)",
    );
    expect(badgeFor("error", undefined, "up").title).toBe("GeoMind: routing error");
  });

  it("puts a failed WebRTC policy ahead of every routing state", () => {
    const failed = { text: "!", title: "GeoMind: WebRTC protection failed", color: "#b00020" };
    expect(badgeFor("active", undefined, "up", "error")).toEqual(failed);
    expect(badgeFor("unavailable", "policy unavailable", "down", "error")).toEqual(failed);
    expect(badgeFor("error", "invalid policy", undefined, "error")).toEqual(failed);
    expect(badgeFor("active", undefined, "up", "active").text).toBe("");
  });
});
