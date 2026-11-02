import type { SessionView, State } from "../src/session.js";

export type FetchCall = { path: string; init: RequestInit };

type ViewOverrides = {
  id?: string;
  state?: State;
  region?: string;
  expiresAt?: string;
  streamUrl?: string;
};

export function view(overrides: ViewOverrides = {}): SessionView {
  const state = overrides.state ?? "ready";
  const streamUrl =
    overrides.streamUrl ?? (state === "ready" || state === "active" ? "https://stream.example/session" : undefined);
  return {
    id: overrides.id ?? "sess-1",
    state,
    region: overrides.region ?? "de-test",
    expiresAt: overrides.expiresAt ?? new Date(Date.now() + 60_000).toISOString(),
    ...(streamUrl === undefined ? {} : { streamUrl }),
  };
}

export function jsonResponse(status: number, body: unknown, headers?: Record<string, string>): Response {
  return new Response(typeof body === "string" ? body : JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json", ...headers },
  });
}

export function emptyResponse(status = 204): Response {
  return new Response(null, { status });
}

// queue answers calls in order and records every request the client makes.
export function fakeFetch(queue: Response[]): { fetchImpl: typeof fetch; calls: FetchCall[] } {
  const calls: FetchCall[] = [];
  const fetchImpl = (async (input: string | URL | Request, init?: RequestInit) => {
    calls.push({ path: String(input), init: init ?? {} });
    const next = queue.shift();
    if (next === undefined) throw new Error("fakeFetch: no queued response");
    return next;
  }) as unknown as typeof fetch;
  return { fetchImpl, calls };
}
