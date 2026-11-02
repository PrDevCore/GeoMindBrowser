import { describe, expect, it } from "vitest";
import {
  canTransition,
  isStreaming,
  isTerminal,
  isState,
  parseSessionView,
  shouldPoll,
} from "../src/session.js";

const base = {
  id: "sess-1",
  region: "de-test",
  expiresAt: "2026-11-02T12:30:00Z",
};

describe("session state machine", () => {
  it("accepts only the transitions the control plane defines", () => {
    expect(canTransition("requested", "provisioning")).toBe(true);
    expect(canTransition("provisioning", "ready")).toBe(true);
    expect(canTransition("ready", "active")).toBe(true);
    expect(canTransition("active", "terminating")).toBe(true);
    expect(canTransition("terminating", "gone")).toBe(true);
    expect(canTransition("requested", "ready")).toBe(false);
    expect(canTransition("gone", "active")).toBe(false);
    expect(canTransition("terminating", "failed")).toBe(false);
  });

  it("treats only gone and failed as terminal", () => {
    expect(isTerminal("gone")).toBe(true);
    expect(isTerminal("failed")).toBe(true);
    expect(isTerminal("terminating")).toBe(false);
  });

  it("exposes a stream URL only in ready and active", () => {
    expect(isStreaming("ready")).toBe(true);
    expect(isStreaming("active")).toBe(true);
    expect(isStreaming("provisioning")).toBe(false);
    expect(isStreaming("terminating")).toBe(false);
  });

  it("polls only while work is pending", () => {
    expect(shouldPoll("requested")).toBe(true);
    expect(shouldPoll("provisioning")).toBe(true);
    expect(shouldPoll("ready")).toBe(false);
    expect(shouldPoll("terminating")).toBe(false);
  });

  it("recognises known states", () => {
    expect(isState("ready")).toBe(true);
    expect(isState("unknown")).toBe(false);
    expect(isState(7)).toBe(false);
  });
});

describe("parseSessionView", () => {
  it("parses a valid view", () => {
    const view = parseSessionView({ ...base, state: "ready", streamUrl: "https://stream.example/" });
    expect(view.state).toBe("ready");
    expect(view.streamUrl).toBe("https://stream.example/");
  });

  it("rejects an unknown state", () => {
    expect(() => parseSessionView({ ...base, state: "weird" })).toThrow(/unknown state/);
  });

  it("rejects a missing or malformed id", () => {
    expect(() => parseSessionView({ ...base, id: "", state: "ready" })).toThrow(/missing an id/);
  });

  it("rejects an unusable expiresAt", () => {
    expect(() => parseSessionView({ ...base, state: "ready", expiresAt: "not-a-date" })).toThrow(
      /unusable expiresAt/
    );
  });

  it("refuses a non-HTTPS stream origin", () => {
    expect(() =>
      parseSessionView({ ...base, state: "ready", streamUrl: "http://stream.example/" })
    ).toThrow(/HTTPS/);
  });

  it("omits streamUrl when the server does not send one", () => {
    const view = parseSessionView({ ...base, state: "requested" });
    expect("streamUrl" in view).toBe(false);
  });
});
