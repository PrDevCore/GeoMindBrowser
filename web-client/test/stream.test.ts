import { describe, expect, it } from "vitest";
import { leaksToken, streamLink } from "../src/stream.js";
import { view } from "./helpers.js";

describe("stream link", () => {
  it("offers a link only while the session is streaming", () => {
    expect(streamLink(null)).toBeNull();
    expect(streamLink(view({ state: "provisioning" }))).toBeNull();
    expect(streamLink(view({ state: "terminating" }))).toBeNull();
    expect(streamLink(view({ state: "ready" }))?.href).toBe("https://stream.example/session");
  });

  it("refuses a plain HTTP or absent stream URL", () => {
    expect(streamLink(view({ state: "ready", streamUrl: "http://insecure.example/" }))).toBeNull();
    expect(streamLink({ id: "a", state: "active", region: "de-test", expiresAt: new Date().toISOString() })).toBeNull();
  });

  it("carries the attributes that keep the URL off the referrer and history", () => {
    const link = streamLink(view({ state: "active" }));
    expect(link).toEqual({
      href: "https://stream.example/session",
      target: "_blank",
      rel: "noreferrer noopener",
      referrerPolicy: "no-referrer",
    });
  });

  it("detects a token embedded in the stream URL", () => {
    const link = streamLink(view({ state: "ready", streamUrl: "https://s.example/?t=abc123" }));
    expect(link && leaksToken(link, "abc123")).toBe(true);
    expect(link && leaksToken(link, "")).toBe(false);
    const clean = streamLink(view({ state: "ready" }));
    expect(clean && leaksToken(clean, "abc123")).toBe(false);
  });
});
