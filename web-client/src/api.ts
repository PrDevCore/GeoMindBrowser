import { parseSessionView, type SessionView } from "./session.js";

export type ApiError = Error & { status: number; code: string; retryAfterSeconds?: number };

export interface ControlPlane {
  health(): Promise<boolean>;
  create(region: string): Promise<SessionView>;
  get(id: string): Promise<SessionView>;
  remove(id: string, keepalive?: boolean): Promise<void>;
}

// The token lives only here, in a module-scoped closure. Nothing in this file
// may write it to storage, the URL, or console output.
export function createTokenHolder(): { set(token: string): void; clear(): void; value(): string } {
  let token = "";
  return {
    set(next: string): void {
      token = next.trim();
    },
    clear(): void {
      token = "";
    },
    value(): string {
      return token;
    },
  };
}

type ErrorBody = { error?: unknown; message?: unknown };

async function request(
  fetchImpl: typeof fetch,
  token: string,
  method: string,
  path: string,
  body?: unknown,
  keepalive?: boolean
): Promise<{ status: number; body: unknown; retryAfterSeconds?: number }> {
  const headers = new Headers({
    Accept: "application/json",
    Authorization: `Bearer ${token}`,
  });
  if (body !== undefined) headers.set("Content-Type", "application/json");
  const response = await fetchImpl(path, {
    method,
    headers,
    body: body === undefined ? null : JSON.stringify(body),
    credentials: "omit",
    cache: "no-store",
    referrerPolicy: "no-referrer",
    ...(keepalive === true ? { keepalive: true } : {}),
  });
  const retryAfter = Number(response.headers.get("Retry-After"));
  const text = await response.text();
  let parsed: unknown = undefined;
  if (text !== "") {
    try {
      parsed = JSON.parse(text) as ErrorBody;
    } catch {
      parsed = {};
    }
  }
  return {
    status: response.status,
    body: parsed,
    ...(Number.isFinite(retryAfter) && retryAfter > 0 ? { retryAfterSeconds: retryAfter } : {}),
  };
}

function apiError(status: number, body: unknown, retryAfterSeconds?: number): ApiError {
  const record = (typeof body === "object" && body !== null ? body : {}) as ErrorBody;
  const code = typeof record.error === "string" ? record.error : "unavailable";
  const message = typeof record.message === "string" ? record.message : "request failed";
  const error = new Error(`web-client: ${code}: ${message}`) as ApiError;
  error.status = status;
  error.code = code;
  if (retryAfterSeconds !== undefined) error.retryAfterSeconds = retryAfterSeconds;
  return error;
}

export function createControlPlane(fetchImpl: typeof fetch, token: () => string): ControlPlane {
  async function json(
    method: string,
    path: string,
    body?: unknown,
    keepalive?: boolean
  ): Promise<unknown> {
    const result = await request(fetchImpl, token(), method, path, body, keepalive);
    if (result.status < 200 || result.status >= 300) {
      throw apiError(result.status, result.body, result.retryAfterSeconds);
    }
    return result.body;
  }

  return {
    async health(): Promise<boolean> {
      const response = await fetchImpl("/healthz", {
        method: "GET",
        credentials: "omit",
        cache: "no-store",
        referrerPolicy: "no-referrer",
      });
      return response.ok;
    },
    async create(region: string): Promise<SessionView> {
      return parseSessionView(await json("POST", "/v1/sessions", { region }));
    },
    async get(id: string): Promise<SessionView> {
      return parseSessionView(await json("GET", `/v1/sessions/${encodeURIComponent(id)}`));
    },
    async remove(id: string, keepalive?: boolean): Promise<void> {
      await json("DELETE", `/v1/sessions/${encodeURIComponent(id)}`, undefined, keepalive);
    },
  };
}
