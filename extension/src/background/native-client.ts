/**
 * Typed client for the native messaging protocol. Pure logic; the caller owns
 * the actual chrome.runtime.Port so this module is testable without a browser.
 */

export interface PortLike {
  postMessage(message: unknown): void;
  disconnect(): void;
  onMessage: { addListener(listener: (message: unknown) => void): void };
  onDisconnect: { addListener(listener: () => void): void };
}

export interface Envelope {
  readonly id: string;
  readonly type: string;
  readonly payload?: Record<string, unknown>;
}

export class ProtocolError extends Error {
  readonly code: string;

  constructor(code: string, message: string) {
    super(message);
    this.name = "ProtocolError";
    this.code = code;
  }
}

const ID_PATTERN = /^[A-Za-z0-9_-]{1,128}$/;
const TYPE_PATTERN = /^[a-z][a-z0-9]*(\.[a-z0-9]+)*$/;
const MAX_TYPE_LENGTH = 64;
const DEFAULT_TIMEOUT_MS = 10_000;

/** Envelope id reserved for unsolicited companion events. */
export const EVENT_ID = "event";

interface Pending {
  readonly expected: string;
  readonly resolve: (envelope: Envelope) => void;
  readonly reject: (error: Error) => void;
  readonly timer: ReturnType<typeof setTimeout>;
}

interface ClientOptions {
  readonly newId?: () => string;
  readonly requestTimeoutMs?: number;
}

function asRecord(value: unknown, where: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new ProtocolError("malformed", `${where} must be an object`);
  }
  return value as Record<string, unknown>;
}

/** Validates a message received from the companion. */
export function parseEnvelope(value: unknown): Envelope {
  const obj = asRecord(value, "message");
  const id = obj.id;
  if (typeof id !== "string" || !ID_PATTERN.test(id)) {
    throw new ProtocolError("malformed", "message id is invalid");
  }
  const type = obj.type;
  if (typeof type !== "string" || type.length > MAX_TYPE_LENGTH || !TYPE_PATTERN.test(type)) {
    throw new ProtocolError("malformed", "message type is invalid");
  }
  const rawPayload = obj.payload;
  if (rawPayload === undefined) {
    return { id, type };
  }
  return { id, type, payload: asRecord(rawPayload, "payload") };
}

/** Sends typed requests and matches responses by request id. */
export class NativeClient {
  readonly #port: PortLike;
  readonly #newId: () => string;
  readonly #timeoutMs: number;
  readonly #pending = new Map<string, Pending>();
  #disconnected = false;

  onDisconnect: (() => void) | undefined;
  onProtocolError: ((error: Error) => void) | undefined;
  onEvent: ((envelope: Envelope) => void) | undefined;

  constructor(port: PortLike, options: ClientOptions = {}) {
    this.#port = port;
    this.#newId = options.newId ?? (() => crypto.randomUUID());
    this.#timeoutMs = options.requestTimeoutMs ?? DEFAULT_TIMEOUT_MS;
    port.onMessage.addListener((message) => this.#handleMessage(message));
    port.onDisconnect.addListener(() => this.#handleDisconnect());
  }

  request(
    requestType: string,
    expectedType: string,
    payload: Record<string, unknown> = {},
  ): Promise<Envelope> {
    if (this.#disconnected) {
      return Promise.reject(new ProtocolError("disconnected", "native host is disconnected"));
    }
    if (!TYPE_PATTERN.test(requestType) || !TYPE_PATTERN.test(expectedType)) {
      return Promise.reject(new ProtocolError("malformed", "request type is invalid"));
    }
    const id = this.#newId();
    if (!ID_PATTERN.test(id)) {
      return Promise.reject(new ProtocolError("malformed", "generated request id is invalid"));
    }

    return new Promise<Envelope>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.#pending.delete(id);
        reject(new ProtocolError("timeout", "native host did not respond in time"));
      }, this.#timeoutMs);
      this.#pending.set(id, { expected: expectedType, resolve, reject, timer });
      try {
        this.#port.postMessage({ id, type: requestType, payload });
      } catch {
        clearTimeout(timer);
        this.#pending.delete(id);
        reject(new ProtocolError("disconnected", "native host is unavailable"));
      }
    });
  }

  disconnect(): void {
    this.#port.disconnect();
    this.#failAll(new ProtocolError("disconnected", "native host connection closed"));
  }

  #handleMessage(raw: unknown): void {
    let envelope: Envelope;
    try {
      envelope = parseEnvelope(raw);
    } catch (error) {
      this.onProtocolError?.(error instanceof Error ? error : new Error("malformed message"));
      return;
    }
    const pending = this.#pending.get(envelope.id);
    if (pending === undefined) {
      // Only the reserved id is an event; anything else is a late response.
      if (envelope.id === EVENT_ID) {
        this.onEvent?.(envelope);
      }
      return;
    }
    clearTimeout(pending.timer);
    this.#pending.delete(envelope.id);

    if (envelope.type === "error") {
      const code = envelope.payload?.code;
      const message = envelope.payload?.message;
      pending.reject(
        new ProtocolError(
          typeof code === "string" ? code : "internal",
          typeof message === "string" ? message : "request failed",
        ),
      );
      return;
    }
    if (envelope.type !== pending.expected) {
      pending.reject(new ProtocolError("unexpected_type", `expected ${pending.expected}`));
      return;
    }
    pending.resolve(envelope);
  }

  #handleDisconnect(): void {
    if (this.#disconnected) {
      return;
    }
    this.#disconnected = true;
    this.#failAll(new ProtocolError("disconnected", "native host disconnected"));
    this.onDisconnect?.();
  }

  #failAll(error: Error): void {
    for (const pending of this.#pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.#pending.clear();
  }
}
