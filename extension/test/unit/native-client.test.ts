import { describe, expect, it, vi } from "vitest";
import {
  NativeClient,
  ProtocolError,
  parseEnvelope,
  type PortLike,
} from "../../src/background/native-client";

class FakePort implements PortLike {
  readonly sent: unknown[] = [];
  readonly messageListeners: ((message: unknown) => void)[] = [];
  readonly disconnectListeners: (() => void)[] = [];

  postMessage(message: unknown): void {
    this.sent.push(message);
  }

  disconnect(): void {
    for (const listener of this.disconnectListeners) {
      listener();
    }
  }

  onMessage = {
    addListener: (listener: (message: unknown) => void): void => {
      this.messageListeners.push(listener);
    },
  };

  onDisconnect = {
    addListener: (listener: () => void): void => {
      this.disconnectListeners.push(listener);
    },
  };

  receive(message: unknown): void {
    for (const listener of this.messageListeners) {
      listener(message);
    }
  }
}

function sequenceIds(): () => string {
  let next = 0;
  return () => {
    next += 1;
    return `req-${next}`;
  };
}

describe("parseEnvelope", () => {
  it("accepts a minimal and a payload envelope", () => {
    expect(parseEnvelope({ id: "a-1", type: "hello" })).toEqual({ id: "a-1", type: "hello" });
    expect(parseEnvelope({ id: "a-1", type: "hello.result", payload: { x: 1 } })).toEqual({
      id: "a-1",
      type: "hello.result",
      payload: { x: 1 },
    });
  });

  const bad: unknown[] = [
    null,
    [],
    "text",
    { type: "hello" },
    { id: "bad id", type: "hello" },
    { id: "a", type: "Hello" },
    { id: "a", type: "hello", payload: [] },
    { id: "a", type: "hello", payload: null },
    { id: "a", type: "x".repeat(65) },
  ];

  for (const value of bad) {
    it(`rejects ${JSON.stringify(value)}`, () => {
      expect(() => parseEnvelope(value)).toThrow(ProtocolError);
    });
  }
});

describe("NativeClient", () => {
  it("sends a typed request and resolves the matching response", async () => {
    const port = new FakePort();
    const client = new NativeClient(port, { newId: sequenceIds() });
    const promise = client.request("hello", "hello.result", { extensionVersion: "0.1.0" });

    expect(port.sent).toEqual([
      { id: "req-1", type: "hello", payload: { extensionVersion: "0.1.0" } },
    ]);

    port.receive({ id: "req-1", type: "hello.result", payload: { protocolVersion: 1 } });
    await expect(promise).resolves.toMatchObject({
      id: "req-1",
      type: "hello.result",
      payload: { protocolVersion: 1 },
    });
  });

  it("rejects a typed error response with its code", async () => {
    const port = new FakePort();
    const client = new NativeClient(port, { newId: sequenceIds() });
    const promise = client.request("policy.get", "policy.result", {});
    port.receive({
      id: "req-1",
      type: "error",
      payload: { code: "policy_unavailable", message: "no policy" },
    });
    await expect(promise).rejects.toMatchObject({ code: "policy_unavailable" });
  });

  it("rejects an unexpected response type", async () => {
    const port = new FakePort();
    const client = new NativeClient(port, { newId: sequenceIds() });
    const promise = client.request("hello", "hello.result", {});
    port.receive({ id: "req-1", type: "tunnel.status.result", payload: {} });
    await expect(promise).rejects.toMatchObject({ code: "unexpected_type" });
  });

  it("ignores stale ids and malformed messages without rejecting", async () => {
    const port = new FakePort();
    const errors: Error[] = [];
    const client = new NativeClient(port, { newId: sequenceIds() });
    client.onProtocolError = (error) => errors.push(error);

    const promise = client.request("hello", "hello.result", {});
    port.receive({ id: "other", type: "hello.result", payload: {} });
    port.receive({ id: "req-1", type: "hello.result", payload: "not-an-object" });
    expect(errors).toHaveLength(1);

    port.receive({ id: "req-1", type: "hello.result", payload: {} });
    await expect(promise).resolves.toMatchObject({ type: "hello.result" });
  });

  it("dispatches reserved-id events and ignores other unknown ids", async () => {
    const port = new FakePort();
    const client = new NativeClient(port, { newId: sequenceIds() });
    const events: unknown[] = [];
    client.onEvent = (envelope) => events.push(envelope);

    const promise = client.request("hello", "hello.result", {});
    port.receive({ id: "event", type: "policy.changed", payload: {} });
    port.receive({ id: "late", type: "policy.changed", payload: {} });
    port.receive({ id: "event", type: "policy.changed", payload: "not-an-object" });
    expect(events).toEqual([{ id: "event", type: "policy.changed", payload: {} }]);

    port.receive({ id: "req-1", type: "hello.result", payload: {} });
    await expect(promise).resolves.toMatchObject({ type: "hello.result" });
  });

  it("rejects pending requests on disconnect and calls the callback once", async () => {
    const port = new FakePort();
    const client = new NativeClient(port, { newId: sequenceIds() });
    let disconnected = 0;
    client.onDisconnect = () => {
      disconnected += 1;
    };

    const promise = client.request("hello", "hello.result", {});
    port.disconnect();

    await expect(promise).rejects.toMatchObject({ code: "disconnected" });
    expect(disconnected).toBe(1);
    await expect(client.request("hello", "hello.result", {})).rejects.toMatchObject({
      code: "disconnected",
    });
  });

  it("rejects immediately when the port cannot send", async () => {
    const port = new FakePort();
    port.postMessage = (): void => {
      throw new Error("gone");
    };
    const client = new NativeClient(port, { newId: sequenceIds() });
    await expect(client.request("hello", "hello.result", {})).rejects.toMatchObject({
      code: "disconnected",
    });
  });

  it("times out with a stable code", async () => {
    vi.useFakeTimers();
    try {
      const port = new FakePort();
      const client = new NativeClient(port, { newId: sequenceIds(), requestTimeoutMs: 100 });
      const promise = client.request("hello", "hello.result", {});
      const rejection = expect(promise).rejects.toMatchObject({ code: "timeout" });
      await vi.advanceTimersByTimeAsync(101);
      await rejection;
    } finally {
      vi.useRealTimers();
    }
  });
});
